package daemon

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
)

func TestTaskEventStore_Since(t *testing.T) {
	store := NewTaskEventStore(3)

	store.Add(event.StreamEvent{Type: event.EventTypePlannerPlanSnapshot, Seq: 1, Content: "p1"})
	store.Add(event.StreamEvent{Type: event.EventTypeExecutorState, Seq: 2, Content: "running"})
	store.Add(event.StreamEvent{Type: event.EventTypeWorkerStart, Seq: 3, Content: "toolA", WorkerID: "w1"})
	store.Add(event.StreamEvent{Type: event.EventTypeWorkerEnd, Seq: 4, Content: "success", WorkerID: "w1"})
	store.Add(event.StreamEvent{Type: event.EventTypePlannerPlanUpdate, Seq: 5, Content: "p2"})

	if got := store.LastSeq(); got != 5 {
		t.Fatalf("LastSeq=%d, want %d", got, 5)
	}

	filterAll := func(ev event.StreamEvent) bool { return true }

	since := store.Since(3, filterAll)
	if len(since) != 2 {
		t.Fatalf("Since returned %d events, want 2", len(since))
	}
	for _, ev := range since {
		if ev.Seq <= 3 {
			t.Fatalf("Since returned ev with seq<=3: %+v", ev)
		}
	}
}
