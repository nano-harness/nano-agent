// Package sandbox provides process-level sandboxing for shell command execution
// and path-level access control for filesystem tools.
//
// Shell commands are wrapped using OS-native tools:
//   - Linux: Bubblewrap (bwrap)
//   - macOS: sandbox-exec
//
// Filesystem tools (read_file, write_file, etc.) use PathChecker for
// AllowedPaths / BlockedPaths / ReadOnlyPaths validation.
package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// defaultBlockedPaths lists individual files that are always blocked,
// regardless of user configuration.
var defaultBlockedPaths = []string{
	"/etc/shadow", "/etc/gshadow", "/etc/sudoers", "/etc/passwd", "/etc/group",
}

// defaultBlockedDirs lists directory path suffixes (slash-normalised) that are
// always blocked. Matching uses exact equality (path IS the dir) or prefix+"/",
// so /home/user/.ssh and /root/.ssh are both caught.
var defaultBlockedDirs = []string{
	"/.ssh", "/.gnupg", "/.aws", "/.kube", "/.docker",
}

// FileOperation represents a filesystem operation type.
type FileOperation int

const (
	// OpRead represents a file read operation.
	OpRead FileOperation = iota //nolint:revive
	// OpWrite represents a file write/create operation.
	OpWrite
	// OpDelete represents a file delete operation.
	OpDelete
	// OpList represents a directory listing operation.
	OpList
)

// IsWrite returns true when the operation mutates the filesystem.
func (op FileOperation) IsWrite() bool {
	return op == OpWrite || op == OpDelete
}

// String returns a human-readable name for the operation.
func (op FileOperation) String() string {
	switch op {
	case OpRead:
		return "read"
	case OpWrite:
		return "write"
	case OpDelete:
		return "delete"
	case OpList:
		return "list"
	default:
		return "unknown"
	}
}

// Sandbox wraps shell commands with OS-native isolation (bwrap / sandbox-exec).
// For platforms without a supported backend, commands are passed through unchanged.
type Sandbox interface {
	// WrapCommand wraps the given command and arguments for sandboxed execution.
	// Returns the (possibly modified) executable and argument list.
	// workingDir is the directory the command will run in.
	WrapCommand(workingDir, cmd string, args []string) (string, []string, error)

	// IsEnabled reports whether this sandbox will actually apply restrictions.
	IsEnabled() bool
}

// PathChecker enforces AllowedPaths / BlockedPaths / ReadOnlyPaths for
// filesystem tools that run inside the Go process (not as a child process).
type PathChecker struct {
	cfg *config.SandboxConfig
}

// NewPathChecker constructs a PathChecker.  A nil config produces a checker
// with sandboxing enabled by default (secure-by-default).
func NewPathChecker(cfg *config.SandboxConfig) *PathChecker {
	if cfg == nil {
		return &PathChecker{cfg: &config.SandboxConfig{Enabled: true}}
	}
	return &PathChecker{cfg: cfg}
}

// IsEnabled reports whether path-level checks are active.
func (p *PathChecker) IsEnabled() bool {
	return p.cfg != nil && p.cfg.Enabled
}

