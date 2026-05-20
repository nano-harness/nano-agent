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
	ErrCodeSessionNotFound = -32001
)

// SessionNewParams represents parameters for session/new
type SessionNewParams struct {
	CWD          string                 `json:"cwd,omitempty"`
	Env          map[string]string      `json:"env,omitempty"`
	Capabilities SessionCapabilities    `json:"capabilities,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	MCPServers   []MCPServerConfig      `json:"mcpServers,omitempty"`
}

// MCPServerConfig represents MCP server configuration
type MCPServerConfig struct {
	Name    string                 `json:"name"`
	Command string                 `json:"command,omitempty"`
	Args    []string               `json:"args,omitempty"`
	Env     []EnvVariable          `json:"env,omitempty"`
	Type    string                 `json:"type,omitempty"`
	URL     string                 `json:"url,omitempty"`
	Headers []HTTPHeader           `json:"headers,omitempty"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

// EnvVariable represents an environment variable
type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HTTPHeader represents an HTTP header
type HTTPHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// SessionNewResult represents the result of session/new
type SessionNewResult struct {
	SessionID string `json:"sessionId"`
}

// SessionCapabilities describes what the session supports
type SessionCapabilities struct {
	Resume *struct{} `json:"resume,omitempty"`
	Close  *struct{} `json:"close,omitempty"`
	List   *struct{} `json:"list,omitempty"`
}

// emptyObj is used for capability fields that should serialize as {}
var emptyObj = &struct{}{}

// FSCapabilities describes filesystem capabilities
type FSCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

// TerminalCapabilities describes terminal capabilities
type TerminalCapabilities struct {
	Create      bool `json:"create"`
	Output      bool `json:"output"`
	Release     bool `json:"release"`
	WaitForExit bool `json:"waitForExit"`
	Kill        bool `json:"kill"`
}

// SessionPromptParams represents parameters for session/prompt
type SessionPromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// SessionPromptResult represents the result of session/prompt
type SessionPromptResult struct {
	StopReason string `json:"stopReason"` // "end_turn", "max_tokens", "max_turn_requests", "refusal", "cancelled"
}

// ContentBlock represents a content block in a prompt
type ContentBlock struct {
	Type string `json:"type"` // "text", "image", "audio", "resource", "resource_link"
	Text string `json:"text,omitempty"`
	// Image fields (for image type)
	Source *ContentSource `json:"source,omitempty"`
	// Image/Audio fields (for inline content)
	Data     string `json:"data,omitempty"`     // Base64 encoded data
	MimeType string `json:"mimeType,omitempty"` // MIME type
	// Resource fields (embedded content, requires embeddedContext)
	Resource *ResourceContent `json:"resource,omitempty"`
	// Resource link fields (URI reference, baseline capability)
	URI         string `json:"uri,omitempty"`
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Size        int64  `json:"size,omitempty"`
	// Common fields
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Annotations map[string]interface{} `json:"annotations,omitempty"`
}

// ResourceContent represents embedded resource content (ACP resource type)
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"` // For text resources
	Blob     string `json:"blob,omitempty"` // For binary resources (Base64)
}

// ContentSource represents an image source
type ContentSource struct {
	Type      string `json:"type"` // "base64", "url"
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// SessionCancelParams represents parameters for session/cancel notification
type SessionCancelParams struct {
	SessionID string `json:"sessionId"`
}

// SessionUpdateEvent represents an event pushed via session/update notification
// Changed to map[string]interface{} to avoid JSON tag conflicts between different update types
type SessionUpdateEvent = map[string]interface{}

// ToolCallUpdate represents a tool call status update
type ToolCallUpdate struct {
	ToolCallID string                 `json:"toolCallId"`
	Title      string                 `json:"title,omitempty"`
	Kind       string                 `json:"kind,omitempty"` // ToolKind enum
	Status     string                 `json:"status"`         // ToolCallStatus enum
	Content    []ContentBlock         `json:"content,omitempty"`
	Locations  []Location             `json:"locations,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Location represents a code location
type Location struct {
	Path  string `json:"path"`
	Line  int    `json:"line,omitempty"`
	Range *Range `json:"range,omitempty"`
}

// Range represents a code range
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Position represents a position in code
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
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

// PermissionRequestParams represents parameters for session/request_permission (Agent→Client)
type PermissionRequestParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  *ToolCallUpdate    `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// PermissionOption represents a permission choice
type PermissionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// PermissionRequestResult represents the result from session/request_permission
type PermissionRequestResult struct {
	Outcome  PermissionOutcome `json:"outcome"`
	OptionID string            `json:"optionId,omitempty"`
}

// PermissionOutcome represents the outcome of a permission request
type PermissionOutcome struct {
	Outcome  string `json:"outcome"` // "selected" or "cancelled"
	OptionID string `json:"optionId,omitempty"`
}

// PermissionRequest represents a permission request (deprecated)
type PermissionRequest struct {
	Type   string                 `json:"type"`
	Tool   string                 `json:"tool"`
	Args   map[string]interface{} `json:"args"`
	Reason string                 `json:"reason"`
}

// PermissionResult represents the result of a permission request (deprecated)
type PermissionResult struct {
	Approved      bool   `json:"approved"`
	ApproveAlways bool   `json:"approveAlways,omitempty"` // K2.2: If true, always approve similar requests
	Message       string `json:"message,omitempty"`
}

// ACPSession represents an active ACP session
type ACPSession struct {
	ACPSessionID  string
	NanoSessionID string
	CWD           string
	Env           map[string]string
	ClientCaps    SessionCapabilities
	ClientInfo    ClientCapabilities // Store full client capabilities
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

// InitializeParams represents parameters for the initialize method
type InitializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
	ClientInfo         ClientInfo         `json:"clientInfo"`
}

// ClientInfo contains information about the client
type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

// ClientCapabilities describes capabilities supported by the client
type ClientCapabilities struct {
	FS       *FSCapabilities `json:"fs,omitempty"`
	Terminal bool            `json:"terminal,omitempty"`
}

// AuthMethod represents an authentication method supported by the agent
type AuthMethod struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Type        string            `json:"type,omitempty"` // "agent" | "terminal"
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

// InitializeResult represents the result of the initialize method
type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AgentInfo         AgentInfo         `json:"agentInfo"`
	AuthMethods       []AuthMethod      `json:"authMethods"`
}

// AgentInfo contains information about the agent
type AgentInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

// AgentCapabilities describes capabilities supported by the agent
type AgentCapabilities struct {
	LoadSession         bool                  `json:"loadSession"`
	PromptCapabilities  PromptCapabilities    `json:"promptCapabilities"`
	MCP                 MCPCapabilities       `json:"mcpCapabilities"`
	SessionCapabilities *SessionCapabilities  `json:"sessionCapabilities,omitempty"`
	FS                  *FSCapabilities       `json:"fs,omitempty"`
	Terminal            *TerminalCapabilities `json:"terminal,omitempty"`
	Tools               []string              `json:"tools,omitempty"`
}

// PromptCapabilities describes prompt-related capabilities
type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// MCPCapabilities describes MCP-related capabilities
type MCPCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

// AvailableCommand represents a slash command advertised to the client
type AvailableCommand struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Input       *CommandInputConfig `json:"input,omitempty"`
}

// CommandInputConfig describes the input configuration for a slash command
type CommandInputConfig struct {
	Hint string `json:"hint,omitempty"`
}
