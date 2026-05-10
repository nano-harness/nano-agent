package bubbletea_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	ui "github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	btuitest "github.com/nano-harness/nano-agent/pkg/ui/bubbletea/testing"
	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

func TestStream_ConcatsDeltas(t *testing.T) {
	tm := newStreamTestModel(t)
	btuitest.InjectStreamChunk(tm, "hello", " ", "world")
	tm.Send(ui.TaskCompletionMsg{Reason: "done"})
	btuitest.WaitForText(t, tm, "hello world", time.Second)
}

func TestStream_CancelMidway(t *testing.T) {
	cancelled := false
	m := ui.New(nil, make(chan struct{}, 1), "", t.TempDir())
	m.BindOutbound(func(o eventsource.Outbound) error {
		if o.Kind == "cancel" {
			cancelled = true
		}
		return nil
	})
	_, _ = m.Update(ui.Message{Role: "assistant_stream", Content: "partial"})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		_ = cmd()
	}

	if !cancelled {
		t.Fatal("ctrl+c should emit cancel outbound while streaming")
	}
}

func TestStream_FinalFlushesBuffer(t *testing.T) {
	tm := newStreamTestModel(t)
	btuitest.InjectStreamChunk(tm, "final")
	tm.Send(ui.TaskCompletionMsg{Reason: "done"})
	btuitest.WaitForText(t, tm, "final", time.Second)
}

func TestStream_NewlineDoesNotSplitBlock(t *testing.T) {
	tm := newStreamTestModel(t)
	btuitest.InjectStreamChunk(tm, "hello\n", "world")
	tm.Send(ui.TaskCompletionMsg{Reason: "done"})
	btuitest.WaitForText(t, tm, "(?s)hello.*world", time.Second)
}

func TestStream_MultiChunkWithNewlinesConcats(t *testing.T) {
	tm := newStreamTestModel(t)
	btuitest.InjectStreamChunk(tm, "alpha\n", "beta\n", "gamma")
	tm.Send(ui.TaskCompletionMsg{Reason: "done"})
	btuitest.WaitForText(t, tm, "(?s)alpha.*beta.*gamma", time.Second)
}

func TestStream_MarkdownRendered(t *testing.T) {
	tm := newStreamTestModel(t)
	btuitest.InjectStreamChunk(tm, "**bold**")
	tm.Send(ui.TaskCompletionMsg{Reason: "done"})
	btuitest.WaitForText(t, tm, "bold", time.Second)
	out := btuitest.OutputString(t, tm)
	if strings.Contains(out, "**bold**") {
		t.Fatalf("markdown stream was not rendered:\n%s", out)
	}
}

func newStreamTestModel(t *testing.T) *teatest.TestModel {
	t.Helper()
	m := ui.New(nil, nil, "", t.TempDir())
	tm := btuitest.NewTeatestModel(t, m)
	t.Cleanup(func() { _ = tm.Quit() })
	return tm
}
