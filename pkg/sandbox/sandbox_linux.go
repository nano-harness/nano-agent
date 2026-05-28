//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// SandboxExecSandbox is a stub on Linux – sandbox-exec is a macOS-only tool.
type SandboxExecSandbox struct {
	cfg        *config.SandboxConfig
	workingDir string
}

// NewSandboxExecSandbox is a stub on Linux.
func NewSandboxExecSandbox(cfg *config.SandboxConfig, workingDir string) *SandboxExecSandbox {
	return &SandboxExecSandbox{cfg: cfg, workingDir: workingDir}
}

// BuildProfileForInspection is a stub on Linux.
func (s *SandboxExecSandbox) BuildProfileForInspection() string {
	return "# sandbox-exec profiles are only available on macOS\n"
}

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

// newBwrapSandbox creates a BwrapSandbox.  If bwrap cannot be located or if
// user namespaces are disabled/restricted, it falls back to a NoopSandbox
// and logs a warning.
func newBwrapSandbox(cfg *config.SandboxConfig, workingDir string) Sandbox {
	bwrapPath, err := lookupBwrap(cfg.BwrapPath)
	if err != nil {
		logger.Warnf("sandbox: %v – falling back to no isolation", err)
		return &NoopSandbox{}
	}

	// Self-test: verify that user namespaces are actually available.
	// This catches environments where kernel.unprivileged_userns_clone=0,
	// missing CAP_SYS_ADMIN inside docker, or seccomp filters that block
	// clone(CLONE_NEWUSER). Without this check, bwrap commands would fail
	// silently at execution time.
	probe := exec.Command(bwrapPath, "--unshare-all", "--ro-bind", "/", "/", "--", "/bin/true")
	if err := probe.Run(); err != nil {
		logger.Warnf("sandbox: bwrap self-test failed (likely user namespaces disabled or restricted): %v – falling back to no isolation", err)
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

	// ── Environment isolation ────────────────────────────────────────────────
	// Clear all environment variables and only pass through a safe whitelist.
	// This prevents leaking secrets like OPENAI_API_KEY, AWS_*, GITHUB_TOKEN,
	// SSH_AUTH_SOCK, and SYMPHONY_TOKEN into the sandboxed environment.
	a = append(a, "--clearenv")
	for _, e := range os.Environ() {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		// Only pass through NANO_* prefixed variables and essential runtime vars
		switch {
		case strings.HasPrefix(k, "NANO_"),
			k == "PATH", k == "TERM", k == "LANG", k == "LC_ALL":
			a = append(a, "--setenv", k, v)
		}
	}

	// ── Pseudo-filesystems ───────────────────────────────────────────────────
	a = append(a, "--proc", "/proc")
	a = append(a, "--dev", "/dev")
	a = append(a, "--tmpfs", "/tmp")
	// Add /dev/shm for POSIX shared memory support (needed by multiprocessing,
	// semaphores, and tools like PostgreSQL/Redis clients)
	a = append(a, "--tmpfs", "/dev/shm")
	// Add /run for systemd integration (dbus, journal, user runtime dir)
	a = append(a, "--tmpfs", "/run")

	// ── Read-only system paths ───────────────────────────────────────────────
	// Handle symlinks properly on /usr-merge systems (Debian 12+, Ubuntu 24.04+)
	// where /bin -> usr/bin, /lib -> usr/lib, etc. are symlinks.
	for _, p := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc"} {
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		// If it's a symlink, preserve it as a symlink in the sandbox
		if fi.Mode()&os.ModeSymlink != 0 {
			target, lerr := os.Readlink(p)
			if lerr != nil {
				continue
			}
			a = append(a, "--symlink", target, p)
			continue
		}
		// Otherwise bind it as a directory
		if fi.IsDir() {
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

	// ── HOME environment variable ────────────────────────────────────────────
	// HOME must point to a directory that exists inside the sandbox.
	// The host HOME is not mounted, so pointing to it would cause tools
	// that use $HOME to fail. Set it to the working directory instead.
	if workingDir != "" {
		a = append(a, "--setenv", "HOME", workingDir)
	}

	// ── Process lifecycle ────────────────────────────────────────────────────
	// Ensure sandboxed processes die when nano-agent terminates, preventing
	// orphaned processes that could accumulate and cause resource issues.
	a = append(a, "--die-with-parent")

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
