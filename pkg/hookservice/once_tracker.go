package hookservice

import "sync"

// OnceTracker records once hooks that have already been executed by a Service.
type OnceTracker struct {
	executed sync.Map
}

func NewOnceTracker() *OnceTracker {
	return &OnceTracker{}
}

func (t *OnceTracker) TryMark(name string) bool {
	if t == nil {
		return true
	}
	if name == "" {
		name = "<unnamed>"
	}
	_, loaded := t.executed.LoadOrStore(name, true)
	return !loaded
}
