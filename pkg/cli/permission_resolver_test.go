package cli

import (
	"os"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/config"
)

// TestResolvePermission_OnlyConfig verifies that config-only resolution works.
func TestResolvePermission_OnlyConfig(t *testing.T) {
	cfg := &config.Config{PermissionMode: ""}
	opts := PermissionResolveOpts{}

	res, warns := ResolvePermission(cfg, opts)

	if res.Mode != permission.ModeDefault {
		t.Errorf("expected mode=default, got mode=%s", res.Mode)
	}
	if res.Source != "default" {
		t.Errorf("expected source=default, got source=%s", res.Source)
	}
	if len(warns) > 0 {
		t.Errorf("expected no warnings, got %v", warns)
	}
	if cfg.PermissionMode != "default" {
		t.Errorf("expected cfg.PermissionMode=default, got %s", cfg.PermissionMode)
	}
}

// TestResolvePermission_ConfigPlusEnv verifies environment variables override config.
func TestResolvePermission_ConfigPlusEnv(t *testing.T) {
	// Set env var
	oldNano := os.Getenv("NANO_PERMISSION_MODE")
	os.Setenv("NANO_PERMISSION_MODE", "yolo")
	defer os.Setenv("NANO_PERMISSION_MODE", oldNano)

	cfg := &config.Config{PermissionMode: "default"}
	opts := PermissionResolveOpts{EnvHintEnabled: true}

	res, warns := ResolvePermission(cfg, opts)

	if res.Mode != permission.ModeYOLO {
		t.Errorf("expected mode=yolo, got mode=%s", res.Mode)
	}
	if res.Source != "env:NANO_PERMISSION_MODE" {
		t.Errorf("expected source=env:NANO_PERMISSION_MODE, got source=%s", res.Source)
	}
	if res.SandboxBackend != "docker" {
		t.Errorf("expected sandbox backend=docker for yolo mode, got %s", res.SandboxBackend)
	}
	if len(warns) > 0 {
		t.Errorf("expected no warnings, got %v", warns)
	}
	if cfg.Sandbox == nil || cfg.Sandbox.Backend != "docker" {
		t.Errorf("expected cfg.Sandbox.Backend=docker, got nil or wrong value")
	}
}

// TestResolvePermission_FlagOverridesAll verifies --permission-mode flag has highest priority (except skip-perms).
func TestResolvePermission_FlagOverridesAll(t *testing.T) {
	// Set env var
	oldNano := os.Getenv("NANO_PERMISSION_MODE")
	os.Setenv("NANO_PERMISSION_MODE", "yolo")
	defer os.Setenv("NANO_PERMISSION_MODE", oldNano)

	cfg := &config.Config{PermissionMode: "default"}
	opts := PermissionResolveOpts{
		FlagMode:       "acceptEdits",
		EnvHintEnabled: true,
	}

	res, _ := ResolvePermission(cfg, opts)

	if res.Mode != permission.ModeAcceptEdits {
		t.Errorf("expected mode=acceptEdits (from flag), got mode=%s", res.Mode)
	}
	if res.Source != "flag:permission-mode" {
		t.Errorf("expected source=flag:permission-mode, got source=%s", res.Source)
	}
}

// TestResolvePermission_SkipPermsWins verifies --dangerously-skip-permissions always wins.
func TestResolvePermission_SkipPermsWins(t *testing.T) {
	// Set env var
	oldNano := os.Getenv("NANO_PERMISSION_MODE")
	os.Setenv("NANO_PERMISSION_MODE", "plan")
	defer os.Setenv("NANO_PERMISSION_MODE", oldNano)

	cfg := &config.Config{PermissionMode: "default"}
	opts := PermissionResolveOpts{
		SkipPerms:      true,
		FlagMode:       "acceptEdits",
		EnvHintEnabled: true,
	}

	res, _ := ResolvePermission(cfg, opts)

	if res.Mode != permission.ModeYOLO {
		t.Errorf("expected mode=yolo (from skip-perms flag), got mode=%s", res.Mode)
	}
	if res.Source != "flag:dangerously-skip-permissions" {
		t.Errorf("expected source=flag:dangerously-skip-permissions, got source=%s", res.Source)
	}
	if res.SandboxBackend != "docker" {
		t.Errorf("expected sandbox backend=docker for yolo mode, got %s", res.SandboxBackend)
	}
}

