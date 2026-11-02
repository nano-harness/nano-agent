package bubbletea_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	ui "github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
)

func TestClearSession_ResetsAllState(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	_, _ = m.Update(ui.ThinkingMsg{Title: "thinking", ReasoningDelta: "old thought"})
	_, _ = m.Update(ui.Message{Role: "assistant_stream", Content: "partial"})
	_, _ = m.Update(ui.ShowConfirmationMsg{Message: "confirm?", ToolInfo: map[string]interface{}{"Name": "write_file"}})
	typeText(m, "#draft")

	_ = m.ClearSession()

	if got := m.ThinkingWindow(); len(got) != 0 {
		t.Fatalf("ThinkingWindow() after clear = %#v, want empty", got)
	}
	showing, _ := m.ConfirmationState()
	if showing {
		t.Fatal("confirmation should be hidden after clear")
	}
	active, _, _, _ := m.FilePickerState()
	if active {
		t.Fatal("file picker should be hidden after clear")
	}
	if got := m.InputValue(); got != "" {
		t.Fatalf("InputValue() after clear = %q, want empty", got)
	}
}

func TestClearSession_ReturnsNewSessionID(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	m.SetNewSessionHandler(func() string { return "session-new" })

	if got := m.ClearSession(); got != "session-new" {
		t.Fatalf("ClearSession() = %q, want session-new", got)
	}
}

func TestClearSession_CommandReturnsNewSessionIDFeedback(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	m.SetNewSessionHandler(func() string { return "session-new" })
	m.InputValue()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+l should return a clear command")
	}
}
