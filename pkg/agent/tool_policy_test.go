package agent

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/middleware"
)

type policySecurityTool struct {
	*testTool
	action int
	reason string
	err    error
}

func (t *policySecurityTool) AnalyzeSecurity(context.Context, map[string]interface{}) (int, string, error) {
	return t.action, t.reason, t.err
}

func TestToolPolicyPreflightRespectsAllowlist(t *testing.T) {
	scheduler := NewToolSchedulerWithOptions(ToolSchedulerOptions{})
	scheduler.SetAllowedTools([]string{"allowed_*"})

	preflight := scheduler.policyEngine().PreflightTool(context.Background(), "denied_tool", nil, &testTool{name: "denied_tool"})
	if !preflight.HasAllowPolicy {
		t.Fatal("expected allow policy to be active")
	}
	if preflight.Allowed {
		t.Fatal("expected denied tool to be rejected")
	}
	if preflight.SecurityAnalysis.Supported {
		t.Fatal("expected security analysis to be skipped for denied tools")
	}
}

func TestToolPolicyPreflightCollectsApprovalAndSecurity(t *testing.T) {
	scheduler := NewToolSchedulerWithOptions(ToolSchedulerOptions{})
	scheduler.SetAllowedTools([]string{"run_shell_command"})
	tool := &policySecurityTool{
		testTool: &testTool{name: "run_shell_command"},
		action:   int(middleware.ActionConfirm),
		reason:   "compound command",
	}

	preflight := scheduler.policyEngine().PreflightTool(context.Background(), "run_shell_command", map[string]interface{}{"command": "a && b"}, tool)
	if !preflight.HasAllowPolicy || !preflight.Allowed {
		t.Fatalf("expected allowed preflight, got %#v", preflight)
	}
	if preflight.RequiresApproval {
		t.Fatal("testTool does not require approval before security analysis")
	}
	if !preflight.SecurityAnalysis.Supported {
		t.Fatal("expected security analysis to run")
	}
	if preflight.SecurityAnalysis.Decision == nil || preflight.SecurityAnalysis.Decision.Action != middleware.ActionConfirm {
		t.Fatalf("unexpected security decision: %#v", preflight.SecurityAnalysis.Decision)
	}
}
