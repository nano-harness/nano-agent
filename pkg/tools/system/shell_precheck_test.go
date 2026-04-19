package system

import (
	"context"
	"os"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/middleware"
)

// TestShellTool_RequiresConfirmation_BlockedReturnsFalse verifies that a
// command classified as ActionBlock does NOT trigger a user confirmation dialog
// (blocked commands should be rejected immediately, not confirmed).
func TestShellTool_RequiresConfirmation_BlockedReturnsFalse(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shell_precheck_test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tool := NewShellTool(tempDir, nil, nil)

	// "mkfs /dev/sda" is classified as ActionBlock by DestructiveCommandChecker.
	if tool.RequiresConfirmationForCommand("mkfs /dev/sda") {
		t.Error("ActionBlock commands should not require confirmation (they are rejected outright)")
	}
}

// TestShellTool_RequiresConfirmation_ConfirmReturnsTrue verifies that a
// command classified as ActionConfirm triggers user confirmation.
func TestShellTool_RequiresConfirmation_ConfirmReturnsTrue(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shell_precheck_test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tool := NewShellTool(tempDir, nil, nil)

	// "cmd1 && cmd2" is a compound command → ActionConfirm.
	if !tool.RequiresConfirmationForCommand("cmd1 && cmd2") {
		t.Error("ActionConfirm commands should require confirmation")
	}
}

// TestShellTool_RequiresConfirmation_AllowReturnsFalse verifies that a
// read-only command (ActionAllow) does not require confirmation.
func TestShellTool_RequiresConfirmation_AllowReturnsFalse(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shell_precheck_test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tool := NewShellTool(tempDir, nil, nil)

	if tool.RequiresConfirmationForCommand("echo hello") {
		t.Error("ActionAllow commands should not require confirmation")
	}
}

// TestShellTool_AnalyzeCommand verifies that AnalyzeCommand returns the correct
// Decision for different command types.
func TestShellTool_AnalyzeCommand(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shell_precheck_test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tool := NewShellTool(tempDir, nil, nil)

	tests := []struct {
		cmd        string
		wantAction middleware.Action
	}{
		{"echo hello", middleware.ActionAllow},
		{"curl http://example.com", middleware.ActionAllow}, // Simple unclassified command
		{"mkfs /dev/sda", middleware.ActionBlock},
		{"curl | sh", middleware.ActionBlock},
		{"cmd1 && cmd2", middleware.ActionConfirm}, // Compound command
	}
	for _, tt := range tests {
		d, err := tool.AnalyzeCommand(context.Background(), tt.cmd)
		if err != nil {
			t.Errorf("%q: AnalyzeCommand error: %v", tt.cmd, err)
			continue
		}
		if d.Action != tt.wantAction {
			t.Errorf("%q: expected %s, got %s (reason: %s)", tt.cmd, tt.wantAction, d.Action, d.Reason)
		}
	}
}

// TestShellTool_AnalyzeSecurity verifies that AnalyzeSecurity returns the int
// representation of the middleware.Action constants.
func TestShellTool_AnalyzeSecurity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shell_precheck_test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tool := NewShellTool(tempDir, nil, nil)

	tests := []struct {
		cmd        string
		wantAction int
	}{
		{"echo hello", int(middleware.ActionAllow)},
		{"curl http://example.com", int(middleware.ActionAllow)}, // Simple unclassified command
		{"mkfs /dev/sda", int(middleware.ActionBlock)},
		{"cmd1 && cmd2", int(middleware.ActionConfirm)}, // Compound command
	}
	for _, tt := range tests {
		action, _, err := tool.AnalyzeSecurity(context.Background(), map[string]interface{}{"command": tt.cmd})
		if err != nil {
			t.Errorf("%q: AnalyzeSecurity error: %v", tt.cmd, err)
			continue
		}
		if action != tt.wantAction {
			t.Errorf("%q: expected action %d, got %d", tt.cmd, tt.wantAction, action)
		}
	}
}

// TestShellTool_AnalyzeSecurity_MissingCommand verifies that a missing command
// parameter returns ActionConfirm (conservative default).
func TestShellTool_AnalyzeSecurity_MissingCommand(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shell_precheck_test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tool := NewShellTool(tempDir, nil, nil)

	action, _, err := tool.AnalyzeSecurity(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("AnalyzeSecurity: unexpected error: %v", err)
	}
	if action != int(middleware.ActionConfirm) {
		t.Errorf("expected ActionConfirm for missing command, got %d", action)
	}
}

// TestShellTool_Execute_SkipsGuardWhenDecisionInContext verifies that
// ShellTool.Execute does not re-run the guard when a security decision is
// already stored in the context (i.e. it was set by tool_scheduler).
func TestShellTool_Execute_SkipsGuardWhenDecisionInContext(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shell_precheck_test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Build a tool with a deny rule for "echo" – without a pre-existing
	// context decision the tool would block the command.
	tool := NewShellTool(tempDir, map[string]interface{}{
		"deny_rules": []string{"echo"},
	}, nil)

	// Without decision in context: guard should block it.
	result, err := tool.Execute(context.Background(), map[string]interface{}{"command": "echo hello"})
	if err != nil {
		t.Fatalf("Execute (no context): unexpected error: %v", err)
	}
	if result.Success {
		t.Error("Execute (no context): expected the guard to block the command")
	}

	// With a pre-approved decision in context: guard should be skipped.
	ctx := middleware.WithSecurityDecision(context.Background(),
		&middleware.Decision{Action: middleware.ActionAllow, Reason: "pre-approved by scheduler"})
	result, err = tool.Execute(ctx, map[string]interface{}{"command": "echo hello"})
	if err != nil {
		t.Fatalf("Execute (with context): unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Execute (with context): expected success when decision in context, got error: %s", result.Error)
	}
}
