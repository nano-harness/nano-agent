package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"

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
func (t *Transport) ReadRequest() (*RPCRequest, error) {
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("read request: %w", err)
	}

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
