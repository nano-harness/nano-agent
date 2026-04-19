package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

func TestCompressionStrategy_ShouldCompress(t *testing.T) {
	cs := NewCompressionStrategyWithConfig(100, 0.3, 4)

	messages := []llm.Message{
		{Role: "system", Content: "sys"},
	}
	// Add enough messages to pass minMessagesToKeep+1 check
	for i := 0; i < 6; i++ {
		messages = append(messages, llm.Message{Role: "user", Content: "hello"})
		messages = append(messages, llm.Message{Role: "assistant", Content: "world"})
	}

	if cs.ShouldCompress(messages, 40) {
		t.Fatalf("should not compress when below threshold")
	}
	if !cs.ShouldCompress(messages, 80) {
		t.Fatalf("should compress when above threshold")
	}
}

func TestCompressionStrategy_SegmentHistory(t *testing.T) {
	cs := NewCompressionStrategyWithConfig(1000, 0.2, 4)

	messages := []llm.Message{{Role: "system", Content: "sys"}}
	for i := 0; i < 10; i++ {
		messages = append(messages, llm.Message{Role: "user", Content: "u"})
		messages = append(messages, llm.Message{Role: "assistant", Content: "a"})
	}

	toCompress, toPreserve := cs.SegmentHistory(messages)
	if len(toCompress) == 0 {
		t.Fatalf("expected some messages to compress")
	}
	// preserve should include system + preserveCount (max(minKeep, ratio*remaining)) => 1 + 4 = 5
	if len(toPreserve) != 5 {
		t.Fatalf("unexpected preserve size: got %d want %d", len(toPreserve), 5)
	}
	// Ensure first preserved is system
	if toPreserve[0].Role != "system" {
		t.Fatalf("first preserved message should be system")
	}
}

func TestCompressionStrategy_FindSmartSplitIndex(t *testing.T) {
	cs := NewCompressionStrategyWithConfig(1000, 0.3, 3)

	msgs := []llm.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "call", ToolCalls: []tools.ToolCall{{Name: "tool", Arguments: map[string]interface{}{"x": 1}}}},
		{Role: "assistant", Content: "call2", ToolCalls: []tools.ToolCall{{Name: "tool", Arguments: map[string]interface{}{"y": 2}}}},
		{Role: "user", Content: "u2"},
	}
	split := cs.findSmartSplitIndex(msgs, 1)
	// Should move split to the next user message (index 3)
	if split != 3 {
		t.Fatalf("unexpected split index: got %d want %d", split, 3)
	}
}

func TestCompressionStrategy_FormatAndFallbackSummary(t *testing.T) {
	cs := NewCompressionStrategyWithConfig(1000, 0.3, 3)

	msgs := []llm.Message{
		{Role: "user", Content: "ask"},
		{Role: "assistant", Content: "thinking", ToolCalls: []tools.ToolCall{{Name: "search", Arguments: map[string]interface{}{"q": "go"}}}},
		{Role: "tool", Content: "result", ToolCallID: "call-1"},
	}

	formatted := cs.formatMessagesForSummary(msgs)
	if formatted == "" || !(containsAll(formatted, []string{"[1] user:", "Tools called:", "search", "[3] tool[call-1]:"})) {
		t.Fatalf("formatted summary missing expected parts: %s", formatted)
	}

	fallback := cs.createFallbackSummary(msgs)
	if !containsAll(fallback, []string{"state_snapshot", "Conversation history:", "1 user messages", "1 assistant responses"}) {
		t.Fatalf("fallback summary content unexpected: %s", fallback)
	}
}

func TestCompressionStrategy_EstimateTokenCount(t *testing.T) {
	cs := NewCompressionStrategyWithConfig(1000, 0.3, 3)

	msgs := []llm.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "short text"},
		{Role: "assistant", Content: "reply", ToolCalls: []tools.ToolCall{{Name: "tool", Arguments: map[string]interface{}{"k": "v"}}}},
	}
	tokens := cs.EstimateTokenCount(msgs)
	if tokens <= 0 {
		t.Fatalf("token estimate should be positive")
	}
	total := cs.EstimateTokenCountWithSystemPrompt(msgs, "extra")
	if total <= tokens {
		t.Fatalf("token count with system prompt should be greater")
	}
}

func TestCompressionStrategy_CompressMessages_ForceNoLLM(t *testing.T) {
	// We only test the orchestration logic without invoking LLM by forcing and giving empty compressible set
	cs := NewCompressionStrategyWithConfig(1000, 0.3, 1000) // preserve all so toCompress is empty

	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}

	ctx := context.Background()
	compressed, info, err := cs.CompressMessages(ctx, nil, msgs, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Since nothing to compress after segmentation, it returns original messages and nil info
	if info != nil {
		t.Fatalf("expected no compression info when nothing to compress")
	}
	if len(compressed) != len(msgs) {
		t.Fatalf("messages length changed unexpectedly")
	}
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func TestComputeMinKeep(t *testing.T) {
	cases := []struct {
		window int
		want   int
	}{
		{4_096, 3},
		{8_192, 3},
		{16_384, 4},
		{32_768, 6},
		{131_072, 6},
		{200_000, 8},
		{400_000, 10},
		{1_000_000, 10},
	}
	for _, tc := range cases {
		got := computeMinKeep(tc.window)
		if got != tc.want {
			t.Errorf("computeMinKeep(%d) = %d, want %d", tc.window, got, tc.want)
		}
	}
}
