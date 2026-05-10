package agent

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
)

func TestStreamingTokenStatsForwardingInjectsContextUsage(t *testing.T) {
	turn := newTestTurn()
	turn.LLMClient = &fakeStreamClient{events: [][]event.StreamEvent{{
		{Type: event.EventTypeTokenStats, TokenStats: &event.TokenStats{InputTokens: 10, TotalTokens: 10}},
		{Type: event.EventTypeTokenStats, TokenStats: &event.TokenStats{InputTokens: 20, TotalTokens: 20}},
		{Type: event.EventTypeTokenStats, TokenStats: &event.TokenStats{InputTokens: 30, TotalTokens: 30}},
	}}}

	var forwarded []*event.TokenStats
	turn.SetEventHandler(func(ev event.StreamEvent) {
		if ev.Type == event.EventTypeTokenStats {
			forwarded = append(forwarded, ev.TokenStats)
		}
	})

	_, _, err := turn.requestOpenAIAPI(context.Background())
	if err != nil {
		t.Fatalf("requestOpenAIAPI returned error: %v", err)
	}
	if len(forwarded) != 3 {
		t.Fatalf("expected 3 stats events, got %d", len(forwarded))
	}
	for i, stats := range forwarded {
		if stats.ContextWindowMax <= 0 || stats.ContextUsedTokens <= 0 {
			t.Fatalf("event %d missing context usage: %+v", i, stats)
		}
	}
}
