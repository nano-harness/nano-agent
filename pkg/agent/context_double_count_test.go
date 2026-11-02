package agent

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/llm"
)

func TestEstimateTokenCountWithSystemPrompt_DoesNotDoubleCountSystemMessage(t *testing.T) {
	cs := NewCompressionStrategyWithConfig(1000, 0.3, 3)
	systemPrompt := "system prompt with enough content to count"
	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "hello"},
	}

	withoutExternal := cs.EstimateTokenCount(messages)
	withExternal := cs.EstimateTokenCountWithSystemPrompt(messages, systemPrompt)
	if withExternal != withoutExternal {
		t.Fatalf("system prompt was double-counted: got %d, want %d", withExternal, withoutExternal)
	}
}
