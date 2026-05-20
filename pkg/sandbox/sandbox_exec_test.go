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

// TestSandboxExec_Actually_Runs_Trivial_Command verifies that sandbox-exec
// can actually execute a minimal command without abort_trap.
func TestSandboxExec_Actually_Runs_Trivial_Command(t *testing.T) {
	tmp := t.TempDir()
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, tmp)
	cmd, args, err := sb.WrapCommand(tmp, "/usr/bin/true", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(cmd, args...).Run(); err != nil {
		t.Fatalf("/usr/bin/true must succeed under sandbox-exec, got: %v", err)
	}
}

// TestSandboxExec_Allows_Workdir_Write verifies that sandboxed processes
// can write inside their working directory.
func TestSandboxExec_Allows_Workdir_Write(t *testing.T) {
	tmp := t.TempDir()
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, tmp)
	target := filepath.Join(tmp, "ok.txt")
	cmd, args, err := sb.WrapCommand(tmp, "/bin/sh", []string{"-c", "echo hi > " + target})
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(cmd, args...).Run(); err != nil {
		t.Fatalf("write inside workdir must succeed, got: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected %s to exist, got: %v", target, err)
	}
}

// TestSandboxExec_Blocks_Outside_Write verifies that the sandbox blocks
// writes to paths outside the working directory (e.g., user home).
func TestSandboxExec_Blocks_Outside_Write(t *testing.T) {
	tmp := t.TempDir()
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, tmp)
	home, _ := os.UserHomeDir()
	target := filepath.Join(home, ".sb-test-pwned-"+t.Name())
	defer os.Remove(target)
	cmd, args, err := sb.WrapCommand(tmp, "/bin/sh", []string{"-c", "echo pwned > " + target})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(cmd, args...).CombinedOutput()
	if err == nil {
		t.Fatalf("write to home should be blocked, but succeeded; out=%q", out)
	}
	if !strings.Contains(string(out), "Operation not permitted") {
		t.Logf("expected EPERM from sandbox, got: %q (non-fatal: sandbox may use different error)", out)
	}
}

// TestSandboxExec_Blocks_Network_When_Denied verifies that network access
// is blocked when NetworkAccess=false.
func TestSandboxExec_Blocks_Network_When_Denied(t *testing.T) {
	tmp := t.TempDir()
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, tmp)
	cmd, args, err := sb.WrapCommand(tmp, "/usr/bin/curl", []string{
		"-sS", "--max-time", "2", "https://example.com", "-o", "/dev/null",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(cmd, args...).Run(); err == nil {
		t.Fatal("network must be blocked when NetworkAccess=false, but curl succeeded")
	}
}

// TestSandboxExec_Strips_Secrets verifies that environment variables
// containing secrets are not leaked into the sandbox.
func TestSandboxExec_Strips_Secrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-leak-me")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "leak")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-leak")
	t.Setenv("GITHUB_TOKEN", "ghp_leak")
	tmp := t.TempDir()
	sb := New(&config.SandboxConfig{Enabled: true, Backend: "native"}, tmp)
	cmd, args, _ := sb.WrapCommand(tmp, "/usr/bin/env", nil)
	out, _ := exec.Command(cmd, args...).CombinedOutput()
	outStr := string(out)
	if strings.Contains(outStr, "sk-leak-me") {
		t.Errorf("OPENAI_API_KEY leaked into sandbox: %s", outStr)
	}
	if strings.Contains(outStr, "AWS_SECRET_ACCESS_KEY=leak") {
		t.Errorf("AWS_SECRET_ACCESS_KEY leaked into sandbox: %s", outStr)
	}
	if strings.Contains(outStr, "sk-ant-leak") {
		t.Errorf("ANTHROPIC_API_KEY leaked into sandbox: %s", outStr)
	}
	if strings.Contains(outStr, "ghp_leak") {
		t.Errorf("GITHUB_TOKEN leaked into sandbox: %s", outStr)
	}
	// Verify that PATH is present (should be whitelisted)
	if !strings.Contains(outStr, "PATH=") {
		t.Errorf("PATH should be present in sandbox environment")
	}
}

// TestSandboxExec_Resolves_Symlinked_Workdir guards against the regression where
// (subpath %q) using a literal path like "/var/folders/..." did not match the
// kernel's canonical view "/private/var/folders/..." on macOS, causing all
// writes inside t.TempDir() to fail with EPERM. See PR #183 for the original fix.
func TestSandboxExec_Resolves_Symlinked_Workdir(t *testing.T) {
	tmp := t.TempDir()
	// On macOS, t.TempDir() typically returns "/var/folders/...", which is a
	// symlink to "/private/var/folders/...". The sandbox kernel canonicalizes
	// before policy evaluation, so the profile rule must too.
	resolved, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatalf("evalsymlinks(%q): %v", tmp, err)
	}
	if resolved == tmp {
		t.Skipf("test only meaningful when t.TempDir() returns a symlinked path; got non-symlinked %q", tmp)
	}

	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, tmp)
	target := filepath.Join(tmp, "ok.txt")
	cmd, args, err := sb.WrapCommand(tmp, "/bin/sh", []string{"-c", "echo hi > " + target})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cmd, args...).CombinedOutput(); err != nil {
		t.Fatalf("write to symlinked workdir must succeed (regression: subpath rule did not canonicalize)\n  out=%q\n  err=%v", out, err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected %s to exist, got: %v", target, err)
	}
}

// TestSandboxExec_Resolves_Symlinked_ExtraWritablePath verifies that
// canonicalization also applies to ExtraWritablePaths so user-configured
// paths like /var/log/foo work as expected.
func TestSandboxExec_Resolves_Symlinked_ExtraWritablePath(t *testing.T) {
	tmp := t.TempDir()
	resolved, err := filepath.EvalSymlinks(tmp)
	if err != nil || resolved == tmp {
		t.Skip("test only meaningful on macOS where t.TempDir() returns a symlinked path")
	}

	// Workdir on /Users (non-symlinked), extra writable on /var/folders (symlinked).
	workdir := t.TempDir() // we'll only use tmp for ExtraWritablePaths
	if home, _ := os.UserHomeDir(); home != "" {
		// Prefer a clearly-non-symlinked workdir under home to isolate the test
		// to the ExtraWritablePaths path.
		if d, mkErr := os.MkdirTemp(home, "sb-test-"); mkErr == nil {
			defer os.RemoveAll(d)
			workdir = d
		}
	}

	cfg := &config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
		ExtraWritablePaths: []string{tmp}, // symlinked /var/folders path
	}
	sb := New(cfg, workdir)
	target := filepath.Join(tmp, "extra-ok.txt")
	cmd, args, _ := sb.WrapCommand(workdir, "/bin/sh", []string{"-c", "echo hi > " + target})
	if out, err := exec.Command(cmd, args...).CombinedOutput(); err != nil {
		t.Fatalf("write to symlinked ExtraWritablePath must succeed\n  out=%q\n  err=%v", out, err)
	}
}
