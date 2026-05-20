package config

import (
	"strings"
	"testing"
)

func TestValidateHooks_CommandType(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-command-hook",
			Event:   "pre_tool_use",
			Pattern: "bash:*",
			Type:    "command",
			Command: "echo test",
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) != 0 {
		t.Errorf("Expected no errors for valid command hook, got %d errors: %v", len(errors), errors)
	}
}

func TestValidateHooks_CommandTypeDefault(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-default-hook",
			Event:   "pre_tool_use",
			Pattern: "bash:*",
			Type:    "", // Empty type should default to command
			Command: "echo test",
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) != 0 {
		t.Errorf("Expected no errors for hook with empty type (defaults to command), got %d errors: %v", len(errors), errors)
	}
}

func TestValidateHooks_HTTPType_Valid(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-http-hook",
			Event:   "permission_request",
			Pattern: "*",
			Type:    "http",
			HTTP: &HookHTTPConfig{
				URL:          "http://127.0.0.1:23333/permission",
				URLAllowlist: []string{"http://127.0.0.1:23333/*"},
			},
			Enabled:       true,
			FailurePolicy: "allow",
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) != 0 {
		t.Errorf("Expected no errors for valid HTTP hook, got %d errors: %v", len(errors), errors)
	}
}

func TestValidateHooks_HTTPType_MissingConfig(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-http-hook",
			Event:   "permission_request",
			Pattern: "*",
			Type:    "http",
			HTTP:    nil, // Missing HTTP config
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) == 0 {
		t.Error("Expected error for HTTP hook with missing HTTP config, got none")
	}
	if !strings.Contains(errors[0].Error(), "http config is missing") {
		t.Errorf("Expected error about missing http config, got: %v", errors[0])
	}
}

func TestValidateHooks_HTTPType_MissingURL(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-http-hook",
			Event:   "permission_request",
			Pattern: "*",
			Type:    "http",
			HTTP: &HookHTTPConfig{
				URL: "", // Missing URL
			},
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) < 1 {
		t.Error("Expected at least one error for HTTP hook with missing URL, got none")
	}
	foundURLError := false
	for _, err := range errors {
		if strings.Contains(err.Error(), "http.url") {
			foundURLError = true
			break
		}
	}
	if !foundURLError {
		t.Errorf("Expected error about missing http.url, got errors: %v", errors)
	}
}

func TestValidateHooks_HTTPType_MissingAllowlist(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-http-hook",
			Event:   "permission_request",
			Pattern: "*",
			Type:    "http",
			HTTP: &HookHTTPConfig{
				URL:          "http://127.0.0.1:23333/permission",
				URLAllowlist: []string{}, // Empty allowlist
			},
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) < 1 {
		t.Error("Expected at least one error for HTTP hook with empty allowlist, got none")
	}
	foundAllowlistError := false
	for _, err := range errors {
		if strings.Contains(err.Error(), "url_allowlist") {
			foundAllowlistError = true
			break
		}
	}
	if !foundAllowlistError {
		t.Errorf("Expected error about missing url_allowlist, got errors: %v", errors)
	}
}

func TestValidateHooks_PromptType_Valid(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-prompt-hook",
			Event:   "pre_tool_use",
			Pattern: "*",
			Type:    "prompt",
			Prompt: &HookPromptConfig{
				Prompt: "Should this command be allowed?",
				Model:  "claude-3-5-sonnet-20241022",
			},
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) != 0 {
		t.Errorf("Expected no errors for valid prompt hook, got %d errors: %v", len(errors), errors)
	}
}

func TestValidateHooks_PromptType_MissingConfig(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-prompt-hook",
			Event:   "pre_tool_use",
			Pattern: "*",
			Type:    "prompt",
			Prompt:  nil, // Missing prompt config
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) == 0 {
		t.Error("Expected error for prompt hook with missing prompt config, got none")
	}
	if !strings.Contains(errors[0].Error(), "prompt config is missing") {
		t.Errorf("Expected error about missing prompt config, got: %v", errors[0])
	}
}

func TestValidateHooks_PromptType_MissingPrompt(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-prompt-hook",
			Event:   "pre_tool_use",
			Pattern: "*",
			Type:    "prompt",
			Prompt: &HookPromptConfig{
				Prompt: "", // Missing prompt text
			},
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) == 0 {
		t.Error("Expected error for prompt hook with missing prompt text, got none")
	}
	if !strings.Contains(errors[0].Error(), "prompt.prompt") {
		t.Errorf("Expected error about missing prompt.prompt, got: %v", errors[0])
	}
}

