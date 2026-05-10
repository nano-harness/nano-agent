package llm

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

type partialErrorClient struct{ err error }

func (p partialErrorClient) StreamCompletion(_ context.Context, _ []Message, onEvent func(event.StreamEvent)) error {
	onEvent(event.NewStreamEvent(event.EventTypeStreamContent, "test").WithContent("partial"))
	return p.err
}
func (p partialErrorClient) StreamCompletionWithoutReasoning(ctx context.Context, messages []Message, onEvent func(event.StreamEvent)) error {
	return p.StreamCompletion(ctx, messages, onEvent)
}
func (p partialErrorClient) GenerateContent(context.Context, string) (string, error) {
	return "", p.err
}
func (p partialErrorClient) UpdateTools([]interfaces.Tool) {}

func TestMultiRouteClientFallbacksOnFailbackError(t *testing.T) {
	primaryErr := fmt.Errorf("POST \"https://api.example/v1/chat/completions\": 429 Too Many Requests")
	client := testMultiRouteClient([]MockResponse{{Error: primaryErr}, {Content: "fallback ok"}})
	var events []event.StreamEvent
	err := client.StreamCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(ev event.StreamEvent) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("StreamCompletion failed: %v", err)
	}
	if len(client.clientNames()) != 2 {
		t.Fatalf("clients used = %v, want both routes", client.clientNames())
	}
	if !hasEvent(events, event.EventTypeProviderFallback) {
		t.Fatalf("provider_fallback event missing: %+v", events)
	}
}

func TestMultiRouteClientDoesNotFallbackOnAuthError(t *testing.T) {
	authErr := fmt.Errorf("POST \"https://api.example/v1/chat/completions\": 401 Unauthorized")
	client := testMultiRouteClient([]MockResponse{{Error: authErr}, {Content: "should not run"}})
	err := client.StreamCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(event.StreamEvent) {})
	if err == nil {
		t.Fatal("expected auth error")
	}
	if len(client.clientNames()) != 1 {
		t.Fatalf("clients used = %v, want only primary", client.clientNames())
	}
}

func TestMultiRouteClientSkipsOpenPrimaryBreaker(t *testing.T) {
	health := NewRouteHealth(DefaultCircuitBreakerConfig())
	for i := 0; i < DefaultCircuitBreakerConfig().FailureThreshold; i++ {
		health.BreakerFor("primary").RecordFailure()
	}
	client := testMultiRouteClientWithHealth([]MockResponse{{Content: "primary"}, {Content: "fallback"}}, health)
	err := client.StreamCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(event.StreamEvent) {})
	if err != nil {
		t.Fatalf("StreamCompletion failed: %v", err)
	}
	names := client.clientNames()
	if len(names) != 1 || names[0] != "fallback" {
		t.Fatalf("clients used = %v, want fallback only", names)
	}
}

func TestMultiRouteClientDoesNotFallbackAfterPartialStream(t *testing.T) {
	failbackErr := fmt.Errorf("POST \"https://api.example/v1/chat/completions\": 500 Internal Server Error")
	routes := testRoutes()
	client := newMultiRouteClient(routes, nil, nil, nil, func(route ResolvedRoute, _ []interfaces.Tool) LLMClient {
		if route.Name == "primary" {
			return partialErrorClient{err: failbackErr}
		}
		return mockWithResponse(MockResponse{Content: "fallback"})
	})
	err := client.StreamCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(event.StreamEvent) {})
	if !errors.Is(err, failbackErr) && err.Error() != failbackErr.Error() {
		t.Fatalf("err = %v, want primary partial error", err)
	}
	if len(client.clientNames()) != 1 {
		t.Fatalf("clients used = %v, want no fallback", client.clientNames())
	}
}

func TestMultiRouteClientAllRoutesExhaustedReturnsLastError(t *testing.T) {
	firstErr := fmt.Errorf("POST \"https://api.example/v1/chat/completions\": 500 Internal Server Error")
	lastErr := fmt.Errorf("POST \"https://api.example/v1/chat/completions\": 503 Service Unavailable")
	client := testMultiRouteClient([]MockResponse{{Error: firstErr}, {Error: lastErr}})
	err := client.StreamCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(event.StreamEvent) {})
	if err == nil || err.Error() != lastErr.Error() {
		t.Fatalf("err = %v, want last error", err)
	}
}

func TestMultiRouteClientPerRouteBreakerIsolation(t *testing.T) {
	health := NewRouteHealth(DefaultCircuitBreakerConfig())
	health.BreakerFor("primary").RecordFailure()
	if health.BreakerFor("fallback").Stats()["total_failures"] != int64(0) {
		t.Fatalf("fallback breaker was affected by primary failure")
	}
}

func TestMultiRouteClientGenerateContentFallback(t *testing.T) {
	primaryErr := fmt.Errorf("POST \"https://api.example/v1/chat/completions\": 429 Too Many Requests")
	client := testMultiRouteClient([]MockResponse{{Error: primaryErr}, {Content: "fallback text"}})
	content, err := client.GenerateContent(context.Background(), "hi")
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}
	if content != "fallback text" {
		t.Fatalf("content = %q", content)
	}
}

func testRoutes() []ResolvedRoute {
	return []ResolvedRoute{
		{Name: "primary", ProviderID: "openai", Model: "gpt-4.1", BaseURL: "https://api.openai.com/v1"},
		{Name: "fallback", ProviderID: "deepseek", Model: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1"},
	}
}

func testMultiRouteClient(responses []MockResponse) *MultiRouteClient {
	return testMultiRouteClientWithHealth(responses, nil)
}

func testMultiRouteClientWithHealth(responses []MockResponse, health *RouteHealth) *MultiRouteClient {
	resetCircuitBreakerRegistryForTest()
	routes := testRoutes()
	idx := 0
	return newMultiRouteClient(routes, nil, nil, health, func(route ResolvedRoute, _ []interfaces.Tool) LLMClient {
		resp := MockResponse{Content: route.Name}
		if idx < len(responses) {
			resp = responses[idx]
		}
		idx++
		return mockWithResponse(resp)
	})
}

func mockWithResponse(resp MockResponse) *MockClient {
	mock := NewMockClient()
	mock.Responses = []MockResponse{resp}
	return mock
}

func (m *MultiRouteClient) clientNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	return names
}

func hasEvent(events []event.StreamEvent, eventType event.EventType) bool {
	for _, ev := range events {
		if ev.Type == eventType {
			return true
		}
	}
	return false
}
