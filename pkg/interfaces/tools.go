// Package interfaces defines the interfaces for the agent's tools and capabilities.
package interfaces

import (
	"context"

	"github.com/nano-harness/nano-agent/pkg/policy"
)

// Tool represents a function or capability that can be executed by the agent
type Tool interface {
	// Name returns the unique identifier for this tool
	Name() string

	// Description returns a human-readable description of what this tool does
	Description() string

	// Schema returns the JSON schema for the tool's parameters
	Schema() *ToolSchema

	// Execute runs the tool with the provided parameters
	Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error)

	// RequiresConfirmation returns true if this tool requires user confirmation before execution
	RequiresConfirmation() bool

	// Category returns the category this tool belongs to
	Category() ToolCategory

	// ConcurrencySafe returns true if this tool can be executed concurrently with other
	// concurrency-safe tools without risk of data races or conflicting side effects.
	// Tools that mutate shared state (e.g. file writes, shell commands that modify the
	// filesystem) must return false so the scheduler serialises them.
	ConcurrencySafe() bool
}

// ToolGate decides whether a tool should be exposed to the LLM schema.
type ToolGate interface {
	ShouldExpose(toolName string) bool
}

// ContextualConfirmationTool represents a tool that can dynamically determine
// whether confirmation is required based on the specific parameters being used
type ContextualConfirmationTool interface {
	Tool
	// RequiresConfirmationForParams checks if confirmation is required for specific parameters
	RequiresConfirmationForParams(params map[string]interface{}) bool
}

// SecurityAnalyzableTool represents a tool that can perform security analysis
// on its parameters before execution. This enables the tool_scheduler to
// perform a single security check and propagate the result downstream,
// avoiding redundant analysis in middleware and tool execution layers.
type SecurityAnalyzableTool interface {
	Tool
	// AnalyzeSecurityDecision examines the parameters and returns the full
	// security decision including any hook-proposed parameter modifications.
	// The provided context allows callers to propagate cancellation, deadlines,
	// and request-scoped values into the security analysis.
	AnalyzeSecurityDecision(ctx context.Context, params map[string]interface{}) (*policy.PermissionDecision, error)
}

// ToolCategory represents different categories of tools
type ToolCategory string

const (
	CategoryFileSystem  ToolCategory = "filesystem" //nolint:revive
	CategoryShell       ToolCategory = "shell"
	CategoryWeb         ToolCategory = "web"
	CategoryGit         ToolCategory = "git"
	CategoryBuild       ToolCategory = "build"
	CategoryTest        ToolCategory = "test"
	CategoryLint        ToolCategory = "lint"
	CategoryFormat      ToolCategory = "format"
	CategorySearch      ToolCategory = "search"
	CategoryMemory      ToolCategory = "memory"
	CategoryDebug       ToolCategory = "debug"
	CategoryDocker      ToolCategory = "docker"
	CategoryKubernetes  ToolCategory = "kubernetes"
	CategoryMCP         ToolCategory = "mcp"
	CategoryDiagnostics ToolCategory = "diagnostics"
	CategoryAgent       ToolCategory = "agent"
	CategoryDevelopment ToolCategory = "development"
	CategoryOpenSpec    ToolCategory = "openspec" //nolint:revive
)

// ToolSchema defines the structure of tool parameters
type ToolSchema struct {
	Type        string                     `json:"type"`
	Properties  map[string]*PropertySchema `json:"properties"`
	Required    []string                   `json:"required"`
	Description string                     `json:"description"`
}

// PropertySchema defines individual parameter schemas
type PropertySchema struct {
	Type        string          `json:"type"`
	Description string          `json:"description"`
	Items       *PropertySchema `json:"items,omitempty"`
	Default     interface{}     `json:"default,omitempty"`
	Enum        []string        `json:"enum,omitempty"`
	Pattern     string          `json:"pattern,omitempty"`
	MinLength   *int            `json:"minLength,omitempty"`
	MaxLength   *int            `json:"maxLength,omitempty"`
	Minimum     *float64        `json:"minimum,omitempty"`
	Maximum     *float64        `json:"maximum,omitempty"`
	Examples    []string        `json:"examples,omitempty"` // Realistic example values
	Usage       string          `json:"usage,omitempty"`    // Usage tips and recommendations
}

// ToolResult represents the result of a tool execution.
// This is the canonical definition; pkg/tools.ToolResult is an alias.
type ToolResult struct {
	// ID is the tool_use ID that correlates this result to the original call.
	ID string `json:"id,omitempty"`
	// Content is the raw string content for LLM message passing (wire format).
	Content string `json:"content,omitempty"`

	Success     bool                   `json:"success"`
	Data        interface{}            `json:"data,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	LLMContent  string                 `json:"llm_content"`  // Content formatted for LLM consumption
	UserContent string                 `json:"user_content"` // Content formatted for user display
}

// ToolRegistry manages the collection of available tools
type ToolRegistry interface {
	// Register adds a tool to the registry
	Register(tool Tool) error

	// Unregister removes a tool from the registry
	Unregister(name string) error

	// Get retrieves a tool by name
	Get(name string) (Tool, bool)

	// List returns all registered tools
	List() []Tool

	// ListByCategory returns tools in a specific category
	ListByCategory(category ToolCategory) []Tool

	// Schemas returns all tool schemas for LLM consumption
	Schemas() map[string]*ToolSchema

	// Execute runs a tool with given parameters
	Execute(ctx context.Context, name string, params map[string]interface{}) (*ToolResult, error)
}

// ValidationError represents a parameter validation error
type ValidationError struct {
	Parameter string      `json:"parameter"`
	Message   string      `json:"message"`
	Value     interface{} `json:"value"`
}

func (e *ValidationError) Error() string {
	return e.Message
}

// NewStringProperty creates a new string property schema
func NewStringProperty(description string) *PropertySchema {
	return &PropertySchema{
		Type:        "string",
		Description: description,
	}
}

func NewStringPropertyWithEnum(description string, enum []string) *PropertySchema { //nolint:revive
	return &PropertySchema{
		Type:        "string",
		Description: description,
		Enum:        enum,
	}
}

func NewNumberProperty(description string) *PropertySchema { //nolint:revive
	return &PropertySchema{
		Type:        "number",
		Description: description,
	}
}

func NewBooleanProperty(description string) *PropertySchema { //nolint:revive
	return &PropertySchema{
		Type:        "boolean",
		Description: description,
	}
}

func NewArrayProperty(description string, itemType string) *PropertySchema { //nolint:revive
	if itemType == "" {
		itemType = "string"
	}
	return &PropertySchema{
		Type:        "array",
		Description: description,
		Items:       &PropertySchema{Type: itemType},
	}
}

// CreateSchema creates a new tool schema
func CreateSchema(description string, properties map[string]*PropertySchema, required []string) *ToolSchema {
	return &ToolSchema{
		Type:        "object",
		Description: description,
		Properties:  properties,
		Required:    required,
	}
}
