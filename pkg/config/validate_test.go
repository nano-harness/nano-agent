package config

import (
	"strings"
	"testing"
)

func TestValidateHooks_AllowsNil(t *testing.T) {
	if errs := ValidateHooks(nil); len(errs) != 0 {
		t.Fatalf("ValidateHooks(nil) errors=%v, want none", errs)
	}
}

func TestValidateHooks_RequiresCommand(t *testing.T) {
	cfg := &HooksConfig{
		Events: map[string][]HookCommand{
			"Stop": {{
				Matcher: "*",
				Command: "",
				Timeout: 0,
			}},
		},
	}
	errs := ValidateHooks(cfg)
	if len(errs) == 0 {
		t.Fatalf("expected validation error, got none")
	}
}

func TestValidateHooks_RejectsNegativeTimeout(t *testing.T) {
	cfg := &HooksConfig{
		Events: map[string][]HookCommand{
			"Stop": {{
				Matcher: "*",
				Command: "echo hi",
				Timeout: -1,
			}},
		},
	}
	errs := ValidateHooks(cfg)
	if len(errs) == 0 {
		t.Fatalf("expected validation error, got none")
	}
}

func TestValidateHooks_RejectsUnknownEvent(t *testing.T) {
	cfg := &HooksConfig{
		Events: map[string][]HookCommand{
			"BinaryStop": {{
				Matcher: "*",
				Command: "echo hi",
				Timeout: 5,
			}},
		},
	}
	errs := ValidateHooks(cfg)
	if len(errs) == 0 {
		t.Fatalf("expected validation error for unknown event, got none")
	}
	if !strings.Contains(errs[0].Error(), "unknown hook event") {
		t.Fatalf("unexpected error: %v", errs[0])
	}
}

func TestValidateHooks_AcceptsAllKnownEvents(t *testing.T) {
	events := []string{
		"PreToolUse", "PostToolUse", "PostToolUseFailure",
		"UserPromptSubmit", "SessionStart", "SessionEnd",
		"PreCompact", "PostCompact", "Stop", "StopFailure",
		"SubagentStart", "SubagentStop",
		"PermissionRequest", "PermissionDenied", "Notification",
	}
	for _, ev := range events {
		cfg := &HooksConfig{
			Events: map[string][]HookCommand{
				ev: {{Matcher: "*", Command: "echo ok", Timeout: 5}},
			},
		}
		errs := ValidateHooks(cfg)
		if len(errs) != 0 {
			t.Errorf("event %q should be valid, got errors: %v", ev, errs)
		}
	}
}

func TestValidateHooks_CaseInsensitiveEventNames(t *testing.T) {
	variants := []string{"stop", "STOP", "pre_tool_use", "Pre-Tool-Use", "permission_request"}
	for _, v := range variants {
		cfg := &HooksConfig{
			Events: map[string][]HookCommand{
				v: {{Matcher: "*", Command: "echo ok", Timeout: 5}},
			},
		}
		errs := ValidateHooks(cfg)
		if len(errs) != 0 {
			t.Errorf("event %q should be valid (case-insensitive), got errors: %v", v, errs)
		}
	}
}
