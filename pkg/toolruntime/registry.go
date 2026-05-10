// Package toolruntime provides behavior-preserving tool registry and execution
// boundaries used by higher-level agent orchestration.
package toolruntime

import (
	"context"
	"fmt"
	"sync"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// Registry is the default implementation of interfaces.ToolRegistry.
// It only owns registration, lookup, enumeration, and direct execution.
type Registry struct {
	tools map[string]interfaces.Tool
	mutex sync.RWMutex
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]interfaces.Tool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(tool interfaces.Tool) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	name := tool.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool '%s' is already registered", name)
	}

	r.tools[name] = tool
	return nil
}

// Unregister removes a tool from the registry.
func (r *Registry) Unregister(name string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.tools[name]; !exists {
		return fmt.Errorf("tool '%s' is not registered", name)
	}

	delete(r.tools, name)
	return nil
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (interfaces.Tool, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	tool, exists := r.tools[name]
	return tool, exists
}

// List returns all registered tools.
func (r *Registry) List() []interfaces.Tool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	tools := make([]interfaces.Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

// ListByCategory returns tools in a specific category.
func (r *Registry) ListByCategory(category interfaces.ToolCategory) []interfaces.Tool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var tools []interfaces.Tool
	for _, tool := range r.tools {
		if tool.Category() == category {
			tools = append(tools, tool)
		}
	}
	return tools
}

// Schemas returns all tool schemas for LLM consumption.
func (r *Registry) Schemas() map[string]*interfaces.ToolSchema {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	schemas := make(map[string]*interfaces.ToolSchema)
	for name, tool := range r.tools {
		schemas[name] = tool.Schema()
	}
	return schemas
}

// Execute runs a tool with given parameters directly.
func (r *Registry) Execute(ctx context.Context, name string, params map[string]interface{}) (*interfaces.ToolResult, error) {
	tool, exists := r.Get(name)
	if !exists {
		return nil, fmt.Errorf("tool '%s' not found", name)
	}

	return tool.Execute(ctx, params)
}
