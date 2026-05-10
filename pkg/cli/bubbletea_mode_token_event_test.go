package cli

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
)

func TestBuildTokenStatsUpdate_PreservesContextFields(t *testing.T) {
	update := buildTokenStatsUpdate(&event.TokenStats{
		InputTokens:         10,
		OutputTokens:        20,
		TotalTokens:         30,
		PeakTokensPerSecond: 4.5,
		ContextWindowMax:    192600,
		ContextUsedTokens:   69900,
	})
	if update.InputTokens != 10 || update.OutputTokens != 20 || update.TotalTokens != 30 || update.Peak != 4.5 {
		t.Fatalf("basic fields not preserved: %+v", update)
	}
	if update.ContextWindowMax != 192600 || update.ContextUsedTokens != 69900 {
		t.Fatalf("context fields not preserved: %+v", update)
	}
}

func TestBuildTokenStatsUpdate_NilInputReturnsZero(t *testing.T) {
	if got := buildTokenStatsUpdate(nil); got != (struct {
		InputTokens       int
		OutputTokens      int
		TotalTokens       int
		Peak              float64
		ContextWindowMax  int
		ContextUsedTokens int
	}{}) {
		t.Fatalf("nil stats should return zero value, got %+v", got)
	}
}
