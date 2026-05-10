package agent

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestFirewallHookAutoRegistered(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := newAgentHookEngine(cfg, t.TempDir())
	if engine == nil {
		t.Fatal("expected hook engine")
	}
	if !engine.HasProgrammaticHook("builtin_firewall") {
		t.Fatal("expected builtin firewall hook to be registered")
	}
}

func TestFirewallHookDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Firewall.Enabled = false
	engine := newAgentHookEngine(cfg, t.TempDir())
	if engine != nil && engine.HasProgrammaticHook("builtin_firewall") {
		t.Fatal("expected builtin firewall hook to be disabled")
	}
}
