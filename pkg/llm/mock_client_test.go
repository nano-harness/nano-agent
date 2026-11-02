package llm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

func TestMockClient_GenerateContent(t *testing.T) {
	client := NewMockClient()

	// Test default response
	ctx := context.Background()
	resp, err := client.GenerateContent(ctx, "hello")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp != "Mock response" {
		t.Errorf("Expected 'Mock response', got '%s'", resp)
	}

	// Test sequential responses
	client.Responses = []MockResponse{
		{Content: "First response"},
		{Content: "Second response"},
		{Error: errors.New("simulated error")},
	}
	client.responseIdx = 0

	resp, err = client.GenerateContent(ctx, "seq 1")
	if err != nil || resp != "First response" {
		t.Errorf("Expected 'First response' and no error, got %v, %v", resp, err)
	}

	resp, err = client.GenerateContent(ctx, "seq 2")
	if err != nil || resp != "Second response" {
		t.Errorf("Expected 'Second response' and no error, got %v, %v", resp, err)
	}

	_, err = client.GenerateContent(ctx, "seq 3")
	if err == nil || err.Error() != "simulated error" {
		t.Errorf("Expected 'simulated error', got %v", err)
	}

	// Test rule-based responses
	client.Rules["trigger"] = MockResponse{Content: "Rule triggered"}
	resp, err = client.GenerateContent(ctx, "this is a trigger prompt")
	if err != nil || resp != "Rule triggered" {
		t.Errorf("Expected 'Rule triggered' and no error, got %v, %v", resp, err)
	}
}

func TestMockClient_StreamCompletion(t *testing.T) {
	client := NewMockClient()
	client.Responses = []MockResponse{
		{
			Content:   "Hello stream",
			Reasoning: "Thinking about it",
			ToolCalls: []tools.ToolCall{
				{
					ID:   "call_1",
					Name: "test_tool",
					Arguments: map[string]interface{}{
						"arg1": "val1",
					},
				},
			},
		},
	}

	ctx := context.Background()
	messages := []Message{
		{Role: "user", Content: "test"},
	}

	var events []event.StreamEvent
	onEvent := func(e event.StreamEvent) {
		events = append(events, e)
	}

	err := client.StreamCompletion(ctx, messages, onEvent)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify events
	var hasThinking, hasContent, hasToolCall, hasDone bool
	for _, e := range events {
		switch e.Type {
		case event.EventTypeThinking:
			hasThinking = true
		case event.EventTypeContent:
			hasContent = true
		case event.EventTypeToolCall:
			hasToolCall = true
			if len(e.ToolCalls) == 0 || e.ToolCalls[0].ID != "call_1" {
				t.Errorf("Invalid tool call event: %+v", e)
			}
		case event.EventTypeDone:
			hasDone = true
		}
	}

	if !hasThinking {
		t.Error("Expected thinking event")
	}
	if !hasContent {
		t.Error("Expected content event")
	}
	if !hasToolCall {
		t.Error("Expected tool call event")
	}
	if !hasDone {
		t.Error("Expected done event")
	}
}

func TestMockClient_ContextCancellation(t *testing.T) {
	client := NewMockClient()
	client.Responses = []MockResponse{
		{
			Content: "Delayed response",
			Delay:   100 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	messages := []Message{
		{Role: "user", Content: "test"},
	}

	err := client.StreamCompletion(ctx, messages, func(e event.StreamEvent) {})
	if err == nil || err != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded error, got %v", err)
	}
}
