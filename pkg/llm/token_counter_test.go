package llm

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/tools"
)

func TestTokenCounterBasic(t *testing.T) {
	counter, err := NewTokenCounter("gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create token counter: %v", err)
	}

	if counter == nil {
		t.Log("Token counter is nil (expected for fallback)")
		return
	}

	text := "Hello, world! This is a test message for token counting."
	tokens := counter.CountTokens(text)

	if tokens <= 0 {
		t.Errorf("Expected positive token count, got %d", tokens)
	}

	// Basic sanity check - should be reasonable
	if tokens > len(text) {
		t.Errorf("Token count %d exceeds text length %d", tokens, len(text))
	}
}

func TestEstimateTokensFromChars(t *testing.T) {
	text := "Hello, world!"
	actual := EstimateTokensFromChars(text)

	if actual <= 0 {
		t.Errorf("Expected positive estimate, got %d", actual)
	}

	// Should be roughly in the right ballpark
	if actual > len(text) || actual < 1 {
		t.Errorf("Estimate %d seems unreasonable for text length %d", actual, len(text))
	}
}

func TestTokenCounterMessage(t *testing.T) {
	counter, err := NewTokenCounter("gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create token counter: %v", err)
	}

	if counter == nil {
		t.Log("Token counter is nil (expected for fallback)")
		return
	}

	msg := Message{
		Role:    "user",
		Content: "This is a test message",
	}

	tokens := counter.CountMessagesTokens([]Message{msg})
	if tokens <= 0 {
		t.Errorf("Expected positive token count for message, got %d", tokens)
	}
}

func TestTokenStats(t *testing.T) {
	stats := NewTokenStats()

	if stats.InputTokens != 0 {
		t.Errorf("Expected initial input tokens to be 0, got %d", stats.InputTokens)
	}

	stats.SetInputTokens(100)
	if stats.InputTokens != 100 {
		t.Errorf("Expected input tokens 100, got %d", stats.InputTokens)
	}

	stats.AddOutputTokens(50)
	if stats.OutputTokens != 50 {
		t.Errorf("Expected output tokens 50, got %d", stats.OutputTokens)
	}

	event := stats.GetEvent()
	if event.InputTokens != 100 || event.OutputTokens != 50 || event.TotalTokens != 150 {
		t.Errorf("Token stats event incorrect: %+v", event)
	}
}

func TestCountMessagesTokensWithToolCalls(t *testing.T) {
	counter, err := NewTokenCounter("gpt-3.5-turbo")
	if err != nil {
		// Even if token counter falls back, test should still pass
		t.Logf("NewTokenCounter error (fallback may apply): %v", err)
	}

	user := Message{Role: "user", Content: "Please write a file with content."}
	assistantWithTool := Message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []tools.ToolCall{
			{
				Name: "write_file",
				Arguments: map[string]interface{}{
					"path":    "/tmp/demo.txt",
					"content": "hello world",
				},
			},
		},
	}

	without := counter.CountMessagesTokens([]Message{user})
	with := counter.CountMessagesTokens([]Message{user, assistantWithTool})

	if with <= without {
		t.Errorf("Expected more tokens with tool calls: with=%d without=%d", with, without)
	}
}
