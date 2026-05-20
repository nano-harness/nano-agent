package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// MockACPClient is a test client for the ACP protocol
type MockACPClient struct {
	reader        *bufio.Reader
	writer        io.Writer
	mu            sync.Mutex
	events        []SessionUpdateEvent
	notifications []RPCNotification
}

// NewMockACPClient creates a new mock ACP client
func NewMockACPClient(reader io.Reader, writer io.Writer) *MockACPClient {
	return &MockACPClient{
		reader:        bufio.NewReader(reader),
		writer:        writer,
		events:        make([]SessionUpdateEvent, 0),
		notifications: make([]RPCNotification, 0),
	}
}

// SendRequest sends a JSON-RPC request and returns the response
func (c *MockACPClient) SendRequest(method string, params interface{}, id interface{}) (*RPCResponse, error) {
	req := RPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	// Send request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.mu.Lock()
	data = append(data, '\n')
	if _, err := c.writer.Write(data); err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("write request: %w", err)
	}
	c.mu.Unlock()

	// Read response
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp RPCResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// ReadNotification reads a notification from the server (non-blocking)
func (c *MockACPClient) ReadNotification() (*RPCNotification, error) {
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	var notif RPCNotification
	if err := json.Unmarshal(line, &notif); err != nil {
		return nil, fmt.Errorf("unmarshal notification: %w", err)
	}

	c.mu.Lock()
	c.notifications = append(c.notifications, notif)
	c.mu.Unlock()

	return &notif, nil
}

// Initialize calls initialize
func (c *MockACPClient) Initialize() (*InitializeResult, error) {
	params := InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientCapabilities: ClientCapabilities{
			FS: &FSCapabilities{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: true,
		},
		ClientInfo: ClientInfo{
			Name:    "mock-client",
			Title:   "Mock ACP Client",
			Version: "1.0.0",
		},
	}

	resp, err := c.SendRequest("initialize", params, "init-1")
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	var result InitializeResult
	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SessionNew calls session/new
func (c *MockACPClient) SessionNew(cwd string) (*SessionNewResult, error) {
	params := SessionNewParams{
		CWD: cwd,
		Capabilities: SessionCapabilities{
			Resume: emptyObj,
			Close:  emptyObj,
			List:   emptyObj,
		},
	}

	resp, err := c.SendRequest("session/new", params, 1)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	var result SessionNewResult
	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SessionPrompt calls session/prompt and collects events
func (c *MockACPClient) SessionPrompt(sessionID, message string) (*SessionPromptResult, error) {
	params := SessionPromptParams{
		SessionID: sessionID,
		Prompt: []ContentBlock{
			{
				Type: "text",
				Text: message,
			},
		},
	}

	resp, err := c.SendRequest("session/prompt", params, 2)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	var result SessionPromptResult
	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SessionCancel sends a session/cancel notification
func (c *MockACPClient) SessionCancel(sessionID string) error {
	params := SessionCancelParams{
		SessionID: sessionID,
	}

	// Notifications don't have an ID
	notif := RPCNotification{
		JSONRPC: "2.0",
		Method:  "session/cancel",
		Params:  params,
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	c.mu.Lock()
	data = append(data, '\n')
	_, err = c.writer.Write(data)
	c.mu.Unlock()

	return err
}

// SessionClose calls session/close
func (c *MockACPClient) SessionClose(sessionID string) (*SessionCloseResult, error) {
	params := SessionCloseParams{
		SessionID: sessionID,
	}

	resp, err := c.SendRequest("session/close", params, 3)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	var result SessionCloseResult
	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetEvents returns all collected events
func (c *MockACPClient) GetEvents() []SessionUpdateEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]SessionUpdateEvent{}, c.events...)
}

// GetNotifications returns all collected notifications
func (c *MockACPClient) GetNotifications() []RPCNotification {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]RPCNotification{}, c.notifications...)
}
