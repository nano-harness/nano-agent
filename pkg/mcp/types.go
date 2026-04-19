package mcp

import (
	"context"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// MCPConfig holds configuration for MCP client functionality
type MCPConfig struct { //nolint:revive
	// Client configuration
	EnableClient bool              `json:"enable_client"`
	MCPServers   []MCPServerConfig `json:"servers"`

	// Transport configuration
	DefaultTransport string        `json:"default_transport"`
	Timeout          time.Duration `json:"timeout"`
	MaxRetries       int           `json:"max_retries"`

	// Health check configuration
	EnableHealthCheck   bool          `json:"enable_health_check"`
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	HealthCheckTimeout  time.Duration `json:"health_check_timeout"`

	// Security and authentication
	EnableAuth bool              `json:"enable_auth"`
	AuthTokens map[string]string `json:"auth_tokens"`
	TLSConfig  *TLSConfig        `json:"tls"`
}

// MCPServerConfig defines configuration for connecting to an MCP server
type MCPServerConfig struct { //nolint:revive
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Command     []string          `json:"command"`
	URL         string            `json:"url"`
	Transport   string            `json:"transport"`
	Headers     map[string]string `json:"headers"`
	Enabled     bool              `json:"enabled"`
	Timeout     time.Duration     `json:"timeout"`
}

// TLSConfig holds TLS configuration
type TLSConfig struct {
	Enabled    bool   `json:"enabled"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
	CAFile     string `json:"ca_file"`
	SkipVerify bool   `json:"skip_verify"`
}

// MCPToolAdapter adapts nano tools to MCP tools
type MCPToolAdapter struct { //nolint:revive
	tool interfaces.Tool //nolint:unused
}

// MCPResourceAdapter adapts data sources to MCP resources
type MCPResourceAdapter struct { //nolint:revive
	name        string          //nolint:unused
	description string          //nolint:unused
	uri         string          //nolint:unused
	handler     ResourceHandler //nolint:unused
}

// ResourceHandler defines the function signature for resource handlers
type ResourceHandler func(ctx context.Context, uri string, params map[string]interface{}) (*ResourceResult, error)

// ResourceResult represents the result of reading a resource
type ResourceResult struct {
	Content      []Content              `json:"content"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	URI          string                 `json:"uri"`
	MimeType     string                 `json:"mime_type,omitempty"`
	LastModified *time.Time             `json:"last_modified,omitempty"`
}

// Content represents MCP content types
type Content interface {
	ContentType() string
}

// TextContent represents textual content
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (tc *TextContent) ContentType() string { //nolint:revive
	return "text"
}

// ImageContent represents image content
type ImageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

func (ic *ImageContent) ContentType() string { //nolint:revive
	return "image"
}

// MCPPromptAdapter adapts templates to MCP prompts
type MCPPromptAdapter struct { //nolint:revive
	name        string           //nolint:unused
	description string           //nolint:unused
	template    string           //nolint:unused
	arguments   []PromptArgument //nolint:unused
	handler     PromptHandler    //nolint:unused
}

// PromptArgument defines prompt template arguments
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// PromptHandler defines the function signature for prompt handlers
type PromptHandler func(ctx context.Context, name string, arguments map[string]string) (*PromptResult, error)

// PromptResult represents the result of getting a prompt
type PromptResult struct {
	Description string                 `json:"description,omitempty"`
	Messages    []PromptMessage        `json:"messages"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// PromptMessage represents a message in a prompt
type PromptMessage struct {
	Role    string    `json:"role"`
	Content []Content `json:"content"`
}

// TransportType defines supported MCP transport types
type TransportType string

const (
	TransportSTDIO      TransportType = "stdio" //nolint:revive
	TransportHTTP       TransportType = "http"
	TransportWebSocket  TransportType = "websocket"
	TransportStreamable TransportType = "streamable"
	TransportInMemory   TransportType = "inmemory"
)

// MCPSessionInfo holds information about an active MCP session (client-side)
type MCPSessionInfo struct { //nolint:revive
	SessionID     string                 `json:"session_id"`
	ServerName    string                 `json:"server_name"`
	ClientName    string                 `json:"client_name"`
	Transport     TransportType          `json:"transport"`
	StartTime     time.Time              `json:"start_time"`
	LastActivity  time.Time              `json:"last_activity"`
	Status        string                 `json:"status"`
	ToolsUsed     []string               `json:"tools_used"`
	ResourcesRead []string               `json:"resources_read"`
	Metadata      map[string]interface{} `json:"metadata"`
}
