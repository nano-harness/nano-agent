package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreakerStates(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenTimeout:      100 * time.Millisecond,
		BaseDelay:        10 * time.Millisecond,
		MaxDelay:         1 * time.Second,
		BackoffFactor:    2.0,
	})

	// Initially closed
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed, got %s", cb.State())
	}

	// Should allow requests when closed
	if err := cb.AllowRequest(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Record failures below threshold
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed after 2 failures, got %s", cb.State())
	}

	// Third failure should open the circuit
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open after 3 failures, got %s", cb.State())
	}

	// Should reject requests when open
	err := cb.AllowRequest()
	if err == nil {
		t.Fatal("expected error when circuit is open")
	}
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	// Simulate open timeout passing by setting lastFailureTime in the past
	cb.mu.Lock()
	cb.lastFailureTime = time.Now().Add(-200 * time.Millisecond)
	cb.mu.Unlock()

	// Should allow a probe request (half-open)
	if err := cb.AllowRequest(); err != nil {
		t.Fatalf("expected no error after timeout, got %v", err)
	}
	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open, got %s", cb.State())
	}

	// Second concurrent probe in half-open should be rejected
	if err := cb.AllowRequest(); err == nil {
		t.Fatal("expected error for second probe in half-open")
	}

	// One success in half-open (below threshold of 2)
	cb.RecordSuccess()
	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open after 1 success, got %s", cb.State())
	}

	// Second success should close the circuit
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed after 2 successes, got %s", cb.State())
	}
}

func TestCircuitBreakerRegistrySharesProviderEndpointBreakers(t *testing.T) {
	resetCircuitBreakerRegistryForTest()
	t.Cleanup(resetCircuitBreakerRegistryForTest)

	cfg := DefaultCircuitBreakerConfig()
	firstHealth := NewRouteHealth(cfg)
	secondHealth := NewRouteHealth(cfg)
	routeA := ResolvedRoute{Name: "primary", ProviderID: "openai", Model: "gpt-4.1", BaseURL: "https://api.openai.com/v1"}
	routeB := ResolvedRoute{Name: "fallback-1", ProviderID: "openai", Model: "gpt-4o", BaseURL: "https://api.openai.com/v1"}

	first := firstHealth.BreakerForRoute(routeA)
	second := secondHealth.BreakerForRoute(routeB)
	if first != second {
		t.Fatal("expected provider/baseURL routes to share a circuit breaker")
	}
	first.RecordFailure()
	if second.Stats()["total_failures"] != int64(1) {
		t.Fatalf("shared breaker did not observe failure: %#v", second.Stats())
	}
}

func TestCircuitBreakerHalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenTimeout:      50 * time.Millisecond,
	})

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open, got %s", cb.State())
	}

	// Simulate timeout passing by setting lastFailureTime in the past
	cb.mu.Lock()
	cb.lastFailureTime = time.Now().Add(-100 * time.Millisecond)
	cb.mu.Unlock()
	if err := cb.AllowRequest(); err != nil {
		t.Fatal(err)
	}
	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open, got %s", cb.State())
	}

	// First failure in half-open should stay half-open (gradual recovery)
	cb.RecordFailure()
	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open after first probe failure, got %s", cb.State())
	}

	// Allow another probe request
	if err := cb.AllowRequest(); err != nil {
		t.Fatal(err)
	}

	// Second consecutive failure in half-open should reopen
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open after 2 consecutive failures in half-open, got %s", cb.State())
	}
}

func TestCircuitBreakerSuccessResetsFailCount(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
	})

	// Two failures then a success
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()

	// Two more failures - should not open because success reset the count
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed, success should have reset counter, got %s", cb.State())
	}
}

func TestCircuitBreakerStats(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	cb.RecordSuccess()
	cb.RecordFailure()
	cb.RecordRetry()

	stats := cb.Stats()
	if stats["total_requests"].(int64) != 2 {
		t.Fatalf("expected 2 total requests, got %v", stats["total_requests"])
	}
	if stats["total_failures"].(int64) != 1 {
		t.Fatalf("expected 1 failure, got %v", stats["total_failures"])
	}
	if stats["total_retries"].(int64) != 1 {
		t.Fatalf("expected 1 retry, got %v", stats["total_retries"])
	}
}

func TestCircuitBreakerRetryDelay(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		BaseDelay:     1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		JitterFactor:  0, // Disable jitter for deterministic test
	})

	// attempt 0: base * 2^0 = 1s
	d0 := cb.CalculateRetryDelay(0, 500)
	if d0 != 1*time.Second {
		t.Fatalf("expected 1s, got %v", d0)
	}

	// attempt 1: base * 2^1 = 2s
	d1 := cb.CalculateRetryDelay(1, 500)
	if d1 != 2*time.Second {
		t.Fatalf("expected 2s, got %v", d1)
	}

	// attempt 2: base * 2^2 = 4s
	d2 := cb.CalculateRetryDelay(2, 500)
	if d2 != 4*time.Second {
		t.Fatalf("expected 4s, got %v", d2)
	}

	// 429 errors get double delay: attempt 0 = 1s * 2 = 2s
	d429 := cb.CalculateRetryDelay(0, 429)
	if d429 != 2*time.Second {
		t.Fatalf("expected 2s for 429, got %v", d429)
	}

	// Server errors (500-504) get base exponential delay without 2x multiplier
	d500 := cb.CalculateRetryDelay(0, 500)
	if d500 != 1*time.Second {
		t.Fatalf("expected 1s for 500, got %v", d500)
	}
	d503 := cb.CalculateRetryDelay(1, 503)
	if d503 != 2*time.Second {
		t.Fatalf("expected 2s for 503 attempt 1, got %v", d503)
	}

	// High attempt should be capped at MaxDelay
	dMax := cb.CalculateRetryDelay(10, 500)
	if dMax > 30*time.Second {
		t.Fatalf("expected <= 30s, got %v", dMax)
	}
}

