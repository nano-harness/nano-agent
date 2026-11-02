package agent

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/hookservice"
)

func TestNewAgentHookEngine_BuildsHooksFromEvents(t *testing.T) {
	cfg := &config.Config{
		Hooks: &config.HooksConfig{
			Events: map[string][]config.HookCommand{
				"Stop": {{
					Matcher: "*",
					Command: "echo stop",
					Timeout: 30,
				}},
			},
		},
		Firewall: &config.FirewallConfig{Enabled: false},
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	if engine == nil {
		t.Fatal("Expected non-nil hook engine")
	}

	hooks := engine.Hooks()
	if len(hooks) != 1 {
		t.Fatalf("Expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].Event != hookservice.EventStop {
		t.Fatalf("Expected event %q, got %q", hookservice.EventStop, hooks[0].Event)
	}
	if hooks[0].Pattern != "*" {
		t.Fatalf("Expected pattern '*', got %q", hooks[0].Pattern)
	}
	if hooks[0].Command != "echo stop" {
		t.Fatalf("Expected command %q, got %q", "echo stop", hooks[0].Command)
	}
	if !hooks[0].Enabled {
		t.Fatalf("Expected hook Enabled=true")
	}
}

func TestNewAgentHookEngine_DefaultMatcher(t *testing.T) {
	cfg := &config.Config{
		Hooks: &config.HooksConfig{
			Events: map[string][]config.HookCommand{
				"Stop": {{
					Command: "echo stop",
				}},
			},
		},
		Firewall: &config.FirewallConfig{Enabled: false},
	}

	engine := newAgentHookEngine(cfg, "/tmp")
	if engine == nil {
		t.Fatal("Expected non-nil hook engine")
	}

	hooks := engine.Hooks()
	if len(hooks) != 1 {
		t.Fatalf("Expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].Pattern != "*" {
		t.Fatalf("Expected default pattern '*', got %q", hooks[0].Pattern)
	}
}

func TestNewAgentHookEngine_NoHooksAndFirewallDisabledReturnsNil(t *testing.T) {
	cfg := &config.Config{
		Firewall: &config.FirewallConfig{Enabled: false},
	}
	engine := newAgentHookEngine(cfg, "/tmp")
	if engine != nil {
		t.Fatalf("Expected nil hook engine when no hooks and firewall disabled")
	}
}
