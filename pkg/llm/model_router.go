package llm

import (
	"strings"
	"sync"
	"time"
)

// Route describes a concrete model endpoint selected by ModelRouter.
type Route struct {
	Name    string
	Model   string
	BaseURL string
	APIKey  string
	Profile ModelProfile
}

// RouteConfig configures a model endpoint.
type RouteConfig struct {
	Name    string
	Model   string
	BaseURL string
	APIKey  string
}

// RouteMetrics captures observable routing outcomes.
type RouteMetrics struct {
	Requests       int
	Successes      int
	Failures       int
	Fallbacks      int
	InputTokens    int
	OutputTokens   int
	TotalCostUSD   float64
	TotalLatency   time.Duration
	LastLatency    time.Duration
	LastError      string
	LastSelectedAt time.Time
}

// TokenUsage is a normalized token/cost summary for a routed request.
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
}

// ModelRouter selects the primary model route and tracks fallback/usage metrics.
//
// It is intentionally independent from OpenAI client construction so existing
// OpenAI-compatible behavior remains unchanged while routing support is adopted
// incrementally by callers.
type ModelRouter struct {
	mu       sync.RWMutex
	primary  Route
	fallback []Route
	metrics  map[string]RouteMetrics
}

// NewModelRouter creates a router from primary and fallback route configs.
func NewModelRouter(primary RouteConfig, fallback ...RouteConfig) *ModelRouter {
	r := &ModelRouter{
		primary: routeFromConfig(primary),
		metrics: make(map[string]RouteMetrics),
	}
	for _, cfg := range fallback {
		r.fallback = append(r.fallback, routeFromConfig(cfg))
	}
	return r
}

// Select returns the primary route, preserving default OpenAI-compatible behavior.
func (r *ModelRouter) Select() Route {
	if r == nil {
		return routeFromConfig(RouteConfig{})
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markSelectedLocked(r.primary)
	return r.primary
}

// Fallbacks returns configured fallback routes in order.
func (r *ModelRouter) Fallbacks() []Route {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Route(nil), r.fallback...)
}

// SelectFallback returns the fallback route at index and records a fallback event.
func (r *ModelRouter) SelectFallback(index int) (Route, bool) {
	if r == nil {
		return Route{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if index < 0 || index >= len(r.fallback) {
		return Route{}, false
	}
	route := r.fallback[index]
	metrics := r.metrics[routeKey(route)]
	metrics.Fallbacks++
	r.metrics[routeKey(route)] = metrics
	r.markSelectedLocked(route)
	return route, true
}

// RecordResult updates route metrics after a request completes.
func (r *ModelRouter) RecordResult(route Route, usage TokenUsage, latency time.Duration, err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	key := routeKey(route)
	metrics := r.metrics[key]
	metrics.Requests++
	metrics.InputTokens += usage.InputTokens
	metrics.OutputTokens += usage.OutputTokens
	metrics.TotalCostUSD += usage.CostUSD
	metrics.TotalLatency += latency
	metrics.LastLatency = latency
	if err != nil {
		metrics.Failures++
		metrics.LastError = err.Error()
	} else {
		metrics.Successes++
		metrics.LastError = ""
	}
	r.metrics[key] = metrics
}

// Metrics returns a snapshot of metrics keyed by route name/model.
func (r *ModelRouter) Metrics() map[string]RouteMetrics {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]RouteMetrics, len(r.metrics))
	for key, value := range r.metrics {
		out[key] = value
	}
	return out
}

func (r *ModelRouter) markSelectedLocked(route Route) {
	key := routeKey(route)
	metrics := r.metrics[key]
	metrics.LastSelectedAt = time.Now()
	r.metrics[key] = metrics
}

func routeFromConfig(cfg RouteConfig) Route {
	model := strings.TrimSpace(cfg.Model)
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = model
	}
	if name == "" {
		name = "default"
	}
	baseURL := NewProviderInfo(cfg.BaseURL).BaseURL
	return Route{
		Name:    name,
		Model:   model,
		BaseURL: baseURL,
		APIKey:  cfg.APIKey,
		Profile: InferModelProfile(model),
	}
}

func routeKey(route Route) string {
	if route.Name != "" {
		return route.Name
	}
	if route.Model != "" {
		return route.Model
	}
	return "default"
}
