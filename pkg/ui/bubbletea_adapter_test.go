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
	adapter.AttachCronTracker(tracker)

	tracker.Handle(cronStarted("task-a"))
	msg := readTeaMsg(t, adapter.sendCh).(bt.CronStatusMsg)
	if msg.Indicator != "⏰ 1 running" {
		t.Fatalf("Indicator = %q", msg.Indicator)
	}

	tracker.Handle(cronFinished("task-a"))
	msg = readTeaMsg(t, adapter.sendCh).(bt.CronStatusMsg)
	if msg.Indicator != "" {
		t.Fatalf("Indicator = %q, want empty", msg.Indicator)
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
