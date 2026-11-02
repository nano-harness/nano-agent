package bubbletea

import (
	"fmt"
	"sync/atomic"
	"time"
)

// messageSeq is a monotonic counter used to generate unique IDs for
// messages appended through MessageStore.Add. It is incremented atomically
// as a forward-looking precaution: MessageStore itself is currently used
// from the Bubble Tea event loop goroutine, but the atomic counter ensures
// IDs remain unique if future callers append from auxiliary goroutines.
// Note that the rest of MessageStore is NOT goroutine-safe — only ID
// generation is.
var messageSeq uint64

// MessageStore is an ordered append-only collection of FormattedMessage
// values shared between the inline (`--tea`) and fullscreen (`--milktea`)
// Bubble Tea models. It centralises message ownership so future work can
// migrate both models off ad-hoc `[]string` / `[]*FormattedMessage` storage
// onto a single declarative source of truth.
//
// MessageStore is intentionally minimal: it exposes only the operations
// listed in the Phase 0 refactor plan (Add / Get / Last / Len / Range /
// InvalidateCache). Higher-level concerns such as virtual scrolling and
// height caching remain in the models that own the store.
type MessageStore struct {
	messages []*FormattedMessage
}

// NewMessageStore returns an empty store with a small pre-allocated backing
// slice. The capacity is a hint only; the store grows as needed.
func NewMessageStore() *MessageStore {
	return &MessageStore{messages: make([]*FormattedMessage, 0, 16)}
}

// Add appends a new FormattedMessage with the given role and content and
// returns a pointer to the stored value so callers can attach rendered
// output, height, or metadata. A unique, monotonic ID is generated from
// the role, the current Unix-nanosecond timestamp, and an atomic sequence
// number; the sequence component guarantees uniqueness even when multiple
// messages are appended within the same nanosecond.
func (s *MessageStore) Add(role, content string) *FormattedMessage {
	seq := atomic.AddUint64(&messageSeq, 1)
	id := fmt.Sprintf("%s-%d-%d", role, time.Now().UnixNano(), seq)
	msg := NewFormattedMessage(id, role, content)
	s.messages = append(s.messages, msg)
	return msg
}

// AddMessage appends a pre-constructed FormattedMessage. It is useful when
// callers need to control the ID or seed metadata before the message is
// inserted into the store.
func (s *MessageStore) AddMessage(msg *FormattedMessage) {
	if msg == nil {
		return
	}
	s.messages = append(s.messages, msg)
}

// Get returns the message at index i or nil if i is out of range. The
// returned pointer aliases the stored value; callers may mutate fields
// such as Content or Rendered directly.
func (s *MessageStore) Get(i int) *FormattedMessage {
	if i < 0 || i >= len(s.messages) {
		return nil
	}
	return s.messages[i]
}

// Last returns the most recently appended message, or nil if the store is
// empty.
func (s *MessageStore) Last() *FormattedMessage {
	if len(s.messages) == 0 {
		return nil
	}
	return s.messages[len(s.messages)-1]
}

// Len reports the number of messages currently in the store.
func (s *MessageStore) Len() int {
	return len(s.messages)
}

// Range invokes fn for each message in insertion order. Iteration stops
// when fn returns false. This mirrors the iteration style adopted by
// Go's sync.Map.Range and avoids exposing the underlying slice so callers
// cannot accidentally retain or mutate it.
func (s *MessageStore) Range(fn func(int, *FormattedMessage) bool) {
	for i, msg := range s.messages {
		if !fn(i, msg) {
			return
		}
	}
}

// InvalidateCache clears the cached Rendered output and Height on every
// stored message. Models call this after a terminal resize or theme
// change so the next render recomputes layout-dependent fields.
func (s *MessageStore) InvalidateCache() {
	for _, msg := range s.messages {
		msg.InvalidateCache()
	}
}
