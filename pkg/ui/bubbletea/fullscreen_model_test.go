package bubbletea

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
)

// fsKey constructs a KeyPressMsg for a single ASCII rune.
func fsKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func newReadyFullscreenModel() *FullscreenModel {
	m := NewFullscreenModel("", "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return updated.(*FullscreenModel)
}

// TestFullscreen_TextareaAcceptsAllLetters verifies the core bug fix: vim-style
// scroll keys (j/k/g/G) and arrow keys must NOT be intercepted at the
// fullscreen model level, so the textarea can record them as regular text.
func TestFullscreen_TextareaAcceptsAllLetters(t *testing.T) {
	m := newReadyFullscreenModel()

	for _, r := range []rune{'h', 'e', 'l', 'l', 'o', 'j', 'k', 'g', 'G'} {
		updated, _ := m.Update(fsKey(r))
		m = updated.(*FullscreenModel)
	}

	got := m.textarea.Value()
	if !strings.Contains(got, "j") || !strings.Contains(got, "k") ||
		!strings.Contains(got, "g") || !strings.Contains(got, "G") {
		t.Fatalf("textarea did not capture vim-style letters; got %q", got)
	}
	if !strings.HasPrefix(got, "hello") {
		t.Fatalf("expected textarea to start with 'hello', got %q", got)
	}
}

// TestFullscreen_EnterSubmits verifies that pressing Enter submits the input
// and clears the textarea.
func TestFullscreen_EnterSubmits(t *testing.T) {
	m := newReadyFullscreenModel()

	var sent string
	m.BindOutbound(func(o eventsource.Outbound) error {
		sent = o.Text
		return nil
	})

	for _, r := range []rune("hi there") {
		updated, _ := m.Update(fsKey(r))
		m = updated.(*FullscreenModel)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*FullscreenModel)

	if sent != "hi there" {
		t.Fatalf("expected outbound text 'hi there', got %q", sent)
	}
	if strings.TrimSpace(m.textarea.Value()) != "" {
		t.Fatalf("textarea should be reset after submit, got %q", m.textarea.Value())
	}
	if len(m.messages) != 1 || m.messages[0].Role != "user" {
		t.Fatalf("expected a user message to be appended, got %+v", m.messages)
	}
}

// TestFullscreen_ShiftEnterInsertsNewline verifies that Shift+Enter inserts a
// newline into the textarea instead of submitting.
func TestFullscreen_ShiftEnterInsertsNewline(t *testing.T) {
	m := newReadyFullscreenModel()

	for _, r := range []rune("line1") {
		updated, _ := m.Update(fsKey(r))
		m = updated.(*FullscreenModel)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	m = updated.(*FullscreenModel)
	for _, r := range []rune("line2") {
		updated, _ := m.Update(fsKey(r))
		m = updated.(*FullscreenModel)
	}

	got := m.textarea.Value()
	if !strings.Contains(got, "\n") {
		t.Fatalf("expected textarea to contain a newline, got %q", got)
	}
	if len(m.messages) != 0 {
		t.Fatalf("Shift+Enter must not submit, but got %d messages", len(m.messages))
	}
}

// TestFullscreen_MouseWheelScrolls verifies that mouse-wheel messages drive
// the viewport (since vim-style keyboard scrolling was removed).
func TestFullscreen_MouseWheelScrolls(t *testing.T) {
	m := newReadyFullscreenModel()
	// Populate enough messages to make the viewport scrollable.
	for i := 0; i < 50; i++ {
		m.addUserMessage("message")
	}
	// Force sticky off and scroll to top so we can observe a scroll-down.
	m.viewport.ScrollToTop()
	before := m.viewport.ScrollOffset

	updated, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	m = updated.(*FullscreenModel)

	if m.viewport.ScrollOffset <= before {
		t.Fatalf("expected mouse wheel down to advance scroll offset, got %d -> %d",
			before, m.viewport.ScrollOffset)
	}
}
