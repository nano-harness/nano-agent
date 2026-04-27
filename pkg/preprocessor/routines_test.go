package preprocessor

import (
	"strings"
	"testing"
)

func TestRewriteRoutinesCommandIgnoresNonRoutineInput(t *testing.T) {
	input := "hello"
	got, ok := RewriteRoutinesCommand(input)
	if ok {
		t.Fatal("expected non-routine input to be ignored")
	}
	if got != input {
		t.Fatalf("expected original input, got %q", got)
	}
}

func TestRewriteRoutinesCommandList(t *testing.T) {
	got, ok := RewriteRoutinesCommand("/routines")
	if !ok {
		t.Fatal("expected routine command to be handled")
	}
	if got != "Please list all routine tasks by calling manage_routine with action='list'." {
		t.Fatalf("unexpected rewrite: %q", got)
	}
}

func TestRewriteRoutinesCommandAdd(t *testing.T) {
	got, ok := RewriteRoutinesCommand("/routines add every 5 minutes run go test")
	if !ok {
		t.Fatal("expected routine command to be handled")
	}
	if !strings.Contains(got, "every 5 minutes run go test") || !strings.Contains(got, "action='create'") {
		t.Fatalf("unexpected rewrite: %q", got)
	}
}

func TestRewriteRoutinesCommandUsage(t *testing.T) {
	got, ok := RewriteRoutinesCommand("/routines run")
	if !ok {
		t.Fatal("expected routine command to be handled")
	}
	if got != "Usage: /routines run <id>" {
		t.Fatalf("unexpected usage rewrite: %q", got)
	}
}
