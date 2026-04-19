package llm

import (
	"encoding/json"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/tools"
)

func TestValidateMessageSequence(t *testing.T) {
	client := &Client{}

	// Test case 1: Valid sequence
	t.Run("Valid message sequence", func(t *testing.T) {
		messages := []Message{
			{Role: "user", Content: "Hello"},
			{
				Role:    "assistant",
				Content: "I'll help you.",
				ToolCalls: []tools.ToolCall{
					{ID: "call_123", Name: "test_tool", Arguments: map[string]interface{}{}},
				},
			},
			{Role: "tool", Content: "Tool result", ToolCallID: "call_123"},
			{Role: "assistant", Content: "Done!"},
		}

		err := client.validateMessageSequence(messages)
		if err != nil {
			t.Errorf("Expected no error for valid sequence, got: %v", err)
		}
	})

	// Test case 2: Invalid sequence - missing tool message
	t.Run("Invalid message sequence - missing tool message", func(t *testing.T) {
		messages := []Message{
			{Role: "user", Content: "Hello"},
			{
				Role:    "assistant",
				Content: "I'll help you.",
				ToolCalls: []tools.ToolCall{
					{ID: "call_123", Name: "test_tool", Arguments: map[string]interface{}{}},
				},
			},
			// Missing tool message
			{Role: "assistant", Content: "Done!"},
		}

		err := client.validateMessageSequence(messages)
		if err == nil {
			t.Error("Expected error for invalid sequence, got nil")
		}
	})

	// Test case 3: Empty messages
	t.Run("Empty messages", func(t *testing.T) {
		messages := []Message{}

		err := client.validateMessageSequence(messages)
		if err != nil {
			t.Errorf("Expected no error for empty messages, got: %v", err)
		}
	})
}

func TestCleanupMessages(t *testing.T) {
	client := &Client{}

	// Test case: Cleanup incomplete tool calls
	t.Run("Cleanup incomplete tool calls", func(t *testing.T) {
		messages := []Message{
			{Role: "user", Content: "Hello"},
			{
				Role:    "assistant",
				Content: "I'll help you.",
				ToolCalls: []tools.ToolCall{
					{ID: "call_123", Name: "test_tool", Arguments: map[string]interface{}{}},
				},
			},
			// Missing tool message
			{Role: "user", Content: "Another question"},
		}

		cleanedMessages := client.cleanupMessages(messages)

		// Check that the tool calls were removed from the assistant message
		if len(cleanedMessages) != 3 {
			t.Errorf("Expected 3 messages after cleanup, got %d", len(cleanedMessages))
		}

		assistantMsg := cleanedMessages[1]
		if len(assistantMsg.ToolCalls) != 0 {
			t.Errorf("Expected tool calls to be removed, but found %d tool calls", len(assistantMsg.ToolCalls))
		}

		if assistantMsg.Content != "I'll help you." {
			t.Errorf("Expected content to be preserved, got: %s", assistantMsg.Content)
		}
	})

	// Test case: Keep valid tool call sequences
	t.Run("Keep valid tool call sequences", func(t *testing.T) {
		messages := []Message{
			{Role: "user", Content: "Hello"},
			{
				Role:    "assistant",
				Content: "I'll help you.",
				ToolCalls: []tools.ToolCall{
					{ID: "call_123", Name: "test_tool", Arguments: map[string]interface{}{}},
				},
			},
			{Role: "tool", Content: "Tool result", ToolCallID: "call_123"},
			{Role: "assistant", Content: "Done!"},
		}

		cleanedMessages := client.cleanupMessages(messages)

		// Check that valid sequences are preserved
		if len(cleanedMessages) != 4 {
			t.Errorf("Expected 4 messages after cleanup, got %d", len(cleanedMessages))
		}

		assistantMsg := cleanedMessages[1]
		if len(assistantMsg.ToolCalls) != 1 {
			t.Errorf("Expected tool calls to be preserved, but found %d tool calls", len(assistantMsg.ToolCalls))
		}
	})
}

func TestConvertMessagesWithValidation(t *testing.T) {
	client := &Client{}

	// Test case: Convert messages with invalid sequence should auto-cleanup
	t.Run("Auto-cleanup during conversion", func(t *testing.T) {
		messages := []Message{
			{Role: "user", Content: "Hello"},
			{
				Role:    "assistant",
				Content: "I'll help you.",
				ToolCalls: []tools.ToolCall{
					{ID: "call_123", Name: "test_tool", Arguments: map[string]interface{}{}},
				},
			},
			// Missing tool message - should be auto-cleaned
		}

		// This should not panic and should handle the cleanup automatically
		openaiMessages := client.convertMessages(messages)

		// Should have 2 messages: user and assistant (with tool calls removed)
		if len(openaiMessages) != 2 {
			t.Errorf("Expected 2 messages after conversion, got %d", len(openaiMessages))
		}
	})
}

func TestUnmarshalFrontendToolMessages(t *testing.T) {
	raw := `[
		{"role":"user","content":"Hello"},
		{"role":"assistant","content":"","type":"tool_call","tool_calls":[{"id":"call_123","name":"test_tool","arguments":{"foo":"bar"}}]},
		{"role":"tool","content":"ok","tool_call_id":"call_123","type":"tool_result","tool_results":[{"id":"call_123","content":"ok"}]},
		{"role":"assistant","content":"Done!"}
	]`

	var messages []Message
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}

	if got := len(messages[1].ToolCalls); got != 1 {
		t.Fatalf("expected 1 tool_call on assistant, got %d", got)
	}
	if messages[2].Role != "tool" || messages[2].ToolCallID != "call_123" {
		t.Fatalf("expected tool message with tool_call_id, got role=%s tool_call_id=%s", messages[2].Role, messages[2].ToolCallID)
	}
	if got := len(messages[2].ToolResults); got != 1 {
		t.Fatalf("expected 1 tool_result on tool message, got %d", got)
	}

	client := &Client{}
	if err := client.validateMessageSequence(messages); err != nil {
		t.Fatalf("expected valid sequence, got: %v", err)
	}
}
