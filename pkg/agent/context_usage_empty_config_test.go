package agent

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
)

func TestContextUsageInjectionWhenCompressionAndConfigBothMissing(t *testing.T) {
	turn := newTestTurn()
	turn.compressionStrategy = nil
	turn.agentConfig = &config.Config{Model: "aliyun-glm-5.1"}
	turn.LLMClient = &fakeStreamClient{events: [][]event.StreamEvent{{
		{Type: event.EventTypeTokenStats, TokenStats: &event.TokenStats{InputTokens: 123, TotalTokens: 123}},
	}}}

	var forwarded *event.TokenStats
	turn.SetEventHandler(func(ev event.StreamEvent) {
		if ev.Type == event.EventTypeTokenStats {
			forwarded = ev.TokenStats
		}
	})

	_, _, err := turn.requestOpenAIAPI(context.Background())
	if err != nil {
		t.Fatalf("requestOpenAIAPI returned error: %v", err)
	}
	if forwarded == nil {
		t.Fatal("expected token stats event")
	}
	if forwarded.ContextWindowMax <= 0 {
		t.Fatalf("expected model registry context fallback, got %+v", forwarded)
	}
	if forwarded.ContextUsedTokens != 123 {
		t.Fatalf("ContextUsedTokens = %d, want 123", forwarded.ContextUsedTokens)
	}
}
