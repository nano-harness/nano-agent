package bubbletea

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestInlineMessageWindowScrollsByOffset verifies that
// messageScrollOffsetLines visually advances the message window
// without changing the underlying store.
func TestInlineMessageWindowScrollsByOffset(t *testing.T) {
	m := newTestModel(80)
	m.termHeight = 20
	for i := 0; i < 5; i++ {
		m.messages.Add("user", strings.Repeat("line\n", 4))
	}

	base := m.renderMessageWindow(10)
	m.messageScrollOffsetLines = 3
	scrolled := m.renderMessageWindow(10)
	if base == scrolled {
		t.Fatalf("expected different output after scroll offset; got identical")
	}
}

// TestInlinePgUpAdjustsScrollOffset verifies that PgUp increases the
// scroll offset while PgDn decreases it.
func TestInlinePgUpAdjustsScrollOffset(t *testing.T) {
	m := newTestModel(80)
	m.termWidth = 80
	m.termHeight = 24
	for i := 0; i < 10; i++ {
		m.messages.Add("user", strings.Repeat("line\n", 5))
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m = updated.(*Model)
	if m.messageScrollOffsetLines == 0 {
		t.Fatalf("PgUp should increase scroll offset")
	}
	before := m.messageScrollOffsetLines

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = updated.(*Model)
	if m.messageScrollOffsetLines >= before {
		t.Fatalf("PgDn should decrease scroll offset (was %d, now %d)", before, m.messageScrollOffsetLines)
	}
}

// TestInlineSubmitResetsScrollOffset verifies that submitting input
// snaps the message view back to the bottom.
func TestInlineSubmitResetsScrollOffset(t *testing.T) {
	m := newTestModel(80)
	m.termWidth = 80
	m.termHeight = 24
	for i := 0; i < 10; i++ {
		m.messages.Add("user", "x")
	}
	m.messageScrollOffsetLines = 5
	m.input.SetValue("hello")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*Model)
	if m.messageScrollOffsetLines != 0 {
		t.Fatalf("submit should reset scroll offset to 0, got %d", m.messageScrollOffsetLines)
	}
}
