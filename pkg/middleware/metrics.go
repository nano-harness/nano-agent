// Package middleware metrics.go provides a lightweight metrics middleware that
// replaces pkg/diagnostics.  It tracks tool execution latency and error rates
// using in-process counters, with no external dependencies.
package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// ToolMetrics holds aggregated metrics for a single tool.
type ToolMetrics struct {
	Name       string
	Calls      int64
	Errors     int64
	TotalMs    int64 // Cumulative execution time in milliseconds
	PeakMs     int64 // Highest single-call duration
	LastCallAt time.Time
}

// ErrorRate returns the fraction of calls that failed (0.0–1.0).
func (m *ToolMetrics) ErrorRate() float64 {
	if m.Calls == 0 {
		return 0
	}
	return float64(m.Errors) / float64(m.Calls)
}

// AvgMs returns the average call duration in milliseconds.
func (m *ToolMetrics) AvgMs() float64 {
	if m.Calls == 0 {
		return 0
	}
	return float64(m.TotalMs) / float64(m.Calls)
}

// MetricsRegistry stores ToolMetrics for all registered tools.
type MetricsRegistry struct {
	mu    sync.RWMutex
	tools map[string]*ToolMetrics
}

// globalMetrics is the singleton registry used by MetricsMiddleware.
var globalMetrics = &MetricsRegistry{tools: make(map[string]*ToolMetrics)}

// GlobalMetrics returns the global MetricsRegistry.
func GlobalMetrics() *MetricsRegistry {
	return globalMetrics
}

func (r *MetricsRegistry) record(toolName string, durationMs int64, errored bool) {
	r.mu.Lock()
	m, ok := r.tools[toolName]
	if !ok {
		m = &ToolMetrics{Name: toolName}
		r.tools[toolName] = m
	}
	m.Calls++
	m.TotalMs += durationMs
	m.LastCallAt = time.Now()
	if durationMs > m.PeakMs {
		m.PeakMs = durationMs
	}
	if errored {
		m.Errors++
	}
	r.mu.Unlock()
}

// Snapshot returns a copy of all collected metrics.
func (r *MetricsRegistry) Snapshot() []ToolMetrics {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ToolMetrics, 0, len(r.tools))
	for _, m := range r.tools {
		result = append(result, *m)
	}
	return result
}

// MetricsMiddleware records per-tool call counts, latencies, and error rates.
type MetricsMiddleware struct {
	registry *MetricsRegistry
}

// NewMetricsMiddleware creates a MetricsMiddleware using the global registry.
func NewMetricsMiddleware() *MetricsMiddleware {
	return &MetricsMiddleware{registry: globalMetrics}
}

func (m *MetricsMiddleware) Name() string { return "metrics" }

func (m *MetricsMiddleware) Execute(
	ctx context.Context,
	tool interfaces.Tool,
	params map[string]interface{},
	next MiddlewareFunc,
) (*interfaces.ToolResult, error) {
	start := time.Now()
	result, err := next(ctx, tool, params)
	elapsed := time.Since(start).Milliseconds()

	errored := err != nil || (result != nil && !result.Success)
	m.registry.record(tool.Name(), elapsed, errored)
	return result, err
}
