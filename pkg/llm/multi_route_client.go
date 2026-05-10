package llm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

type routeClientFactory func(ResolvedRoute, []interfaces.Tool) LLMClient

// MultiRouteClient implements LLMClient by trying configured routes in order.
type MultiRouteClient struct {
	routes []ResolvedRoute
	health *RouteHealth
	tools  []interfaces.Tool
	gate   interfaces.ToolGate
	cfg    *config.Config

	mu      sync.Mutex
	clients map[string]LLMClient
	factory routeClientFactory
}

// NewMultiRouteClient creates an LLM client with provider/model fallback support.
func NewMultiRouteClient(routes []ResolvedRoute, tools []interfaces.Tool, cfg *config.Config) *MultiRouteClient {
	return newMultiRouteClient(routes, tools, cfg, nil, nil)
}

func newMultiRouteClient(routes []ResolvedRoute, tools []interfaces.Tool, cfg *config.Config, health *RouteHealth, factory routeClientFactory) *MultiRouteClient {
	if health == nil {
		health = NewRouteHealth(circuitBreakerConfigFromConfig(cfg))
	}
	if factory == nil {
		factory = func(route ResolvedRoute, tools []interfaces.Tool) LLMClient {
			return NewClient(route.APIKey, route.BaseURL, route.Model, tools)
		}
	}
	copiedRoutes := append([]ResolvedRoute(nil), routes...)
	return &MultiRouteClient{
		routes:  copiedRoutes,
		health:  health,
		tools:   append([]interfaces.Tool(nil), tools...),
		cfg:     cfg,
		clients: make(map[string]LLMClient),
		factory: factory,
	}
}

// Routes returns a copy of the normalized route list.
func (m *MultiRouteClient) Routes() []ResolvedRoute {
	if m == nil {
		return nil
	}
	return append([]ResolvedRoute(nil), m.routes...)
}

// HealthSnapshot returns per-route circuit breaker stats.
func (m *MultiRouteClient) HealthSnapshot() map[string]map[string]any {
	if m == nil {
		return nil
	}
	return m.health.Snapshot()
}

// StreamCompletion streams using the first healthy route, falling back only on failback-safe errors.
func (m *MultiRouteClient) StreamCompletion(ctx context.Context, messages []Message, onEvent func(event.StreamEvent)) error {
	return m.stream(ctx, messages, onEvent, func(client LLMClient, wrapped func(event.StreamEvent)) error {
		return client.StreamCompletion(ctx, messages, wrapped)
	})
}

// StreamCompletionWithoutReasoning streams without reasoning and applies the same route fallback policy.
func (m *MultiRouteClient) StreamCompletionWithoutReasoning(ctx context.Context, messages []Message, onEvent func(event.StreamEvent)) error {
	return m.stream(ctx, messages, onEvent, func(client LLMClient, wrapped func(event.StreamEvent)) error {
		return client.StreamCompletionWithoutReasoning(ctx, messages, wrapped)
	})
}

// GenerateContent generates content using the same route fallback policy.
func (m *MultiRouteClient) GenerateContent(ctx context.Context, prompt string) (string, error) {
	if m == nil || len(m.routes) == 0 {
		return "", fmt.Errorf("no model routes configured")
	}
	var lastErr error
	for _, route := range m.routes {
		breaker := m.health.BreakerForRoute(route)
		if err := breaker.AllowRequest(); err != nil {
			lastErr = err
			continue
		}
		content, err := m.clientFor(route).GenerateContent(ctx, prompt)
		if err == nil {
			breaker.RecordSuccess()
			return content, nil
		}
		lastErr = err
		if !shouldFallbackRoute(err) {
			return "", err
		}
		breaker.RecordFailure()
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("all model routes exhausted")
}

// UpdateTools updates all cached route clients and future route clients.
func (m *MultiRouteClient) UpdateTools(tools []interfaces.Tool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tools = append([]interfaces.Tool(nil), tools...)
	for _, client := range m.clients {
		client.UpdateTools(tools)
	}
}

// SetToolGate updates the progressive disclosure gate on cached and future clients.
func (m *MultiRouteClient) SetToolGate(gate interfaces.ToolGate) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gate = gate
	for _, client := range m.clients {
		if setter, ok := client.(interface{ SetToolGate(interfaces.ToolGate) }); ok {
			setter.SetToolGate(gate)
		}
	}
}

