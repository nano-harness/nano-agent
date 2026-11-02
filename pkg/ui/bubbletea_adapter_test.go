package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nano-harness/nano-agent/pkg/event"
	bt "github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
)

func TestBubbleTeaAdapter_AttachCronTrackerBridgesOnChangeToProgram(t *testing.T) {
	adapter := &BubbleTeaAdapter{
		program: &tea.Program{},
		sendCh:  make(chan tea.Msg, 4),
	}
	tracker := NewCronStatusTracker()
	tracker.SetScheduledCountFn(func() int { return 1 })
	adapter.AttachCronTracker(tracker)

	tracker.Handle(cronStarted("task-a"))
	msg := readTeaMsg(t, adapter.sendCh).(bt.CronStatusMsg)
	if msg.Indicator != "⏰ 1 scheduled, 1 running" {
		t.Fatalf("Indicator = %q", msg.Indicator)
	}

	tracker.Handle(cronFinished("task-a"))
	msg = readTeaMsg(t, adapter.sendCh).(bt.CronStatusMsg)
	if msg.Indicator != "⏰ 1 scheduled" {
		t.Fatalf("Indicator = %q", msg.Indicator)
	}
}

func TestBubbleTeaAdapter_SendEventDoesNotForwardCronEvents(t *testing.T) {
	adapter := &BubbleTeaAdapter{
		program: &tea.Program{},
		sendCh:  make(chan tea.Msg, 4),
	}

	adapter.sendEvent(event.StreamEvent{
		Type: event.EventTypeCronTaskStarted,
		Metadata: map[string]interface{}{
			"task_id": "task-a",
		},
	})

	select {
	case msg := <-adapter.sendCh:
		t.Fatalf("unexpected message for cron event: %#v", msg)
	case <-time.After(50 * time.Millisecond):
	}
}

func readTeaMsg(t *testing.T, ch <-chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tea message")
		return nil
	}
}

// TestBubbleTeaAdapter_ForwardsCapabilitiesToFullscreenModel verifies that
// `SetPermissionManager`, `SetNewSessionHandler` and `SetModelLister` are
// forwarded to the fullscreen model so milktea (--milktea) gets the same
// capabilities (Shift+Tab permission cycling, Ctrl+L new session, /models
// completion) that the inline tea mode already enjoys.
func TestBubbleTeaAdapter_ForwardsCapabilitiesToFullscreenModel(t *testing.T) {
	fs := bt.NewFullscreenModel("", "")
	adapter := &BubbleTeaAdapter{model: fs}

	adapter.SetNewSessionHandler(func() string { return "session-1" })
	if fs.NewSessionHandler() == nil {
		t.Fatal("SetNewSessionHandler was not forwarded to FullscreenModel")
	}

	adapter.SetModelLister(func() string { return "gpt-4 gpt-5" })
	if fs.ModelLister() == nil {
		t.Fatal("SetModelLister was not forwarded to FullscreenModel")
	}
}
