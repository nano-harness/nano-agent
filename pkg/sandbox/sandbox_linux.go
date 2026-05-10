//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// newSandboxExecSandbox is a stub on Linux – sandbox-exec is a macOS-only tool.
func newSandboxExecSandbox(cfg *config.SandboxConfig, _ string) Sandbox {
	logger.Warnf("sandbox: sandbox-exec is only supported on macOS – falling back to no isolation")
	return &NoopSandbox{}
}

// BwrapSandbox wraps commands using Bubblewrap (bwrap) on Linux.
// It creates an unprivileged container with controlled filesystem visibility
// and optional network isolation.
type BwrapSandbox struct {
	cfg        *config.SandboxConfig
	workingDir string
	bwrapPath  string
}

// newBwrapSandbox creates a BwrapSandbox.  If bwrap cannot be located it falls
// back to a NoopSandbox and logs a warning.
func newBwrapSandbox(cfg *config.SandboxConfig, workingDir string) Sandbox {
	bwrapPath, err := lookupBwrap(cfg.BwrapPath)
	if err != nil {
		logger.Warnf("sandbox: %v – falling back to no isolation", err)
		return &NoopSandbox{}
	}
	return &BwrapSandbox{
		cfg:        cfg,
		workingDir: workingDir,
		bwrapPath:  bwrapPath,
	}
}

// IsEnabled always returns true for a BwrapSandbox (bwrap was found).
func (b *BwrapSandbox) IsEnabled() bool { return true }

// Backend returns the native backend family.
func (b *BwrapSandbox) Backend() Backend { return BackendNative }

// BackendDetail returns the concrete Linux backend.
func (b *BwrapSandbox) BackendDetail() string { return "bwrap" }

// WrapCommand transforms (cmd, args) into a bwrap-wrapped invocation.
//
// The resulting argument list follows the pattern:
//
//	bwrap <bwrap-flags> -- cmd args...
func (b *BwrapSandbox) WrapCommand(workingDir, cmd string, args []string) (string, []string, error) {
	dir := workingDir
	if dir == "" {
		dir = b.workingDir
	}
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", nil, fmt.Errorf("sandbox: cannot determine working directory: %w", err)
		}
	}

	bwrapArgs := b.buildBwrapArgs(dir)

	// Append the separator and the real command.
	bwrapArgs = append(bwrapArgs, "--")
	bwrapArgs = append(bwrapArgs, cmd)
	bwrapArgs = append(bwrapArgs, args...)

	return b.bwrapPath, bwrapArgs, nil
}

// buildBwrapArgs constructs the bwrap flag list (without the trailing "--" and user command).
func (b *BwrapSandbox) buildBwrapArgs(workingDir string) []string {
	var a []string

	// ── Namespace isolation ──────────────────────────────────────────────────
	// --unshare-all: isolate user, mount, UTS, IPC, PID, cgroup namespaces.
	// --share-net is added back only when network access is allowed.
	a = append(a, "--unshare-all")
	if b.cfg.NetworkAccess {
		a = append(a, "--share-net")
	}

	// ── Pseudo-filesystems ───────────────────────────────────────────────────
	a = append(a, "--proc", "/proc")
	a = append(a, "--dev", "/dev")
	a = append(a, "--tmpfs", "/tmp")

	// ── Read-only system paths ───────────────────────────────────────────────
	for _, p := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc"} {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			a = append(a, "--ro-bind", p, p)
		}
	}

	// ── Working directory: read-write ────────────────────────────────────────
	if workingDir != "" {
		if fi, err := os.Stat(workingDir); err == nil && fi.IsDir() {
			a = append(a, "--bind", workingDir, workingDir)
			a = append(a, "--chdir", workingDir)
		}
	}

	// ── Extra read-only paths from config ────────────────────────────────────
	for _, p := range b.cfg.ExtraReadOnlyPaths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			a = append(a, "--ro-bind", p, p)
		}
	}

	// ── Extra writable paths from config ─────────────────────────────────────
	for _, p := range b.cfg.ExtraWritablePaths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			a = append(a, "--bind", p, p)
		}
	}

	// Create a new session so the sandboxed process cannot interact with the
	// controlling terminal.
	a = append(a, "--new-session")

	return a
}

// lookupBwrap returns the path to the bwrap binary.
// cfg.BwrapPath is used when non-empty; otherwise exec.LookPath("bwrap") is tried.
func lookupBwrap(bwrapPath string) (string, error) {
	if bwrapPath != "" {
		if _, err := os.Stat(bwrapPath); err != nil {
			return "", fmt.Errorf("bwrap not found at configured path %q: %w", bwrapPath, err)
		}
		return bwrapPath, nil
	}
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return "", fmt.Errorf("bwrap not found in PATH – install bubblewrap (e.g. apt install bubblewrap): %w", err)
	}
	return path, nil
}
