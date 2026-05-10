package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// Server implements the ACP protocol server
type Server struct {
	transport        *Transport
	registry         *SessionRegistry
	engine           *engine.Engine
	config           *config.Config
	fsMode           FSMode
	ctx              context.Context
	cancel           context.CancelFunc
	permissionBridge *PermissionBridge // Permission bridge for approval requests
	fsBridge         *FSBridge         // Filesystem bridge for fs/* operations
	terminalBridge   *TerminalBridge   // Terminal bridge for terminal/* operations
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

		// Handle request
		go s.handleRequest(req)
	}
}

// handleRequest routes and handles a single RPC request
func (s *Server) handleRequest(req *RPCRequest) {
	switch req.Method {
	case "session/new":
		s.handleSessionNew(req)
	case "session/update":
		s.handleSessionUpdate(req)
	case "session/close":
		s.handleSessionClose(req)
	case "session/list":
		s.handleSessionList(req)
	case "session/respond_permission":
		s.handlePermissionResponse(req)
	case "fs/read":
		s.handleFSRead(req)
	case "fs/write":
		s.handleFSWrite(req)
	case "fs/list":
		s.handleFSList(req)
	case "fs/delete":
		s.handleFSDelete(req)
	case "terminal/run":
		s.handleTerminalRun(req)
	case "terminal/input":
		s.handleTerminalInput(req)
	case "terminal/kill":
		s.handleTerminalKill(req)
	default:
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeMethodNotFound, "Method not found", req.Method)
	}
}

// handleSessionNew handles session/new requests
func (s *Server) handleSessionNew(req *RPCRequest) {
	var params SessionNewParams
	if err := unmarshalParams(req.Params, &params); err != nil {
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

	// Create ACP session
	session, err := s.registry.Create(nanoSessionID, cwd, params.Env, params.Capabilities, s.fsMode)
	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to create session", err.Error())
		return
	}

	// Create permission bridge for this session
	s.permissionBridge = NewPermissionBridge(session.ACPSessionID, s.transport)

	// Create filesystem bridge for this session
	s.fsBridge = NewFSBridge(session.ACPSessionID, s.transport, cwd)

	// Create terminal bridge for this session
	s.terminalBridge = NewTerminalBridge(session.ACPSessionID, s.transport)

	// Prepare response with server capabilities
	result := SessionNewResult{
		SessionID: session.ACPSessionID,
		Capabilities: SessionCapabilities{
			FS: &FSCapabilities{
				Read:   true,
				Write:  true,
				List:   true,
				Delete: true,
			},
			Terminal: &TerminalCapabilities{
				Run:    true,
				Input:  true,
				Output: true,
				Kill:   true,
			},
			Tools:  s.getToolList(),
			Swarm:  false, // TODO: Support swarm mode with flag
			Resume: true,
			Close:  true,
			List:   true,
		},
	}

	_ = s.transport.SendSuccessResponse(req.ID, result)
	logger.Infof("ACP: Session created: %s (nano: %s, cwd: %s)", session.ACPSessionID, nanoSessionID, cwd)
}

// handleSessionUpdate handles session/update requests
func (s *Server) handleSessionUpdate(req *RPCRequest) {
	var params SessionUpdateParams
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

	// Create event bridge for this session
	bridge := NewEventBridge(session.ACPSessionID, s.transport)

	// Create context for this request
	ctx, cancel := context.WithCancel(s.ctx)
	s.registry.SetCancel(session.ACPSessionID, cancel)
	defer cancel()

	// Set the active session
	s.engine.Agent.SetActiveSessionID(session.NanoSessionID)

	// Set up approval handler using permission bridge if available
	if s.permissionBridge != nil {
		s.engine.Agent.SetApprovalHandler(func(info *agent.ToolCallInfo) bool {
			approved, err := s.permissionBridge.RequestApproval(ctx, info)
			if err != nil {
				logger.Errorf("ACP: Permission request error: %v", err)
				return false
			}
			return approved
		})
	}

	// Process the message
	err := s.engine.Agent.ProcessStreamWithMultimodal(
		ctx,
		params.Message.Content,
		nil, // TODO: Convert images if provided
		bridge.OnStreamEvent,
	)

	if err != nil {
		// Send error event
		_ = s.transport.SendNotification("session/update", map[string]interface{}{
			"sessionId": session.ACPSessionID,
			"event": SessionUpdateEvent{
				Type:  "error",
				Error: err.Error(),
			},
		})
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Session update failed", err.Error())
		return
	}

	// Send done event
	_ = s.transport.SendNotification("session/update", map[string]interface{}{
		"sessionId": session.ACPSessionID,
		"event": SessionUpdateEvent{
			Type: "done",
		},
	})

	// Send success response
	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"success": true,
	})
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

