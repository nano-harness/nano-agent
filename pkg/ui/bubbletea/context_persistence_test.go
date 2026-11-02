package bubbletea

import (
	"strings"
	"testing"
)

func TestTokenStatsUpdate_ContextPersistence(t *testing.T) {
	t.Run("first update shows context", func(t *testing.T) {
		m := newTestModel(80)
		_, _ = m.Update(TokenStatsUpdate{InputTokens: 100, TotalTokens: 100, ContextUsedTokens: 100, ContextWindowMax: 1000})
		if !strings.Contains(m.tokenStatus, "上下文: 100/1.0K (10%)") {
			t.Fatalf("expected context status, got %q", m.tokenStatus)
		}
	})

	t.Run("missing context keeps previous values", func(t *testing.T) {
		m := newTestModel(80)
		_, _ = m.Update(TokenStatsUpdate{InputTokens: 100, TotalTokens: 100, ContextUsedTokens: 100, ContextWindowMax: 1000})
		_, _ = m.Update(TokenStatsUpdate{InputTokens: 200, TotalTokens: 200})
		if !strings.Contains(m.tokenStatus, "上下文: 100/1.0K (10%)") {
			t.Fatalf("expected sticky context status, got %q", m.tokenStatus)
		}
	})

	t.Run("new context replaces previous values", func(t *testing.T) {
		m := newTestModel(80)
		_, _ = m.Update(TokenStatsUpdate{InputTokens: 100, TotalTokens: 100, ContextUsedTokens: 100, ContextWindowMax: 1000})
		_, _ = m.Update(TokenStatsUpdate{InputTokens: 200, TotalTokens: 200, ContextUsedTokens: 500, ContextWindowMax: 2000})
		if !strings.Contains(m.tokenStatus, "上下文: 500/2.0K (25%)") {
			t.Fatalf("expected updated context status, got %q", m.tokenStatus)
		}
	})
}
