//go:build darwin

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

// TestSandboxExec_Allows_Read_Anywhere_In_Home verifies that the blanket
// HOME read-allow lets sandboxed processes read arbitrary files under $HOME.
func TestSandboxExec_Allows_Read_Anywhere_In_Home(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Create test files
	for _, rel := range []string{".gitconfig", "notes.txt", "Documents/note.txt"} {
		p := filepath.Join(tempHome, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("test-content-"+rel), 0644); err != nil {
			t.Fatal(err)
		}
	}

	workdir := t.TempDir()
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, workdir)

	for _, rel := range []string{".gitconfig", "notes.txt", "Documents/note.txt"} {
		target := filepath.Join(tempHome, rel)
		cmd, args, err := sb.WrapCommand(workdir, "/bin/cat", []string{target})
		if err != nil {
			t.Fatal(err)
		}
		c := exec.Command(cmd, args...)
		c.Dir = workdir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Errorf("reading %s should succeed under sandbox, got err=%v out=%q", rel, err, out)
		}
	}
}

// TestSandboxExec_Denies_Sensitive_Home_Paths verifies that the built-in
// critical-path blacklist denies read access to sensitive directories.
func TestSandboxExec_Denies_Sensitive_Home_Paths(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Create sensitive files
	sensitiveFiles := []string{
		".ssh/id_rsa",
		".aws/credentials",
		".config/gh/hosts.yml",
	}
	for _, rel := range sensitiveFiles {
		p := filepath.Join(tempHome, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("secret"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	workdir := t.TempDir()
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, workdir)

	for _, rel := range sensitiveFiles {
		target := filepath.Join(tempHome, rel)
		cmd, args, err := sb.WrapCommand(workdir, "/bin/cat", []string{target})
		if err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(cmd, args...).CombinedOutput()
		if err == nil {
			t.Errorf("reading %s should be denied, but succeeded with out=%q", rel, out)
		}
	}
}

// TestSandboxExec_Operator_ExtraDeniedPaths_Override_Allow verifies that
// operator-supplied ExtraDeniedPaths deny access even where HOME read is allowed.
func TestSandboxExec_Operator_ExtraDeniedPaths_Override_Allow(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	secretDir := filepath.Join(tempHome, "secrets")
	if err := os.MkdirAll(secretDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretDir, "api.key"), []byte("top-secret"), 0644); err != nil {
		t.Fatal(err)
	}

	workdir := t.TempDir()

	// Without extra deny - should succeed
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, workdir)
	cmd, args, _ := sb.WrapCommand(workdir, "/bin/cat", []string{filepath.Join(secretDir, "api.key")})
	if err := exec.Command(cmd, args...).Run(); err != nil {
		t.Fatalf("without extra deny, cat should succeed, got: %v", err)
	}

	// With extra deny - should fail
	sb2 := New(&config.SandboxConfig{
		Enabled:          true,
		Backend:          "native",
		NetworkAccess:    false,
		ExtraDeniedPaths: []string{secretDir},
	}, workdir)
	cmd, args, _ = sb2.WrapCommand(workdir, "/bin/cat", []string{filepath.Join(secretDir, "api.key")})
	if err := exec.Command(cmd, args...).Run(); err == nil {
		t.Fatal("with ExtraDeniedPaths, cat should be denied")
	}
}

// TestSandboxExec_Writes_Still_Default_Deny_Outside_Workdir verifies that
// the HOME read-allow does not accidentally grant write access.
func TestSandboxExec_Writes_Still_Default_Deny_Outside_Workdir(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	workdir := t.TempDir()
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, workdir)

	target := filepath.Join(tempHome, "touched.txt")
	cmd, args, _ := sb.WrapCommand(workdir, "/bin/sh", []string{"-c", "echo x > " + target})
	if err := exec.Command(cmd, args...).Run(); err == nil {
		t.Fatal("write to HOME should be denied (writes default-deny outside workdir)")
	}
}

// TestSandboxExec_AntiTamper_Cannot_Rewrite_NanoNamespace verifies that
// the .nano/ directory inside workdir is write-protected.
func TestSandboxExec_AntiTamper_Cannot_Rewrite_NanoNamespace(t *testing.T) {
	workdir := t.TempDir()
	nanoDir := filepath.Join(workdir, ".nano")
	if err := os.MkdirAll(nanoDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nanoDir, "nano.yaml"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nanoDir, "scratch.txt"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, workdir)

	// Write to .nano/nano.yaml should fail
	cmd, args, _ := sb.WrapCommand(workdir, "/bin/sh", []string{"-c",
		"echo evil >> " + filepath.Join(nanoDir, "nano.yaml")})
	if err := exec.Command(cmd, args...).Run(); err == nil {
		t.Error("write to .nano/nano.yaml should be denied (anti-tamper)")
	}

	// Write to .nano/scratch.txt should fail
	cmd, args, _ = sb.WrapCommand(workdir, "/bin/sh", []string{"-c",
		"echo evil >> " + filepath.Join(nanoDir, "scratch.txt")})
	if err := exec.Command(cmd, args...).Run(); err == nil {
		t.Error("write to .nano/scratch.txt should be denied (anti-tamper)")
	}

	// Read from .nano/nano.yaml should still succeed
	cmd, args, _ = sb.WrapCommand(workdir, "/bin/cat", []string{filepath.Join(nanoDir, "nano.yaml")})
	if out, err := exec.Command(cmd, args...).CombinedOutput(); err != nil {
		t.Errorf("read from .nano/nano.yaml should succeed, got err=%v out=%q", err, out)
	}
}

// TestSandboxExec_HOME_Passthrough verifies that the real HOME is passed
// through to the sandboxed environment (not rewritten to workdir).
func TestSandboxExec_HOME_Passthrough(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	workdir := t.TempDir()
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, workdir)

	cmd, args, _ := sb.WrapCommand(workdir, "/bin/sh", []string{"-c", "echo $HOME"})
	c := exec.Command(cmd, args...)
	c.Dir = workdir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("echo $HOME failed: %v, out=%q", err, out)
	}

	got := strings.TrimSpace(string(out))
	// HOME should be the real home (resolved), not the workdir
	resolvedHome := canonicalize(tempHome)
	if got != resolvedHome && got != tempHome {
		t.Errorf("HOME inside sandbox = %q, want %q or %q", got, tempHome, resolvedHome)
	}
	if got == workdir || got == canonicalize(workdir) {
		t.Errorf("HOME should not be workdir %q, got %q", workdir, got)
	}
}
