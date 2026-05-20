package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_HTTPHook(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	_ = os.Chdir(tempDir)

	// Create a .nano.yaml file with HTTP hook
	configYAML := `security:
  hooks:
    - name: test-http-hook
      event: permission_request
      pattern: "*"
      type: http
      enabled: true
      failure_policy: allow
      http:
        url: http://127.0.0.1:23333/permission
        method: POST
        headers:
          Authorization: Bearer test-token
        url_allowlist:
          - http://127.0.0.1:23333/*
        timeout_seconds: 30
        max_response_kb: 100
`
	err := os.WriteFile(".nano.yaml", []byte(configYAML), 0644)
	require.NoError(t, err)

	// Load config
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify HTTP hook was loaded correctly
	require.NotNil(t, cfg.Security)
	require.Len(t, cfg.Security.Hooks, 1)

	hook := cfg.Security.Hooks[0]
	assert.Equal(t, "test-http-hook", hook.Name)
	assert.Equal(t, "permission_request", hook.Event)
	assert.Equal(t, "*", hook.Pattern)
	assert.Equal(t, "http", hook.Type)
	assert.True(t, hook.Enabled)
	assert.Equal(t, "allow", hook.FailurePolicy)

	// Verify HTTP sub-config
	require.NotNil(t, hook.HTTP)
	assert.Equal(t, "http://127.0.0.1:23333/permission", hook.HTTP.URL)
	assert.Equal(t, "POST", hook.HTTP.Method)
	assert.Equal(t, "Bearer test-token", hook.HTTP.Headers["Authorization"])
	assert.Equal(t, []string{"http://127.0.0.1:23333/*"}, hook.HTTP.URLAllowlist)
	assert.Equal(t, 30, hook.HTTP.TimeoutSeconds)
	assert.Equal(t, 100, hook.HTTP.MaxResponseKB)
}

func TestLoadConfig_PromptHook(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	_ = os.Chdir(tempDir)

	// Create a .nano.yaml file with Prompt hook
	configYAML := `security:
  hooks:
    - name: test-prompt-hook
      event: pre_tool_use
      pattern: "*"
      type: prompt
      enabled: true
      prompt:
        prompt: Should this command be allowed?
        model: claude-3-5-sonnet-20241022
        max_tokens: 1024
`
	err := os.WriteFile(".nano.yaml", []byte(configYAML), 0644)
	require.NoError(t, err)

	// Load config
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify Prompt hook was loaded correctly
	require.NotNil(t, cfg.Security)
	require.Len(t, cfg.Security.Hooks, 1)

	hook := cfg.Security.Hooks[0]
	assert.Equal(t, "test-prompt-hook", hook.Name)
	assert.Equal(t, "pre_tool_use", hook.Event)
	assert.Equal(t, "*", hook.Pattern)
	assert.Equal(t, "prompt", hook.Type)
	assert.True(t, hook.Enabled)

	// Verify Prompt sub-config
	require.NotNil(t, hook.Prompt)
	assert.Equal(t, "Should this command be allowed?", hook.Prompt.Prompt)
	assert.Equal(t, "claude-3-5-sonnet-20241022", hook.Prompt.Model)
	assert.Equal(t, 1024, hook.Prompt.MaxTokens)
}

func TestLoadConfig_AgentHook(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	_ = os.Chdir(tempDir)

	// Create a .nano.yaml file with Agent hook
	configYAML := `security:
  hooks:
    - name: test-agent-hook
      event: pre_tool_use
      pattern: "bash:rm*"
      type: agent
      enabled: true
      agent:
        agent: security-reviewer
        task: Review this command for security issues
`
	err := os.WriteFile(".nano.yaml", []byte(configYAML), 0644)
	require.NoError(t, err)

	// Load config
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify Agent hook was loaded correctly
	require.NotNil(t, cfg.Security)
	require.Len(t, cfg.Security.Hooks, 1)

	hook := cfg.Security.Hooks[0]
	assert.Equal(t, "test-agent-hook", hook.Name)
	assert.Equal(t, "pre_tool_use", hook.Event)
	assert.Equal(t, "bash:rm*", hook.Pattern)
	assert.Equal(t, "agent", hook.Type)
	assert.True(t, hook.Enabled)

	// Verify Agent sub-config
	require.NotNil(t, hook.Agent)
	assert.Equal(t, "security-reviewer", hook.Agent.Agent)
	assert.Equal(t, "Review this command for security issues", hook.Agent.Task)
}

func TestLoadConfig_MultipleHookTypes(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	_ = os.Chdir(tempDir)

	// Create a .nano.yaml file with multiple hook types
	configYAML := `security:
  hooks:
    - name: command-hook
      event: pre_tool_use
      pattern: "bash:echo*"
      type: command
      command: echo "checking command"
      enabled: true
    - name: http-hook
      event: permission_request
      pattern: "*"
      type: http
      enabled: true
      http:
        url: http://localhost:8080/check
        url_allowlist:
          - http://localhost:8080/*
    - name: prompt-hook
      event: post_tool_use
      pattern: "*"
      type: prompt
      enabled: true
      prompt:
        prompt: Was this safe?
    - name: agent-hook
      event: pre_tool_use
      pattern: "bash:rm*"
      type: agent
      enabled: true
      agent:
        agent: safety-checker
`
	err := os.WriteFile(".nano.yaml", []byte(configYAML), 0644)
	require.NoError(t, err)

	// Load config
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify all hooks were loaded correctly
	require.NotNil(t, cfg.Security)
	require.Len(t, cfg.Security.Hooks, 4)

	// Verify command hook
	assert.Equal(t, "command", cfg.Security.Hooks[0].Type)
	assert.Equal(t, "echo \"checking command\"", cfg.Security.Hooks[0].Command)

	// Verify http hook
	assert.Equal(t, "http", cfg.Security.Hooks[1].Type)
	require.NotNil(t, cfg.Security.Hooks[1].HTTP)
	assert.Equal(t, "http://localhost:8080/check", cfg.Security.Hooks[1].HTTP.URL)

	// Verify prompt hook
	assert.Equal(t, "prompt", cfg.Security.Hooks[2].Type)
	require.NotNil(t, cfg.Security.Hooks[2].Prompt)
	assert.Equal(t, "Was this safe?", cfg.Security.Hooks[2].Prompt.Prompt)

	// Verify agent hook
	assert.Equal(t, "agent", cfg.Security.Hooks[3].Type)
	require.NotNil(t, cfg.Security.Hooks[3].Agent)
	assert.Equal(t, "safety-checker", cfg.Security.Hooks[3].Agent.Agent)
}

func TestConfigValidation_Integration(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "invalid.yaml")

	// Create config with invalid HTTP hook (missing URL)
	invalidConfig := `security:
  hooks:
    - name: invalid-http
      event: permission_request
      pattern: "*"
      type: http
      enabled: true
      http:
        url: ""
        url_allowlist: []
`
	err := os.WriteFile(configPath, []byte(invalidConfig), 0644)
	require.NoError(t, err)

	// Load config
	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	// Validate and expect errors
	errors := cfg.ValidateConfig()
	require.Greater(t, len(errors), 0, "Expected validation errors for invalid HTTP hook")

	// Check that we got the expected error messages
	foundURLError := false
	foundAllowlistError := false
	for _, err := range errors {
		msg := err.Error()
		if contains(msg, "http.url") {
			foundURLError = true
		}
		if contains(msg, "url_allowlist") {
			foundAllowlistError = true
		}
	}
	assert.True(t, foundURLError, "Expected error about missing http.url")
	assert.True(t, foundAllowlistError, "Expected error about missing url_allowlist")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
