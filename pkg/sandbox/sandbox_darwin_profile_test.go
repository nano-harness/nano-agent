//go:build darwin

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

// TestBuildProfile_RuleOrdering verifies that the SBPL profile has the correct
// rule ordering: allows before denies, with the critical-path blacklist and
// anti-tamper rule at the end.
func TestBuildProfile_RuleOrdering(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	workdir := t.TempDir()
	cfg := &config.SandboxConfig{
		Enabled:            true,
		Backend:            "native",
		NetworkAccess:      true,
		ExtraReadOnlyPaths: []string{"/opt/tools"},
		ExtraWritablePaths: []string{"/opt/cache"},
		ExtraDeniedPaths:   []string{"/secrets/vault"},
	}

	sb := NewSandboxExecSandbox(cfg, workdir)
	profile := sb.BuildProfileForInspection()

	// Verify basic structure
	if !strings.Contains(profile, "(version 1)") {
		t.Error("profile must start with (version 1)")
	}
	if !strings.Contains(profile, "(deny default)") {
		t.Error("profile must contain (deny default)")
	}

	// Verify HOME read-allow is present
	resolvedHome := canonicalize(tempHome)
	homeAllow := `(allow file-read* (subpath "` + resolvedHome + `"))`
	if !strings.Contains(profile, homeAllow) {
		t.Errorf("profile must contain HOME read-allow:\n  want: %s\n  profile:\n%s", homeAllow, profile)
	}

	// Verify built-in denies are present
	sshDeny := `(deny file-read* file-write* (subpath "` + filepath.Join(resolvedHome, ".ssh") + `"))`
	if !strings.Contains(profile, sshDeny) {
		t.Errorf("profile must contain .ssh deny:\n  want: %s", sshDeny)
	}

	// Verify ordering: HOME allow comes BEFORE .ssh deny
	homeAllowIdx := strings.Index(profile, homeAllow)
	sshDenyIdx := strings.Index(profile, sshDeny)
	if homeAllowIdx >= sshDenyIdx {
		t.Error("HOME read-allow must appear before .ssh deny (later rules override)")
	}

	// Verify ExtraDeniedPaths is present
	vaultDeny := `(deny file-read* file-write* (subpath "/secrets/vault"))`
	if !strings.Contains(profile, vaultDeny) {
		t.Errorf("profile must contain extra denied path:\n  want: %s", vaultDeny)
	}

	// Verify anti-tamper rule is present
	nanoDir := canonicalize(filepath.Join(workdir, ".nano"))
	antiTamper := `(deny file-write* (subpath "` + nanoDir + `"))`
	if !strings.Contains(profile, antiTamper) {
		t.Errorf("profile must contain anti-tamper deny-write:\n  want: %s\n  profile:\n%s", antiTamper, profile)
	}

	// Verify anti-tamper comes after workdir allow
	workdirResolved := canonicalize(workdir)
	workdirAllow := `(allow file-read* file-write* (subpath "` + workdirResolved + `"))`
	workdirIdx := strings.Index(profile, workdirAllow)
	antiTamperIdx := strings.Index(profile, antiTamper)
	if workdirIdx >= antiTamperIdx {
		t.Error("anti-tamper deny-write must appear after workdir allow")
	}

	// Verify all built-in deny paths are present
	for _, rel := range BuiltinDeniedRelPaths {
		expected := filepath.Join(resolvedHome, rel)
		deny := `(deny file-read* file-write* (subpath "` + expected + `"))`
		if !strings.Contains(profile, deny) {
			t.Errorf("profile must contain deny for %q", rel)
		}
	}
}

// TestBuildProfile_NoHome verifies graceful handling when HOME is empty.
func TestBuildProfile_NoHome(t *testing.T) {
	t.Setenv("HOME", "")

	workdir := t.TempDir()
	cfg := &config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}

	sb := NewSandboxExecSandbox(cfg, workdir)
	profile := sb.BuildProfileForInspection()

	// Should not contain any HOME-related rules
	if strings.Contains(profile, "(allow file-read* (subpath") && strings.Contains(profile, "Users") {
		t.Error("profile should not contain HOME read-allow when HOME is empty")
	}
	// Should still have basic structure
	if !strings.Contains(profile, "(deny default)") {
		t.Error("profile must contain (deny default)")
	}
}