// Check validates whether the given path is accessible for the requested
// operation.  Returns a descriptive error when access is denied.
func (p *PathChecker) Check(path string, op FileOperation) error {
	if !p.IsEnabled() {
		return nil
	}

	absPath, err := p.resolve(path)
	if err != nil {
		return fmt.Errorf("sandbox: invalid path %q: %w", path, err)
	}

	// Blocked paths take priority.
	if p.matchesAny(absPath, p.cfg.BlockedPaths) {
		return fmt.Errorf("sandbox: access denied – path %q is blocked", absPath)
	}

	// Always enforce the hard-coded sensitive path blacklist.
	// We check both the symlink-resolved path and the pre-resolution clean path
	// because on some OSes (e.g. macOS) /etc is a symlink to /private/etc, so
	// EvalSymlinks would turn /etc/passwd into /private/etc/passwd, bypassing the
	// pattern "/etc/passwd".  Comparing both paths ensures detection in all cases.
	cleanAbs := filepath.Clean(path)
	if !filepath.IsAbs(cleanAbs) {
		if wd, wdErr := os.Getwd(); wdErr == nil {
			cleanAbs = filepath.Join(wd, cleanAbs)
		}
	}
	slashPath := filepath.ToSlash(absPath)
	slashClean := filepath.ToSlash(cleanAbs)
	for _, blocked := range defaultBlockedPaths {
		if slashPath == blocked || slashClean == blocked {
			return fmt.Errorf("sandbox: access denied – path %q is a protected system file", absPath)
		}
	}
	for _, dir := range defaultBlockedDirs {
		if slashPath == dir || strings.HasPrefix(slashPath, dir+"/") ||
			slashClean == dir || strings.HasPrefix(slashClean, dir+"/") {
			return fmt.Errorf("sandbox: access denied – path %q is in a protected directory", absPath)
		}
	}

	// If an allowlist is configured, the path must match it.
	if len(p.cfg.AllowedPaths) > 0 && !p.matchesAny(absPath, p.cfg.AllowedPaths) {
		return fmt.Errorf("sandbox: access denied – path %q is not in allowed_paths", absPath)
	}

	// Write operations are rejected for read-only paths.
	if op.IsWrite() && p.matchesAny(absPath, p.cfg.ReadOnlyPaths) {
		return fmt.Errorf("sandbox: access denied – path %q is read-only", absPath)
	}

	return nil
}

// resolve converts a path to a clean absolute path, resolving symlinks to
// prevent traversal attacks.
func (p *PathChecker) resolve(path string) (string, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("cannot determine working directory: %w", err)
		}
		clean = filepath.Join(wd, clean)
	}
	// EvalSymlinks resolves all symlinks in the path.  If the file does not
	// exist yet (e.g. a write target), resolve the nearest existing parent.
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		if os.IsNotExist(err) {
			parent := filepath.Dir(clean)
			realParent, err2 := filepath.EvalSymlinks(parent)
			if err2 != nil {
				return clean, nil // best-effort: return cleaned absolute path
			}
			return filepath.Join(realParent, filepath.Base(clean)), nil
		}
		return "", fmt.Errorf("symlink resolution failed: %w", err)
	}
	return real, nil
}

// matchesAny returns true when path matches at least one entry in patterns.
// Matching rules (in order):
//  1. Exact string match
//  2. Path is inside the pattern directory (prefix + separator)
//  3. filepath.Match glob
//
// Patterns are also resolved via EvalSymlinks so that /etc matches /private/etc on macOS.
func (p *PathChecker) matchesAny(path string, patterns []string) bool {
	for _, pat := range patterns {
		// Also resolve symlinks in the pattern for cross-platform consistency (e.g. /tmp → /private/tmp on macOS).
		if resolved, err := filepath.EvalSymlinks(pat); err == nil {
			pat = resolved
		}
		if path == pat {
			return true
		}
		if strings.HasPrefix(path, pat+string(filepath.Separator)) {
			return true
		}
		if matched, err := filepath.Match(pat, path); err == nil && matched {
			return true
		}
	}
	return false
}

// ── Sandbox factory ──────────────────────────────────────────────────────────

// New returns the appropriate Sandbox implementation for the current platform.
// When cfg is nil or cfg.Enabled is false a NoopSandbox is returned.
func New(cfg *config.SandboxConfig, workingDir string) Sandbox {
	if cfg == nil || !cfg.Enabled {
		return &NoopSandbox{}
	}

	switch runtime.GOOS {
	case "linux":
		return newBwrapSandbox(cfg, workingDir)
	case "darwin":
		return newSandboxExecSandbox(cfg, workingDir)
	default:
		logger.Warnf("sandbox: unsupported platform %q – running without process isolation", runtime.GOOS)
		return &NoopSandbox{}
	}
}

// ── NoopSandbox ──────────────────────────────────────────────────────────────

// NoopSandbox passes commands through without modification.
type NoopSandbox struct{}

// WrapCommand returns the original command unchanged.
func (n *NoopSandbox) WrapCommand(_ string, cmd string, args []string) (string, []string, error) {
	return cmd, args, nil
}

// IsEnabled always returns false.
func (n *NoopSandbox) IsEnabled() bool { return false }
