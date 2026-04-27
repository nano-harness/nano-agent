// Package daemon provides the daemon client and server implementation
package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/tools"
	"github.com/gorilla/websocket"
)

// Client represents a client for communicating with nano daemon
type Client struct {
	baseURL   string
	apiKey    string
	client    *http.Client
	userAgent string
}

// NewClient creates a new daemon client
func NewClient(host string, port int, apiKey string) *Client {
	baseURL := fmt.Sprintf("http://%s:%d/api/v1", normalizeClientHost(host), port)

	// Get timeout from config, fallback to 30 seconds
	timeout := 30 * time.Second
	cfg := config.Get()
	if cfg != nil {
		timeout = cfg.HTTPTimeout
	}

	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: timeout,
		},
		userAgent: "nano-client/1.0",
	}
}

// NewClientFromConfig creates a client from daemon config
func NewClientFromConfig(daemonConfig *config.DaemonConfig) *Client {
	baseURL := fmt.Sprintf("http://%s:%d/api/v1", normalizeClientHost(daemonConfig.Host), daemonConfig.Port)

	// Get timeout from main config, fallback to 30 seconds
	timeout := 30 * time.Second
	cfg := config.Get()
	if cfg != nil {
		timeout = cfg.HTTPTimeout
	}

	return &Client{
		baseURL: baseURL,
		apiKey:  daemonConfig.APIKey,
		client: &http.Client{
			Timeout: timeout,
		},
		userAgent: "nano-client/1.0",
	}
}

func normalizeClientHost(host string) string {
	h := strings.TrimSpace(host)
	if h == "" || h == "0.0.0.0" || h == "::" || h == "[::]" {
		return "localhost"
	}
	return h
}

