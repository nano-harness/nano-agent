package agent

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
)

func TestTurnEventEmitterAddsTurnID(t *testing.T) {
	var events []event.StreamEvent
	turn := newTestTurn()
	turn.eventHandler = func(ev event.StreamEvent) {
		events = append(events, ev)
	}

	turn.events().plannerDecision("request_llm", map[string]interface{}{"iteration": 1})

	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != event.EventTypePlannerDecision {
		t.Fatalf("event type = %q, want %q", events[0].Type, event.EventTypePlannerDecision)
	}
	if events[0].Metadata["turn_id"] != turn.ID {
		t.Fatalf("turn_id metadata = %v, want %s", events[0].Metadata["turn_id"], turn.ID)
	}
	if events[0].Metadata["iteration"] != 1 {
		t.Fatalf("iteration metadata = %v, want 1", events[0].Metadata["iteration"])
	}
	payload, ok := events[0].Payload.(event.PlannerDecisionPayload)
	if !ok {
		t.Fatalf("payload = %T, want PlannerDecisionPayload", events[0].Payload)
	}
	if payload.TurnID != turn.ID || payload.Decision != "request_llm" {
		t.Fatalf("payload = %#v", payload)
	}
}
