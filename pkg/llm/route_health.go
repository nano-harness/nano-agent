package llm

import (
	"strings"
	"sync"
)

var sharedCircuitBreakers = struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
}{
	breakers: make(map[string]*CircuitBreaker),
}

// RouteHealth stores one circuit breaker per resolved route.
type RouteHealth struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	config   CircuitBreakerConfig
}

// NewRouteHealth creates an empty per-route health registry.
func NewRouteHealth(cfg CircuitBreakerConfig) *RouteHealth {
	return &RouteHealth{breakers: make(map[string]*CircuitBreaker), config: cfg}
}

// BreakerFor returns the route's circuit breaker, creating it on first use.
func (rh *RouteHealth) BreakerFor(routeName string) *CircuitBreaker {
	if rh == nil {
		return NewCircuitBreaker(DefaultCircuitBreakerConfig())
	}
	rh.mu.RLock()
	breaker := rh.breakers[routeName]
	rh.mu.RUnlock()
	if breaker != nil {
		return breaker
	}
	rh.mu.Lock()
	defer rh.mu.Unlock()
	if breaker = rh.breakers[routeName]; breaker != nil {
		return breaker
	}
	breaker = NewCircuitBreaker(rh.config)
	rh.breakers[routeName] = breaker
	return breaker
}

// BreakerForRoute returns the shared circuit breaker for a provider endpoint.
func (rh *RouteHealth) BreakerForRoute(route ResolvedRoute) *CircuitBreaker {
	if rh == nil {
		return getOrCreateCircuitBreaker(route.ProviderID, route.BaseURL, DefaultCircuitBreakerConfig())
	}
	rh.mu.RLock()
	breaker := rh.breakers[route.Name]
	rh.mu.RUnlock()
	if breaker != nil {
		return breaker
	}
	breaker = getOrCreateCircuitBreaker(route.ProviderID, route.BaseURL, rh.config)
	rh.mu.Lock()
	defer rh.mu.Unlock()
	if existing := rh.breakers[route.Name]; existing != nil {
		return existing
	}
	rh.breakers[route.Name] = breaker
	return breaker
}

// Snapshot returns breaker statistics keyed by route name.
func (rh *RouteHealth) Snapshot() map[string]map[string]any {
	if rh == nil {
		return nil
	}
	rh.mu.RLock()
	defer rh.mu.RUnlock()
	out := make(map[string]map[string]any, len(rh.breakers))
	for name, breaker := range rh.breakers {
		stats := breaker.Stats()
		converted := make(map[string]any, len(stats))
		for key, value := range stats {
			converted[key] = value
		}
		out[name] = converted
	}
	return out
}

func getOrCreateCircuitBreaker(providerID, baseURL string, cfg CircuitBreakerConfig) *CircuitBreaker {
	key := circuitBreakerRegistryKey(providerID, baseURL)
	sharedCircuitBreakers.mu.RLock()
	breaker := sharedCircuitBreakers.breakers[key]
	sharedCircuitBreakers.mu.RUnlock()
	if breaker != nil {
		return breaker
	}
	sharedCircuitBreakers.mu.Lock()
	defer sharedCircuitBreakers.mu.Unlock()
	if breaker = sharedCircuitBreakers.breakers[key]; breaker != nil {
		return breaker
	}
	breaker = NewCircuitBreaker(cfg)
	sharedCircuitBreakers.breakers[key] = breaker
	return breaker
}

func circuitBreakerRegistryKey(providerID, baseURL string) string {
	normalizedBaseURL := NewProviderInfo(baseURL).BaseURL
	normalizedProvider := strings.ToLower(strings.TrimSpace(providerID))
	if normalizedProvider == "" {
		normalizedProvider = InferProviderID(normalizedBaseURL, "")
	}
	return normalizedProvider + "|" + normalizedBaseURL
}

func resetCircuitBreakerRegistryForTest() {
	sharedCircuitBreakers.mu.Lock()
	defer sharedCircuitBreakers.mu.Unlock()
	sharedCircuitBreakers.breakers = make(map[string]*CircuitBreaker)
}
