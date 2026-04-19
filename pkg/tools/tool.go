// Package tools provides the tool interfaces and implementations for the agent.
package tools

// ToolCall represents a function call from the LLM
type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}
