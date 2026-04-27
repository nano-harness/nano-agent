package agent

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/llm"
)

func TestContextPackagerCopiesTurnMessages(t *testing.T) {
	turn := newTestTurn()
	turn.Messages = []llm.Message{{Role: "user", Content: "hello"}}

	pkg := NewContextPackager().PackageTurn(turn)
	turn.Messages[0].Content = "changed"

	if pkg.TurnID != turn.ID {
		t.Fatalf("turn id = %q, want %q", pkg.TurnID, turn.ID)
	}
	if pkg.Messages[0].Content != "hello" {
		t.Fatalf("packaged message mutated: %q", pkg.Messages[0].Content)
	}
}

func TestTurnCheckpointRestoresCoreState(t *testing.T) {
	turn := newTestTurn()
	turn.Messages = []llm.Message{{Role: "assistant", Content: "done"}}
	turn.Response.WriteString("done")
	turn.CompletionCriteria.CurrentIteration = 2

	cp := CaptureTurnCheckpoint(turn)
	turn.Messages = nil
	turn.Response.Reset()
	turn.CompletionCriteria.CurrentIteration = 0

	RestoreTurnCheckpoint(turn, cp)
	if len(turn.Messages) != 1 || turn.Messages[0].Content != "done" {
		t.Fatalf("messages not restored: %#v", turn.Messages)
	}
	if turn.Response.String() != "done" {
		t.Fatalf("response = %q, want done", turn.Response.String())
	}
	if turn.CompletionCriteria.CurrentIteration != 2 {
		t.Fatalf("iteration = %d, want 2", turn.CompletionCriteria.CurrentIteration)
	}
}
