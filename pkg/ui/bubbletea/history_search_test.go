package bubbletea

import "testing"

func TestHistorySearch_BasicFlow(t *testing.T) {
	h := NewHistorySearch([]string{"git status", "ls -la", "git log", "echo hi"})
	if h.Active() {
		t.Fatal("not active before Begin")
	}
	h.Begin()
	if !h.Active() {
		t.Fatal("expected active after Begin")
	}
	h.TypeRune('g')
	h.TypeRune('i')
	h.TypeRune('t')
	if got := h.Selected(); got != "git log" {
		t.Errorf("expected newest match 'git log', got %q", got)
	}
	h.Next()
	if got := h.Selected(); got != "git status" {
		t.Errorf("expected next-older 'git status', got %q", got)
	}
	h.Next() // already oldest match; should not wrap
	if got := h.Selected(); got != "git status" {
		t.Errorf("Next should not wrap; got %q", got)
	}
}

func TestHistorySearch_CaseInsensitive(t *testing.T) {
	h := NewHistorySearch([]string{"Git Status", "ls"})
	h.Begin()
	h.TypeRune('G')
	h.TypeRune('I')
	h.TypeRune('T')
	if got := h.Selected(); got != "Git Status" {
		t.Errorf("expected case-insensitive match, got %q", got)
	}
}

func TestHistorySearch_BackspaceResetsCursor(t *testing.T) {
	h := NewHistorySearch([]string{"alpha", "beta", "alphabet"})
	h.Begin()
	h.TypeRune('a')
	h.TypeRune('l')
	h.TypeRune('p')
	// "alphabet" is most recent containing "alp".
	if got := h.Selected(); got != "alphabet" {
		t.Errorf("expected 'alphabet', got %q", got)
	}
	h.Next()
	if got := h.Selected(); got != "alpha" {
		t.Errorf("expected 'alpha', got %q", got)
	}
	h.Backspace() // query -> "al"
	// After backspace the cursor resets to most-recent match.
	if got := h.Selected(); got != "alphabet" {
		t.Errorf("after backspace expected newest 'alphabet', got %q", got)
	}
}

func TestHistorySearch_BackspaceUnicode(t *testing.T) {
	h := NewHistorySearch([]string{"你好 world"})
	h.Begin()
	h.TypeRune('你')
	h.TypeRune('好')
	if got := h.Selected(); got != "你好 world" {
		t.Fatalf("expected unicode match, got %q", got)
	}
	h.Backspace()
	if got := h.Query(); got != "你" {
		t.Fatalf("expected rune-aware backspace, got query %q", got)
	}
}

func TestHistorySearch_EndResets(t *testing.T) {
	h := NewHistorySearch([]string{"foo"})
	h.Begin()
	h.TypeRune('f')
	h.End()
	if h.Active() || h.Query() != "" || h.Selected() != "" {
		t.Errorf("End should reset all state; got active=%v query=%q selected=%q",
			h.Active(), h.Query(), h.Selected())
	}
}

func TestHistorySearch_NoMatchSelectedEmpty(t *testing.T) {
	h := NewHistorySearch([]string{"foo", "bar"})
	h.Begin()
	h.TypeRune('z')
	if h.Selected() != "" {
		t.Errorf("expected empty selection on no match, got %q", h.Selected())
	}
}

func TestHistorySearch_BeginIdempotent(t *testing.T) {
	h := NewHistorySearch([]string{"a", "b"})
	h.Begin()
	h.TypeRune('a')
	h.Begin() // should not reset
	if h.Query() != "a" {
		t.Errorf("Begin should be idempotent; query=%q", h.Query())
	}
}
