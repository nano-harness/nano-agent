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
