package preprocessor

import (
	"strings"
	"testing"
)

func TestProcessOpenSpecCommandDisabled(t *testing.T) {
	result := ProcessOpenSpecCommand("/opsx:status", OpenSpecOptions{})
	if result.Handled {
		t.Fatal("expected disabled OpenSpec preprocessing to ignore command")
	}
}

func TestProcessOpenSpecCommandIgnoresNonCommand(t *testing.T) {
	result := ProcessOpenSpecCommand("hello", OpenSpecOptions{Enabled: true, WorkingDir: t.TempDir()})
	if result.Handled {
		t.Fatal("expected non-command input to be ignored")
	}
}

func TestProcessOpenSpecCommandStatus(t *testing.T) {
	result := ProcessOpenSpecCommand("/opsx:status", OpenSpecOptions{
		Enabled:    true,
		WorkingDir: t.TempDir(),
	})
	if !result.Handled {
		t.Fatal("expected command to be handled")
	}
	if result.Err != nil {
		t.Fatalf("expected no error, got %v", result.Err)
	}
	if result.CommandType != "status" {
		t.Fatalf("unexpected command type: %q", result.CommandType)
	}
	if !strings.Contains(result.UserInput, "No active OpenSpec changes") {
		t.Fatalf("unexpected user input: %q", result.UserInput)
	}
}