func TestCircuitBreakerRetryDelayJitter(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		BaseDelay:     1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		JitterFactor:  0.5, // 50% jitter
	})

	// With jitter, delays should vary but be in the expected range
	delays := make(map[time.Duration]bool)
	for i := 0; i < 20; i++ {
		d := cb.CalculateRetryDelay(0, 500)
		delays[d] = true
		// base is 1s with +50% jitter, so 1s to 1.5s
		if d < 1*time.Second || d > 1500*time.Millisecond {
			t.Fatalf("delay %v outside expected range [1s, 1.5s]", d)
		}
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"429 rate limit", errors.New("429 Too Many Requests"), true},
		{"rate limit text", errors.New("rate limit exceeded"), true},
		{"too many requests", errors.New("too many requests"), true},
		{"500 server error", errors.New("500 Internal Server Error"), true},
		{"502 bad gateway", errors.New("502 Bad Gateway"), true},
		{"503 unavailable", errors.New("503 Service Unavailable"), true},
		{"504 timeout", errors.New("504 Gateway Timeout"), true},
		{"timeout", errors.New("request timeout"), true},
		{"deadline exceeded", errors.New("context deadline exceeded"), true},
		{"connection refused", errors.New("connection refused"), true},
		{"network error", errors.New("network unreachable"), true},
		{"unauthorized", errors.New("401 Unauthorized"), false},
		{"invalid api key", errors.New("invalid api key"), false},
		{"quota exceeded", errors.New("insufficient_quota"), false},
		{"billing issue", errors.New("billing error"), false},
		{"context canceled", errors.New("context canceled"), false},
		{"context overflow", errors.New("400 context_length_exceeded: maximum context length exceeded"), false},
		{"request aborted", errors.New("request aborted by client"), false},
		{"output format", errors.New("json schema validation failed"), false},
		{"model not found", errors.New("model not found"), false},
		{"random error", errors.New("some random error"), false},
		{
			"bigmodel 429",
			errors.New(`POST "https://open.bigmodel.cn/api/paas/v4/chat/completions": 429 Too Many Requests {"code":"1302","message":"rate limit"}`),
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryableError(tt.err)
			if result != tt.expected {
				t.Errorf("IsRetryableError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestShouldRecordCBFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"RateLimit", errors.New("429 Too Many Requests"), true},
		{"Server", errors.New("500 Internal Server Error"), true},
		{"Auth", errors.New("401 Unauthorized"), false},
		{"Quota", errors.New("insufficient_quota"), false},
		{"ContextOverflow", errors.New("400 context_length_exceeded: maximum context length exceeded"), false},
		{"Aborted", context.Canceled, false},
		{"OutputFormat", errors.New("tool_call_validation invalid_tool_call"), false},
		{"Network", errors.New("connection reset by peer"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldRecordCBFailure(tt.err); got != tt.expected {
				t.Fatalf("ShouldRecordCBFailure(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestExtractHTTPStatus(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{"nil error", nil, 0},
		{
			"OpenAI SDK format 429",
			errors.New(`POST "https://open.bigmodel.cn/api/paas/v4/chat/completions": 429 Too Many Requests {"code":"1302"}`),
			429,
		},
		{
			"OpenAI SDK format 500",
			errors.New(`POST "https://api.openai.com/v1/chat/completions": 500 Internal Server Error {}`),
			500,
		},
		{
			"OpenAI SDK format 502",
			errors.New(`GET "https://api.example.com/endpoint": 502 Bad Gateway {}`),
			502,
		},
		{"plain 429 text", errors.New("429 Too Many Requests"), 429},
		{"plain 500 text", errors.New("server returned 500"), 500},
		{"no status code", errors.New("some random error"), 0},
		{"embedded digits not matched", errors.New("error code 1500 occurred"), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractHTTPStatus(tt.err)
			if result != tt.expected {
				t.Errorf("ExtractHTTPStatus(%v) = %d, want %d", tt.err, result, tt.expected)
			}
		})
	}
}

func TestCircuitStateString(t *testing.T) {
	tests := []struct {
		state    CircuitState
		expected string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.FailureThreshold != 5 {
		t.Errorf("FailureThreshold = %d, want 5", cfg.FailureThreshold)
	}
	if cfg.SuccessThreshold != 2 {
		t.Errorf("SuccessThreshold = %d, want 2", cfg.SuccessThreshold)
	}
	if cfg.OpenTimeout != 60*time.Second {
		t.Errorf("OpenTimeout = %v, want 60s", cfg.OpenTimeout)
	}
	if cfg.BaseDelay != 2*time.Second {
		t.Errorf("BaseDelay = %v, want 2s", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 60*time.Second {
		t.Errorf("MaxDelay = %v, want 60s", cfg.MaxDelay)
	}
}

func TestNewCircuitBreakerDefaults(t *testing.T) {
	// Zero config should get sensible defaults
	cb := NewCircuitBreaker(CircuitBreakerConfig{})
	if cb.config.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cb.config.MaxRetries)
	}
	if cb.config.FailureThreshold != 5 {
		t.Errorf("FailureThreshold = %d, want 5", cb.config.FailureThreshold)
	}
	if cb.config.OpenTimeout != 60*time.Second {
		t.Errorf("OpenTimeout = %v, want 60s", cb.config.OpenTimeout)
	}
}
