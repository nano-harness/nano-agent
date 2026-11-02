package config

import "testing"

func TestDefaultConfig_AuditRotationDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Middleware == nil {
		t.Fatal("Middleware config is nil")
	}
	if cfg.Middleware.AuditMaxSizeMB != 100 {
		t.Fatalf("AuditMaxSizeMB = %d, want 100", cfg.Middleware.AuditMaxSizeMB)
	}
	if cfg.Middleware.AuditMaxBackups != 3 {
		t.Fatalf("AuditMaxBackups = %d, want 3", cfg.Middleware.AuditMaxBackups)
	}
	if cfg.Middleware.AuditMaxAgeDays != 28 {
		t.Fatalf("AuditMaxAgeDays = %d, want 28", cfg.Middleware.AuditMaxAgeDays)
	}
	if !cfg.Middleware.AuditCompress {
		t.Fatal("AuditCompress = false, want true")
	}
}

func TestConfigDeepCopyCopiesHooksEvents(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Hooks = &HooksConfig{
		Events: map[string][]HookCommand{
			"Stop": {{
				Matcher: "*",
				Command: "echo hi",
				Timeout: 10,
			}},
		},
	}

	copied := cfg.DeepCopy()
	copied.Hooks.Events["Stop"][0].Command = "echo changed"

	if cfg.Hooks.Events["Stop"][0].Command != "echo hi" {
		t.Fatalf("DeepCopy aliased hooks.events: %#v", cfg.Hooks.Events["Stop"][0])
	}
}

func TestConfigDeepCopyCopiesForkLimits(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Advanced = &AdvancedConfig{
		Fork: &ForkAdvConfig{
			MaxDepth:      1,
			MaxConcurrent: 3,
			MaxRuntimeSec: 7,
		},
	}

	copied := cfg.DeepCopy()
	if copied.Advanced == nil || copied.Advanced.Fork == nil {
		t.Fatalf("DeepCopy omitted advanced fork config: %#v", copied.Advanced)
	}
	if copied.Advanced.Fork.MaxDepth != 1 || copied.Advanced.Fork.MaxConcurrent != 3 || copied.Advanced.Fork.MaxRuntimeSec != 7 {
		t.Fatalf("DeepCopy fork limits = %#v, want depth=1 concurrent=3 runtime=7", copied.Advanced.Fork)
	}

	copied.Advanced.Fork.MaxConcurrent = 9
	if cfg.Advanced.Fork.MaxConcurrent != 3 {
		t.Fatalf("DeepCopy aliased fork config: original MaxConcurrent = %d", cfg.Advanced.Fork.MaxConcurrent)
	}
}
