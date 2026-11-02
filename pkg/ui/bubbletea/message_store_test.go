package bubbletea

import (
	"testing"
)

func TestMessageStore_AddGetLast(t *testing.T) {
	s := NewMessageStore()
	if s.Len() != 0 {
		t.Fatalf("new store should be empty, got Len=%d", s.Len())
	}
	if s.Last() != nil {
		t.Fatalf("Last() on empty store should be nil, got %+v", s.Last())
	}
	if s.Get(0) != nil {
		t.Fatalf("Get(0) on empty store should be nil")
	}

	first := s.Add("user", "hello")
	if first == nil {
		t.Fatal("Add returned nil")
	}
	if first.Role != "user" || first.Content != "hello" {
		t.Fatalf("Add did not seed fields, got role=%q content=%q", first.Role, first.Content)
	}
	if first.ID == "" {
		t.Fatal("Add should assign a non-empty ID")
	}

	second := s.Add("assistant", "world")
	if second.ID == first.ID {
		t.Fatalf("Add should produce unique IDs, got duplicate %q", first.ID)
	}

	if s.Len() != 2 {
		t.Fatalf("expected Len=2 after two Adds, got %d", s.Len())
	}
	if got := s.Get(0); got != first {
		t.Fatalf("Get(0) should return first message")
	}
	if got := s.Get(1); got != second {
		t.Fatalf("Get(1) should return second message")
	}
	if got := s.Last(); got != second {
		t.Fatalf("Last() should return the most recently added message")
	}

	if s.Get(-1) != nil {
		t.Fatal("Get(-1) should be nil")
	}
	if s.Get(99) != nil {
		t.Fatal("Get(out-of-range) should be nil")
	}
}

func TestMessageStore_AddMessagePreservesPointer(t *testing.T) {
	s := NewMessageStore()
	msg := NewFormattedMessage("custom-id", "tool", "result")
	s.AddMessage(msg)
	if s.Last() != msg {
		t.Fatal("AddMessage should store the exact pointer provided")
	}
	if s.Get(0).ID != "custom-id" {
		t.Fatalf("AddMessage should preserve custom ID, got %q", s.Get(0).ID)
	}

	// AddMessage(nil) is a no-op.
	s.AddMessage(nil)
	if s.Len() != 1 {
		t.Fatalf("AddMessage(nil) should not grow the store, got Len=%d", s.Len())
	}
}

func TestMessageStore_Range(t *testing.T) {
	s := NewMessageStore()
	s.Add("user", "one")
	s.Add("assistant", "two")
	s.Add("tool", "three")

	// Full traversal collects every message in order.
	var roles []string
	s.Range(func(_ int, msg *FormattedMessage) bool {
		roles = append(roles, msg.Role)
		return true
	})
	want := []string{"user", "assistant", "tool"}
	if len(roles) != len(want) {
		t.Fatalf("Range visited %d messages, want %d", len(roles), len(want))
	}
	for i, r := range want {
		if roles[i] != r {
			t.Fatalf("Range[%d] = %q, want %q", i, roles[i], r)
		}
	}

	// Range stops early when the callback returns false.
	visited := 0
	s.Range(func(_ int, _ *FormattedMessage) bool {
		visited++
		return visited < 2
	})
	if visited != 2 {
		t.Fatalf("Range should stop after callback returns false; visited=%d", visited)
	}

	// Indices are passed in insertion order.
	var indices []int
	s.Range(func(i int, _ *FormattedMessage) bool {
		indices = append(indices, i)
		return true
	})
	for i, idx := range indices {
		if idx != i {
			t.Fatalf("Range index[%d]=%d, want %d", i, idx, i)
		}
	}
}

func TestMessageStore_InvalidateCache(t *testing.T) {
	s := NewMessageStore()
	a := s.Add("user", "hello")
	b := s.Add("assistant", "world")
	a.SetRendered("RA\nRA")
	b.SetRendered("RB\nRB\nRB")
	if a.Rendered == "" || b.Rendered == "" || a.Height == 0 || b.Height == 0 {
		t.Fatal("precondition: messages should have cached Rendered/Height")
	}

	s.InvalidateCache()

	if a.Rendered != "" || a.Height != 0 {
		t.Fatalf("InvalidateCache should clear Rendered/Height on first message, got Rendered=%q Height=%d", a.Rendered, a.Height)
	}
	if b.Rendered != "" || b.Height != 0 {
		t.Fatalf("InvalidateCache should clear Rendered/Height on second message, got Rendered=%q Height=%d", b.Rendered, b.Height)
	}
}

func TestFormattedMessage_InvalidateCache(t *testing.T) {
	msg := NewFormattedMessage("id-1", "assistant", "raw content")
	msg.SetRendered("rendered\noutput")
	if msg.Rendered == "" || msg.Height == 0 {
		t.Fatal("precondition: SetRendered should populate Rendered and Height")
	}

	// Content is preserved; only the rendered cache is cleared.
	msg.InvalidateCache()

	if msg.Rendered != "" {
		t.Fatalf("InvalidateCache should clear Rendered, got %q", msg.Rendered)
	}
	if msg.Height != 0 {
		t.Fatalf("InvalidateCache should reset Height to 0, got %d", msg.Height)
	}
	if msg.Content != "raw content" {
		t.Fatalf("InvalidateCache must not touch Content, got %q", msg.Content)
	}
	if msg.ID != "id-1" || msg.Role != "assistant" {
		t.Fatalf("InvalidateCache must not touch ID/Role, got id=%q role=%q", msg.ID, msg.Role)
	}
}
