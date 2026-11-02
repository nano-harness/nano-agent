package llm

import (
	"errors"
	"testing"
	"time"
)

func TestModelRouterDefaultRoutePreservesOpenAIBaseURL(t *testing.T) {
	router := NewModelRouter(RouteConfig{Model: "gpt-4.1"})
	route := router.Select()

	if route.Model != "gpt-4.1" {
		t.Fatalf("model = %q, want gpt-4.1", route.Model)
	}
	if route.BaseURL != defaultOpenAIBaseURL {
		t.Fatalf("base URL = %q, want %q", route.BaseURL, defaultOpenAIBaseURL)
	}
	if route.Profile.ContextWindow == 0 {
		t.Fatalf("expected inferred profile, got %+v", route.Profile)
	}
}

func TestModelRouterFallbackAndMetrics(t *testing.T) {
	router := NewModelRouter(
		RouteConfig{Name: "primary", Model: "gpt-4.1"},
		RouteConfig{Name: "fallback", Model: "gpt-4o-mini", BaseURL: "https://example.invalid/v1"},
	)

	fallback, ok := router.SelectFallback(0)
	if !ok {
		t.Fatal("expected fallback route")
	}
	if fallback.Name != "fallback" || fallback.BaseURL != "https://example.invalid/v1" {
		t.Fatalf("unexpected fallback route: %+v", fallback)
	}

	router.RecordResult(fallback, TokenUsage{InputTokens: 10, OutputTokens: 5, CostUSD: 0.001}, 25*time.Millisecond, nil)
	router.RecordResult(fallback, TokenUsage{InputTokens: 2, OutputTokens: 1}, 10*time.Millisecond, errors.New("temporary"))

	metrics := router.Metrics()["fallback"]
	if metrics.Fallbacks != 1 || metrics.Requests != 2 || metrics.Successes != 1 || metrics.Failures != 1 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
	if metrics.InputTokens != 12 || metrics.OutputTokens != 6 || metrics.TotalLatency != 35*time.Millisecond {
		t.Fatalf("unexpected usage metrics: %+v", metrics)
	}
	if metrics.LastError != "temporary" {
		t.Fatalf("LastError = %q, want temporary", metrics.LastError)
	}
}
