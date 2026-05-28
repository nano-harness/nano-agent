package config

import (
	"fmt"
	"strings"
)

// knownHookEvents is the authoritative set of valid hook event PascalCase names
// (mirrors hookservice.KnownEventNames without importing the package to avoid cycles).
var knownHookEvents = []string{
	"PreToolUse", "PostToolUse", "PostToolUseFailure",
	"UserPromptSubmit", "SessionStart", "SessionEnd",
	"PreCompact", "PostCompact", "Stop", "StopFailure",
	"SubagentStart", "SubagentStop",
	"PermissionRequest", "PermissionDenied", "Notification",
}

// ValidateHooks validates hook configurations and returns errors for invalid configurations.
func ValidateHooks(cfg *HooksConfig) []error {
	var errors []error
	if cfg == nil || len(cfg.Events) == 0 {
		return nil
	}
	for eventName, hooks := range cfg.Events {
		// Validate event name is known.
		if !isKnownHookEventKey(eventName) {
			errors = append(errors, fmt.Errorf("unknown hook event %q under hooks: (known: %s)", eventName, strings.Join(knownHookEvents, ", ")))
			continue
		}
		for i, hook := range hooks {
			if strings.TrimSpace(hook.Command) == "" {
				errors = append(errors, fmt.Errorf("hooks.%s[%d]: command is required", eventName, i))
			}
			if hook.Timeout < 0 {
				errors = append(errors, fmt.Errorf("hooks.%s[%d]: timeout must be >= 0", eventName, i))
			}
		}
	}
	return errors
}

// isKnownHookEventKey checks if a YAML key maps to a known event.
func isKnownHookEventKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")

	switch normalized {
	case "pretooluse", "posttooluse", "posttoolusefailure",
		"sessionstart", "sessionend", "precompact", "postcompact",
		"userpromptsubmit", "stop", "stopfailure",
		"subagentstart", "subagentstop",
		"permissionrequest", "permissiondenied", "notification":
		return true
	}
	return false
}

// ValidateConfig validates the entire configuration and returns errors.
func (c *Config) ValidateConfig() []error {
	var errors []error

	errors = append(errors, ValidateHooks(c.Hooks)...)

	return errors
}
