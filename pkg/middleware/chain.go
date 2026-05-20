// Package middleware provides a unified middleware chain framework for tool execution.
// It implements security, resilience, auditing, and metrics middleware layers.
package middleware

import (
	"context"
	"sort"

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

// PrioritizedMiddleware is a middleware that declares its priority in the chain.
// Lower priority values execute first (earlier in the chain).
type PrioritizedMiddleware interface {
	ToolMiddleware
	Priority() int
}

// Chain is an ordered list of ToolMiddleware that wraps tool execution.
type Chain struct {
	middlewares []ToolMiddleware
}

// NewChain creates a new middleware chain with the given middlewares.
// Middlewares are automatically sorted by priority if they implement PrioritizedMiddleware.
func NewChain(middlewares ...ToolMiddleware) *Chain {
	c := &Chain{middlewares: make([]ToolMiddleware, len(middlewares))}
	copy(c.middlewares, middlewares)
	c.sortByPriority()
	return c
}

// Use appends a middleware to the chain and re-sorts by priority.
func (c *Chain) Use(m ToolMiddleware) {
	c.middlewares = append(c.middlewares, m)
	c.sortByPriority()
}

// sortByPriority sorts middlewares by priority.
// Middlewares implementing PrioritizedMiddleware are sorted by their Priority() value.
// Middlewares not implementing PrioritizedMiddleware are treated as having priority 999
// and maintain their relative order at the end of the chain.
func (c *Chain) sortByPriority() {
	sort.SliceStable(c.middlewares, func(i, j int) bool {
		pi := getPriority(c.middlewares[i])
		pj := getPriority(c.middlewares[j])
		return pi < pj
	})
}

// getPriority returns the priority of a middleware.
// If the middleware implements PrioritizedMiddleware, returns its Priority().
// Otherwise, returns 999 (low priority, executes near the end).
func getPriority(m ToolMiddleware) int {
	if pm, ok := m.(PrioritizedMiddleware); ok {
		return pm.Priority()
	}
	return 999 // Default low priority for non-prioritized middleware
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