func TestValidateHooks_AgentType_Valid(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-agent-hook",
			Event:   "pre_tool_use",
			Pattern: "*",
			Type:    "agent",
			Agent: &HookAgentConfig{
				Agent: "security-reviewer",
				Task:  "Review this command for security issues",
			},
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) != 0 {
		t.Errorf("Expected no errors for valid agent hook, got %d errors: %v", len(errors), errors)
	}
}

func TestValidateHooks_AgentType_MissingConfig(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-agent-hook",
			Event:   "pre_tool_use",
			Pattern: "*",
			Type:    "agent",
			Agent:   nil, // Missing agent config
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) == 0 {
		t.Error("Expected error for agent hook with missing agent config, got none")
	}
	if !strings.Contains(errors[0].Error(), "agent config is missing") {
		t.Errorf("Expected error about missing agent config, got: %v", errors[0])
	}
}

func TestValidateHooks_AgentType_MissingAgent(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-agent-hook",
			Event:   "pre_tool_use",
			Pattern: "*",
			Type:    "agent",
			Agent: &HookAgentConfig{
				Agent: "", // Missing agent name
			},
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) == 0 {
		t.Error("Expected error for agent hook with missing agent name, got none")
	}
	if !strings.Contains(errors[0].Error(), "agent.agent") {
		t.Errorf("Expected error about missing agent.agent, got: %v", errors[0])
	}
}

func TestValidateHooks_UnknownType(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-unknown-hook",
			Event:   "pre_tool_use",
			Pattern: "*",
			Type:    "unknown_type",
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) == 0 {
		t.Error("Expected error for hook with unknown type, got none")
	}
	if !strings.Contains(errors[0].Error(), "unknown type") {
		t.Errorf("Expected error about unknown type, got: %v", errors[0])
	}
}

func TestValidateHooks_DisabledHook(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "test-disabled-hook",
			Event:   "pre_tool_use",
			Pattern: "*",
			Type:    "http",
			HTTP:    nil, // Invalid config, but hook is disabled
			Enabled: false,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) != 0 {
		t.Errorf("Expected no errors for disabled hook with invalid config, got %d errors: %v", len(errors), errors)
	}
}

func TestValidateHooks_MultipleHooks(t *testing.T) {
	hooks := []HookConfig{
		{
			Name:    "valid-command",
			Event:   "pre_tool_use",
			Pattern: "bash:*",
			Type:    "command",
			Command: "echo test",
			Enabled: true,
		},
		{
			Name:    "invalid-http",
			Event:   "permission_request",
			Pattern: "*",
			Type:    "http",
			HTTP:    nil, // Invalid
			Enabled: true,
		},
		{
			Name:    "valid-prompt",
			Event:   "pre_tool_use",
			Pattern: "*",
			Type:    "prompt",
			Prompt: &HookPromptConfig{
				Prompt: "Should this be allowed?",
			},
			Enabled: true,
		},
		{
			Name:    "invalid-agent",
			Event:   "pre_tool_use",
			Pattern: "*",
			Type:    "agent",
			Agent: &HookAgentConfig{
				Agent: "", // Invalid
			},
			Enabled: true,
		},
	}

	errors := ValidateHooks(hooks)
	if len(errors) != 2 {
		t.Errorf("Expected 2 errors (invalid-http and invalid-agent), got %d errors: %v", len(errors), errors)
	}
}

func TestConfigValidateConfig(t *testing.T) {
	cfg := &Config{
		Security: &SecurityConfig{
			Hooks: []HookConfig{
				{
					Name:    "test-hook",
					Event:   "pre_tool_use",
					Pattern: "*",
					Type:    "http",
					HTTP:    nil, // Invalid
					Enabled: true,
				},
			},
		},
	}

	errors := cfg.ValidateConfig()
	if len(errors) == 0 {
		t.Error("Expected errors from ValidateConfig, got none")
	}
}

func TestConfigValidateConfig_NoSecurity(t *testing.T) {
	cfg := &Config{
		Security: nil,
	}

	errors := cfg.ValidateConfig()
	if len(errors) != 0 {
		t.Errorf("Expected no errors for config with no security section, got %d errors: %v", len(errors), errors)
	}
}
