package permission

import (
	"context"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/hookservice"
)

// ── A9: normalizeCommandForOverride ───────────────────────────────────────────

// TestNormalizeCommandForOverride verifies that the normalization function
// collapses whitespace so that differently-formatted versions of the same
// override entry still match.
func TestNormalizeCommandForOverride_CollapseWhitespace(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"  git   status  ", "git status"},
		{"git\tstatus", "git status"},
		{"git  log  --oneline", "git log --oneline"},
		{"git status", "git status"},
		{"", ""},
	}
	for _, tc := range cases {
		got := normalizeCommandForOverride(tc.input)
		if got != tc.want {
			t.Errorf("normalizeCommandForOverride(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestNormalizeCommandForOverride_SymmetricMatch verifies that two strings that
// differ only in internal whitespace are equal after normalization.
func TestNormalizeCommandForOverride_SymmetricMatch(t *testing.T) {
	a := "git  log  --oneline"
	b := "git log --oneline"
	if normalizeCommandForOverride(a) != normalizeCommandForOverride(b) {
		t.Errorf("expected %q and %q to normalize to the same string", a, b)
	}
}

// ── A9: FirewallHook.Execute override path ─────────────────────────────────────

// TestFirewallHook_OverrideAllowsNormalizedMatch verifies that a command is
// allowed when an override entry matches after whitespace normalization (A9
// fix: without normalization, extra spaces in the config entry would silently
// fail to match the command string as received).
func TestFirewallHook_OverrideAllowsNormalizedMatch(t *testing.T) {
	cfg := FirewallConfig{
		Enabled:           true,
		SeverityThreshold: SeverityMedium,
		FailurePolicy:     "block",
		// Override entry has extra whitespace; must still match "rm -rf /tmp/safe".
		Overrides: []string{"rm  -rf  /tmp/safe"},
	}
	hook := NewFirewallHook(cfg)

	decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse,
		"run_shell_command", map[string]interface{}{"command": "rm -rf /tmp/safe"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if decision.Action != hookservice.ActionAllow {
		t.Errorf("override should allow the command; got action=%v reason=%q", decision.Action, decision.Reason)
	}
	if !strings.Contains(decision.Reason, "override") {
		t.Errorf("reason should mention 'override'; got %q", decision.Reason)
	}
}

// TestFirewallHook_DangerousCommandBlocked verifies that a dangerous command is
// blocked (not allowed) when the failure policy is "block".
func TestFirewallHook_DangerousCommandBlocked(t *testing.T) {
	cfg := FirewallConfig{
		Enabled:           true,
		SeverityThreshold: SeverityMedium,
		FailurePolicy:     "block",
	}
	hook := NewFirewallHook(cfg)

	decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse,
		"run_shell_command", map[string]interface{}{"command": "rm -rf /"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if decision.Action != hookservice.ActionBlock {
		t.Errorf("dangerous command should be blocked; got action=%v", decision.Action)
	}
}

// TestFirewallHook_NonShellToolAlwaysAllowed verifies that non-shell tools pass
// through the firewall unconditionally.
func TestFirewallHook_NonShellToolAlwaysAllowed(t *testing.T) {
	cfg := FirewallConfig{
		Enabled:           true,
		SeverityThreshold: SeverityLow,
		FailurePolicy:     "block",
	}
	hook := NewFirewallHook(cfg)

	decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse,
		"read_file", map[string]interface{}{"file_path": "/etc/passwd"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if decision.Action != hookservice.ActionAllow {
		t.Errorf("non-shell tool should always be allowed; got action=%v", decision.Action)
	}
}

// TestFirewallHook_SafeCommandAllowed verifies that a safe command is allowed.
func TestFirewallHook_SafeCommandAllowed(t *testing.T) {
	cfg := FirewallConfig{
		Enabled:           true,
		SeverityThreshold: SeverityMedium,
		FailurePolicy:     "block",
	}
	hook := NewFirewallHook(cfg)

	decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse,
		"run_shell_command", map[string]interface{}{"command": "echo hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if decision.Action != hookservice.ActionAllow {
		t.Errorf("safe command should be allowed; got action=%v", decision.Action)
	}
}

// TestFirewallHook_SeverityThresholdFiltering verifies that a command below the
// configured severity threshold is not blocked.
func TestFirewallHook_SeverityThresholdFiltering(t *testing.T) {
	// Set threshold to High — medium-severity patterns must pass through.
	cfg := FirewallConfig{
		Enabled:           true,
		SeverityThreshold: SeverityHigh,
		FailurePolicy:     "block",
	}
	hook := NewFirewallHook(cfg)

	// find with -delete is medium severity (A3 addition); should not be blocked
	// when threshold=High.
	decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse,
		"run_shell_command", map[string]interface{}{"command": "find /tmp -name '*.tmp' -delete"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if decision.Action == hookservice.ActionBlock {
		t.Errorf("medium-severity command should pass when threshold=High; got action=%v reason=%q",
			decision.Action, decision.Reason)
	}
}

// TestFirewallHook_ConfirmPolicyOnDangerous verifies that FailurePolicy=confirm
// produces ActionConfirm rather than ActionBlock.
func TestFirewallHook_ConfirmPolicyOnDangerous(t *testing.T) {
	cfg := FirewallConfig{
		Enabled:           true,
		SeverityThreshold: SeverityMedium,
		FailurePolicy:     "confirm",
	}
	hook := NewFirewallHook(cfg)

	decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse,
		"run_shell_command", map[string]interface{}{"command": "rm -rf /"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if decision.Action != hookservice.ActionConfirm {
		t.Errorf("confirm policy should produce ActionConfirm; got action=%v", decision.Action)
	}
}

// TestFirewallHook_BashToolAlsoCovered verifies that the "bash" tool name (in
// addition to "run_shell_command") is intercepted by the firewall.
func TestFirewallHook_BashToolAlsoCovered(t *testing.T) {
	cfg := FirewallConfig{
		Enabled:           true,
		SeverityThreshold: SeverityMedium,
		FailurePolicy:     "block",
	}
	hook := NewFirewallHook(cfg)

	decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse,
		"bash", map[string]interface{}{"command": "rm -rf /"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if decision.Action != hookservice.ActionBlock {
		t.Errorf("bash tool dangerous command should be blocked; got action=%v", decision.Action)
	}
}
