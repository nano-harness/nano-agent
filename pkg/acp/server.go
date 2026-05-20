package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// Server implements the ACP protocol server
type Server struct {
	transport          *Transport
	registry           *SessionRegistry
	engine             *engine.Engine
	config             *config.Config
	fsMode             FSMode
	ctx                context.Context
	cancel             context.CancelFunc
	permissionBridge   *PermissionBridge  // Permission bridge for approval requests
	fsBridge           *FSBridge          // Filesystem bridge for fs/* operations
	terminalBridge     *TerminalBridge    // Terminal bridge for terminal/* operations
	clientCapabilities ClientCapabilities // Store client capabilities from initialize
}

// ServerOptions configures the ACP server
type ServerOptions struct {
	Config      *config.Config
	FSMode      FSMode
	EnableSwarm bool
	WorkDir     string
	Env         map[string]string
}

// NewServer creates a new ACP server
func NewServer(opts ServerOptions) (*Server, error) {
	// Default config if not provided
	if opts.Config == nil {
		cfg, err := config.LoadConfig("")
		if err != nil {
			logger.Warnf("Failed to load config, using defaults: %v", err)
			opts.Config = config.DefaultConfig()
		} else {
			opts.Config = cfg
		}
	}

	// Default working directory
	if opts.WorkDir != "" {
		if err := os.Chdir(opts.WorkDir); err != nil {
			return nil, fmt.Errorf("change working directory: %w", err)
		}
	}

	// Create engine with nil approval handler (will be set per-session if needed)
	eng, err := engine.New(opts.Config, nil)
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	srv := &Server{
		transport: NewTransport(os.Stdin, os.Stdout),
		registry:  NewSessionRegistry(),
		engine:    eng,
		config:    opts.Config,
		fsMode:    opts.FSMode,
		ctx:       ctx,
		cancel:    cancel,
	}

	return srv, nil
}

// Serve starts the ACP server main loop
func (s *Server) Serve() error {
	logger.Info("ACP: Server starting")
	defer logger.Info("ACP: Server stopped")

	for {
		req, err := s.transport.ReadRequest()
		if err != nil {
			if err == io.EOF {
				logger.Info("ACP: Client disconnected")
				return nil
			}
			if rpcErr, ok := err.(*RPCError); ok {
				// Send error response if we can determine the request ID
				_ = s.transport.SendErrorResponse(nil, rpcErr.Code, rpcErr.Message, rpcErr.Data)
				continue
			}
			logger.Errorf("ACP: Failed to read request: %v", err)
			continue
		}

		// If req is nil, it means we received a response (handled by transport)
		if req == nil {
			continue
		}

		// Handle request
		go s.handleRequest(req)
	}
}

// handleRequest routes and handles a single RPC request
func (s *Server) handleRequest(req *RPCRequest) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "session/new":
		s.handleSessionNew(req)
	case "session/prompt":
		s.handleSessionPrompt(req)
	case "session/cancel":
		s.handleSessionCancel(req)
	case "session/close":
		s.handleSessionClose(req)
	case "session/list":
		s.handleSessionList(req)
	case "session/load":
		s.handleSessionLoad(req)
	case "session/respond_permission":
		s.handlePermissionResponse(req)
	case "fs/read_text_file":
		s.handleFSRead(req)
	case "fs/write_text_file":
		s.handleFSWrite(req)
	case "terminal/create":
		s.handleTerminalCreate(req)
	default:
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeMethodNotFound, "Method not found", req.Method)
	}
}

// handleSessionNew handles session/new requests
func (s *Server) handleSessionNew(req *RPCRequest) {
	// Log raw params for debugging
	if rawJSON, err := json.Marshal(req.Params); err == nil {
		logger.Debugf("ACP: session/new raw params: %s", string(rawJSON))
	}

	var params SessionNewParams
	if err := unmarshalParams(req.Params, &params); err != nil {
		logger.Errorf("ACP: Failed to unmarshal session/new params: %v", err)
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", err.Error())
		return
	}

	// Determine working directory
	cwd := params.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// Create a new nano session
	nanoSessionID := s.engine.Agent.StartNewSession()

	// Create ACP session with client capabilities
	session, err := s.registry.Create(nanoSessionID, cwd, params.Env, params.Capabilities, s.clientCapabilities, s.fsMode)
	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to create session", err.Error())
		return
	}

	// Create permission bridge for this session
	s.permissionBridge = NewPermissionBridge(session.ACPSessionID, s.transport)

	// Check if client has FS capabilities
	clientHasFSCaps := s.clientCapabilities.FS != nil &&
		(s.clientCapabilities.FS.ReadTextFile || s.clientCapabilities.FS.WriteTextFile)

	// Check if client has Terminal capabilities
	clientHasTermCaps := s.clientCapabilities.Terminal

	// Create filesystem bridge for this session
	s.fsBridge = NewFSBridge(session.ACPSessionID, s.transport, cwd, s.fsMode, clientHasFSCaps)

	// Create terminal bridge for this session
	s.terminalBridge = NewTerminalBridge(session.ACPSessionID, s.transport, clientHasTermCaps)

	// Prepare response with server capabilities
	result := SessionNewResult{
		SessionID: session.ACPSessionID,
	}

	_ = s.transport.SendSuccessResponse(req.ID, result)
	logger.Infof("ACP: Session created: %s (nano: %s, cwd: %s, fs mode: %s, client fs caps: %v, client term caps: %v)",
		session.ACPSessionID, nanoSessionID, cwd, s.fsMode, clientHasFSCaps, clientHasTermCaps)

	// Advertise available slash commands to the client
	go s.advertiseSlashCommands(session.ACPSessionID, cwd)
}

