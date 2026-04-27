package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestTurnPreprocessInputRunsRoutinePipeline(t *testing.T) {
	turn := newTestTurn()
	turn.UserInput = "/routines list"

	turn.preprocessInput(context.Background(), &config.Config{})

	if !strings.Contains(turn.UserInput, "manage_routine") {
		t.Fatalf("expected routine command to be rewritten, got %q", turn.UserInput)
	}
}

func TestTurnPreprocessInputHandlesNilConfig(t *testing.T) {
	turn := newTestTurn()
	turn.UserInput = "hello"

	turn.preprocessInput(context.Background(), nil)

	if turn.UserInput != "hello" {
		t.Fatalf("expected input to remain unchanged, got %q", turn.UserInput)
	}
}
