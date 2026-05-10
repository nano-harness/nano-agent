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

func TestTaskEventStore_AssignsSequenceToPersistedUnsequencedEvents(t *testing.T) {
	store := NewTaskEventStore(3)

	store.Add(event.StreamEvent{Type: event.EventTypeTaskStart, Content: "start"})
	store.Add(event.StreamEvent{Type: event.EventTypeWorkerStart, Seq: 1, Content: "old"})
	store.Add(event.StreamEvent{Type: event.EventTypeTaskCompletion, Content: "done"})

	events := store.Since(0, func(event.StreamEvent) bool { return true })
	if len(events) != 3 {
		t.Fatalf("Since returned %d events, want 3", len(events))
	}
	for i, ev := range events {
		wantSeq := int64(i + 1)
		if ev.Seq != wantSeq {
			t.Fatalf("event %d Seq=%d, want %d", i, ev.Seq, wantSeq)
		}
	}
	if got := store.LastSeq(); got != 3 {
		t.Fatalf("LastSeq=%d, want 3", got)
	}
}

func TestTaskEventStore_SandboxSince(t *testing.T) {
	store := NewTaskEventStore(3)
	store.Publish(event.StreamEvent{Type: event.EventTypeContent, Content: "not sandbox"})
	store.Publish(event.StreamEvent{Type: event.EventTypeSandboxDecisionCreated, Content: "decision"})
	store.Publish(event.StreamEvent{Type: event.EventTypeSandboxCommandFinished, Content: "finished"})
	store.Publish(event.StreamEvent{Type: event.EventTypeSandboxEnvironmentCleaned, Content: "cleaned"})

	events := store.SandboxSince(0)
	if len(events) != 3 {
		t.Fatalf("SandboxSince returned %d events, want 3", len(events))
	}
	if events[0].Type != event.EventTypeSandboxDecisionCreated || events[1].Type != event.EventTypeSandboxCommandFinished || events[2].Type != event.EventTypeSandboxEnvironmentCleaned {
		t.Fatalf("unexpected sandbox events: %#v", events)
	}
}
