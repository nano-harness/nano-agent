package hookservice

import (
	"context"
	"strings"
	"testing"
	"time"
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
		{Name: "allow", Event: EventPreToolUse, Pattern: "*", Command: "exit 0", Enabled: true},
		{Name: "block", Event: EventPreToolUse, Pattern: "*", Command: "echo blocked >&2; exit 2", Enabled: true},
		{Name: "confirm", Event: EventPreToolUse, Pattern: "*", Command: `printf '{"decision":"confirm","reason":"confirm me"}'`, Enabled: true},
	})

	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", map[string]interface{}{"command": "rm -rf tmp"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionBlock || decision.Rule != "block" || decision.Reason != "blocked" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestServiceStructuredOutputCanConfirm(t *testing.T) {
	service := New([]Hook{
		{
			Name:    "json",
			Event:   EventPreToolUse,
			Pattern: "*",
			Command: `printf '{"hookSpecificOutput":{"permissionDecision":"confirm","permissionDecisionReason":"needs human"}}'`,
			Enabled: true,
		},
	})

	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", map[string]interface{}{"command": "rm -rf tmp"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionConfirm || decision.Reason != "needs human" || decision.Rule != "json" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestServiceStructuredOutputTypedUnionRoundTrip(t *testing.T) {
	// Test that HookSpecificOutput with PermissionRequestDecision round-trips correctly.
	service := New([]Hook{
		{
			Name:    "typed",
			Event:   EventPermissionRequest,
			Pattern: "*",
			Command: `printf '{"hookSpecificOutput":{"decision":{"behavior":"deny","message":"not allowed"}}}'`,
			Enabled: true,
		},
	})

	decision, err := service.Execute(context.Background(), EventPermissionRequest, "run_shell_command", nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionBlock || decision.Reason != "not allowed" || decision.Rule != "typed" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestServiceFlatDecisionFieldsIgnored(t *testing.T) {
	// Flat-field decision/action at top level is silently dropped (no HookSpecificOutput).
	service := New([]Hook{
		{
			Name:    "flat",
			Event:   EventPreToolUse,
			Pattern: "*",
			Command: `printf '{"decision":"block","reason":"old style"}'`,
			Enabled: true,
		},
	})

	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	// Without HookSpecificOutput, the output is treated as allow with no decision.
	if decision.Action != ActionAllow {
		t.Fatalf("expected flat fields to be ignored (allow), got %v", decision.Action)
	}
}

func TestServiceOtherExitCodeWarnsAndAllows(t *testing.T) {
	service := New([]Hook{
		{Name: "warn", Event: EventPreToolUse, Pattern: "*", Command: "echo bad >&2; exit 7", Enabled: true},
	})
	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionAllow || len(decision.Warnings) != 1 || !strings.Contains(decision.Warnings[0], "bad") {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestServiceSendsInputJSONOnStdin(t *testing.T) {
	service := New([]Hook{
		{
			Name:    "stdin",
			Event:   EventPreToolUse,
			Pattern: "*",
			Command: `payload="$(cat)" && printf '%s' "$payload" | grep '"event":"pre_tool_use"' >/dev/null && printf '%s' "$payload" | grep '"tool_name":"run_shell_command"' >/dev/null`,
			Enabled: true,
		},
	})
	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", map[string]interface{}{"command": "pwd"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestServiceTruncatesLargePayload(t *testing.T) {
	service := NewWithOptions([]Hook{
		{
			Name:    "stdin",
			Event:   EventPreToolUse,
			Pattern: "*",
			Command: `cat | grep '"_truncated":true' >/dev/null`,
			Enabled: true,
		},
	}, Options{Timeout: 2 * time.Second})

	large := strings.Repeat("x", (1<<20)+128)
	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", map[string]interface{}{"command": large})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestMatcherAlternation(t *testing.T) {
	service := New([]Hook{
		{Name: "gate", Event: EventPreToolUse, Pattern: "Write|Edit", Command: "exit 2", Enabled: true},
	})

	// Should block Write
	decision, err := service.Execute(context.Background(), EventPreToolUse, "Write", nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if decision.Action != ActionBlock {
		t.Fatalf("expected block for Write, got %v", decision.Action)
	}

	// Should block Edit
	decision, err = service.Execute(context.Background(), EventPreToolUse, "Edit", nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if decision.Action != ActionBlock {
		t.Fatalf("expected block for Edit, got %v", decision.Action)
	}

	// Should allow Read (not in alternation)
	decision, err = service.Execute(context.Background(), EventPreToolUse, "Read", nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("expected allow for Read, got %v", decision.Action)
	}
}

func TestMatcherRegex(t *testing.T) {
	service := New([]Hook{
		{Name: "regex", Event: EventPreToolUse, Pattern: "run_.*_command", Command: "exit 2", Enabled: true},
	})

	// Should block run_shell_command (matches regex)
	decision, err := service.Execute(context.Background(), EventPreToolUse, "run_shell_command", nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if decision.Action != ActionBlock {
		t.Fatalf("expected block for run_shell_command, got %v", decision.Action)
	}

	// Should allow read_file (doesn't match)
	decision, err = service.Execute(context.Background(), EventPreToolUse, "read_file", nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("expected allow for read_file, got %v", decision.Action)
	}
}

func TestMatcherNoTargetEventAlwaysFires(t *testing.T) {
	service := New([]Hook{
		{Name: "stop-hook", Event: EventStop, Pattern: "anything", Command: "exit 0", Enabled: true},
	})

	// Stop event has no matcher target — hook always fires regardless of pattern.
	decision, err := service.Execute(context.Background(), EventStop, "", nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("expected allow, got %v", decision.Action)
	}
}

func TestIsKnownEvent(t *testing.T) {
	if !IsKnownEvent(EventPreToolUse) {
		t.Fatal("EventPreToolUse should be known")
	}
	if !IsKnownEvent(EventPermissionRequest) {
		t.Fatal("EventPermissionRequest should be known")
	}
	if IsKnownEvent(Event("binary_stop")) {
		t.Fatal("binary_stop should not be known")
	}
}

func TestExecutePromotesWellKnownParams(t *testing.T) {
	// Use a hook that echoes stdin JSON back on stdout so we can capture it.
	service := New([]Hook{
		{
			Name:    "echo",
			Event:   EventPermissionRequest,
			Pattern: "*",
			Command: `cat > /dev/null; exit 0`,
			Enabled: true,
		},
	})

	params := map[string]interface{}{
		"session_id":  "S1",
		"input":       map[string]interface{}{"command": "ls"},
		"tool_use_id": "call_1",
		"cwd":         "/project",
	}

	// Directly test promoteWellKnownParams
	input := Input{
		Event:      EventPermissionRequest,
		ToolName:   "Bash",
		Params:     copyParams(params),
		WorkingDir: "/wd",
	}
	promoteWellKnownParams(&input, params)

	if input.SessionID != "S1" {
		t.Fatalf("expected SessionID=S1, got %q", input.SessionID)
	}
	if input.ToolUseID != "call_1" {
		t.Fatalf("expected ToolUseID=call_1, got %q", input.ToolUseID)
	}
	if input.Cwd != "/project" {
		t.Fatalf("expected Cwd=/project, got %q", input.Cwd)
	}
	toolInput, ok := input.ToolInput.(map[string]interface{})
	if !ok {
		t.Fatalf("expected ToolInput to be map, got %T", input.ToolInput)
	}
	if toolInput["command"] != "ls" {
		t.Fatalf("expected ToolInput.command=ls, got %v", toolInput["command"])
	}
	// Params should still be present (backward compat)
	if input.Params["session_id"] != "S1" {
		t.Fatal("params.session_id should be preserved")
	}

	// Verify Execute doesn't panic
	_, err := service.Execute(context.Background(), EventPermissionRequest, "Bash", params)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}

func TestExecuteDoesNotOverridePresetFields(t *testing.T) {
	input := Input{
		Event:     EventPreToolUse,
		SessionID: "explicit",
		ToolUseID: "preset_id",
	}
	params := map[string]interface{}{
		"session_id":  "different",
		"tool_use_id": "other_id",
	}
	promoteWellKnownParams(&input, params)

	if input.SessionID != "explicit" {
		t.Fatalf("expected SessionID=explicit, got %q", input.SessionID)
	}
	if input.ToolUseID != "preset_id" {
		t.Fatalf("expected ToolUseID=preset_id, got %q", input.ToolUseID)
	}
}

func TestExecutePromoteIgnoresNonStringValues(t *testing.T) {
	input := Input{Event: EventPreToolUse}
	params := map[string]interface{}{
		"session_id":  123,
		"tool_use_id": true,
		"cwd":         []string{"/a"},
	}
	promoteWellKnownParams(&input, params)

	if input.SessionID != "" {
		t.Fatalf("expected empty SessionID for non-string, got %q", input.SessionID)
	}
	if input.ToolUseID != "" {
		t.Fatalf("expected empty ToolUseID for non-string, got %q", input.ToolUseID)
	}
	if input.Cwd != "" {
		t.Fatalf("expected empty Cwd for non-string, got %q", input.Cwd)
	}
}

func TestExecutePromoteNilParams(t *testing.T) {
	input := Input{Event: EventPreToolUse}
	promoteWellKnownParams(&input, nil)
	// Should not panic
	if input.SessionID != "" {
		t.Fatal("expected empty SessionID for nil params")
	}
}

func TestIsToolEvent(t *testing.T) {
	if !IsToolEvent(EventPreToolUse) {
		t.Fatal("PreToolUse should be a tool event")
	}
	if !IsToolEvent(EventPermissionRequest) {
		t.Fatal("PermissionRequest should be a tool event")
	}
	if IsToolEvent(EventStop) {
		t.Fatal("Stop should not be a tool event")
	}
	if IsToolEvent(EventNotification) {
		t.Fatal("Notification should not be a tool event")
	}
}

// TestSanitizeHookEnv verifies that credential-bearing variables are stripped
// from the environment before hook subprocesses are launched.
func TestSanitizeHookEnv(t *testing.T) {
	input := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/root",
		"OPENAI_API_KEY=sk-secret",
		"NANO_ANTHROPIC_API_KEY=ant-secret",
		"GITHUB_TOKEN=ghp_secret",
		"DB_PASSWORD=letmein",
		"AWS_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE",
		"MY_CREDENTIALS=top-secret",
		"MY_PRIVATE_KEY=pem-data",
		"APP_SECRET=mysecret",
		"NANO_SESSION_ID=sess-123",
		"TERM=xterm",
	}

	result := sanitizeHookEnv(input)

	// Variables that must be preserved.
	wantPresent := []string{"PATH=/usr/bin:/bin", "HOME=/root", "NANO_SESSION_ID=sess-123", "TERM=xterm"}
	for _, want := range wantPresent {
		found := false
		for _, got := range result {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("sanitizeHookEnv: expected %q to be present", want)
		}
	}

	// Variables that must be stripped.
	wantAbsent := []string{
		"OPENAI_API_KEY=sk-secret",
		"NANO_ANTHROPIC_API_KEY=ant-secret",
		"GITHUB_TOKEN=ghp_secret",
		"DB_PASSWORD=letmein",
		"AWS_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE",
		"MY_CREDENTIALS=top-secret",
		"MY_PRIVATE_KEY=pem-data",
		"APP_SECRET=mysecret",
	}
	for _, absent := range wantAbsent {
		for _, got := range result {
			if got == absent {
				t.Errorf("sanitizeHookEnv: expected %q to be stripped, but it was present", absent)
			}
		}
	}
}