// handleFSRead handles fs/read requests
func (s *Server) handleFSRead(req *RPCRequest) {
	var params struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
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

	if s.fsBridge == nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Filesystem bridge not initialized", nil)
		return
	}

	// Read file
	content, err := s.fsBridge.ReadFile(context.Background(), params.Path)
	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to read file", err.Error())
		return
	}

	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"content": content,
	})
}

// handleFSWrite handles fs/write requests
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

	// Verify session exists
	if _, ok := s.registry.Get(params.SessionID); !ok {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeSessionNotFound, "Session not found", params.SessionID)
		return
	}

	if s.fsBridge == nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Filesystem bridge not initialized", nil)
		return
	}

	// Write file
	err := s.fsBridge.WriteFile(context.Background(), params.Path, params.Content)
	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to write file", err.Error())
		return
	}

	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"success": true,
	})
}

// handleFSList handles fs/list requests
func (s *Server) handleFSList(req *RPCRequest) {
	var params struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
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

	if s.fsBridge == nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Filesystem bridge not initialized", nil)
		return
	}

	// List files
	entries, err := s.fsBridge.ListFiles(context.Background(), params.Path)
	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to list files", err.Error())
		return
	}

	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"entries": entries,
	})
}

// handleFSDelete handles fs/delete requests
func (s *Server) handleFSDelete(req *RPCRequest) {
	var params struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
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

	if s.fsBridge == nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Filesystem bridge not initialized", nil)
		return
	}

	// Delete file
	err := s.fsBridge.DeleteFile(context.Background(), params.Path)
	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to delete file", err.Error())
		return
	}

	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"success": true,
	})
}

// handleTerminalRun handles terminal/run requests
func (s *Server) handleTerminalRun(req *RPCRequest) {
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

	// Verify session exists
	if _, ok := s.registry.Get(params.SessionID); !ok {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeSessionNotFound, "Session not found", params.SessionID)
		return
	}

	if s.terminalBridge == nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Terminal bridge not initialized", nil)
		return
	}

	// Run command
	processID, err := s.terminalBridge.Run(context.Background(), params.Command, params.CWD, params.Env)
	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to run command", err.Error())
		return
	}

	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"processId": processID,
	})
}

// handleTerminalInput handles terminal/input requests
func (s *Server) handleTerminalInput(req *RPCRequest) {
	var params struct {
		SessionID string `json:"sessionId"`
		ProcessID string `json:"processId"`
		Data      string `json:"data"`
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

	if s.terminalBridge == nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Terminal bridge not initialized", nil)
		return
	}

	// Send input to process
	err := s.terminalBridge.Input(context.Background(), params.ProcessID, params.Data)
	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to send input", err.Error())
		return
	}

	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"success": true,
	})
}

// handleTerminalKill handles terminal/kill requests
func (s *Server) handleTerminalKill(req *RPCRequest) {
	var params struct {
		SessionID string `json:"sessionId"`
		ProcessID string `json:"processId"`
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

	if s.terminalBridge == nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Terminal bridge not initialized", nil)
		return
	}

	// Kill process
	err := s.terminalBridge.Kill(context.Background(), params.ProcessID)
	if err != nil {
		_ = s.transport.SendErrorResponse(req.ID, ErrCodeInternalError, "Failed to kill process", err.Error())
		return
	}

	_ = s.transport.SendSuccessResponse(req.ID, map[string]interface{}{
		"success": true,
	})
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