func (c *Client) streamURL() (string, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	} else {
		parsed.Scheme = "ws"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/stream"
	q := parsed.Query()
	if strings.TrimSpace(c.apiKey) != "" {
		q.Set("api_key", strings.TrimSpace(c.apiKey))
	}
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

// StreamURL returns the daemon-wide WebSocket stream URL.
func (c *Client) StreamURL() (string, error) {
	return c.streamURL()
}

func (c *Client) teamLeadStreamURL(sessionID string) (string, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	} else {
		parsed.Scheme = "ws"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/teams/sessions/" + url.PathEscape(sessionID) + "/stream"
	q := parsed.Query()
	if strings.TrimSpace(c.apiKey) != "" {
		q.Set("api_key", strings.TrimSpace(c.apiKey))
	}
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

// TeamLeadStreamURL returns the WebSocket stream URL for a team-lead session.
func (c *Client) TeamLeadStreamURL(sessionID string) (string, error) {
	return c.teamLeadStreamURL(sessionID)
}

func parseSeqFromMessage(msg map[string]interface{}) int64 {
	if v, ok := msg["seq"]; ok {
		switch s := v.(type) {
		case float64:
			return int64(s)
		case int64:
			return s
		case int:
			return int64(s)
		}
	}
	if v, ok := msg["last_seq"]; ok {
		switch s := v.(type) {
		case float64:
			return int64(s)
		case int64:
			return s
		case int:
			return int64(s)
		}
	}
	return 0
}

func parseMessageType(msg map[string]interface{}) string {
	v, _ := msg["type"].(string)
	return strings.TrimSpace(v)
}

func mergeChunks(parts map[int]string, _ int) string {
	keys := make([]int, 0, len(parts))
	for k := range parts {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(parts[k])
	}
	return b.String()
}

// SubscribeOptions options for subscribing to a session
type SubscribeOptions struct {
	SessionID       string
	RunID           string
	SinceSeq        int64
	Streams         []string
	ReconnectDelay  time.Duration
	ApprovalHandler func(ToolApprovalRequest) bool
}

// LeadInputOptions configures a team-lead WebSocket input frame.
type LeadInputOptions struct {
	SessionID       string
	Command         string
	TaskID          string
	SinceSeq        int64
	ReconnectDelay  time.Duration
	ApprovalHandler func(ToolApprovalRequest) bool
}

// ToolApprovalRequest describes an approval prompt emitted by the daemon stream.
type ToolApprovalRequest struct {
	CallID     string
	ToolName   string
	Parameters map[string]interface{}
}

// CreateTeamLeadSession creates or resumes a daemon team-lead session.
func (c *Client) CreateTeamLeadSession(sessionID, teamName string) (*TeamLeadSessionResponse, error) {
	return c.CreateTeamLeadSessionWithOptions(sessionID, teamName, false)
}

// CreateTeamLeadSessionWithOptions creates or resumes a daemon team-lead session.
func (c *Client) CreateTeamLeadSessionWithOptions(sessionID, teamName string, interactiveConfirm bool) (*TeamLeadSessionResponse, error) {
	req := CreateTeamLeadSessionRequest{
		SessionID:          strings.TrimSpace(sessionID),
		TeamName:           strings.TrimSpace(teamName),
		InteractiveConfirm: interactiveConfirm,
	}
	var response TeamLeadSessionResponse
	err := c.doRequest("POST", "/teams/sessions", req, &response)
	return &response, err
}

// SendLeadInputWithResume sends one lead_input WebSocket frame and streams events until completion.
func (c *Client) SendLeadInputWithResume(ctx context.Context, opts LeadInputOptions, onMessage func(map[string]interface{})) (map[string]interface{}, int64, error) {
	if strings.TrimSpace(opts.SessionID) == "" {
		return nil, opts.SinceSeq, fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(opts.Command) == "" {
		return nil, opts.SinceSeq, fmt.Errorf("command is required")
	}
	reconnectDelay := opts.ReconnectDelay
	if reconnectDelay <= 0 {
		reconnectDelay = 1500 * time.Millisecond
	}
	sinceSeq := opts.SinceSeq
	streamURL, err := c.teamLeadStreamURL(opts.SessionID)
	if err != nil {
		return nil, sinceSeq, err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	sentInput := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, sinceSeq, err
		}
		conn, _, err := dialer.DialContext(ctx, streamURL, nil)
		if err != nil {
			if err := ctx.Err(); err != nil {
				return nil, sinceSeq, err
			}
			select {
			case <-ctx.Done():
				return nil, sinceSeq, ctx.Err()
			case <-time.After(reconnectDelay):
				continue
			}
		}
		payload := map[string]interface{}{}
		if sentInput {
			payload["type"] = "subscribe"
			payload["since_seq"] = sinceSeq
		} else {
			payload["type"] = "lead_input"
			payload["command"] = strings.TrimSpace(opts.Command)
			payload["since_seq"] = sinceSeq
			if strings.TrimSpace(opts.TaskID) != "" {
				payload["task_id"] = strings.TrimSpace(opts.TaskID)
			}
			sentInput = true
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(payload); err != nil {
			_ = conn.Close()
			if err := ctx.Err(); err != nil {
				return nil, sinceSeq, err
			}
			select {
			case <-ctx.Done():
				return nil, sinceSeq, ctx.Err()
			case <-time.After(reconnectDelay):
				continue
			}
		}
		_ = conn.SetWriteDeadline(time.Time{})
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			return nil
		})
		for {
			if err := ctx.Err(); err != nil {
				_ = conn.Close()
				return nil, sinceSeq, err
			}
			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				_ = conn.Close()
				if err := ctx.Err(); err != nil {
					return nil, sinceSeq, err
				}
				if !sleepBeforeReconnect(ctx, reconnectDelay) {
					return nil, sinceSeq, ctx.Err()
				}
				break
			}
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			if seq := parseSeqFromMessage(msg); seq > sinceSeq {
				sinceSeq = seq
			}
			if onMessage != nil {
				onMessage(msg)
			}
			if req, ok := toolApprovalRequestFromMessage(msg); ok && opts.ApprovalHandler != nil {
				approved := opts.ApprovalHandler(req)
				if err := writeToolApproval(conn, req.CallID, approved); err != nil {
					_ = conn.Close()
					return nil, sinceSeq, err
				}
				continue
			}
			switch parseMessageType(msg) {
			case "completion":
				_ = conn.Close()
				return msg, sinceSeq, nil
			case "error":
				errText, _ := msg["error"].(string)
				if errText != "" {
					_ = conn.Close()
					return msg, sinceSeq, errors.New(errText)
				}
			}
		}
	}
}

