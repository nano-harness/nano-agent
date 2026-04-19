package daemon

import (
	"sync"

	"github.com/nano-harness/nano-agent/pkg/event"
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

// Add adds an event to the store
func (s *TaskEventStore) Add(ev event.StreamEvent) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, ev)
	if ev.Seq > s.lastSeq {
		s.lastSeq = ev.Seq
	}
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