// TestResolvePermission_AutoFlipsConfirmPolicy verifies auto mode sets confirm_policy=block.
func TestResolvePermission_AutoFlipsConfirmPolicy(t *testing.T) {
	cfg := &config.Config{PermissionMode: "auto"}
	opts := PermissionResolveOpts{}

	res, warns := ResolvePermission(cfg, opts)

	if res.Mode != permission.ModeAuto {
		t.Errorf("expected mode=auto, got mode=%s", res.Mode)
	}
	if res.ConfirmPolicy != config.ConfirmPolicyBlock {
		t.Errorf("expected confirm_policy=block, got %s", res.ConfirmPolicy)
	}
	if cfg.Daemon == nil || cfg.Daemon.ConfirmPolicy != config.ConfirmPolicyBlock {
		t.Errorf("expected cfg.Daemon.ConfirmPolicy=block, got nil or wrong value")
	}
	if len(warns) != 1 {
		t.Errorf("expected 1 warning (no PermissionAuto config), got %v", warns)
	}
}

// TestResolvePermission_AutoWithConfig verifies auto mode with PermissionAuto config doesn't warn.
func TestResolvePermission_AutoWithConfig(t *testing.T) {
	cfg := &config.Config{
		PermissionMode: "auto",
		PermissionAuto: &config.PermissionAutoConfig{Backend: "llm"},
	}
	opts := PermissionResolveOpts{}

	res, warns := ResolvePermission(cfg, opts)

	if res.Mode != permission.ModeAuto {
		t.Errorf("expected mode=auto, got mode=%s", res.Mode)
	}
	if len(warns) > 0 {
		t.Errorf("expected no warnings when PermissionAuto is configured, got %v", warns)
	}
}

// TestResolvePermission_AutoPreservesBlockPolicy verifies auto mode doesn't override explicit block policy.
func TestResolvePermission_AutoPreservesBlockPolicy(t *testing.T) {
	cfg := &config.Config{
		PermissionMode: "auto",
		Daemon: &config.DaemonConfig{
			ConfirmPolicy: config.ConfirmPolicyBlock,
		},
	}
	opts := PermissionResolveOpts{}

	res, _ := ResolvePermission(cfg, opts)

	if res.ConfirmPolicy != config.ConfirmPolicyBlock {
		t.Errorf("expected confirm_policy=block to be preserved, got %s", res.ConfirmPolicy)
	}
}

// TestResolvePermission_Idempotent verifies calling ResolvePermission twice is safe.
func TestResolvePermission_Idempotent(t *testing.T) {
	cfg := &config.Config{PermissionMode: "yolo"}
	opts := PermissionResolveOpts{}

	res1, warns1 := ResolvePermission(cfg, opts)
	res2, warns2 := ResolvePermission(cfg, opts)

	if res1.Mode != res2.Mode {
		t.Errorf("mode changed: %s -> %s", res1.Mode, res2.Mode)
	}
	if res1.Source != res2.Source {
		t.Errorf("source changed: %s -> %s", res1.Source, res2.Source)
	}
	if res1.SandboxBackend != res2.SandboxBackend {
		t.Errorf("sandbox backend changed: %s -> %s", res1.SandboxBackend, res2.SandboxBackend)
	}
	if len(warns1) != len(warns2) {
		t.Errorf("warnings count changed: %d -> %d", len(warns1), len(warns2))
	}
}

// TestResolvePermission_EnvDisabled verifies that env resolution is skipped when EnvHintEnabled=false.
func TestResolvePermission_EnvDisabled(t *testing.T) {
	// Set env var
	oldNano := os.Getenv("NANO_PERMISSION_MODE")
	os.Setenv("NANO_PERMISSION_MODE", "yolo")
	defer os.Setenv("NANO_PERMISSION_MODE", oldNano)

	cfg := &config.Config{PermissionMode: "default"}
	opts := PermissionResolveOpts{EnvHintEnabled: false} // Disabled

	res, _ := ResolvePermission(cfg, opts)

	// Should use config value, not env
	if res.Mode != permission.ModeDefault {
		t.Errorf("expected mode=default (env disabled), got mode=%s", res.Mode)
	}
	if res.Source != "config" {
		t.Errorf("expected source=config, got source=%s", res.Source)
	}
}

// TestResolvePermission_YOLOPreservesExistingSandbox verifies yolo mode doesn't override existing sandbox config.
func TestResolvePermission_YOLOPreservesExistingSandbox(t *testing.T) {
	cfg := &config.Config{
		PermissionMode: "yolo",
		Sandbox: &config.SandboxConfig{
			Backend: "podman",
		},
	}
	opts := PermissionResolveOpts{}

	res, _ := ResolvePermission(cfg, opts)

	if res.SandboxBackend != "podman" {
		t.Errorf("expected existing sandbox backend=podman to be preserved, got %s", res.SandboxBackend)
	}
	if cfg.Sandbox.Backend != "podman" {
		t.Errorf("expected cfg.Sandbox.Backend=podman to be preserved, got %s", cfg.Sandbox.Backend)
	}
}
