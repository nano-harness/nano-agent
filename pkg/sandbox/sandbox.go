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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/bmatcuk/doublestar/v4"
)

// Backend identifies the execution isolation backend selected for a sandbox.
type Backend string

const (
	// BackendNone means commands execute without process-level isolation.
	BackendNone Backend = "none"
	// BackendNative means the OS-native backend is used (bwrap or sandbox-exec).
	BackendNative Backend = "native"
	// BackendDocker means commands execute through a Docker container backend.
	BackendDocker Backend = "docker"
)

// BuiltinDeniedRelPaths is the hardcoded list of HOME-relative paths that are
// always denied (read+write) inside the macOS sandbox-exec profile. Operators
// can extend this list via ExtraDeniedPaths but cannot remove from it.
var BuiltinDeniedRelPaths = []string{
	".ssh", ".aws", ".gnupg", ".kube", ".config/gh", ".docker/config.json",
}

// NetworkPolicy describes whether the sandboxed command receives host network access.
type NetworkPolicy string

const (
	// NetworkInherited means the backend keeps its default network behavior.
	NetworkInherited NetworkPolicy = "inherited"
	// NetworkAllowed means network access is explicitly available to the sandbox.
	NetworkAllowed NetworkPolicy = "allowed"
	// NetworkDenied means network access is explicitly denied where the backend supports it.
	NetworkDenied NetworkPolicy = "denied"
)

// MountMode describes how a host path is exposed to the sandbox.
type MountMode string

const (
	// MountReadOnly exposes a path read-only.
	MountReadOnly MountMode = "ro"
	// MountReadWrite exposes a path read-write.
	MountReadWrite MountMode = "rw"
)

// Mount describes a host path made visible to a sandbox environment.
type Mount struct {
	HostPath string    `json:"host_path"`
	Path     string    `json:"path"`
	Mode     MountMode `json:"mode"`
}

// ResourceLimits captures command-level resource limits known to the sandbox layer.
// Existing native backends only receive these as metadata; stronger backends can enforce them.
type ResourceLimits struct {
	Timeout     time.Duration `json:"timeout,omitempty"`
	CPU         float64       `json:"cpu,omitempty"`           // Number of CPUs (e.g. 2.0). Maps to --cpus on Docker.
	MemoryMB    int           `json:"memory_mb,omitempty"`     // Maps to --memory on Docker.
	PIDsLimit   int           `json:"pids_limit,omitempty"`    // Maps to --pids-limit on Docker.
	MaxLifetime time.Duration `json:"max_lifetime,omitempty"`  // Hard wall-clock cap.
	MaxOutputKB int           `json:"max_output_kb,omitempty"` // Truncate stdout/stderr.
}