// handleSessionClose handles session/close requests
func (s *Server) handleSessionClose(req *RPCRequest) {
	var params SessionCloseParams
	if err := unmarshalParams(req.Params, &params); err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", err.Error())
		return
	}

	// Get session
	session, ok := s.registry.Get(params.SessionID)
	if !ok {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeSessionNotFound, "Session not found", params.SessionID)
		return
	}

	// Get session stats if available
	stats := make(map[string]interface{})
	if nanoSession, ok := s.engine.Agent.GetSessionManager().GetSession(session.NanoSessionID); ok {
		stats["total_tokens"] = nanoSession.TotalTokens
		stats["total_time_seconds"] = nanoSession.Duration
	}

	// Delete the session
	if err := s.registry.Delete(params.SessionID); err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to close session", err.Error())
		return
	}

	// Optionally delete the nano session
	_, _ = s.engine.Agent.DeleteSession(session.NanoSessionID)

	result := SessionCloseResult{
		Success: true,
		Stats:   stats,
	}

	_ = s.transport.SendSuccessResponse(req.ID, result)
	logger.Infof("ACP: Session closed: %s", params.SessionID)
}

// handleSessionList handles session/list requests
func (s *Server) handleSessionList(req *RPCRequest) {
	sessions := s.registry.List()

	result := make([]map[string]interface{}, 0, len(sessions))
	for _, sess := range sessions {
		result = append(result, map[string]interface{}{
			"sessionId":  sess.ACPSessionID,
			"createdAt":  sess.CreatedAt,
			"lastActive": sess.LastActiveAt,
			"cwd":        sess.CWD,
		})
	}

	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"sessions": result,
	})
}

// getToolList returns the list of available tools
func (s *Server) getToolList() []string {
	tools := s.engine.Agent.GetToolbox().List()
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name()
	}
	return names
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() error {
	logger.Info("ACP: Shutting down server")
	s.cancel()
	return s.engine.Shutdown()
}

// handlePermissionResponse handles session/respond_permission requests from the client
func (s *Server) handlePermissionResponse(req *RPCRequest) {
	var params struct {
		SessionID string            `json:"sessionId"`
		RequestID string            `json:"requestId"`
		Result    *PermissionResult `json:"result"`
	}

	if err := unmarshalParams(req.Params, &params); err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", err.Error())
		return
	}

	// Verify session exists
	if _, ok := s.registry.Get(params.SessionID); !ok {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeSessionNotFound, "Session not found", params.SessionID)
		return
	}

	// Forward to permission bridge
	if s.permissionBridge != nil {
		s.permissionBridge.HandlePermissionResponse(params.RequestID, params.Result)
	}

	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"success": true,
	})
}

// handleFSRead handles fs/read_text_file requests
func (s *Server) handleFSRead(req *RPCRequest) {
	var params struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
	}

	if err := unmarshalParams(req.Params, &params); err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", err.Error())
		return
	}

	// Get session to verify it exists
	_, ok := s.registry.Get(params.SessionID)
	if !ok {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeSessionNotFound, "Session not found", params.SessionID)
		return
	}

	// Use FSBridge to read file
	if s.fsBridge == nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "FS bridge not initialized", "")
		return
	}

	content, err := s.fsBridge.ReadFile(s.ctx, params.Path)
	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to read file", err.Error())
		return
	}

	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"content": content,
	})
}

