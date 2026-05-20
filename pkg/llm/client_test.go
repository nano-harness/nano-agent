package llm

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
)

// TestFinalizeResponse_EmitsCompleteThinkingWithFullReasoning verifies that
// whenever reasoning content was accumulated during a stream, the
// finalizeResponse path emits a thinking event marked with
// Metadata["is_complete"]=true and carrying the full Reasoning. This is the
// "backfill" event that protects against the LLM-side throttle window
// swallowing the very first reasoning frame for fast reasoning models.
func TestFinalizeResponse_EmitsCompleteThinkingWithFullReasoning(t *testing.T) {
	c := &Client{
		model: "test-model",
		config: &config.Config{
			Reasoning: &config.ReasoningConfig{Enabled: true, Effort: "medium"},
		},
	}

	const reasoning = "step 1 → step 2 → answer"
	var events []event.StreamEvent
	onEvent := func(ev event.StreamEvent) { events = append(events, ev) }

	if err := c.finalizeResponse("the answer", reasoning, nil, onEvent, NewTokenStats()); err != nil {
		t.Fatalf("finalizeResponse returned error: %v", err)
	}

	var found bool
	for _, ev := range events {
		if ev.Type != event.EventTypeThinking {
			continue
		}
		if ev.Metadata == nil {
			continue
		}
		done, ok := ev.Metadata["is_complete"].(bool)
		if !ok || !done {
			continue
		}
		if ev.Reasoning != reasoning {
			t.Fatalf("complete thinking event must carry full Reasoning, got %q", ev.Reasoning)
		}
		found = true
	}
	if !found {
		t.Fatalf("expected a thinking event with Metadata[is_complete]=true and full Reasoning; got %#v", events)
	}
}

// TestFinalizeResponse_NoCompleteEventWhenReasoningEmpty verifies that we do
// NOT emit a spurious "is_complete" thinking event when no reasoning was
// produced — that would render as "思考完成 [0 字符]" in the UI.
func TestFinalizeResponse_NoCompleteEventWhenReasoningEmpty(t *testing.T) {
	c := &Client{
		model: "test-model",
		config: &config.Config{
			Reasoning: &config.ReasoningConfig{Enabled: true},
		},
	}

	var events []event.StreamEvent
	onEvent := func(ev event.StreamEvent) { events = append(events, ev) }

	if err := c.finalizeResponse("hello", "", nil, onEvent, NewTokenStats()); err != nil {
		t.Fatalf("finalizeResponse returned error: %v", err)
	}

	for _, ev := range events {
		if ev.Type == event.EventTypeThinking {
			t.Fatalf("did not expect a thinking event for empty reasoning; got %#v", ev)
		}
	}
}

// TestFinalizeResponse_EmitsCompleteEvenWhenReasoningDisabled verifies that
// even if config.Reasoning is not active, the complete thinking event is
// still emitted when reasoning content was actually received from the model.
// This handles providers that return reasoning_content opportunistically.
func TestFinalizeResponse_EmitsCompleteEvenWhenReasoningDisabled(t *testing.T) {
	c := &Client{
		model:  "test-model",
		config: &config.Config{}, // no Reasoning config
	}

	const reasoning = "implicit reasoning"
	var events []event.StreamEvent
	onEvent := func(ev event.StreamEvent) { events = append(events, ev) }

	if err := c.finalizeResponse("hi", reasoning, nil, onEvent, NewTokenStats()); err != nil {
		t.Fatalf("finalizeResponse returned error: %v", err)
	}

	var found bool
	for _, ev := range events {
		if ev.Type == event.EventTypeThinking && ev.Metadata != nil {
			if done, ok := ev.Metadata["is_complete"].(bool); ok && done && ev.Reasoning == reasoning {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected complete thinking event even without explicit reasoning config; got %#v", events)
	}
}
