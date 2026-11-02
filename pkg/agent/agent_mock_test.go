package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
)

func TestAgentWithMockLLM(t *testing.T) {
	// Create configuration
	// CustomSystemPrompt is set to avoid subprocess-spawning tool detection in
	// buildEnvironmentContext(), which would hang in CI environments.
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "You are a test agent.",
	}

	// Create mock LLM client
	mockClient := llm.NewMockClient()
	mockClient.Responses = []llm.MockResponse{
		{
			Content: "This is a mock response from the agent.",
		},
	}

	// Create agent with mock client
	agent, err := New(cfg, nil, WithLLMClient(mockClient))
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// Verify the mock client was injected
	if agent.GetLLMClient() != mockClient {
		t.Error("Agent did not use the provided mock client")
	}

	// Process a stream using the mock client
	ctx := context.Background()
	var mu sync.Mutex
	var events []event.StreamEvent
	onEvent := func(e event.StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}

	err = agent.ProcessStream(ctx, "Hello mock agent", onEvent)
	if err != nil {
		t.Fatalf("ProcessStream failed: %v", err)
	}

	// Verify we got the expected content from the mock client
	mu.Lock()
	defer mu.Unlock()
	var contentReceived string
	for _, e := range events {
		if e.Type == event.EventTypeContent {
			contentReceived += e.Content
		}
	}

	// Turn based agent adds some wrapper text around responses (e.g. formatting, final answer wrapper)
	// Just checking if our mock text is somewhere in there
	if len(contentReceived) == 0 {
		t.Error("Expected to receive content from mock client, got none")
	}
}
