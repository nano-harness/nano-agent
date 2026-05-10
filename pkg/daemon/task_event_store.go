package daemon

import (
	"sync"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

// TaskEventStore stores task events in memory
type TaskEventStore struct {
	mu      sync.RWMutex
	events  []event.StreamEvent
	lastSeq int64
}

// NewTaskEventStore creates a new task event store
func NewTaskEventStore(capacity int) *TaskEventStore {
	if capacity <= 0 {
		capacity = 5000 // Just used as initial slice capacity now
	}
	return &TaskEventStore{
		events: make([]event.StreamEvent, 0, capacity),
	}
}

// Add adds an event to the store and returns the sequenced event that was stored.
func (s *TaskEventStore) Add(ev event.StreamEvent) event.StreamEvent {
	if s == nil {
		return ev
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if ev.Seq <= 0 || ev.Seq <= s.lastSeq {
		ev.Seq = s.lastSeq + 1
	}
	s.events = append(s.events, ev)
	if ev.Seq > s.lastSeq {
		s.lastSeq = ev.Seq
	}
	return ev
}

// Publish stores an event and allows TaskEventStore to satisfy EventBus-style publishers.
func (s *TaskEventStore) Publish(ev event.StreamEvent) {
	s.Add(ev)
}

// PublishSandboxEvent adapts sandbox runtime audit events into StreamEvent storage.
func (s *TaskEventStore) PublishSandboxEvent(ev sandbox.Event) {
	if s == nil {
		return
	}
	s.Add(event.StreamEvent{
		Type:      event.EventType(ev.Type),
		Content:   ev.Content,
		Source:    ev.Source,
		Timestamp: ev.Timestamp,
		Metadata:  ev.Metadata,
	})
}

// LastSeq returns the last sequence number
func (s *TaskEventStore) LastSeq() int64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSeq
}

// Since returns events since a sequence number
func (s *TaskEventStore) Since(seq int64, filter func(event.StreamEvent) bool) []event.StreamEvent {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.events) == 0 {
		return nil
	}

	var out []event.StreamEvent
	for _, ev := range s.events {
		if ev.Seq <= seq {
			continue
		}
		if filter(ev) {
			out = append(out, ev)
		}
	}
	return out
}

// SandboxSince returns sandbox audit events since a sequence number.
func (s *TaskEventStore) SandboxSince(seq int64) []event.StreamEvent {
	return s.Since(seq, func(ev event.StreamEvent) bool {
		return sandbox.IsSandboxEventType(string(ev.Type))
	})
}