func toolApprovalRequestFromMessage(msg map[string]interface{}) (ToolApprovalRequest, bool) {
	if parseMessageType(msg) != string(event.EventTypeWaitingForUser) {
		return ToolApprovalRequest{}, false
	}
	metadata, ok := msg["metadata"].(map[string]interface{})
	if !ok || strings.TrimSpace(stringFromMap(metadata, "kind")) != "tool_approval_request" {
		return ToolApprovalRequest{}, false
	}
	callID := strings.TrimSpace(stringFromMap(metadata, "call_id"))
	if callID == "" {
		return ToolApprovalRequest{}, false
	}
	params, _ := metadata["parameters"].(map[string]interface{})
	return ToolApprovalRequest{
		CallID:     callID,
		ToolName:   strings.TrimSpace(stringFromMap(metadata, "tool_name")),
		Parameters: params,
	}, true
}

func stringFromMap(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func writeToolApproval(conn *websocket.Conn, callID string, approved bool) error {
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err := conn.WriteJSON(map[string]interface{}{
		"type":     "tool_approval",
		"call_id":  callID,
		"approved": approved,
	})
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

// SubmitToolApproval writes a tool approval decision on an existing WebSocket.
func SubmitToolApproval(conn *websocket.Conn, callID string, approved bool) error {
	return writeToolApproval(conn, callID, approved)
}

func sleepBeforeReconnect(ctx context.Context, delay time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

// SubscribeSessionWithResume subscribes to a session with resume capability
func (c *Client) SubscribeSessionWithResume(ctx context.Context, opts SubscribeOptions, onMessage func(map[string]interface{})) (map[string]interface{}, int64, error) {
	if strings.TrimSpace(opts.SessionID) == "" {
		return nil, opts.SinceSeq, fmt.Errorf("session_id is required")
	}
	reconnectDelay := opts.ReconnectDelay
	if reconnectDelay <= 0 {
		reconnectDelay = 1500 * time.Millisecond
	}
	sinceSeq := opts.SinceSeq
	streamURL, err := c.streamURL()
	if err != nil {
		return nil, sinceSeq, err
	}
	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, sinceSeq, err
		}
		conn, _, err := dialer.DialContext(ctx, streamURL, nil)
		if err != nil {
			if err := ctx.Err(); err != nil {
				return nil, sinceSeq, err
			}
			select {
			case <-ctx.Done():
				return nil, sinceSeq, ctx.Err()
			case <-time.After(reconnectDelay):
				continue
			}
		}
		connWatcherDone := make(chan struct{})
		var connWatcherOnce sync.Once
		stopConnWatcher := func() {
			connWatcherOnce.Do(func() {
				close(connWatcherDone)
			})
		}
		go func(wsConn *websocket.Conn) {
			select {
			case <-ctx.Done():
				// Force any in-flight ReadMessage call to unblock promptly.
				_ = wsConn.Close()
			case <-connWatcherDone:
			}
		}(conn)
		subscribePayload := map[string]interface{}{
			"type":       "subscribe",
			"session_id": opts.SessionID,
			"since_seq":  sinceSeq,
		}
		if strings.TrimSpace(opts.RunID) != "" {
			subscribePayload["run_id"] = strings.TrimSpace(opts.RunID)
		}
		if len(opts.Streams) > 0 {
			subscribePayload["streams"] = opts.Streams
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(subscribePayload); err != nil {
			stopConnWatcher()
			_ = conn.Close()
			if err := ctx.Err(); err != nil {
				return nil, sinceSeq, err
			}
			select {
			case <-ctx.Done():
				return nil, sinceSeq, ctx.Err()
			case <-time.After(reconnectDelay):
				continue
			}
		}
		_ = conn.SetWriteDeadline(time.Time{})
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			return nil
		})
		chunkBuffers := make(map[string]map[int]string)
		chunkTotals := make(map[string]int)
		for {
			if err := ctx.Err(); err != nil {
				stopConnWatcher()
				_ = conn.Close()
				return nil, sinceSeq, err
			}
			_, message, err := conn.ReadMessage()
			if err != nil {
				stopConnWatcher()
				_ = conn.Close()
				if err := ctx.Err(); err != nil {
					return nil, sinceSeq, err
				}
				select {
				case <-ctx.Done():
					return nil, sinceSeq, ctx.Err()
				case <-time.After(reconnectDelay):
					goto reconnect
				}
			}
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}
			if isChunk, _ := msg["is_chunk"].(bool); isChunk {
				id, _ := msg["id"].(string)
				if id == "" {
					continue
				}
				indexFloat, idxOK := msg["index"].(float64)
				totalFloat, totalOK := msg["total"].(float64)
				data, dataOK := msg["data"].(string)
				if !idxOK || !totalOK || !dataOK {
					continue
				}
				index := int(indexFloat)
				total := int(totalFloat)
				if _, ok := chunkBuffers[id]; !ok {
					chunkBuffers[id] = make(map[int]string)
				}
				chunkBuffers[id][index] = data
				chunkTotals[id] = total
				if len(chunkBuffers[id]) < total {
					continue
				}
				merged := mergeChunks(chunkBuffers[id], chunkTotals[id])
				delete(chunkBuffers, id)
				delete(chunkTotals, id)
				msg = map[string]interface{}{}
				if err := json.Unmarshal([]byte(merged), &msg); err != nil {
					continue
				}
			}
			if seq := parseSeqFromMessage(msg); seq > sinceSeq {
				sinceSeq = seq
			}
			if onMessage != nil {
				onMessage(msg)
			}
			if req, ok := toolApprovalRequestFromMessage(msg); ok && opts.ApprovalHandler != nil {
				approved := opts.ApprovalHandler(req)
				if err := writeToolApproval(conn, req.CallID, approved); err != nil {
					stopConnWatcher()
					_ = conn.Close()
					return nil, sinceSeq, err
				}
				continue
			}
			msgType := parseMessageType(msg)
			if msgType == "completion" {
				stopConnWatcher()
				_ = conn.Close()
				return msg, sinceSeq, nil
			}
			if msgType == "error" {
				errText, _ := msg["error"].(string)
				if errText != "" && strings.Contains(strings.ToLower(errText), "run_id mismatch") {
					stopConnWatcher()
					_ = conn.Close()
					return msg, sinceSeq, errors.New(errText)
				}
				if errText != "" && strings.Contains(strings.ToLower(errText), "no task found for session") {
					stopConnWatcher()
					_ = conn.Close()
					select {
					case <-ctx.Done():
						return nil, sinceSeq, ctx.Err()
					case <-time.After(reconnectDelay):
						goto reconnect
					}
				}
			}
		}
	reconnect:
	}
}

