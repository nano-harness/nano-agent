package agent

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/middleware"
)

type policySecurityTool struct {
	*testTool
	decision *middleware.Decision
	err      error
}

func (t *policySecurityTool) AnalyzeSecurityDecision(context.Context, map[string]interface{}) (*middleware.Decision, error) {
	return t.decision, t.err
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
		decision: &middleware.Decision{
			Action: middleware.ActionConfirm,
			Reason: "compound command",
		},
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

func TestToolPolicyPreflightPreservesFullSecurityDecision(t *testing.T) {
	scheduler := NewToolSchedulerWithOptions(ToolSchedulerOptions{})
	tool := &policySecurityTool{
		testTool: &testTool{name: "run_shell_command"},
		decision: &middleware.Decision{
			Action:         middleware.ActionAllow,
			Reason:         "rewritten by hook",
			ModifiedParams: map[string]interface{}{"command": "git status"},
		},
	}

	preflight := scheduler.policyEngine().PreflightTool(context.Background(), "run_shell_command", map[string]interface{}{"command": "custom"}, tool)
	if !preflight.SecurityAnalysis.Supported || preflight.SecurityAnalysis.Decision == nil {
		t.Fatalf("expected security analysis decision, got %#v", preflight.SecurityAnalysis)
	}
	if preflight.SecurityAnalysis.Decision.ModifiedParams["command"] != "git status" {
		t.Fatalf("modified params were not preserved: %#v", preflight.SecurityAnalysis.Decision)
	}
}