// SandboxRequest is the stable request protocol used by tool, hook, MCP, daemon,
// and future sub-agent execution paths to ask for a sandboxed command.
type SandboxRequest struct {
	Command        string                 `json:"command"`
	Args           []string               `json:"args,omitempty"`
	WorkingDir     string                 `json:"working_dir,omitempty"`
	Env            []string               `json:"-"`
	Network        NetworkPolicy          `json:"network,omitempty"`
	ResourceLimits ResourceLimits         `json:"resource_limits,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// SandboxEnvironment describes the prepared execution environment and the actual
// command that should be passed to exec.CommandContext.
type SandboxEnvironment struct {
	Backend        Backend                `json:"backend"`
	BackendDetail  string                 `json:"backend_detail,omitempty"`
	Enabled        bool                   `json:"enabled"`
	Command        string                 `json:"command"`
	Args           []string               `json:"args,omitempty"`
	WorkingDir     string                 `json:"working_dir,omitempty"`
	EnvNames       []string               `json:"env_names,omitempty"`
	Network        NetworkPolicy          `json:"network,omitempty"`
	Mounts         []Mount                `json:"mounts,omitempty"`
	ResourceLimits ResourceLimits         `json:"resource_limits,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// AuditMetadata returns a compact, non-secret summary suitable for tool metadata,
// event payloads, and audit logs.
func (e *SandboxEnvironment) AuditMetadata() map[string]interface{} {
	if e == nil {
		return nil
	}
	out := map[string]interface{}{
		"backend":        string(e.Backend),
		"backend_detail": e.BackendDetail,
		"enabled":        e.Enabled,
		"working_dir":    e.WorkingDir,
		"network":        string(e.Network),
	}
	if len(e.Mounts) > 0 {
		out["mounts"] = e.Mounts
	}
	if len(e.EnvNames) > 0 {
		out["env_names"] = e.EnvNames
	}
	if e.ResourceLimits.Timeout > 0 {
		out["resource_limits"] = map[string]interface{}{
			"timeout_seconds": e.ResourceLimits.Timeout.Seconds(),
		}
	}
	for k, v := range e.Metadata {
		out[k] = v
	}
	return out
}

// SandboxResult records the sandbox-relevant result fields for future audit/event use.
type SandboxResult struct {
	Environment *SandboxEnvironment    `json:"environment,omitempty"`
	ExitCode    int                    `json:"exit_code"`
	Duration    time.Duration          `json:"duration"`
	TimedOut    bool                   `json:"timed_out,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Runtime is the stable sandbox protocol. Existing backends are adapted through
// PrepareCommand without changing command execution behavior.
type Runtime interface {
	PrepareCommand(ctx context.Context, req SandboxRequest) (*SandboxEnvironment, error)
	Cleanup(ctx context.Context, env *SandboxEnvironment) error
}

const (
	EventTypeSandboxDecisionCreated       = "sandbox.decision.created"
	EventTypeSandboxEnvironmentCreated    = "sandbox.environment.created"
	EventTypeSandboxEnvironmentCleaned    = "sandbox.environment.cleaned"
	EventTypeSandboxFallbackUsed          = "sandbox.fallback.used"
	EventTypeSandboxCommandStarted        = "sandbox.command.started"
	EventTypeSandboxCommandFinished       = "sandbox.command.finished"
	EventTypeSandboxViolationDetected     = "sandbox.violation.detected"
	EventTypeSandboxResourceLimitExceeded = "sandbox.resource.limit.exceeded"
)

// IsSandboxEventType reports whether an event type belongs to the sandbox audit namespace.
func IsSandboxEventType(eventType string) bool {
	switch eventType {
	case EventTypeSandboxDecisionCreated,
		EventTypeSandboxEnvironmentCreated,
		EventTypeSandboxEnvironmentCleaned,
		EventTypeSandboxFallbackUsed,
		EventTypeSandboxCommandStarted,
		EventTypeSandboxCommandFinished,
		EventTypeSandboxViolationDetected,
		EventTypeSandboxResourceLimitExceeded:
		return true
	default:
		return false
	}
}

// Event is a lightweight sandbox audit event. Consumers adapt it to StreamEvent.
type Event struct {
	Type      string                 `json:"type"`
	Content   string                 `json:"content,omitempty"`
	Source    string                 `json:"source,omitempty"`
	Timestamp int64                  `json:"timestamp,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// EventPublisher is the minimal event surface used by SandboxRuntime.
type EventPublisher interface {
	PublishSandboxEvent(Event)
}

type eventPublisherContextKey struct{}

// WithEventPublisher attaches a sandbox event publisher to a context.
func WithEventPublisher(ctx context.Context, publisher EventPublisher) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if publisher == nil {
		return ctx
	}
	return context.WithValue(ctx, eventPublisherContextKey{}, publisher)
}

// EventPublisherFromContext retrieves a sandbox event publisher from context.
func EventPublisherFromContext(ctx context.Context) EventPublisher {
	if ctx == nil {
		return nil
	}
	publisher, _ := ctx.Value(eventPublisherContextKey{}).(EventPublisher)
	return publisher
}

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

// defaultBlockedPatterns contains glob patterns (doublestar) that are always blocked.
// These patterns use ** for recursive matching across path segments.
var defaultBlockedPatterns = []string{
	"**/.env",
	"**/.env.*",
	"**/credentials",
	"**/*.pem",
	"**/*.key",
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

	// Backend reports the coarse backend family used by this sandbox.
	Backend() Backend

	// BackendDetail reports the concrete backend implementation.
	BackendDetail() string
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

// expandHome expands ~ to the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// enforceDefaultBlocklist checks the given path against the always-enforced default blacklist.
// Returns an error if the path matches any default blocked path, dir, or pattern.
// This function is called unconditionally, even when sandbox is disabled.
func enforceDefaultBlocklist(absPath, cleanAbs string) error {
	// Get user home for expansion
	home, homeErr := os.UserHomeDir()

	// Check against default blocked paths
	slashPath := filepath.ToSlash(absPath)
	slashClean := filepath.ToSlash(cleanAbs)

	for _, blocked := range defaultBlockedPaths {
		if slashPath == blocked || slashClean == blocked {
			return fmt.Errorf("sandbox: access denied – path %q is a protected system file. Hint: use run_shell_command if you genuinely need access", absPath)
		}
	}

	// Check against default blocked directories
	for _, dir := range defaultBlockedDirs {
		// Also check home-expanded versions
		expandedDir := dir
		if homeErr == nil && strings.HasPrefix(dir, "~/") {
			expandedDir = filepath.ToSlash(filepath.Join(home, dir[2:]))
		}

		if slashPath == dir || strings.HasPrefix(slashPath, dir+"/") ||
			slashClean == dir || strings.HasPrefix(slashClean, dir+"/") {
			return fmt.Errorf("sandbox: access denied – path %q is in a protected directory. Hint: use run_shell_command if you genuinely need access", absPath)
		}

		if expandedDir != dir && (slashPath == expandedDir || strings.HasPrefix(slashPath, expandedDir+"/") ||
			slashClean == expandedDir || strings.HasPrefix(slashClean, expandedDir+"/")) {
			return fmt.Errorf("sandbox: access denied – path %q is in a protected directory. Hint: use run_shell_command if you genuinely need access", absPath)
		}
	}

	// Block ~/.nano config files but explicitly allow ~/.nano/skills/**
	if homeErr == nil {
		nanoConfigDir := filepath.ToSlash(filepath.Join(home, ".nano"))
		configNanoDir := filepath.ToSlash(filepath.Join(home, ".config", "nano"))
		skillsDir := filepath.ToSlash(filepath.Join(home, ".nano", "skills"))

		// Check if path is under ~/.nano/skills/ - if so, allow it
		if strings.HasPrefix(slashPath, skillsDir+"/") || slashPath == skillsDir ||
			strings.HasPrefix(slashClean, skillsDir+"/") || slashClean == skillsDir {
			// Explicitly allowed: ~/.nano/skills/** paths
		} else {
			// Block ~/.nano/*.yaml, ~/.nano/*.yml, ~/.nano/config*
			if strings.HasPrefix(slashPath, nanoConfigDir+"/") || slashPath == nanoConfigDir ||
				strings.HasPrefix(slashClean, nanoConfigDir+"/") || slashClean == nanoConfigDir {
				base := filepath.Base(slashPath)
				if strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml") || strings.HasPrefix(base, "config") {
					return fmt.Errorf("sandbox: access denied – path %q is a protected system file. Hint: use run_shell_command if you genuinely need access", absPath)
				}
			}

			// Block ~/.config/nano/*.yaml, ~/.config/nano/*.yml
			if strings.HasPrefix(slashPath, configNanoDir+"/") || slashPath == configNanoDir ||
				strings.HasPrefix(slashClean, configNanoDir+"/") || slashClean == configNanoDir {
				base := filepath.Base(slashPath)
				if strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml") {
					return fmt.Errorf("sandbox: access denied – path %q is a protected system file. Hint: use run_shell_command if you genuinely need access", absPath)
				}
			}
		}
	}

	// Block .nano config files in any directory (workspace-level configs owned by orchestrator)
	// Allow .nano/skills/** for agent use
	nanoDir := "/.nano/"
	if strings.Contains(slashPath, nanoDir) || strings.Contains(slashClean, nanoDir) {
		rel := slashPath
		if idx := strings.LastIndex(rel, nanoDir); idx >= 0 {
			rel = rel[idx+len(nanoDir):]
		}
		// Allow skills/ subtree
		if !strings.HasPrefix(rel, "skills/") && rel != "skills" {
			return fmt.Errorf("sandbox: access denied – path %q is a nano agent config file managed by the orchestrator", absPath)
		}
	}

	// Check against default blocked patterns using doublestar
	for _, pattern := range defaultBlockedPatterns {
		matched, err := doublestar.PathMatch(pattern, slashPath)
		if err == nil && matched {
			return fmt.Errorf("sandbox: access denied – path %q matches protected pattern %q. Hint: use run_shell_command if you genuinely need access", absPath, pattern)
		}

		matched, err = doublestar.PathMatch(pattern, slashClean)
		if err == nil && matched {
			return fmt.Errorf("sandbox: access denied – path %q matches protected pattern %q. Hint: use run_shell_command if you genuinely need access", absPath, pattern)
		}
	}

	return nil
}