// Health checks daemon health
func (c *Client) Health() (*HealthResponse, error) {
	var response HealthResponse
	err := c.doRequest("GET", "/health", nil, &response)
	return &response, err
}

// Status gets daemon status
func (c *Client) Status() (*StatusResponse, error) {
	var response StatusResponse
	err := c.doRequest("GET", "/status", nil, &response)
	return &response, err
}

// ExecuteInSession executes a command in a session
func (c *Client) ExecuteInSession(command string, sessionID string, timeout int, includeSteps bool, async bool) (*ExecuteResponse, error) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = generateClientSessionID()
	}
	request := ExecuteRequest{
		Command:      command,
		Timeout:      timeout,
		IncludeSteps: includeSteps,
		Async:        async,
	}

	if timeout > 0 {
		requestTimeout := time.Duration(NormalizeTaskTimeoutSeconds(timeout)+ClientHTTPGraceSeconds) * time.Second
		if c.client.Timeout < requestTimeout {
			c.client.Timeout = requestTimeout
		}
	}

	var response ExecuteResponse
	err := c.doRequest("POST", fmt.Sprintf("/sessions/%s/execute", url.PathEscape(sessionID)), request, &response)
	if response.SessionID == "" {
		response.SessionID = sessionID
	}
	return &response, err
}

