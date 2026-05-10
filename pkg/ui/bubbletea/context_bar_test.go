package bubbletea_test

import (
	"strings"
	"testing"

	ui "github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	btuitest "github.com/nano-harness/nano-agent/pkg/ui/bubbletea/testing"
)

func TestContextBar_GreenUnder60(t *testing.T) {
	assertContextColor(t, 50, 100, "38;5;82")
}

func TestContextBar_YellowUnder85(t *testing.T) {
	assertContextColor(t, 70, 100, "38;5;220")
}

func TestContextBar_RedAbove85(t *testing.T) {
	assertContextColor(t, 90, 100, "38;5;196")
}

func TestContextBar_HiddenWhenMaxZero(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	_, max, pct := m.ContextBarState()
	if max != 0 || pct != 0 {
		t.Fatalf("ContextBarState max=%d pct=%f, want zero state", max, pct)
	}
	if strings.Contains(m.View().Content, "█") {
		t.Fatalf("context bar should be hidden when max=0:\n%s", m.View().Content)
	}
}

func TestContextBar_NegativePctClamped(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	_, _ = m.Update(ui.TokenStatsUpdate{ContextUsedTokens: -10, ContextWindowMax: 100})

	_, _, pct := m.ContextBarState()
	if pct != 0 {
		t.Fatalf("ContextBarState pct=%f, want 0", pct)
	}
}

func assertContextColor(t *testing.T, used, max int, color string) {
	t.Helper()
	btuitest.ApplyTestTheme(t)
	m := ui.New(nil, nil, "", t.TempDir())
	_, _ = m.Update(ui.TokenStatsUpdate{ContextUsedTokens: used, ContextWindowMax: max})

	out := m.View().Content
	if !strings.Contains(out, color) {
		t.Fatalf("context bar output does not contain ANSI color %s:\n%q", color, out)
	}
}