// handleFSWrite handles fs/write_text_file requests
func (s *Server) handleFSWrite(req *RPCRequest) {
	var params struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
		Content   string `json:"content"`
	}

	if err := unmarshalParams(req.Params, &params); err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", err.Error())
		return
	}

	// Get session to verify it exists
	_, ok := s.registry.Get(params.SessionID)
	if !ok {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeSessionNotFound, "Session not found", params.SessionID)
		return
	}

	// Use FSBridge to write file
	if s.fsBridge == nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "FS bridge not initialized", "")
		return
	}

	err := s.fsBridge.WriteFile(s.ctx, params.Path, params.Content)
	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to write file", err.Error())
		return
	}

	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"success": true,
	})
}

// handleTerminalCreate handles terminal/create requests
func (s *Server) handleTerminalCreate(req *RPCRequest) {
	var params struct {
		SessionID string            `json:"sessionId"`
		Command   string            `json:"command"`
		CWD       string            `json:"cwd"`
		Env       map[string]string `json:"env"`
	}

	if err := unmarshalParams(req.Params, &params); err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", err.Error())
		return
	}

	// Get session to verify it exists
	_, ok := s.registry.Get(params.SessionID)
	if !ok {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeSessionNotFound, "Session not found", params.SessionID)
		return
	}

	// Use TerminalBridge to create terminal
	if s.terminalBridge == nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Terminal bridge not initialized", "")
		return
	}

	// For local mode, we should execute the command locally and return a terminal ID
	// Generate a unique terminal ID based on the RPC request ID
	var terminalID string
	switch v := req.ID.(type) {
	case float64:
		terminalID = fmt.Sprintf("term-%d", int(v))
	case int:
		terminalID = fmt.Sprintf("term-%d", v)
	case string:
		terminalID = fmt.Sprintf("term-%s", v)
	default:
		terminalID = fmt.Sprintf("term-%v", v)
	}

	// For now, just return success with a terminal ID
	// The actual command execution would need to be implemented with proper process management
	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"terminalId": terminalID,
	})

	// Simulate command execution and send output/exit notifications
	go func() {
		// Execute the command using a shell
		// Parse the command properly - if it contains shell syntax like '>' redirection,
		// we need to use sh -c
		var cmd *exec.Cmd
		if strings.Contains(params.Command, ">") || strings.Contains(params.Command, "|") {
			cmd = exec.CommandContext(s.ctx, "sh", "-c", params.Command)
		} else {
			cmdParts := strings.Fields(params.Command)
			if len(cmdParts) == 0 {
				return
			}
			cmd = exec.CommandContext(s.ctx, cmdParts[0], cmdParts[1:]...)
		}
		if params.CWD != "" {
			cmd.Dir = params.CWD
		}

		output, err := cmd.CombinedOutput()

		// Send output notification
		if len(output) > 0 {
			_ = s.transport.SendNotification("terminal/output", map[string]interface{}{
				"terminalId": terminalID,
				"data":       string(output),
			})
		}

		// Send exit notification
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}

		_ = s.transport.SendNotification("terminal/exit", map[string]interface{}{
			"terminalId": terminalID,
			"exitCode":   exitCode,
		})
	}()
}

// handleSessionPrompt handles session/prompt requests
func (s *Server) handleSessionPrompt(req *RPCRequest) {
	var params SessionPromptParams
	if err := unmarshalParams(req.Params, &params); err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", err.Error())
		return
	}

	// Get session
	session, ok := s.registry.Get(params.SessionID)
	if !ok {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeSessionNotFound, "Session not found", params.SessionID)
		return
	}

	// Process all ContentBlock types
	textContent, images, err := s.processContentBlocks(params.Prompt, session.CWD)
	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInvalidParams, "Failed to process content blocks", err.Error())
		return
	}

	// Create event bridge for this session
	bridge := NewEventBridge(session.ACPSessionID, s.transport)

	// Check if the input is a slash command
	if strings.HasPrefix(strings.TrimSpace(textContent), "/") {
		if handled := s.handleSlashCommand(session, textContent, bridge); handled {
			result := SessionPromptResult{StopReason: "end_turn"}
			_ = s.transport.SendSuccessResponse(req.ID, result)
			return
		}
	}

	// Create context for this request
	ctx, cancel := context.WithCancel(s.ctx)
	s.registry.SetCancel(session.ACPSessionID, cancel)
	defer cancel()

	// Set the active session
	s.engine.Agent.SetActiveSessionID(session.NanoSessionID)

	// Set up approval handler V2 using permission bridge if available
	if s.permissionBridge != nil {
		s.engine.Agent.SetApprovalHandlerV2(func(info *agent.ToolCallInfo) agent.ApprovalDecision {
			decision, err := s.permissionBridge.RequestApprovalV2(ctx, info)
			if err != nil {
				logger.Errorf("ACP: Permission request error: %v", err)
				return agent.ApprovalReject
			}
			return decision
		})
	}

	// Process the message
	err = s.engine.Agent.ProcessStreamWithMultimodal(
		ctx,
		textContent,
		images,
		bridge.OnStreamEvent,
	)

	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Session prompt failed", err.Error())
		return
	}

	// Send success response with stop reason
	result := SessionPromptResult{
		StopReason: "end_turn",
	}
	_ = s.transport.SendSuccessResponse(req.ID, result)
}

