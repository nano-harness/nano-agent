package agent

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
)

func TestContextUsageInjectedBeforeTokenStatsForwarded(t *testing.T) {
	turn := newTestTurn()
	turn.LLMClient = &fakeStreamClient{events: [][]event.StreamEvent{{
		{
			Type:          event.EventTypeTokenStats,
			Source:        "llm_client",
			TokenStats:    &event.TokenStats{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			CorrelationID: "stats-1",
		},
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
		t.Fatal("expected token stats event to be forwarded")
	}
	if forwarded.ContextWindowMax <= 0 {
		t.Fatalf("expected context window max to be injected, got %+v", forwarded)
	}
	if forwarded.ContextUsedTokens <= 0 {
		t.Fatalf("expected context used tokens to be injected, got %+v", forwarded)
	}
}

func TestContextUsageInjectedWithoutCompressionStrategy(t *testing.T) {
	turn := newTestTurn()
	turn.compressionStrategy = nil
	turn.agentConfig = &config.Config{
		ContextConfig: config.ContextConfig{MaxTokens: 4096},
	}
	turn.LLMClient = &fakeStreamClient{events: [][]event.StreamEvent{{
		{
			Type:          event.EventTypeTokenStats,
			Source:        "llm_client",
			TokenStats:    &event.TokenStats{InputTokens: 123, OutputTokens: 5, TotalTokens: 128},
			CorrelationID: "stats-1",
		},
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
		t.Fatal("expected token stats event to be forwarded")
	}
	if forwarded.ContextWindowMax != 4096 {
		t.Fatalf("ContextWindowMax = %d, want 4096", forwarded.ContextWindowMax)
	}
	if forwarded.ContextUsedTokens != 123 {
		t.Fatalf("ContextUsedTokens = %d, want input token fallback", forwarded.ContextUsedTokens)
	}
}
