// Package middleware provides a unified middleware chain framework for tool execution.
// It implements security, resilience, auditing, and metrics middleware layers.
package middleware

import (
	"context"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// MiddlewareFunc is the next handler function in the middleware chain.
type MiddlewareFunc func(ctx context.Context, tool interfaces.Tool, params map[string]interface{}) (*interfaces.ToolResult, error)

// ToolMiddleware defines a middleware component that wraps tool execution.
type ToolMiddleware interface {
	// Name returns a unique identifier for this middleware.
	Name() string
	// Execute runs the middleware logic, then calls next to continue the chain.
	Execute(ctx context.Context, tool interfaces.Tool, params map[string]interface{}, next MiddlewareFunc) (*interfaces.ToolResult, error)
}

// Chain is an ordered list of ToolMiddleware that wraps tool execution.
type Chain struct {
	middlewares []ToolMiddleware
}

// NewChain creates a new middleware chain with the given middlewares.
// Middlewares are executed in order: first added is outermost (first to run).
func NewChain(middlewares ...ToolMiddleware) *Chain {
	return &Chain{middlewares: middlewares}
}

// Use appends a middleware to the chain.
func (c *Chain) Use(m ToolMiddleware) {
	c.middlewares = append(c.middlewares, m)
}

// Execute runs the tool through the middleware chain, then calls the final executor.
func (c *Chain) Execute(ctx context.Context, tool interfaces.Tool, params map[string]interface{}, final MiddlewareFunc) (*interfaces.ToolResult, error) {
	return c.buildChain(0, final)(ctx, tool, params)
}

// buildChain constructs the nested middleware call starting from index i.
func (c *Chain) buildChain(i int, final MiddlewareFunc) MiddlewareFunc {
	if i >= len(c.middlewares) {
		return final
	}
	current := c.middlewares[i]
	next := c.buildChain(i+1, final)
	return func(ctx context.Context, tool interfaces.Tool, params map[string]interface{}) (*interfaces.ToolResult, error) {
		return current.Execute(ctx, tool, params, next)
	}
}
