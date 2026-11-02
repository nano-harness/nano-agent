package runtime

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
)

type testSessionManager struct {
	shutdown bool
}

func (s *testSessionManager) Shutdown() {
	s.shutdown = true
}

func TestNewAgentRuntimeStoresDependencies(t *testing.T) {
	sessions := &testSessionManager{}
	var published []event.StreamEvent
	bus := EventHandler(func(ev event.StreamEvent) {
		published = append(published, ev)
	})

	rt := NewAgentRuntime(nil, nil, sessions, nil, bus)
	if rt == nil {
		t.Fatal("expected runtime")
	}
	if rt.Sessions != sessions {
		t.Fatal("expected sessions dependency to be preserved")
	}

	rt.EventBus.Publish(event.StreamEvent{Type: event.EventTypeContent, Content: "hello"})
	if len(published) != 1 || published[0].Content != "hello" {
		t.Fatalf("expected event to be published, got %#v", published)
	}
}

func TestNilEventHandlerPublishIsNoop(t *testing.T) {
	var handler EventHandler
	handler.Publish(event.StreamEvent{Type: event.EventTypeContent})
}
