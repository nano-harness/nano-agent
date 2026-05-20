//go:build linux

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

// TestBwrap_Actually_Runs_Trivial_Command verifies that bwrap
// can actually execute a minimal command without failures.
func TestBwrap_Actually_Runs_Trivial_Command(t *testing.T) {
	tmp := t.TempDir()
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, tmp)
	if !sb.IsEnabled() {
		t.Skip("bwrap not available or user namespaces disabled")
	}
	cmd, args, err := sb.WrapCommand(tmp, "/bin/true", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(cmd, args...).Run(); err != nil {
		t.Fatalf("/bin/true must succeed under bwrap, got: %v", err)
	}
}

// TestBwrap_Allows_Workdir_Write verifies that sandboxed processes
// can write inside their working directory.
func TestBwrap_Allows_Workdir_Write(t *testing.T) {
	tmp := t.TempDir()
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, tmp)
	if !sb.IsEnabled() {
		t.Skip("bwrap not available or user namespaces disabled")
	}
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

// TestBwrap_Blocks_Outside_Write verifies that the sandbox blocks
// writes to paths outside the working directory.
func TestBwrap_Blocks_Outside_Write(t *testing.T) {
	tmp := t.TempDir()
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, tmp)
	if !sb.IsEnabled() {
		t.Skip("bwrap not available or user namespaces disabled")
	}
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
	// The exact error message may vary, so we're lenient
	if !strings.Contains(string(out), "No such file or directory") &&
		!strings.Contains(string(out), "Operation not permitted") &&
		!strings.Contains(string(out), "Permission denied") {
		t.Logf("expected filesystem error from sandbox, got: %q (non-fatal)", out)
	}
}

// TestBwrap_Blocks_Network_When_Denied verifies that network access
// is blocked when NetworkAccess=false.
func TestBwrap_Blocks_Network_When_Denied(t *testing.T) {
	tmp := t.TempDir()
	sb := New(&config.SandboxConfig{
		Enabled: true, Backend: "native", NetworkAccess: false,
	}, tmp)
	if !sb.IsEnabled() {
		t.Skip("bwrap not available or user namespaces disabled")
	}
	// Check if curl is available
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available in this environment")
	}
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

// TestBwrap_HOME_Points_Into_Sandbox verifies that HOME environment
// variable points to a directory that exists inside the sandbox.
func TestBwrap_HOME_Points_Into_Sandbox(t *testing.T) {
	tmp := t.TempDir()
	sb := New(&config.SandboxConfig{Enabled: true, Backend: "native"}, tmp)
	if !sb.IsEnabled() {
		t.Skip("bwrap not available or user namespaces disabled")
	}
	cmd, args, _ := sb.WrapCommand(tmp, "/bin/sh", []string{"-c", "echo $HOME && [ -d \"$HOME\" ]"})
	out, err := exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("HOME should point to an existing dir inside sandbox, got error: %v, output: %s", err, out)
	}
	homeInSandbox := strings.TrimSpace(string(out))
	if homeInSandbox != tmp {
		t.Logf("HOME=%s (expected %s)", homeInSandbox, tmp)
	}
}

// TestBwrap_Strips_Secrets verifies that environment variables
// containing secrets are not leaked into the sandbox.
func TestBwrap_Strips_Secrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-leak-me")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "leak")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-leak")
	t.Setenv("GITHUB_TOKEN", "ghp_leak")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent.sock")
	tmp := t.TempDir()
	sb := New(&config.SandboxConfig{Enabled: true, Backend: "native"}, tmp)
	if !sb.IsEnabled() {
		t.Skip("bwrap not available or user namespaces disabled")
	}
	cmd, args, _ := sb.WrapCommand(tmp, "/usr/bin/env", nil)
	out, _ := exec.Command(cmd, args...).CombinedOutput()
	outStr := string(out)
	if strings.Contains(outStr, "sk-leak-me") {
		t.Errorf("OPENAI_API_KEY leaked into sandbox: %s", outStr)
	}
	if strings.Contains(outStr, "AWS_SECRET_ACCESS_KEY") || strings.Contains(outStr, "leak") {
		t.Errorf("AWS_SECRET_ACCESS_KEY leaked into sandbox: %s", outStr)
	}
	if strings.Contains(outStr, "sk-ant-leak") {
		t.Errorf("ANTHROPIC_API_KEY leaked into sandbox: %s", outStr)
	}
	if strings.Contains(outStr, "ghp_leak") {
		t.Errorf("GITHUB_TOKEN leaked into sandbox: %s", outStr)
	}
	if strings.Contains(outStr, "SSH_AUTH_SOCK") {
		t.Errorf("SSH_AUTH_SOCK leaked into sandbox: %s", outStr)
	}
	// Verify that PATH is present (should be whitelisted)
	if !strings.Contains(outStr, "PATH=") {
		t.Errorf("PATH should be present in sandbox environment")
	}
}
