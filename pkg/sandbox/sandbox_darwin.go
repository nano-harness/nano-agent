//go:build darwin

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// newBwrapSandbox is a stub on Darwin – bwrap is a Linux-only tool.
func newBwrapSandbox(cfg *config.SandboxConfig, _ string) Sandbox {
	logger.Warnf("sandbox: bwrap is only supported on Linux – falling back to no isolation")
	return &NoopSandbox{}
}

// SandboxExecSandbox wraps commands using macOS sandbox-exec on Darwin.
// It generates an SBPL (Scheme-based Sandbox Policy Language) profile at
// runtime and passes it to sandbox-exec via the -p flag so that no temporary
// file is written to disk.
type SandboxExecSandbox struct {
	cfg        *config.SandboxConfig
	workingDir string
}

// newSandboxExecSandbox creates a SandboxExecSandbox.
// sandbox-exec is always present on macOS so no binary look-up is needed.
func newSandboxExecSandbox(cfg *config.SandboxConfig, workingDir string) Sandbox {
	return &SandboxExecSandbox{cfg: cfg, workingDir: workingDir}
}

// IsEnabled always returns true for a SandboxExecSandbox.
func (s *SandboxExecSandbox) IsEnabled() bool { return true }

// Backend returns the native backend family.
func (s *SandboxExecSandbox) Backend() Backend { return BackendNative }

// BackendDetail returns the concrete macOS backend.
func (s *SandboxExecSandbox) BackendDetail() string { return "sandbox-exec" }

// WrapCommand transforms (cmd, args) into a sandbox-exec-wrapped invocation:
//
//	/usr/bin/env -i KEY=val ... sandbox-exec -p <profile> cmd args...
//
// The env wrapper is used to sanitize the environment, preventing secrets
// from leaking into the sandbox.
func (s *SandboxExecSandbox) WrapCommand(workingDir, cmd string, args []string) (string, []string, error) {
	dir := workingDir
	if dir == "" {
		dir = s.workingDir
	}
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", nil, fmt.Errorf("sandbox: cannot determine working directory: %w", err)
		}
	}
	// Resolve symlinks so HOME and the subpath rule below both use the
	// canonical path the kernel will compare against.
	dir = canonicalize(dir)

	profile := s.buildProfile(dir)

	// Build environment whitelist - only pass through safe variables
	var envArgs []string
	for _, e := range os.Environ() {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		// Only pass through NANO_* prefixed variables and essential runtime vars
		switch {
		case strings.HasPrefix(k, "NANO_"),
			k == "PATH", k == "TERM", k == "LANG", k == "LC_ALL":
			envArgs = append(envArgs, k+"="+v)
		}
	}
	// Set HOME to the working directory (sandbox doesn't allow access to real home)
	envArgs = append(envArgs, "HOME="+dir)

	// Build the full command: env -i <env...> sandbox-exec -p <profile> <cmd> <args...>
	wrapped := []string{"-i"}
	wrapped = append(wrapped, envArgs...)
	wrapped = append(wrapped, "sandbox-exec", "-p", profile, cmd)
	wrapped = append(wrapped, args...)
	return "/usr/bin/env", wrapped, nil
}

// canonicalize returns the symlink-resolved absolute path so SBPL subpath rules
// match the kernel's canonical view. macOS resolves /var -> /private/var,
// /tmp -> /private/tmp, etc. before evaluating sandbox policy, so a rule like
// (subpath "/var/folders/...") never matches a write to that path because the
// kernel sees "/private/var/folders/...". On error (path doesn't exist, etc.)
// the input is returned unchanged so policy generation still proceeds.
func canonicalize(p string) string {
	if p == "" {
		return p
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// buildProfile generates the SBPL profile string.
func (s *SandboxExecSandbox) buildProfile(workingDir string) string {
	var b strings.Builder

	b.WriteString("(version 1)\n")
	// Import bsd.sb to provide the minimal BSD foundation required by macOS.
	// Without this, (deny default) blocks file-read-data on /, causing dyld
	// to abort before main() is reached. This is the critical fix for P0 issue
	// where all sandbox-exec commands were failing with abort_trap.
	b.WriteString("(import \"bsd.sb\")\n")
	b.WriteString("(deny default)\n")

	// ── Read-only system paths ───────────────────────────────────────────────
	b.WriteString("(allow file-read*\n")
	for _, p := range []string{"/usr", "/bin", "/sbin", "/lib", "/etc",
		"/System", "/private/etc", "/private/var/db",
		"/Library/Preferences", "/dev"} {
		fmt.Fprintf(&b, "  (subpath %q)\n", p)
	}
	// /private/var/db already includes the dyld cache subdirectory
	b.WriteString(")\n")

	// ── Working directory: read + write ─────────────────────────────────────
	if workingDir != "" {
		fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %q))\n", canonicalize(workingDir))
	}

	// ── /tmp: read + write ───────────────────────────────────────────────────
	b.WriteString("(allow file-read* file-write* (subpath \"/tmp\"))\n")
	b.WriteString("(allow file-read* file-write* (subpath \"/private/tmp\"))\n")

	// ── Extra read-only paths ────────────────────────────────────────────────
	for _, p := range s.cfg.ExtraReadOnlyPaths {
		fmt.Fprintf(&b, "(allow file-read* (subpath %q))\n", canonicalize(p))
	}

	// ── Extra writable paths ─────────────────────────────────────────────────
	for _, p := range s.cfg.ExtraWritablePaths {
		fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %q))\n", canonicalize(p))
	}

	// ── Network ─────────────────────────────────────────────────────────────
	if s.cfg.NetworkAccess {
		b.WriteString("(allow network*)\n")
	} else {
		b.WriteString("(deny network*)\n")
	}

	// ── Process / IPC ────────────────────────────────────────────────────────
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow process-fork)\n")

	// Mach IPC lookups are required for basic macOS system calls.
	b.WriteString("(allow mach-lookup)\n")
	b.WriteString("(allow mach-register)\n")

	// Signal sending to own process group.
	b.WriteString("(allow signal (target self))\n")

	// POSIX IPC.
	b.WriteString("(allow ipc-posix*)\n")

	// System-wide entitlements required for basic shell execution.
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow system-socket)\n")

	return b.String()
}
