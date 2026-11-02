package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// Transport handles stdio-based JSON-RPC 2.0 communication
type Transport struct {
	reader *bufio.Reader
	writer io.Writer
	mu     sync.Mutex
}

// NewTransport creates a new stdio transport
func NewTransport(reader io.Reader, writer io.Writer) *Transport {
	return &Transport{
		reader: bufio.NewReader(reader),
		writer: writer,
	}
}

// ReadRequest reads and parses a JSON-RPC request from stdin
// It can also handle responses from the client
func (t *Transport) ReadRequest() (*RPCRequest, error) {
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("read request: %w", err)
	}

	// Try to parse as response first (responses have result or error, but no method)
	var resp RPCResponse
	if err := json.Unmarshal(line, &resp); err == nil && resp.JSONRPC == "2.0" && resp.ID != nil {
		// Check if this is actually a response (has result OR error, but no method)
		// We need to check for method field to distinguish from requests
		var raw map[string]interface{}
		if err := json.Unmarshal(line, &raw); err == nil {
			_, hasMethod := raw["method"]
			hasResult := raw["result"] != nil || resp.Result != nil
			hasError := raw["error"] != nil || resp.Error != nil

			// It's a response if it has result/error and no method
			if !hasMethod && (hasResult || hasError) {
				t.HandleResponse(&resp)
				// Return nil to indicate this was a response, not a request
				return nil, nil
			}
		}
	}

	// Parse as request
	var req RPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		logger.Debugf("Failed to parse JSON-RPC request: %s", string(line))
		return nil, &RPCError{
			Code:    ErrCodeParseError,
			Message: "Parse error",
			Data:    err.Error(),
		}
	}

	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		return nil, &RPCError{
			Code:    ErrCodeInvalidRequest,
			Message: "Invalid Request",
			Data:    "JSONRPC field must be '2.0'",
		}
	}

	logger.Debugf("ACP: Received request: method=%s, id=%v", req.Method, req.ID)
	return &req, nil
}

// SendResponse sends a JSON-RPC response to stdout
func (t *Transport) SendResponse(resp *RPCResponse) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	data = append(data, '\n')
	if _, err := t.writer.Write(data); err != nil {
		return fmt.Errorf("write response: %w", err)
	}

	logger.Debugf("ACP: Sent response: id=%v, hasResult=%v, hasError=%v",
		resp.ID, resp.Result != nil, resp.Error != nil)
	return nil
}

// SendNotification sends a JSON-RPC notification to stdout
func (t *Transport) SendNotification(method string, params interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	notif := RPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	data = append(data, '\n')
	if _, err := t.writer.Write(data); err != nil {
		return fmt.Errorf("write notification: %w", err)
	}

	logger.Debugf("ACP: Sent notification: method=%s", method)
	return nil
}

// SendErrorResponse sends an error response
func (t *Transport) SendErrorResponse(id interface{}, code int, message string, data interface{}) error {
	return t.SendResponse(&RPCResponse{
		JSONRPC: "2.0",
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
		ID: id,
	})
}

// SendSuccessResponse sends a success response
func (t *Transport) SendSuccessResponse(id interface{}, result interface{}) error {
	return t.SendResponse(&RPCResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	})
}

// SendRPCRequest sends a request to the client and waits for a response
// This is used for Agent→Client RPC calls (fs/*, terminal/*)
func (t *Transport) SendRPCRequest(method string, params interface{}) (*RPCResponse, error) {
	// Generate unique request ID
	requestID := fmt.Sprintf("req-%d", generateRequestID())

	// Create request
	req := RPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      requestID,
	}

	// Create response channel
	responseChan := make(chan *RPCResponse, 1)
	t.registerResponseHandler(requestID, responseChan)
	defer t.unregisterResponseHandler(requestID)

	// Send request
	t.mu.Lock()
	data, err := json.Marshal(req)
	if err != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	data = append(data, '\n')
	if _, err := t.writer.Write(data); err != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("write request: %w", err)
	}
	t.mu.Unlock()

	logger.Debugf("ACP: Sent RPC request to client: method=%s, id=%v", method, requestID)

	// Wait for response with timeout
	select {
	case resp := <-responseChan:
		if resp.Error != nil {
			return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
		}
		return resp, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("RPC request timeout after 30s")
	}
}

var requestIDCounter uint64
var requestIDMu sync.Mutex

func generateRequestID() uint64 {
	requestIDMu.Lock()
	defer requestIDMu.Unlock()
	requestIDCounter++
	return requestIDCounter
}

// Response handler map and methods
var (
	responseHandlers   = make(map[string]chan *RPCResponse)
	responseHandlersMu sync.RWMutex
)

func (t *Transport) registerResponseHandler(id string, ch chan *RPCResponse) {
	responseHandlersMu.Lock()
	defer responseHandlersMu.Unlock()
	responseHandlers[id] = ch
}

func (t *Transport) unregisterResponseHandler(id string) {
	responseHandlersMu.Lock()
	defer responseHandlersMu.Unlock()
	delete(responseHandlers, id)
}

// HandleResponse handles an incoming RPC response from the client
func (t *Transport) HandleResponse(resp *RPCResponse) {
	if resp.ID == nil {
		logger.Warnf("ACP: Received response with nil ID")
		return
	}

	// Convert ID to string (JSON-RPC 2.0 allows string or number IDs)
	var idStr string
	switch v := resp.ID.(type) {
	case string:
		idStr = v
	case float64:
		idStr = fmt.Sprintf("%.0f", v)
	case int:
		idStr = fmt.Sprintf("%d", v)
	default:
		logger.Warnf("ACP: Received response with unsupported ID type: %T %v", resp.ID, resp.ID)
		return
	}

	responseHandlersMu.RLock()
	ch, ok := responseHandlers[idStr]
	responseHandlersMu.RUnlock()

	if !ok {
		logger.Debugf("ACP: Received response for unknown request ID: %s", idStr)
		return
	}

	select {
	case ch <- resp:
		logger.Debugf("ACP: Delivered response for request ID: %s", idStr)
	default:
		logger.Warnf("ACP: Response channel full for request ID: %s", idStr)
	}
}
