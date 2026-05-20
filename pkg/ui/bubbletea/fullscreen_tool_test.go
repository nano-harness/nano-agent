package bubbletea

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestFullscreenToolMessageRendersFromMetadata verifies that tool
// messages are rendered from structured Metadata (name + status +
// params + result) rather than the localized summary in Content.
func TestFullscreenToolMessageRendersFromMetadata(t *testing.T) {
	m := newReadyFullscreenModel()
	m.Update(ToolUseMsg{
		ID:       "t1",
		ToolName: "ls",
		Status:   "success",
		Params:   map[string]interface{}{"path": "/home"},
		Result:   "file1\nfile2\nfile3",
	})

	if m.messages.Len() == 0 {
		t.Fatalf("expected a tool message to be recorded")
	}
	msg := m.messages.Get(m.messages.Len() - 1)
	if msg.Role != "tool" {
		t.Fatalf("expected role=tool, got %q", msg.Role)
	}
	rendered := msg.Rendered
	for _, want := range []string{"ls", "success", "path", "/home"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered tool message missing %q; got:\n%s", want, rendered)
		}
	}
}

// TestFullscreenToolMessageCollapsedByDefault verifies that tool
// messages start collapsed and that long results are summarised.
func TestFullscreenToolMessageCollapsedByDefault(t *testing.T) {
	m := newReadyFullscreenModel()
	long := strings.Repeat("row\n", 20)
	m.Update(ToolUseMsg{
		ID:       "t1",
		ToolName: "grep",
		Status:   "success",
		Result:   long,
	})
	msg := m.messages.Get(m.messages.Len() - 1)
	if !msg.Collapsed {
		t.Fatalf("tool message should start Collapsed=true")
	}
	if !strings.Contains(msg.Rendered, "more lines") {
		t.Fatalf("collapsed tool result should include '... N more lines' summary; got:\n%s", msg.Rendered)
	}
}

// TestFullscreenCtrlOTogglesLastToolMessage exercises the new Ctrl+O
// shortcut which expands/collapses the most recent tool message.
func TestFullscreenCtrlOTogglesLastToolMessage(t *testing.T) {
	m := newReadyFullscreenModel()
	m.Update(ToolUseMsg{
		ID:       "t1",
		ToolName: "cat",
		Status:   "success",
		Result:   strings.Repeat("line\n", 12),
	})
	msg := m.messages.Get(m.messages.Len() - 1)
	if !msg.Collapsed {
		t.Fatalf("precondition: tool message expected collapsed")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	m = updated.(*FullscreenModel)
	if msg.Collapsed {
		t.Fatalf("Ctrl+O should expand tool message")
	}
	if strings.Contains(msg.Rendered, "more lines") {
		t.Fatalf("expanded rendering should not include collapse marker; got:\n%s", msg.Rendered)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	m = updated.(*FullscreenModel)
	if !msg.Collapsed {
		t.Fatalf("second Ctrl+O should re-collapse tool message")
	}
}

// TestFullscreenTaskCompletionStopsSpinner verifies that TaskCompletionMsg
// unconditionally transitions to phaseDone regardless of prior state.
func TestFullscreenTaskCompletionStopsSpinner(t *testing.T) {
	m := newReadyFullscreenModel()
	m.currentPhase = phaseThinking
	updated, _ := m.Update(TaskCompletionMsg{})
	m = updated.(*FullscreenModel)
	if m.currentPhase != phaseDone {
		t.Fatalf("expected phaseDone after TaskCompletionMsg, got %v", m.currentPhase)
	}
}

// TestFullscreenStatusDoneStopsSpinner verifies that StatusUpdate("完成")
// unconditionally transitions to phaseDone.
func TestFullscreenStatusDoneStopsSpinner(t *testing.T) {
	m := newReadyFullscreenModel()
	m.currentPhase = phaseToolCall
	updated, _ := m.Update(StatusUpdate("完成"))
	m = updated.(*FullscreenModel)
	if m.currentPhase != phaseDone {
		t.Fatalf("expected phaseDone after StatusUpdate(完成), got %v", m.currentPhase)
	}
}

// TestFullscreenNonStreamAssistantCompletesTurn verifies that a plain
// "assistant" message advances to phaseDone (stopping the spinner)
// while "assistant_stream" stays in phaseResponse.
func TestFullscreenNonStreamAssistantCompletesTurn(t *testing.T) {
	m := newReadyFullscreenModel()
	m.currentPhase = phaseThinking
	m.Update(Message{Role: "assistant", Content: "done"})
	if m.currentPhase != phaseDone {
		t.Fatalf("expected phaseDone after non-stream assistant, got %v", m.currentPhase)
	}

	m2 := newReadyFullscreenModel()
	m2.currentPhase = phaseThinking
	m2.Update(Message{Role: "assistant_stream", Content: "partial"})
	if m2.currentPhase == phaseDone {
		t.Fatalf("assistant_stream should not auto-transition to phaseDone")
	}
}
