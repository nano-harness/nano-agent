package policy

import (
	"context"
	"testing"
)

func TestTurnPolicyTerminatesOnContextError(t *testing.T) {
	decision := NewTurnPolicy().Evaluate(TurnState{ContextErr: context.Canceled})
	if decision.Action != ActionTerminate {
		t.Fatalf("action = %s, want %s", decision.Action, ActionTerminate)
	}
	if decision.Metadata["reason"] != "context_done" {
		t.Fatalf("reason = %v, want context_done", decision.Metadata["reason"])
	}
}

func TestTurnPolicyContinuesByDefault(t *testing.T) {
	decision := NewTurnPolicy().Evaluate(TurnState{})
	if decision.Action != ActionContinue {
		t.Fatalf("action = %s, want %s", decision.Action, ActionContinue)
	}
}
