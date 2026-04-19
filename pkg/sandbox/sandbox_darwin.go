//go:build darwin

package sandbox

import (
	"fmt"
	"os"
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

// WrapCommand transforms (cmd, args) into a sandbox-exec-wrapped invocation:
//
//	sandbox-exec -p <profile> cmd args...
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

	profile := s.buildProfile(dir)
	wrapped := []string{"-p", profile, cmd}
	wrapped = append(wrapped, args...)
	return "sandbox-exec", wrapped, nil
}

// buildProfile generates the SBPL profile string.
func (s *SandboxExecSandbox) buildProfile(workingDir string) string {
	var b strings.Builder

	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")

	// ── Read-only system paths ───────────────────────────────────────────────
	b.WriteString("(allow file-read*\n")
	for _, p := range []string{"/usr", "/bin", "/sbin", "/lib", "/etc",
		"/System", "/private/etc", "/private/var/db",
		"/Library/Preferences", "/dev"} {
		fmt.Fprintf(&b, "  (subpath %q)\n", p)
	}
	// Allow reading the macOS dynamic linker cache.
	b.WriteString("  (subpath \"/private/var/db/dyld\")\n")
	b.WriteString(")\n")

	// ── Working directory: read + write ─────────────────────────────────────
	if workingDir != "" {
		fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %q))\n", workingDir)
	}

	// ── /tmp: read + write ───────────────────────────────────────────────────
	b.WriteString("(allow file-read* file-write* (subpath \"/tmp\"))\n")
	b.WriteString("(allow file-read* file-write* (subpath \"/private/tmp\"))\n")

	// ── Extra read-only paths ────────────────────────────────────────────────
	for _, p := range s.cfg.ExtraReadOnlyPaths {
		fmt.Fprintf(&b, "(allow file-read* (subpath %q))\n", p)
	}

	// ── Extra writable paths ─────────────────────────────────────────────────
	for _, p := range s.cfg.ExtraWritablePaths {
		fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %q))\n", p)
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
