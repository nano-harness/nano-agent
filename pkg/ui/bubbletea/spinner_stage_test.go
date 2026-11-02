package bubbletea_test

import (
	"testing"
	"time"

	ui "github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	btuitest "github.com/nano-harness/nano-agent/pkg/ui/bubbletea/testing"
)

func TestSpinner_ThinkingFrames(t *testing.T) {
	btuitest.RunSyncTest(t, func(t *testing.T) {
		m := ui.New(nil, nil, "", t.TempDir())
		_, _ = m.Update(ui.ThinkingMsg{Title: "thinking", ReasoningDelta: "step"})
		btuitest.AdvanceTime(time.Millisecond)

		if m.SpinnerStage() != "thinking" {
			t.Fatalf("SpinnerStage() = %q, want thinking", m.SpinnerStage())
		}
		if m.SpinnerFrameIndex() != 1 {
			t.Fatalf("SpinnerFrameIndex() = %d, want 1", m.SpinnerFrameIndex())
		}
	})
}

func TestSpinner_ExecutingFrames(t *testing.T) {
	btuitest.RunSyncTest(t, func(t *testing.T) {
		m := ui.New(nil, nil, "", t.TempDir())
		_, _ = m.Update(ui.ToolUseMsg{ID: "1", ToolName: "read_file", Status: "executing"})
		btuitest.AdvanceTime(time.Millisecond)

		if m.SpinnerStage() != "executing" {
			t.Fatalf("SpinnerStage() = %q, want executing", m.SpinnerStage())
		}
		if m.SpinnerFrameIndex() != 1 {
			t.Fatalf("SpinnerFrameIndex() = %d, want 1", m.SpinnerFrameIndex())
		}
	})
}

func TestSpinner_WritingFrames(t *testing.T) {
	btuitest.RunSyncTest(t, func(t *testing.T) {
		m := ui.New(nil, nil, "", t.TempDir())
		_, _ = m.Update(ui.Message{Role: "assistant", Content: "done"})
		btuitest.AdvanceTime(time.Millisecond)

		if m.SpinnerStage() != "writing" {
			t.Fatalf("SpinnerStage() = %q, want writing", m.SpinnerStage())
		}
		if m.SpinnerFrameIndex() != 1 {
			t.Fatalf("SpinnerFrameIndex() = %d, want 1", m.SpinnerFrameIndex())
		}
	})
}

func TestSpinner_StageSwitchPreservesFrame(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	_, _ = m.Update(ui.ThinkingMsg{Title: "thinking", ReasoningDelta: "step"})
	_, _ = m.Update(ui.ToolUseMsg{ID: "1", ToolName: "write_file", Status: "executing"})

	if m.SpinnerStage() != "executing" {
		t.Fatalf("SpinnerStage() = %q, want executing", m.SpinnerStage())
	}
	if m.SpinnerFrameIndex() != 2 {
		t.Fatalf("SpinnerFrameIndex() = %d, want 2", m.SpinnerFrameIndex())
	}
}
