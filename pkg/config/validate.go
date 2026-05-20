package config

import (
	"fmt"
	"strings"
)

// ValidateHooks validates hook configurations and returns errors for invalid configurations.
func ValidateHooks(hooks []HookConfig) []error {
	var errors []error

	for i, hook := range hooks {
		if !hook.Enabled {
			continue
		}

		// Validate hook type
		hookType := strings.ToLower(strings.TrimSpace(hook.Type))
		if hookType == "" {
			hookType = "command" // default
		}

		switch hookType {
		case "command":
			// Command hooks are always valid (can have empty command)
		case "http":
			if hook.HTTP == nil {
				errors = append(errors, fmt.Errorf("hook %q (index %d): type=http but http config is missing", hook.Name, i))
			} else {
				if hook.HTTP.URL == "" {
					errors = append(errors, fmt.Errorf("hook %q (index %d): type=http requires http.url to be set", hook.Name, i))
				}
				if len(hook.HTTP.URLAllowlist) == 0 {
					// This is a warning in the design doc, but we'll log it as an error for safety
					errors = append(errors, fmt.Errorf("hook %q (index %d): type=http should have http.url_allowlist set for security (see docs/features/HOOKS.md)", hook.Name, i))
				}
			}
		case "prompt":
			if hook.Prompt == nil {
				errors = append(errors, fmt.Errorf("hook %q (index %d): type=prompt but prompt config is missing", hook.Name, i))
			} else if hook.Prompt.Prompt == "" {
				errors = append(errors, fmt.Errorf("hook %q (index %d): type=prompt requires prompt.prompt to be set", hook.Name, i))
			}
		case "agent":
			if hook.Agent == nil {
				errors = append(errors, fmt.Errorf("hook %q (index %d): type=agent but agent config is missing", hook.Name, i))
			} else if hook.Agent.Agent == "" {
				errors = append(errors, fmt.Errorf("hook %q (index %d): type=agent requires agent.agent to be set", hook.Name, i))
			}
		default:
			errors = append(errors, fmt.Errorf("hook %q (index %d): unknown type %q (must be command, http, prompt, or agent)", hook.Name, i, hookType))
		}
	}

	return errors
}

// ValidateConfig validates the entire configuration and returns errors.
func (c *Config) ValidateConfig() []error {
	var errors []error

	if c.Security != nil {
		hookErrors := ValidateHooks(c.Security.Hooks)
		errors = append(errors, hookErrors...)
	}

	return errors
}