// TestBuildProfile_ExtraDeniedPaths_OverrideWorkdir verifies that if an
// ExtraDeniedPaths entry is inside the workdir, the deny still wins.
func TestBuildProfile_ExtraDeniedPaths_OverrideWorkdir(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	workdir := t.TempDir()
	sensitiveInWorkdir := filepath.Join(workdir, "vendor", "secrets")
	os.MkdirAll(sensitiveInWorkdir, 0755)

	cfg := &config.SandboxConfig{
		Enabled:          true,
		Backend:          "native",
		NetworkAccess:    false,
		ExtraDeniedPaths: []string{sensitiveInWorkdir},
	}

	sb := NewSandboxExecSandbox(cfg, workdir)
	profile := sb.BuildProfileForInspection()

	resolvedSensitive := canonicalize(sensitiveInWorkdir)
	deny := `(deny file-read* file-write* (subpath "` + resolvedSensitive + `"))`
	if !strings.Contains(profile, deny) {
		t.Errorf("profile must deny extra path inside workdir:\n  want: %s", deny)
	}

	// The deny must come after the workdir allow
	workdirResolved := canonicalize(workdir)
	workdirAllow := `(allow file-read* file-write* (subpath "` + workdirResolved + `"))`
	workdirIdx := strings.Index(profile, workdirAllow)
	denyIdx := strings.Index(profile, deny)
	if workdirIdx >= denyIdx {
		t.Error("ExtraDeniedPaths deny must appear after workdir allow")
	}
}