func (m *MultiRouteClient) stream(ctx context.Context, messages []Message, onEvent func(event.StreamEvent), call func(LLMClient, func(event.StreamEvent)) error) error {
	if m == nil || len(m.routes) == 0 {
		return fmt.Errorf("no model routes configured")
	}
	var lastErr error
	for i, route := range m.routes {
		breaker := m.health.BreakerForRoute(route)
		if err := breaker.AllowRequest(); err != nil {
			lastErr = err
			m.emitFallbackEvent(onEvent, route, m.nextRouteName(i+1), "circuit_open", "circuit_open")
			continue
		}
		m.emitRouteSelected(onEvent, route)
		tracker := &partialStreamTracker{onEvent: onEvent}
		err := call(m.clientFor(route), tracker.handle)
		if err == nil {
			breaker.RecordSuccess()
			return nil
		}
		lastErr = err
		if !shouldFallbackRoute(err) || tracker.partial {
			return err
		}
		breaker.RecordFailure()
		info := NewAPIErrorHandler(nil).AnalyzeError(err, ExtractHTTPStatus(err))
		reason := "unknown"
		category := "unknown"
		if info != nil {
			reason = string(info.Category)
			category = string(info.Category)
		}
		m.emitFallbackEvent(onEvent, route, m.nextRouteName(i+1), reason, category)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("all model routes exhausted")
}

func (m *MultiRouteClient) clientFor(route ResolvedRoute) LLMClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	if client := m.clients[route.Name]; client != nil {
		return client
	}
	client := m.factory(route, append([]interfaces.Tool(nil), m.tools...))
	if setter, ok := client.(interface{ SetToolGate(interfaces.ToolGate) }); ok {
		setter.SetToolGate(m.gate)
	}
	m.clients[route.Name] = client
	return client
}

func (m *MultiRouteClient) nextRouteName(start int) string {
	for i := start; i < len(m.routes); i++ {
		if m.health.BreakerForRoute(m.routes[i]).State() != CircuitOpen {
			return m.routes[i].Name
		}
	}
	return ""
}

func (m *MultiRouteClient) emitRouteSelected(onEvent func(event.StreamEvent), route ResolvedRoute) {
	if onEvent == nil {
		return
	}
	onEvent(event.NewStreamEvent(event.EventTypeRouteSelected, "llm_router").
		WithMetadata("route", route.Name).
		WithMetadata("provider", route.ProviderID).
		WithMetadata("model", route.Model).
		WithMetadata("base_url", route.BaseURL))
}

func (m *MultiRouteClient) emitFallbackEvent(onEvent func(event.StreamEvent), from ResolvedRoute, toRoute, reason, category string) {
	if onEvent == nil {
		return
	}
	onEvent(event.NewStreamEvent(event.EventTypeProviderFallback, "llm_router").
		WithMetadata("from_route", from.Name).
		WithMetadata("to_route", toRoute).
		WithMetadata("reason", reason).
		WithMetadata("error_category", category))
}

type partialStreamTracker struct {
	onEvent func(event.StreamEvent)
	partial bool
}

func (p *partialStreamTracker) handle(ev event.StreamEvent) {
	switch ev.Type {
	case event.EventTypeStreamContent:
		if ev.Content != "" {
			p.partial = true
		}
	case event.EventTypeContent:
		if ev.Content != "" || len(ev.ToolCalls) > 0 {
			p.partial = true
		}
	case event.EventTypeToolCall:
		p.partial = true
	}
	if p.onEvent != nil {
		p.onEvent(ev)
	}
}

func shouldFallbackRoute(err error) bool {
	if err == nil {
		return false
	}
	info := NewAPIErrorHandler(nil).AnalyzeError(err, ExtractHTTPStatus(err))
	return info != nil && info.ShouldFailback
}

func circuitBreakerConfigFromConfig(cfg *config.Config) CircuitBreakerConfig {
	cbCfg := DefaultCircuitBreakerConfig()
	if cfg != nil && cfg.Advanced != nil && cfg.Advanced.CircuitBreaker != nil {
		cbAdv := cfg.Advanced.CircuitBreaker
		if cbAdv.MaxRetries > 0 {
			cbCfg.MaxRetries = cbAdv.MaxRetries
		}
		if cbAdv.BaseDelayMs > 0 {
			cbCfg.BaseDelay = durationMillis(cbAdv.BaseDelayMs)
		}
		if cbAdv.MaxDelayMs > 0 {
			cbCfg.MaxDelay = durationMillis(cbAdv.MaxDelayMs)
		}
		if cbAdv.OpenTimeoutMs > 0 {
			cbCfg.OpenTimeout = durationMillis(cbAdv.OpenTimeoutMs)
		}
		cbCfg.ExcludeNonFailback = cbAdv.ExcludeNonFailback
	}
	return cbCfg
}

func durationMillis(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}