// Check validates whether the given path is accessible for the requested
// operation.  Returns a descriptive error when access is denied.
func (p *PathChecker) Check(path string, op FileOperation) error {
	// Always resolve the path first
	absPath, err := p.resolve(path)
	if err != nil {
		return fmt.Errorf("sandbox: invalid path %q: %w", path, err)
	}

	// Compute clean absolute path for dual checking (symlink-resolved vs clean)
	cleanAbs := filepath.Clean(path)
	if !filepath.IsAbs(cleanAbs) {
		if wd, wdErr := os.Getwd(); wdErr == nil {
			cleanAbs = filepath.Join(wd, cleanAbs)
		}
	}

	// Layer 1: ALWAYS enforce default blocklist (even when sandbox is disabled)
	if err := enforceDefaultBlocklist(absPath, cleanAbs); err != nil {
		return err
	}

	// Layer 2: User-configured sandbox rules (only when sandbox is enabled)
	if !p.IsEnabled() {
		return nil
	}

	// User-configured blocked paths take priority
	if p.matchesAny(absPath, p.cfg.BlockedPaths) {
		return fmt.Errorf("sandbox: access denied – path %q is blocked", absPath)
	}

	// If an allowlist is configured, the path must match it
	if len(p.cfg.AllowedPaths) > 0 && !p.matchesAny(absPath, p.cfg.AllowedPaths) {
		return fmt.Errorf("sandbox: access denied – path %q is not in allowed_paths", absPath)
	}

	// Write operations are rejected for read-only paths
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
	if isNoopBackend(cfg.Backend) {
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

func isNoopBackend(backend string) bool {
	return strings.EqualFold(backend, string(BackendNone))
}

// NewRuntime creates a Sandbox Runtime backed by the current platform adapter.
func NewRuntime(cfg *config.SandboxConfig, workingDir string) Runtime {
	return NewRuntimeWithEventPublisher(cfg, workingDir, nil)
}

// NewRuntimeWithEventPublisher creates a Sandbox Runtime that publishes audit events.
func NewRuntimeWithEventPublisher(cfg *config.SandboxConfig, workingDir string, publisher EventPublisher) Runtime {
	if cfg != nil && cfg.Enabled && strings.EqualFold(cfg.Backend, string(BackendDocker)) {
		return NewDockerRuntimeWithEventPublisher(cfg, workingDir, publisher)
	}
	return &adapterRuntime{
		cfg:        cfg,
		workingDir: workingDir,
		sandbox:    New(cfg, workingDir),
		publisher:  publisher,
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

// Backend returns BackendNone.
func (n *NoopSandbox) Backend() Backend { return BackendNone }

// BackendDetail returns "none".
func (n *NoopSandbox) BackendDetail() string { return "none" }

type adapterRuntime struct {
	cfg        *config.SandboxConfig
	workingDir string
	sandbox    Sandbox
	publisher  EventPublisher
}

func (r *adapterRuntime) PrepareCommand(ctx context.Context, req SandboxRequest) (*SandboxEnvironment, error) {
	if r.sandbox == nil {
		r.sandbox = &NoopSandbox{}
	}

	workingDir := req.WorkingDir
	if workingDir == "" {
		workingDir = r.workingDir
	}
	network := req.Network
	if network == "" {
		network = networkPolicyFromConfig(r.cfg)
	}

	cmd, args, err := r.sandbox.WrapCommand(workingDir, req.Command, append([]string(nil), req.Args...))
	if err != nil {
		return nil, err
	}

	metadata := copyMetadata(req.Metadata)
	if r.cfg != nil && r.cfg.Enabled && !r.sandbox.IsEnabled() {
		metadata["fallback"] = true
		metadata["fallback_reason"] = "process isolation backend unavailable"
	}

	env := &SandboxEnvironment{
		Backend:        r.sandbox.Backend(),
		BackendDetail:  r.sandbox.BackendDetail(),
		Enabled:        r.sandbox.IsEnabled(),
		Command:        cmd,
		Args:           args,
		WorkingDir:     workingDir,
		Network:        network,
		Mounts:         mountsFromConfig(r.cfg, workingDir),
		ResourceLimits: req.ResourceLimits,
		Metadata:       metadata,
	}
	publisher := publisherForContext(ctx, r.publisher)
	PublishEvent(publisher, EventTypeSandboxDecisionCreated, env, "sandbox decision created", nil)
	PublishEvent(publisher, EventTypeSandboxEnvironmentCreated, env, "sandbox environment created", nil)
	if fallback, _ := metadata["fallback"].(bool); fallback {
		PublishEvent(publisher, EventTypeSandboxFallbackUsed, env, "sandbox fallback used", nil)
	}
	return env, nil
}

func (r *adapterRuntime) Cleanup(ctx context.Context, env *SandboxEnvironment) error {
	PublishEvent(publisherForContext(ctx, r.publisher), EventTypeSandboxEnvironmentCleaned, env, "sandbox environment cleaned", nil)
	return nil
}

func networkPolicyFromConfig(cfg *config.SandboxConfig) NetworkPolicy {
	if cfg == nil || !cfg.Enabled {
		return NetworkInherited
	}
	if cfg.NetworkAccess {
		return NetworkAllowed
	}
	return NetworkDenied
}

func mountsFromConfig(cfg *config.SandboxConfig, workingDir string) []Mount {
	var mounts []Mount
	if workingDir != "" {
		mounts = append(mounts, Mount{HostPath: workingDir, Path: workingDir, Mode: MountReadWrite})
	}
	if cfg == nil {
		return mounts
	}
	for _, p := range cfg.ExtraReadOnlyPaths {
		mounts = append(mounts, Mount{HostPath: p, Path: p, Mode: MountReadOnly})
	}
	for _, p := range cfg.ExtraWritablePaths {
		mounts = append(mounts, Mount{HostPath: p, Path: p, Mode: MountReadWrite})
	}
	return mounts
}

func copyMetadata(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// extractEnvNames returns only environment variable names for non-secret audit metadata.
func extractEnvNames(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	names := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func publisherForContext(ctx context.Context, fallback EventPublisher) EventPublisher {
	if publisher := EventPublisherFromContext(ctx); publisher != nil {
		return publisher
	}
	return fallback
}

// PublishEvent publishes a sandbox event with audit metadata when a publisher is configured.
func PublishEvent(publisher EventPublisher, eventType string, env *SandboxEnvironment, content string, metadata map[string]interface{}) {
	if publisher == nil || env == nil {
		return
	}
	eventMetadata := map[string]interface{}{
		"sandbox": env.AuditMetadata(),
	}
	for k, v := range metadata {
		eventMetadata[k] = v
	}
	publisher.PublishSandboxEvent(Event{
		Type:      eventType,
		Content:   content,
		Source:    "sandbox",
		Timestamp: time.Now().Unix(),
		Metadata:  eventMetadata,
	})
}
