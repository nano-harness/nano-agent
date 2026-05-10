package hookservice

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

func TestServiceAllowsWhenNoHooksConfigured(t *testing.T) {
	decision, err := New(nil).Execute(context.Background(), EventPreToolUse, "run_shell_command", nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionAllow || decision.Reason != "no hooks configured" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestServiceReturnsFirstNonAllowDecision(t *testing.T) {
	service := New([]Hook{
		{Name: "allow", Event: EventPreToolUse, Pattern: "run_shell_command:*", Command: "exit 0", Enabled: true},
		{Name: "block", Event: EventPreToolUse, Pattern: "run_shell_command:*", Command: "echo blocked >&2; exit 2", Enabled: true},
		{Name: "confirm", Event: EventPreToolUse, Pattern: "run_shell_command:*", Command: "exit 1", Enabled: true},
	})

	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", map[string]interface{}{"command": "rm -rf tmp"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionBlock || decision.Rule != "block" || decision.Reason != "blocked" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestServiceTimeoutFallsBackToConfirm(t *testing.T) {
	service := NewWithOptions([]Hook{
		{Name: "slow", Event: EventPreToolUse, Pattern: "*", Command: "sleep 1", Enabled: true},
	}, Options{Timeout: 10 * time.Millisecond})

	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionConfirm || !strings.Contains(decision.Reason, "slow") {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestServiceEnvironmentWhitelist(t *testing.T) {
	t.Setenv("NANO_HOOK_ALLOWED", "yes")
	t.Setenv("NANO_HOOK_BLOCKED", "no")

	service := NewWithOptions([]Hook{
		{
			Name:    "env",
			Event:   EventPreToolUse,
			Pattern: "*",
			Command: `[ "$NANO_HOOK_ALLOWED" = yes ] && [ -z "$NANO_HOOK_BLOCKED" ] && [ "$NANO_TOOL_NAME" = run_shell_command ] && printf '%s' "$NANO_TOOL_INPUT" | grep '"command":"pwd"' >/dev/null`,
			Enabled: true,
		},
	}, Options{EnvWhitelist: []string{"NANO_HOOK_ALLOWED"}})

	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", map[string]interface{}{"command": "pwd"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("unexpected decision: %#v", decision)
	}

	if os.Getenv("NANO_HOOK_BLOCKED") != "no" {
		t.Fatal("test setup unexpectedly mutated process environment")
	}
}

func TestServiceInjectsStructuredHookInput(t *testing.T) {
	service := NewWithOptions([]Hook{
		{
			Name:    "input",
			Event:   EventPreToolUse,
			Pattern: "*",
			Command: `printf '%s' "$NANO_HOOK_INPUT" | grep '"event":"pre_tool_use"' >/dev/null && printf '%s' "$NANO_HOOK_INPUT" | grep '"tool_name":"run_shell_command"' >/dev/null`,
			Enabled: true,
		},
	}, Options{EnvWhitelist: []string{"PATH"}})

	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", map[string]interface{}{"command": "pwd"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestServiceInjectsRalphHookInput(t *testing.T) {
	service := NewWithOptions([]Hook{
		{
			Name:    "input",
			Event:   EventStop,
			Pattern: "*",
			Command: `printf '%s' "$NANO_HOOK_INPUT" | grep '"transcript_path":"/tmp/transcript.jsonl"' >/dev/null && printf '%s' "$NANO_HOOK_INPUT" | grep '"stop_hook_active":true' >/dev/null && printf '%s' "$NANO_HOOK_INPUT" | grep '"iteration":3' >/dev/null`,
			Enabled: true,
		},
	}, Options{EnvWhitelist: []string{"PATH"}})

	decision, err := service.Execute(context.Background(), EventStop, "turn_stop", map[string]interface{}{
		"session_id":       "s1",
		"transcript_path":  "/tmp/transcript.jsonl",
		"stop_hook_active": true,
		"iteration":        3,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestServiceStructuredHookOutputCanBlock(t *testing.T) {
	service := New([]Hook{
		{
			Name:    "json",
			Event:   EventPreToolUse,
			Pattern: "*",
			Command: `printf '{"action":"block","reason":"json denied","warnings":["use read-only command"],"audit_metadata":{"risk":"high"}}'`,
			Enabled: true,
		},
	})

	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", map[string]interface{}{"command": "rm -rf tmp"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionBlock || decision.Reason != "json denied" || decision.Rule != "json" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	if len(decision.Warnings) != 1 || decision.Warnings[0] != "use read-only command" {
		t.Fatalf("unexpected warnings: %#v", decision.Warnings)
	}
	if decision.AuditMetadata["risk"] != "high" {
		t.Fatalf("unexpected audit metadata: %#v", decision.AuditMetadata)
	}
}

func TestServiceStructuredDecisionBlockTakesPriority(t *testing.T) {
	service := New([]Hook{
		{
			Name:    "json",
			Event:   EventStop,
			Pattern: "*",
			Command: `printf '{"action":"allow","decision":"block","reason":"keep going"}'`,
			Enabled: true,
		},
	})

	decision, err := service.Execute(context.Background(), EventStop, "turn_stop", nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionBlock || decision.Reason != "keep going" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestServiceOnceHookSkippedAfterFirstExecution(t *testing.T) {
	dir := t.TempDir()
	marker := dir + "/count"
	service := New([]Hook{
		{
			Name:    "once",
			Event:   EventPreToolUse,
			Pattern: "*",
			Command: "echo run >> " + marker,
			Enabled: true,
			Once:    true,
		},
	})
	for i := 0; i < 2; i++ {
		decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", nil)
		if err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
		if decision.Action != ActionAllow {
			t.Fatalf("unexpected decision: %#v", decision)
		}
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if strings.Count(string(data), "run") != 1 {
		t.Fatalf("once hook executed more than once: %q", data)
	}
}

func TestServiceStructuredHookOutputCanModifyParams(t *testing.T) {
	service := New([]Hook{
		{
			Name:    "rewrite",
			Event:   EventPreToolUse,
			Pattern: "*",
			Command: `printf '{"action":"modify_params","modified_params":{"command":"git status","description":"safe rewrite"}}'`,
			Enabled: true,
		},
	})

	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", map[string]interface{}{"command": "git diff"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionAllow || decision.ModifiedParams["command"] != "git status" || decision.ModifiedParams["description"] != "safe rewrite" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestServiceFailurePolicyAllow(t *testing.T) {
	service := New([]Hook{
		{
			Name:          "allow-failure",
			Event:         EventPreToolUse,
			Pattern:       "*",
			Command:       "exit 7",
			Enabled:       true,
			FailurePolicy: FailurePolicyAllow,
		},
	})

	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	if decision.AuditMetadata["failure_policy"] != string(FailurePolicyAllow) {
		t.Fatalf("unexpected audit metadata: %#v", decision.AuditMetadata)
	}
}

func TestServiceRegistersAndListsLifecycleHooks(t *testing.T) {
	service := New(nil)
	service.Register(Hook{Name: "start", Event: EventSessionStart, Pattern: "*", Command: "exit 0", Enabled: true})
	service.Register(Hook{Name: "pre", Event: EventPreToolUse, Pattern: "*", Command: "exit 0", Enabled: true})

	lifecycleHooks := service.HooksForEvent(EventSessionStart)
	if len(lifecycleHooks) != 1 || lifecycleHooks[0].Name != "start" {
		t.Fatalf("unexpected lifecycle hooks: %#v", lifecycleHooks)
	}

	allHooks := service.Hooks()
	if len(allHooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(allHooks))
	}
	allHooks[0].Name = "mutated"
	if service.Hooks()[0].Name != "start" {
		t.Fatal("expected Hooks to return a defensive snapshot")
	}
}

func TestServiceRunsHookThroughSandboxRuntime(t *testing.T) {
	rt := &recordingRuntime{}
	service := NewWithOptions([]Hook{
		{Name: "sandboxed", Event: EventPreToolUse, Pattern: "*", Command: "exit 0", Enabled: true},
	}, Options{SandboxRuntime: rt, WorkingDir: "/tmp"})

	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	if rt.prepareCount != 1 || rt.cleanupCount != 1 {
		t.Fatalf("sandbox prepare/cleanup counts = %d/%d, want 1/1", rt.prepareCount, rt.cleanupCount)
	}
	if rt.lastReq.WorkingDir != "/tmp" || rt.lastReq.Metadata["hook"] != "sandboxed" {
		t.Fatalf("unexpected sandbox request: %#v", rt.lastReq)
	}
}

type recordingRuntime struct {
	prepareCount int
	cleanupCount int
	lastReq      sandbox.SandboxRequest
}

func (r *recordingRuntime) PrepareCommand(_ context.Context, req sandbox.SandboxRequest) (*sandbox.SandboxEnvironment, error) {
	r.prepareCount++
	r.lastReq = req
	return &sandbox.SandboxEnvironment{
		Backend:    sandbox.BackendNone,
		Command:    req.Command,
		Args:       req.Args,
		WorkingDir: req.WorkingDir,
	}, nil
}

func (r *recordingRuntime) Cleanup(_ context.Context, _ *sandbox.SandboxEnvironment) error {
	r.cleanupCount++
	return nil
}
