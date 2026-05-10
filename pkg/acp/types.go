// Package acp implements the Zed Agent Client Protocol (ACP) server for nano-agent.
// It provides stdio-based JSON-RPC 2.0 communication compatible with ACP clients
// like Zed, JetBrains, and Neovim editors.
package acp

import (
	"fmt"
	"time"
)

// ProtocolVersion is the ACP protocol version we support
const ProtocolVersion = 1

// RPCRequest represents a JSON-RPC 2.0 request
type RPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      interface{} `json:"id,omitempty"` // Can be string, number, or null
}

// RPCResponse represents a JSON-RPC 2.0 response
type RPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
	ID      interface{} `json:"id,omitempty"`
}

// RPCNotification represents a JSON-RPC 2.0 notification (no ID field)
type RPCNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// RPCError represents a JSON-RPC 2.0 error
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error implements the error interface
func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// Standard JSON-RPC 2.0 error codes
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603
)

// ACP-specific error codes
const (
	ErrCodeSessionNotFound  = -32001
	ErrCodePermissionDenied = -32002
	ErrCodeToolNotFound     = -32003
	ErrCodeTimeout          = -32000
)

// SessionNewParams represents parameters for session/new
type SessionNewParams struct {
	CWD          string                 `json:"cwd,omitempty"`
	Env          map[string]string      `json:"env,omitempty"`
	Capabilities SessionCapabilities    `json:"capabilities,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// SessionNewResult represents the result of session/new
type SessionNewResult struct {
	SessionID    string              `json:"sessionId"`
	Capabilities SessionCapabilities `json:"capabilities"`
}

// SessionCapabilities describes what the session supports
type SessionCapabilities struct {
	FS       *FSCapabilities       `json:"fs,omitempty"`
	Terminal *TerminalCapabilities `json:"terminal,omitempty"`
	Tools    []string              `json:"tools,omitempty"`
	Swarm    bool                  `json:"swarm,omitempty"`
	Resume   bool                  `json:"resume,omitempty"`
	Close    bool                  `json:"close,omitempty"`
	List     bool                  `json:"list,omitempty"`
}

// FSCapabilities describes filesystem capabilities
type FSCapabilities struct {
	Read   bool `json:"read"`
	Write  bool `json:"write"`
	List   bool `json:"list,omitempty"`
	Delete bool `json:"delete,omitempty"`
}

// TerminalCapabilities describes terminal capabilities
type TerminalCapabilities struct {
	Run    bool `json:"run"`
	Input  bool `json:"input,omitempty"`
	Output bool `json:"output,omitempty"`
	Kill   bool `json:"kill,omitempty"`
}

// SessionUpdateParams represents parameters for session/update
type SessionUpdateParams struct {
	SessionID string         `json:"sessionId"`
	Message   MessageContent `json:"message"`
}

// MessageContent represents a message in the conversation
type MessageContent struct {
	Role    string                   `json:"role"`
	Content string                   `json:"content"`
	Images  []map[string]interface{} `json:"images,omitempty"`
}

// SessionUpdateEvent represents an event pushed during session/update
type SessionUpdateEvent struct {
	Type     string                 `json:"type"`
	Content  string                 `json:"content,omitempty"`
	Tool     string                 `json:"tool,omitempty"`
	Args     map[string]interface{} `json:"args,omitempty"`
	Result   interface{}            `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// SessionCloseParams represents parameters for session/close
type SessionCloseParams struct {
	SessionID string `json:"sessionId"`
}

// SessionCloseResult represents the result of session/close
type SessionCloseResult struct {
	Success bool                   `json:"success"`
	Stats   map[string]interface{} `json:"stats,omitempty"`
}

// PermissionRequest represents a permission request
type PermissionRequest struct {
	Type   string                 `json:"type"`
	Tool   string                 `json:"tool"`
	Args   map[string]interface{} `json:"args"`
	Reason string                 `json:"reason"`
}

// PermissionResult represents the result of a permission request
type PermissionResult struct {
	Approved bool   `json:"approved"`
	Message  string `json:"message,omitempty"`
}

// ACPSession represents an active ACP session
type ACPSession struct {
	ACPSessionID  string
	NanoSessionID string
	CWD           string
	Env           map[string]string
	ClientCaps    SessionCapabilities
	FSMode        FSMode
	CreatedAt     time.Time
	LastActiveAt  time.Time
}

// FSMode determines how filesystem operations are handled
type FSMode string

const (
	FSModeACP   FSMode = "acp"   // Use ACP filesystem RPCs
	FSModeLocal FSMode = "local" // Use local filesystem
	FSModeAuto  FSMode = "auto"  // Try ACP first, fallback to local
)
