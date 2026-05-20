package agent

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/hookservice"
)

func TestNewAgentHookEngine_CommandType(t *testing.T) {
	cfg := &config.Config{
		Security: &config.SecurityConfig{
			Hooks: []config.HookConfig{
				{
					Name:    "test-command-hook",
					Event:   "pre_tool_use",
					Pattern: "bash:*",
					Type:    "command",
					Command: "echo test",
					Enabled: true,
				},
			},
		},
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	if engine == nil {
		t.Fatal("Expected non-nil hook engine")
	}

	hooks := engine.Hooks()
	if len(hooks) != 1 {
		t.Fatalf("Expected 1 hook, got %d", len(hooks))
	}

	hook := hooks[0]
	if hook.Type != hookservice.HookTypeCommand {
		t.Errorf("Expected Type=HookTypeCommand, got %v", hook.Type)
	}
	if hook.Command != "echo test" {
		t.Errorf("Expected Command='echo test', got '%s'", hook.Command)
	}
}

func TestNewAgentHookEngine_DefaultType(t *testing.T) {
	cfg := &config.Config{
		Security: &config.SecurityConfig{
			Hooks: []config.HookConfig{
				{
					Name:    "test-default-hook",
					Event:   "pre_tool_use",
					Pattern: "bash:*",
					Type:    "", // Empty type should default to command
					Command: "echo test",
					Enabled: true,
				},
			},
		},
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	if engine == nil {
		t.Fatal("Expected non-nil hook engine")
	}

	hooks := engine.Hooks()
	if len(hooks) != 1 {
		t.Fatalf("Expected 1 hook, got %d", len(hooks))
	}

	hook := hooks[0]
	if hook.Type != hookservice.HookTypeCommand {
		t.Errorf("Expected Type=HookTypeCommand (default), got %v", hook.Type)
	}
}

func TestNewAgentHookEngine_HTTPType(t *testing.T) {
	cfg := &config.Config{
		Security: &config.SecurityConfig{
			Hooks: []config.HookConfig{
				{
					Name:    "test-http-hook",
					Event:   "permission_request",
					Pattern: "*",
					Type:    "http",
					HTTP: &config.HookHTTPConfig{
						URL:            "http://127.0.0.1:23333/permission",
						Method:         "POST",
						Headers:        map[string]string{"Authorization": "Bearer token"},
						URLAllowlist:   []string{"http://127.0.0.1:23333/*"},
						AllowedEnvVars: []string{"USER", "HOME"},
						TimeoutSeconds: 30,
						MaxResponseKB:  100,
					},
					Enabled:       true,
					FailurePolicy: "allow",
				},
			},
		},
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	if engine == nil {
		t.Fatal("Expected non-nil hook engine")
	}

	hooks := engine.Hooks()
	if len(hooks) != 1 {
		t.Fatalf("Expected 1 hook, got %d", len(hooks))
	}

	hook := hooks[0]
	if hook.Type != hookservice.HookTypeHTTP {
		t.Errorf("Expected Type=HookTypeHTTP, got %v", hook.Type)
	}
	if hook.HTTPConfig == nil {
		t.Fatal("Expected non-nil HTTPConfig")
	}
	if hook.HTTPConfig.URL != "http://127.0.0.1:23333/permission" {
		t.Errorf("Expected URL='http://127.0.0.1:23333/permission', got '%s'", hook.HTTPConfig.URL)
	}
	if hook.HTTPConfig.Method != "POST" {
		t.Errorf("Expected Method='POST', got '%s'", hook.HTTPConfig.Method)
	}
	if len(hook.HTTPConfig.Headers) != 1 {
		t.Errorf("Expected 1 header, got %d", len(hook.HTTPConfig.Headers))
	}
	if hook.HTTPConfig.Headers["Authorization"] != "Bearer token" {
		t.Errorf("Expected Authorization header, got %v", hook.HTTPConfig.Headers)
	}
	if len(hook.HTTPConfig.URLAllowlist) != 1 {
		t.Errorf("Expected 1 URL in allowlist, got %d", len(hook.HTTPConfig.URLAllowlist))
	}
	if len(hook.HTTPConfig.AllowedEnvVars) != 2 {
		t.Errorf("Expected 2 allowed env vars, got %d", len(hook.HTTPConfig.AllowedEnvVars))
	}
	if hook.HTTPConfig.TimeoutSeconds != 30 {
		t.Errorf("Expected TimeoutSeconds=30, got %d", hook.HTTPConfig.TimeoutSeconds)
	}
	if hook.HTTPConfig.MaxResponseKB != 100 {
		t.Errorf("Expected MaxResponseKB=100, got %d", hook.HTTPConfig.MaxResponseKB)
	}
}

func TestNewAgentHookEngine_PromptType(t *testing.T) {
	cfg := &config.Config{
		Security: &config.SecurityConfig{
			Hooks: []config.HookConfig{
				{
					Name:    "test-prompt-hook",
					Event:   "pre_tool_use",
					Pattern: "*",
					Type:    "prompt",
					Prompt: &config.HookPromptConfig{
						Prompt:    "Should this command be allowed?",
						Model:     "claude-3-5-sonnet-20241022",
						MaxTokens: 1024,
					},
					Enabled: true,
				},
			},
		},
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	if engine == nil {
		t.Fatal("Expected non-nil hook engine")
	}

	hooks := engine.Hooks()
	if len(hooks) != 1 {
		t.Fatalf("Expected 1 hook, got %d", len(hooks))
	}

	hook := hooks[0]
	if hook.Type != hookservice.HookTypePrompt {
		t.Errorf("Expected Type=HookTypePrompt, got %v", hook.Type)
	}
	if hook.PromptConfig == nil {
		t.Fatal("Expected non-nil PromptConfig")
	}
	if hook.PromptConfig.Prompt != "Should this command be allowed?" {
		t.Errorf("Expected specific prompt, got '%s'", hook.PromptConfig.Prompt)
	}
	if hook.PromptConfig.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("Expected Model='claude-3-5-sonnet-20241022', got '%s'", hook.PromptConfig.Model)
	}
	if hook.PromptConfig.MaxTokens != 1024 {
		t.Errorf("Expected MaxTokens=1024, got %d", hook.PromptConfig.MaxTokens)
	}
}

func TestNewAgentHookEngine_AgentType(t *testing.T) {
	cfg := &config.Config{
		Security: &config.SecurityConfig{
			Hooks: []config.HookConfig{
				{
					Name:    "test-agent-hook",
					Event:   "pre_tool_use",
					Pattern: "*",
					Type:    "agent",
					Agent: &config.HookAgentConfig{
						Agent: "security-reviewer",
						Task:  "Review this command for security issues",
					},
					Enabled: true,
				},
			},
		},
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	if engine == nil {
		t.Fatal("Expected non-nil hook engine")
	}

	hooks := engine.Hooks()
	if len(hooks) != 1 {
		t.Fatalf("Expected 1 hook, got %d", len(hooks))
	}

	hook := hooks[0]
	if hook.Type != hookservice.HookTypeAgent {
		t.Errorf("Expected Type=HookTypeAgent, got %v", hook.Type)
	}
	if hook.AgentConfig == nil {
		t.Fatal("Expected non-nil AgentConfig")
	}
	if hook.AgentConfig.Agent != "security-reviewer" {
		t.Errorf("Expected Agent='security-reviewer', got '%s'", hook.AgentConfig.Agent)
	}
	if hook.AgentConfig.Task != "Review this command for security issues" {
		t.Errorf("Expected specific task, got '%s'", hook.AgentConfig.Task)
	}
}

func TestNewAgentHookEngine_DisabledHook(t *testing.T) {
	cfg := &config.Config{
		Security: &config.SecurityConfig{
			Hooks: []config.HookConfig{
				{
					Name:    "disabled-hook",
					Event:   "pre_tool_use",
					Pattern: "*",
					Type:    "command",
					Command: "echo test",
					Enabled: false, // Disabled
				},
			},
		},
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	if engine == nil {
		t.Fatal("Expected non-nil hook engine (firewall is enabled by default)")
	}

	hooks := engine.Hooks()
	if len(hooks) != 0 {
		t.Errorf("Expected 0 hooks (disabled hook should be skipped), got %d", len(hooks))
	}
}

func TestNewAgentHookEngine_MultipleHooks(t *testing.T) {
	cfg := &config.Config{
		Security: &config.SecurityConfig{
			Hooks: []config.HookConfig{
				{
					Name:    "command-hook",
					Event:   "pre_tool_use",
					Pattern: "bash:*",
					Type:    "command",
					Command: "echo test",
					Enabled: true,
				},
				{
					Name:    "http-hook",
					Event:   "permission_request",
					Pattern: "*",
					Type:    "http",
					HTTP: &config.HookHTTPConfig{
						URL:          "http://127.0.0.1:23333/permission",
						URLAllowlist: []string{"http://127.0.0.1:23333/*"},
					},
					Enabled: true,
				},
				{
					Name:    "prompt-hook",
					Event:   "post_tool_use",
					Pattern: "*",
					Type:    "prompt",
					Prompt: &config.HookPromptConfig{
						Prompt: "Was this command safe?",
					},
					Enabled: true,
				},
			},
		},
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	if engine == nil {
		t.Fatal("Expected non-nil hook engine")
	}

	hooks := engine.Hooks()
	if len(hooks) != 3 {
		t.Fatalf("Expected 3 hooks, got %d", len(hooks))
	}

	// Verify each hook type
	hooksByName := make(map[string]*hookservice.Hook)
	for i := range hooks {
		hooksByName[hooks[i].Name] = &hooks[i]
	}

	if h, ok := hooksByName["command-hook"]; !ok {
		t.Error("Expected command-hook to be present")
	} else if h.Type != hookservice.HookTypeCommand {
		t.Errorf("Expected command-hook to have Type=HookTypeCommand, got %v", h.Type)
	}

	if h, ok := hooksByName["http-hook"]; !ok {
		t.Error("Expected http-hook to be present")
	} else if h.Type != hookservice.HookTypeHTTP {
		t.Errorf("Expected http-hook to have Type=HookTypeHTTP, got %v", h.Type)
	} else if h.HTTPConfig == nil {
		t.Error("Expected http-hook to have non-nil HTTPConfig")
	}

	if h, ok := hooksByName["prompt-hook"]; !ok {
		t.Error("Expected prompt-hook to be present")
	} else if h.Type != hookservice.HookTypePrompt {
		t.Errorf("Expected prompt-hook to have Type=HookTypePrompt, got %v", h.Type)
	} else if h.PromptConfig == nil {
		t.Error("Expected prompt-hook to have non-nil PromptConfig")
	}
}

func TestNewAgentHookEngine_AllHookFields(t *testing.T) {
	cfg := &config.Config{
		Security: &config.SecurityConfig{
			Hooks: []config.HookConfig{
				{
					Name:          "full-hook",
					Event:         "pre_tool_use",
					Pattern:       "bash:rm*",
					Type:          "command",
					Command:       "echo check",
					Enabled:       true,
					FailurePolicy: "block",
					EnvWhitelist:  []string{"HOME", "USER"},
					Async:         true,
					AsyncRewake:   false,
					Once:          true,
					StatusMessage: "Checking dangerous command",
				},
			},
		},
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	if engine == nil {
		t.Fatal("Expected non-nil hook engine")
	}

	hooks := engine.Hooks()
	if len(hooks) != 1 {
		t.Fatalf("Expected 1 hook, got %d", len(hooks))
	}

	hook := hooks[0]
	if hook.Name != "full-hook" {
		t.Errorf("Expected Name='full-hook', got '%s'", hook.Name)
	}
	if hook.Event != hookservice.EventPreToolUse {
		t.Errorf("Expected Event=EventPreToolUse, got %v", hook.Event)
	}
	if hook.Pattern != "bash:rm*" {
		t.Errorf("Expected Pattern='bash:rm*', got '%s'", hook.Pattern)
	}
	if hook.Type != hookservice.HookTypeCommand {
		t.Errorf("Expected Type=HookTypeCommand, got %v", hook.Type)
	}
	if hook.Command != "echo check" {
		t.Errorf("Expected Command='echo check', got '%s'", hook.Command)
	}
	if hook.FailurePolicy != hookservice.FailurePolicyBlock {
		t.Errorf("Expected FailurePolicy=FailurePolicyBlock, got %v", hook.FailurePolicy)
	}
	if len(hook.EnvWhitelist) != 2 {
		t.Errorf("Expected 2 env whitelist entries, got %d", len(hook.EnvWhitelist))
	}
	if !hook.Async {
		t.Error("Expected Async=true")
	}
	if hook.AsyncRewake {
		t.Error("Expected AsyncRewake=false")
	}
	if !hook.Once {
		t.Error("Expected Once=true")
	}
	if hook.StatusMessage != "Checking dangerous command" {
		t.Errorf("Expected StatusMessage='Checking dangerous command', got '%s'", hook.StatusMessage)
	}
	if !hook.Enabled {
		t.Error("Expected Enabled=true")
	}
}

func TestNewAgentHookEngine_NoSecurity(t *testing.T) {
	cfg := &config.Config{
		Security: nil,
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	// Should still create engine because firewall is enabled by default
	if engine == nil {
		t.Fatal("Expected non-nil hook engine (firewall enabled by default)")
	}

	hooks := engine.Hooks()
	if len(hooks) != 0 {
		t.Errorf("Expected 0 hooks, got %d", len(hooks))
	}
}

func TestNewAgentHookEngine_EmptyHooks(t *testing.T) {
	cfg := &config.Config{
		Security: &config.SecurityConfig{
			Hooks: []config.HookConfig{},
		},
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	// Should still create engine because firewall is enabled by default
	if engine == nil {
		t.Fatal("Expected non-nil hook engine (firewall enabled by default)")
	}

	hooks := engine.Hooks()
	if len(hooks) != 0 {
		t.Errorf("Expected 0 hooks, got %d", len(hooks))
	}
}
func TestNewAgentHookEngine_EnabledFieldPassthrough(t *testing.T) {
	// Test that the Enabled field is properly passed from HookConfig to middleware.Hook
	// This is a regression test for the bug where Enabled was not copied, causing all
	// hooks to be disabled (Go bool zero-value is false).
	cfg := &config.Config{
		Security: &config.SecurityConfig{
			Hooks: []config.HookConfig{
				{
					Name:    "enabled-hook",
					Event:   "user_prompt_submit",
					Pattern: "*",
					Type:    "command",
					Command: "echo test",
					Enabled: true, // Explicitly enabled
				},
			},
		},
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	if engine == nil {
		t.Fatal("Expected non-nil hook engine")
	}

	hooks := engine.Hooks()
	if len(hooks) != 1 {
		t.Fatalf("Expected 1 hook, got %d", len(hooks))
	}

	hook := hooks[0]
	if !hook.Enabled {
		t.Error("Expected Enabled=true but got false; the Enabled field was not passed through from HookConfig")
	}
}
