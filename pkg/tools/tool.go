// Package tools provides the tool interfaces and implementations for the agent.
package tools

import "github.com/nano-harness/nano-agent/pkg/interfaces"

// ToolCall represents a function call from the LLM
type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolResult is an alias for interfaces.ToolResult (canonical definition).
// The ID, Content, and Error fields used by the LLM wire format are defined
// on interfaces.ToolResult alongside the richer execution metadata.
type ToolResult = interfaces.ToolResult