// SessionConfig holds configuration for creating a session
type SessionConfig struct {
	WorkingDir string `json:"working_dir,omitempty"`
}

// SessionResponse represents a session creation response
type SessionResponse struct {
	SessionID string `json:"session_id"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
}

// CreateSession creates a new session with optional configuration
// Note: The daemon creates sessions implicitly on first execute, but this provides
// an explicit way to create and configure a session for testing purposes
func (c *Client) CreateSession(ctx context.Context, sessionID string, config *SessionConfig) (*SessionResponse, error) {
	// If no session ID provided, generate one
	if strings.TrimSpace(sessionID) == "" {
		sessionID = generateClientSessionID()
	}

	// Currently the daemon doesn't have a dedicated session creation endpoint
	// Sessions are created implicitly when you execute a command
	// For now, we'll return a response indicating the session ID to use
	return &SessionResponse{
		SessionID: sessionID,
		Success:   true,
		Message:   "Session ID generated (will be created on first execute)",
	}, nil
}

// SendMessage sends a message to an existing session
// This is an alias for ExecuteInSession for better API clarity
func (c *Client) SendMessage(ctx context.Context, sessionID string, message string) (*ExecuteResponse, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	return c.ExecuteInSession(message, sessionID, 0, false, false)
}

// StreamEvents streams events from a session via WebSocket
func (c *Client) StreamEvents(ctx context.Context, sessionID string, callback func(event.StreamEvent) error) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session_id is required")
	}

	opts := SubscribeOptions{
		SessionID: sessionID,
		SinceSeq:  0,
	}

	_, _, err := c.SubscribeSessionWithResume(ctx, opts, func(msg map[string]interface{}) {
		// Convert the message to a StreamEvent
		ev := messageToStreamEvent(msg)
		if callback != nil {
			_ = callback(ev)
		}
	})

	return err
}

// messageToStreamEvent converts a WebSocket message to a StreamEvent
func messageToStreamEvent(msg map[string]interface{}) event.StreamEvent {
	ev := event.StreamEvent{}

	// Extract type
	if typeStr, ok := msg["type"].(string); ok {
		ev.Type = event.EventType(typeStr)
	}

	// Extract timestamp
	if ts, ok := msg["timestamp"].(float64); ok {
		ev.Timestamp = int64(ts)
	}

	// Extract content
	if content, ok := msg["content"].(string); ok {
		ev.Content = content
	}

	// Extract error
	if errStr, ok := msg["error"].(string); ok {
		ev.Error = errStr
	}

	// Extract source
	if source, ok := msg["source"].(string); ok {
		ev.Source = source
	}

	// Extract worker_id
	if workerID, ok := msg["worker_id"].(string); ok {
		ev.WorkerID = workerID
	}

	// Extract session_id
	if sessionID, ok := msg["session_id"].(string); ok {
		ev.SessionID = sessionID
	}

	// Extract metadata
	if metadata, ok := msg["metadata"].(map[string]interface{}); ok {
		ev.Metadata = metadata
	}

	// Extract tool_calls
	if toolCalls, ok := msg["tool_calls"].([]interface{}); ok {
		var calls []*tools.ToolCall
		for _, tc := range toolCalls {
			if tcMap, ok := tc.(map[string]interface{}); ok {
				call := &tools.ToolCall{}
				if id, ok := tcMap["id"].(string); ok {
					call.ID = id
				}
				if name, ok := tcMap["name"].(string); ok {
					call.Name = name
				}
				if args, ok := tcMap["arguments"].(map[string]interface{}); ok {
					call.Arguments = args
				}
				calls = append(calls, call)
			}
		}
		ev.ToolCalls = calls
	}

	return ev
}

// ListSessions lists sessions
func (c *Client) ListSessions(limit int) (*SessionsListResponse, error) {
	path := "/sessions"
	if limit > 0 {
		path = fmt.Sprintf("/sessions?limit=%d", limit)
	}

	var response SessionsListResponse
	err := c.doRequest("GET", path, nil, &response)
	return &response, err
}

// GetSession gets a session
func (c *Client) GetSession(id string) (map[string]any, error) {
	var response map[string]any
	err := c.doRequest("GET", fmt.Sprintf("/sessions/%s", url.PathEscape(id)), nil, &response)
	return response, err
}

// CancelSession cancels a session
func (c *Client) CancelSession(id string) (map[string]any, error) {
	var response map[string]any
	err := c.doRequest("POST", fmt.Sprintf("/sessions/%s/cancel", url.PathEscape(id)), nil, &response)
	return response, err
}

// CancelTeamLeadSession cancels a team-lead session.
func (c *Client) CancelTeamLeadSession(id string) (map[string]any, error) {
	var response map[string]any
	err := c.doRequest("POST", fmt.Sprintf("/teams/sessions/%s/cancel", url.PathEscape(id)), nil, &response)
	return response, err
}

// DeleteSession deletes a session
func (c *Client) DeleteSession(id string) (map[string]any, error) {
	var response map[string]any
	err := c.doRequest("DELETE", fmt.Sprintf("/sessions/%s", url.PathEscape(id)), nil, &response)
	return response, err
}

// ScheduleTask creates a new scheduled task
func (c *Client) ScheduleTask(cronExpr, command string) (map[string]any, error) {
	req := map[string]string{
		"cron_expression": cronExpr,
		"command":         command,
	}
	var response map[string]any
	err := c.doRequest("POST", "/scheduler/tasks", req, &response)
	return response, err
}

// ListTasks returns all scheduled tasks
func (c *Client) ListTasks() (map[string]any, error) {
	var response map[string]any
	err := c.doRequest("GET", "/scheduler/tasks", nil, &response)
	return response, err
}

// DeleteTask removes a scheduled task
func (c *Client) DeleteTask(id string) (map[string]any, error) {
	var response map[string]any
	err := c.doRequest("DELETE", fmt.Sprintf("/scheduler/tasks/%s", url.PathEscape(id)), nil, &response)
	return response, err
}

// ResetSession resets a session
func (c *Client) ResetSession(sessionID string) (map[string]any, error) {
	var body interface{}
	if sessionID != "" {
		body = map[string]string{"session_id": sessionID}
	}
	var response map[string]any
	err := c.doRequest("POST", "/sessions/reset", body, &response)
	return response, err
}

// MCPStatus gets MCP status information
func (c *Client) MCPStatus() (*MCPStatusResponse, error) {
	var response MCPStatusResponse
	err := c.doRequest("GET", "/mcp/status", nil, &response)
	return &response, err
}

// MCPTools gets available MCP tools
func (c *Client) MCPTools() (*MCPToolsResponse, error) {
	var response MCPToolsResponse
	err := c.doRequest("GET", "/mcp/tools", nil, &response)
	return &response, err
}

// MCPDiagnostics gets MCP diagnostics
func (c *Client) MCPDiagnostics() (*MCPDiagnosticsResponse, error) {
	var response MCPDiagnosticsResponse
	err := c.doRequest("GET", "/mcp/diagnostics", nil, &response)
	return &response, err
}

// ListMemory lists memory entries
func (c *Client) ListMemory() (*MemoryListResponse, error) {
	var response MemoryListResponse
	err := c.doRequest("GET", "/memory", nil, &response)
	return &response, err
}

// SaveMemory saves a memory entry
func (c *Client) SaveMemory(key, content string, tags []string) (*MemorySaveResponse, error) {
	request := MemorySaveRequest{
		Key:     key,
		Content: content,
		Tags:    tags,
	}

	var response MemorySaveResponse
	err := c.doRequest("POST", "/memory", request, &response)
	return &response, err
}

// GetMemory gets a specific memory entry
func (c *Client) GetMemory(key string) (*MemoryGetResponse, error) {
	var response MemoryGetResponse
	err := c.doRequest("GET", fmt.Sprintf("/memory/%s", url.PathEscape(key)), nil, &response)
	return &response, err
}

// DeleteMemory deletes a memory entry
func (c *Client) DeleteMemory(key string) (*MemoryDeleteResponse, error) {
	var response MemoryDeleteResponse
	err := c.doRequest("DELETE", fmt.Sprintf("/memory/%s", url.PathEscape(key)), nil, &response)
	return &response, err
}

// doRequest performs HTTP request to daemon
func (c *Client) doRequest(method, path string, requestBody interface{}, responseBody interface{}) error {
	url := c.baseURL + path

	var bodyReader io.Reader
	if requestBody != nil {
		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("User-Agent", c.userAgent)
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	// Make request
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Read response
	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respData))
	}

	// Parse response
	if responseBody != nil {
		if err := json.Unmarshal(respData, responseBody); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
	}

	return nil
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string  `json:"status"`
	Timestamp int64   `json:"timestamp"`
	Version   string  `json:"version"`
	Uptime    float64 `json:"uptime"`
}

// StatusResponse represents the status check response
type StatusResponse struct {
	AgentStatus string `json:"agent_status"`
	MCPEnabled  bool   `json:"mcp_enabled"`
	MemorySize  int    `json:"memory_size"`
	ActiveTools int    `json:"active_tools"`
}

// ExecuteRequest represents an execution request
type ExecuteRequest struct {
	Command      string `json:"command"`
	Timeout      int    `json:"timeout,omitempty"`
	IncludeSteps bool   `json:"include_steps,omitempty"`
	Async        bool   `json:"async,omitempty"`
}

// ExecuteResponse represents an execution response
type ExecuteResponse struct {
	Success    bool                `json:"success"`
	Result     string              `json:"result"`
	Error      string              `json:"error,omitempty"`
	Steps      []event.StreamEvent `json:"steps,omitempty"`
	TokenStats *event.TokenStats   `json:"token_stats,omitempty"`

	SessionID string `json:"session_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	Status    string `json:"status,omitempty"`
	Completed bool   `json:"completed,omitempty"`
}

func generateClientSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("sess_%s", hex.EncodeToString(b))
}

// SessionsListResponse represents a list of sessions
type SessionsListResponse struct {
	Success  bool             `json:"success"`
	Sessions []SessionSummary `json:"sessions"`
}

// SessionSummary represents a summary of a session
type SessionSummary struct {
	ID           string  `json:"id"`
	Type         string  `json:"type,omitempty"`
	Title        string  `json:"title,omitempty"`
	CreatedAt    string  `json:"created_at,omitempty"`
	LastActiveAt string  `json:"last_active_at,omitempty"`
	Status       string  `json:"status,omitempty"`
	Duration     float64 `json:"duration,omitempty"`
	TotalTokens  int     `json:"total_tokens,omitempty"`
	Stored       bool    `json:"stored,omitempty"`
	Active       bool    `json:"active,omitempty"`
}

// MCPStatusResponse represents the MCP status response
type MCPStatusResponse struct {
	Enabled     bool          `json:"enabled"`
	Servers     int           `json:"servers"`
	Tools       int           `json:"tools"`
	Connections []interface{} `json:"connections"`
}

// MCPToolsResponse represents the MCP tools response
type MCPToolsResponse struct {
	Tools []interface{} `json:"tools"`
}

// MCPDiagnosticsResponse represents the MCP diagnostics response
type MCPDiagnosticsResponse struct {
	Status  string                 `json:"status"`
	Servers []interface{}          `json:"servers"`
	Metrics map[string]interface{} `json:"metrics"`
}

// MemoryListResponse represents the memory list response
type MemoryListResponse struct {
	Entries []interface{} `json:"entries"`
	Count   int           `json:"count"`
}

// MemorySaveRequest represents a memory save request
type MemorySaveRequest struct {
	Key     string   `json:"key"`
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
}

// MemorySaveResponse represents a memory save response
type MemorySaveResponse struct {
	Success bool   `json:"success"`
	Key     string `json:"key"`
}

// MemoryGetResponse represents a memory get response
type MemoryGetResponse struct {
	Key     string `json:"key"`
	Content string `json:"content"`
	Found   bool   `json:"found"`
}

// MemoryDeleteResponse represents a memory delete response
type MemoryDeleteResponse struct {
	Success bool   `json:"success"`
	Key     string `json:"key"`
}