// handleSessionCancel handles session/cancel notification
func (s *Server) handleSessionCancel(req *RPCRequest) {
	var params SessionCancelParams
	if err := unmarshalParams(req.Params, &params); err != nil {
		logger.Errorf("ACP: Invalid session/cancel params: %v", err)
		return
	}

	// Get session cancel function
	cancel := s.registry.GetCancel(params.SessionID)
	if cancel != nil {
		logger.Infof("ACP: Cancelling session: %s", params.SessionID)
		cancel()
	} else {
		logger.Warnf("ACP: No active cancel function for session: %s", params.SessionID)
	}

	// No response for notification
}

// handleSessionLoad handles session/load requests
func (s *Server) handleSessionLoad(req *RPCRequest) {
	var params struct {
		SessionID string `json:"sessionId"`
	}
	if err := unmarshalParams(req.Params, &params); err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", err.Error())
		return
	}

	// Verify session exists
	session, ok := s.registry.Get(params.SessionID)
	if !ok {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeSessionNotFound, "Session not found", params.SessionID)
		return
	}

	// Load session history from nano session
	nanoSession, ok := s.engine.Agent.GetSessionManager().GetSession(session.NanoSessionID)
	if !ok {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to load session history", nil)
		return
	}

	// Convert nano session history to ACP format
	messages := make([]map[string]interface{}, 0)
	for _, msg := range nanoSession.ConversationHistory {
		messages = append(messages, map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	result := map[string]interface{}{
		"sessionId": session.ACPSessionID,
		"messages":  messages,
	}

	_ = s.transport.SendSuccessResponse(req.ID, result)
}

// unmarshalParams unmarshals RPC params into a target struct
func unmarshalParams(params interface{}, target interface{}) error {
	if params == nil {
		return fmt.Errorf("params is nil")
	}

	// Re-marshal and unmarshal to convert map to struct
	data, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal params: %w", err)
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	return nil
}

// handleInitialize handles the initialize method
func (s *Server) handleInitialize(req *RPCRequest) {
	var params InitializeParams
	if err := unmarshalParams(req.Params, &params); err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", err.Error())
		return
	}

	// Store client capabilities for later use
	s.clientCapabilities = params.ClientCapabilities

	// Version negotiation: if client version is higher than server supports, return server version
	protocolVersion := params.ProtocolVersion
	if protocolVersion > ProtocolVersion {
		protocolVersion = ProtocolVersion
	}

	// Check if client supports the negotiated version
	if protocolVersion < params.ProtocolVersion && params.ProtocolVersion > ProtocolVersion {
		logger.Warnf("ACP: Client requested protocol version %d, but server only supports up to %d",
			params.ProtocolVersion, ProtocolVersion)
	}

	result := InitializeResult{
		ProtocolVersion: protocolVersion,
		AgentCapabilities: AgentCapabilities{
			LoadSession: true,
			PromptCapabilities: PromptCapabilities{
				Image:           true,
				Audio:           true,
				EmbeddedContext: true,
			},
			MCP: MCPCapabilities{
				HTTP: true,
				SSE:  true,
			},
			SessionCapabilities: &SessionCapabilities{
				Resume: emptyObj,
				Close:  emptyObj,
				List:   emptyObj,
			},
			FS: &FSCapabilities{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: &TerminalCapabilities{
				Create:      true,
				Output:      true,
				Release:     true,
				WaitForExit: true,
				Kill:        true,
			},
			Tools: s.getToolList(),
		},
		AgentInfo: AgentInfo{
			Name:    "nano-agent",
			Title:   "Nano Agent",
			Version: "1.0.0",
		},
		AuthMethods: []AuthMethod{
			{
				ID:          "terminal-setup",
				Name:        "Run setup in terminal",
				Description: "Configure NANO_API_KEY and provider settings interactively",
				Type:        "terminal",
				Args:        []string{"acp", "setup"},
			},
		},
	}

	_ = s.transport.SendSuccessResponse(req.ID, result)
	logger.Infof("ACP: Initialized with protocol version %d, client: %s %s (fs caps: %v, terminal: %v)",
		protocolVersion, params.ClientInfo.Name, params.ClientInfo.Version,
		params.ClientCapabilities.FS != nil, params.ClientCapabilities.Terminal)
}
