package toolruntime

import (
	"context"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/middleware"
)

// ResultNormalizer standardizes tool execution results after the concrete tool
// and middleware chain finish.
type ResultNormalizer interface {
	Normalize(toolName string, result *interfaces.ToolResult, err error) (*interfaces.ToolResult, error)
}

// PassthroughNormalizer preserves existing tool result behavior unchanged.
type PassthroughNormalizer struct{}

// Normalize returns the original result and error unchanged.
func (PassthroughNormalizer) Normalize(_ string, result *interfaces.ToolResult, err error) (*interfaces.ToolResult, error) {
	return result, err
}

// Runtime executes tools through optional middleware and result normalization.
type Runtime struct {
	registry   interfaces.ToolRegistry
	chain      *middleware.Chain
	normalizer ResultNormalizer
}

// NewRuntime creates a ToolRuntime over an existing registry.
func NewRuntime(registry interfaces.ToolRegistry, chain *middleware.Chain, normalizer ResultNormalizer) *Runtime {
	if normalizer == nil {
		normalizer = PassthroughNormalizer{}
	}
	return &Runtime{
		registry:   registry,
		chain:      chain,
		normalizer: normalizer,
	}
}

// SetMiddlewareChain replaces the middleware chain used for execution.
func (r *Runtime) SetMiddlewareChain(chain *middleware.Chain) {
	r.chain = chain
}

// Execute looks up a tool, routes it through middleware, and normalizes the
// returned result without changing existing semantics.
func (r *Runtime) Execute(ctx context.Context, name string, params map[string]interface{}) (*interfaces.ToolResult, error) {
	tool, ok := r.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool '%s' not found", name)
	}

	directExec := func(ctx context.Context, tool interfaces.Tool, params map[string]interface{}) (*interfaces.ToolResult, error) {
		return tool.Execute(ctx, params)
	}

	var (
		result *interfaces.ToolResult
		err    error
	)
	if r.chain != nil {
		result, err = r.chain.Execute(ctx, tool, params, directExec)
	} else {
		result, err = directExec(ctx, tool, params)
	}

	return r.normalizer.Normalize(name, result, err)
}