// TestValidateSBPLPath verifies that paths with unsafe characters are rejected.
func TestValidateSBPLPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "normal path", path: "/Users/alice/project", wantErr: false},
		{name: "path with spaces", path: "/Users/alice/my project", wantErr: false},
		{name: "path with quote", path: "/tmp/dir\"inject", wantErr: true},
		{name: "path with backslash", path: "/tmp/dir\\inject", wantErr: true},
		{name: "path with newline", path: "/tmp/dir\ninject", wantErr: true},
		{name: "path with tab", path: "/tmp/dir\tinject", wantErr: true},
		{name: "path with null byte", path: "/tmp/dir\x00inject", wantErr: true},
		{name: "empty path", path: "", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSBPLPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSBPLPath(%q) error = %v, wantErr = %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

// TestBuildProfile_RejectsUnsafePaths verifies that buildProfile returns an
// error (not a broken SBPL profile) when ExtraWritablePaths or ExtraDeniedPaths
// contain characters that would inject into the profile.
func TestBuildProfile_RejectsUnsafePaths(t *testing.T) {
	workdir := t.TempDir()
	t.Setenv("HOME", workdir)

	tests := []struct {
		name string
		cfg  *config.SandboxConfig
	}{
		{
			name: "quote in ExtraWritablePaths",
			cfg: &config.SandboxConfig{
				Enabled:            true,
				ExtraWritablePaths: []string{"/tmp/dir\"inject"},
			},
		},
		{
			name: "newline in ExtraDeniedPaths",
			cfg: &config.SandboxConfig{
				Enabled:          true,
				ExtraDeniedPaths: []string{"/secrets/vault\n(allow network*)"},
			},
		},
		{
			name: "backslash in ExtraReadOnlyPaths",
			cfg: &config.SandboxConfig{
				Enabled:            true,
				ExtraReadOnlyPaths: []string{"/opt/tools\\ninjected"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := NewSandboxExecSandbox(tt.cfg, workdir)
			_, err := sb.buildProfile(workdir)
			if err == nil {
				t.Errorf("buildProfile should have returned an error for unsafe path in %s", tt.name)
			}
		})
	}
}

// ── A3: environment variable whitelist ────────────────────────────────────────

// TestWrapCommand_EnvWhitelist_BlocksAPIKeys verifies that credential variables
// (NANO_*_API_KEY, NANO_*_SECRET, etc.) are NOT forwarded to the sandboxed
// subprocess.  Only the explicitly allowlisted variables must appear in the
// wrapped command environment.
func TestWrapCommand_EnvWhitelist_BlocksAPIKeys(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("NANO_OPENAI_API_KEY", "sk-secret-openai")
	t.Setenv("NANO_ANTHROPIC_API_KEY", "sk-secret-anthropic")
	t.Setenv("NANO_DEEPSEEK_API_KEY", "sk-secret-deepseek")
	t.Setenv("NANO_SESSION_ID", "test-session-123")

	workdir := t.TempDir()
	cfg := &config.SandboxConfig{Enabled: true, Backend: "native", NetworkAccess: false}
	sb := NewSandboxExecSandbox(cfg, workdir)

	_, args, err := sb.WrapCommand(workdir, "echo", []string{"hello"})
	if err != nil {
		t.Fatalf("WrapCommand: %v", err)
	}

	envSection := extractEnvSection(args)

	// Credential keys must NOT be forwarded.
	for _, blocked := range []string{"NANO_OPENAI_API_KEY", "NANO_ANTHROPIC_API_KEY", "NANO_DEEPSEEK_API_KEY"} {
		for _, e := range envSection {
			if strings.HasPrefix(e, blocked+"=") {
				t.Errorf("credential variable %q must not be forwarded to sandbox; got %q", blocked, e)
			}
		}
	}

	// PATH and HOME must always be forwarded.
	mustHavePrefix(t, envSection, "PATH=")
	mustHavePrefix(t, envSection, "HOME=")
}

// TestWrapCommand_EnvWhitelist_AllowsSafeNanoVars verifies that the safe NANO_*
// variables (session ID, workspace, etc.) are forwarded to the sandbox.
func TestWrapCommand_EnvWhitelist_AllowsSafeNanoVars(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("NANO_SESSION_ID", "sess-abc")
	t.Setenv("NANO_WORKSPACE", "/workspace/project")
	t.Setenv("NANO_ORCHESTRATOR_MODE", "1")
	t.Setenv("NANO_SANDBOX_MODE", "native")

	workdir := t.TempDir()
	cfg := &config.SandboxConfig{Enabled: true, Backend: "native", NetworkAccess: false}
	sb := NewSandboxExecSandbox(cfg, workdir)

	_, args, err := sb.WrapCommand(workdir, "echo", []string{"hi"})
	if err != nil {
		t.Fatalf("WrapCommand: %v", err)
	}

	envSection := extractEnvSection(args)

	// Safe NANO_* vars must be forwarded.
	for _, safe := range []string{
		"NANO_SESSION_ID=sess-abc",
		"NANO_WORKSPACE=/workspace/project",
		"NANO_ORCHESTRATOR_MODE=1",
		"NANO_SANDBOX_MODE=native",
	} {
		found := false
		for _, e := range envSection {
			if e == safe {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("safe variable %q not found in sandbox env; envSection=%v", safe, envSection)
		}
	}
}

// TestWrapCommand_EnvWhitelist_UnknownNanoVarBlocked verifies that an arbitrary
// NANO_* variable that is not in the allowlist is not forwarded.
func TestWrapCommand_EnvWhitelist_UnknownNanoVarBlocked(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("NANO_UNKNOWN_VAR", "should-not-appear")

	workdir := t.TempDir()
	cfg := &config.SandboxConfig{Enabled: true, Backend: "native", NetworkAccess: false}
	sb := NewSandboxExecSandbox(cfg, workdir)

	_, args, err := sb.WrapCommand(workdir, "true", nil)
	if err != nil {
		t.Fatalf("WrapCommand: %v", err)
	}

	envSection := extractEnvSection(args)
	for _, e := range envSection {
		if strings.HasPrefix(e, "NANO_UNKNOWN_VAR=") {
			t.Errorf("unknown NANO_ variable must not be forwarded; got %q", e)
		}
	}
}

// extractEnvSection returns the env key=value entries from the wrapped args.
// WrapCommand returns ("/usr/bin/env", ["-i", "KEY=val", ..., "sandbox-exec", ...]).
// We extract everything between "-i" and the first "sandbox-exec" entry.
func extractEnvSection(args []string) []string {
	var section []string
	inEnv := false
	for _, a := range args {
		if a == "-i" {
			inEnv = true
			continue
		}
		if a == "sandbox-exec" {
			break
		}
		if inEnv {
			section = append(section, a)
		}
	}
	return section
}

// mustHavePrefix asserts that at least one entry in envSection starts with prefix.
func mustHavePrefix(t *testing.T, envSection []string, prefix string) {
	t.Helper()
	for _, e := range envSection {
		if strings.HasPrefix(e, prefix) {
			return
		}
	}
	t.Errorf("expected an entry starting with %q in sandbox env; got %v", prefix, envSection)
}
