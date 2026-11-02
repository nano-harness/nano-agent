package cli

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/nano-harness/nano-agent/pkg/event"
	bubbletea "github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
)

// TestForwardStreamEvent_TokenStats verifies that ForwardStreamEvent translates
// an EventTypeTokenStats event into a TokenStatsUpdate with all fields intact.
func TestForwardStreamEvent_TokenStats_PreservesContextFields(t *testing.T) {
	var received bubbletea.TokenStatsUpdate
	bubbletea.ForwardStreamEvent(func(m tea.Msg) {
		if u, ok := m.(bubbletea.TokenStatsUpdate); ok {
			received = u
		}
	}, event.StreamEvent{
		Type: event.EventTypeTokenStats,
		TokenStats: &event.TokenStats{
			InputTokens:         10,
			OutputTokens:        20,
			TotalTokens:         30,
			PeakTokensPerSecond: 4.5,
			ContextWindowMax:    192600,
			ContextUsedTokens:   69900,
		},
	})
	if received.InputTokens != 10 || received.OutputTokens != 20 || received.TotalTokens != 30 || received.Peak != 4.5 {
		t.Fatalf("basic fields not preserved: %+v", received)
	}
	if received.ContextWindowMax != 192600 || received.ContextUsedTokens != 69900 {
		t.Fatalf("context fields not preserved: %+v", received)
	}
}

func TestForwardStreamEvent_TokenStats_NilSkipped(t *testing.T) {
	var called bool
	bubbletea.ForwardStreamEvent(func(_ tea.Msg) {
		called = true
	}, event.StreamEvent{
		Type:       event.EventTypeTokenStats,
		TokenStats: nil,
	})
	if called {
		t.Fatal("nil TokenStats should not produce a send call")
	}
}
