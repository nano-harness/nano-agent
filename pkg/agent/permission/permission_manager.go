package permission

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/middleware"
)

// Manager coordinates the global permission mode and the session-scoped
// allowlist to produce a single ShouldConfirm decision.
type Manager struct {
	mu        sync.RWMutex
	mode      PermissionMode
	allowlist *SessionAllowlist
	workdir   string
	// classifier (optional) implements ModeAuto. nil → ModeAuto behaves like ModeDefault.
	classifier Classifier
	// failClosed controls ShouldConfirm behavior when the classifier errors or
	// times out.  true (default) → fail-closed (return true = require confirm);
	// false → fail-open (return false = auto-approve). Set fail-open only for
	// deployment observation / audit phases; not recommended for production.
	failClosed bool
	// transcript holds the latest compact projection of the conversation history.
	// It is set via SetTranscript and forwarded to each ClassifyRequest so the
	// classifier can detect multi-turn threats.
	transcript []TranscriptEntry
	// ConfidenceThreshold is the minimum confidence level for auto-approval.
	// Decisions with confidence >= threshold are auto-approved without user confirmation.
	// Range: 0.0 (strictest, require confirmation for everything) to 1.0 (most permissive).
	// Default: 0.8 (auto-approve high-confidence decisions only)
	//
	// Confidence levels in the system:
	// - 0.95: Read-only commands (ls, cat, git status)
	// - 0.85: Safe development commands (go test, npm install, git add)
	// - 0.60: Simple unclassified commands
	// - 0.50: Compound/complex commands
	confidenceThreshold float64

	denialTracker *DenialTracker

	// lastAutoDecision is best-effort metadata to improve policy-block messages
	// in headless/daemon flows (ConfirmPolicy=block).
	lastAutoDecision autoDecision
}

type autoDecision struct {
	ToolName    string
	MatchValue  string
	ShouldBlock bool
	Reason      string
	At          time.Time
}

// NewManager creates a Manager with the specified initial mode.  Pre-defined
// rules (from config) can be supplied via initialRules.
func NewManager(mode PermissionMode, initialRules []PermissionRule) *Manager {
	return NewManagerWithWorkdir(mode, initialRules, "")
}

// NewManagerWithWorkdir creates a Manager bound to a trusted workdir.
func NewManagerWithWorkdir(mode PermissionMode, initialRules []PermissionRule, workdir string) *Manager {
	m := &Manager{
		mode:                mode,
		allowlist:           NewSessionAllowlist(),
		workdir:             workdir,
		confidenceThreshold: 0.8,
		failClosed:          true, // safe default: classifier errors are treated as blocks
	}
	for _, r := range initialRules {
		m.allowlist.AddRule(r)
	}
	return m
}

// SetMode atomically changes the current permission mode.
func (m *Manager) SetMode(mode PermissionMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mode = mode
}

// GetMode returns the current permission mode.
func (m *Manager) GetMode() PermissionMode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mode
}

// GetSessionAllowlist returns the session-scoped allowlist so callers can add
// or query rules.
func (m *Manager) GetSessionAllowlist() *SessionAllowlist {
	return m.allowlist
}

// ShouldConfirm returns true when the agent should pause and ask the user for
// explicit approval before executing the tool.
//
// Decision order:
//  0. Plan mode → only allow read-only tools.
//  1. YOLO mode → never confirm.
//  2. Session allowlist match → never confirm.
//     2.5 ModeAuto + shell fast-path → never confirm for safe read-only shells.
//     2.6 ModeAuto + IsAutoSafeTool() → never confirm (zero-latency allowlist).
//     2.7 ModeAuto + IsAutoSafeMCPTool() → never confirm (known safe MCP tools).
//     2.8 ModeAuto + IsEditTool() + non-sensitive + path within workdir → never confirm.
//     M2-1 ModeAuto + classifier (two-stage) → use classifier verdict.
//  3. ContextualConfirmationTool.RequiresConfirmationForParams → confirm when
//     the tool marks the specific parameters as sensitive.
//  4. AcceptEdits mode + non-sensitive edit tool → never confirm.
//  5. Filesystem edit tool path inside trusted workdir → never confirm.
//  6. Tool.RequiresConfirmation → use that.
//  7. Default → no confirmation required.
func (m *Manager) ShouldConfirm(toolName string, params map[string]interface{}, tool interfaces.Tool) bool {
	m.mu.RLock()
	mode := m.mode
	workdir := m.workdir
	classifier := m.classifier
	failClosed := m.failClosed
	denialTracker := m.denialTracker
	transcript := m.transcript
	m.mu.RUnlock()

	// M3-3: NANO_AUTO_ACCEPT is deprecated. Log a warning and fall through to
	// the normal permission logic. Use --permission-mode=yolo for headless
	// automation that intentionally waives all confirmations; that path still
	// applies the mode-specific hardening that NANO_AUTO_ACCEPT bypassed.
	if v := os.Getenv("NANO_AUTO_ACCEPT"); v == "1" || strings.EqualFold(v, "true") {
		logger.Warnf("NANO_AUTO_ACCEPT is deprecated and no longer bypasses the permission system. Use --permission-mode=yolo for headless automation.")
	}

	// 0. Plan mode - block tools that aren't in the read-only whitelist.
	// This check comes first to enforce read-only restrictions.
	if mode == ModePlan {
		if !IsToolAllowedInPlanMode(toolName, params) {
			// Return true to force confirmation, which will then be blocked
			// by a separate check in the tool execution pipeline.
			return true
		}
		// For allowed read-only tools, proceed with normal confirmation logic
	}

	// 1. YOLO – skip everything.
	if mode == ModeYOLO {
		return false
	}

	// 2. Session allowlist.
	if m.allowlist.IsAllowed(toolName, params) {
		if denialTracker != nil {
			denialTracker.RecordAllow()
		}
		return false
	}

	// 2.5 ModeAuto shell fast-path: allow fully read-only / allowlisted segments
	// without calling the classifier. Enabled only when a classifier is wired so
	// ModeAuto remains ModeDefault-compatible when PermissionAuto is absent.
	if mode == ModeAuto && classifier != nil && isShellToolName(toolName) {
		if cmd, ok := params["command"].(string); ok {
			if allowShellFastPath(cmd, m.allowlist) {
				if denialTracker != nil {
					denialTracker.RecordAllow()
				}
				return false
			}
		}
	}

	// 2.6 ModeAuto safe-tool fast-path: read-only tools skip the classifier
	// entirely, providing zero latency for the most common benign operations.
	if mode == ModeAuto && classifier != nil && IsAutoSafeTool(toolName) {
		if denialTracker != nil {
			denialTracker.RecordAllow()
		}
		return false
	}

	// 2.7 ModeAuto MCP safe-tool fast-path: known side-effect-free MCP
	// notification tools skip the classifier.
	if mode == ModeAuto && classifier != nil && IsAutoSafeMCPTool(toolName) {
		if denialTracker != nil {
			denialTracker.RecordAllow()
		}
		return false
	}

	// 2.8 ModeAuto workdir-edit fast-path: file edits within the trusted
	// working directory and without sensitive parameters skip the classifier.
	// P0 security: use filepath.EvalSymlinks to resolve symlinks before the
	// workdir check, preventing SymJack-style attacks (CVE-2025-59536) where
	// a symlink inside the workdir points to a sensitive path outside it.
	if mode == ModeAuto && classifier != nil && tool != nil && IsEditTool(tool) && workdir != "" {
		sensitive := false
		if ct, ok := tool.(interfaces.ContextualConfirmationTool); ok && ct.RequiresConfirmationForParams(params) {
			sensitive = true
		}
		if !sensitive {
			if rawPath := extractFilesystemPath(toolName, params); rawPath != "" {
				resolvedPath := rawPath
				if rp, err := filepath.EvalSymlinks(rawPath); err == nil {
					resolvedPath = rp
				}
				if middleware.IsPathWithinWorkdir(workdir, resolvedPath) {
					if denialTracker != nil {
						denialTracker.RecordAllow()
					}
					return false
				}
			}
		}
	}

	// M2-1: Auto mode consults an AI classifier. The classifier is given a
	// bounded timeout; on error or timeout the behaviour is governed by
	// failClosed: true → fail-closed (require confirmation); false → fail-open
	// (auto-approve). Fail-open is for audit/observation deployments only.
	if mode == ModeAuto && classifier != nil {
		ctx, cancel := context.WithTimeout(context.Background(), classifier.Timeout())
		defer cancel()
		result, err := classifier.Classify(ctx, ClassifyRequest{
			ToolName:   toolName,
			Params:     params,
			WorkDir:    workdir,
			PermMode:   mode,
			Transcript: transcript,
		})
		if err == nil && result != nil {
			m.storeAutoDecision(toolName, params, result)
			if denialTracker != nil && !result.ShouldBlock {
				denialTracker.RecordAllow()
			}
			return result.ShouldBlock
		}
		// Classifier error / timeout: apply configurable fail mode.
		if !failClosed {
			logger.Warnf("permission: classifier error (fail-open): %v", err)
			if denialTracker != nil {
				denialTracker.RecordAllow()
			}
			return false
		}
		// fail-closed: fall through to default confirmation logic
	}

	if tool == nil {
		if denialTracker != nil {
			denialTracker.RecordAllow()
		}
		return false
	}

	// 3. Let tools force confirmation for sensitive parameter sets before
	// workspace/acceptEdits shortcuts. Filesystem protected-name checks live in
	// pkg/tools/filesystem/* RequiresConfirmationForParams implementations.
	if ct, ok := tool.(interfaces.ContextualConfirmationTool); ok && ct.RequiresConfirmationForParams(params) {
		return true
	}

	// 4. AcceptEdits – auto-approve non-sensitive file edits.
	if mode == ModeAcceptEdits && tool != nil && IsEditTool(tool) {
		if denialTracker != nil {
			denialTracker.RecordAllow()
		}
		return false
	}

	// 5. Filesystem edit tools inside the trusted workdir are auto-approved.
	if tool != nil && IsEditTool(tool) && workdir != "" {
		if path := extractFilesystemPath(toolName, params); path != "" {
			if denialTracker != nil && middleware.IsPathWithinWorkdir(workdir, path) {
				denialTracker.RecordAllow()
			}
			return !middleware.IsPathWithinWorkdir(workdir, path)
		}
	}

	// 6. Delegate to the tool's own static confirmation logic.
	return tool.RequiresConfirmation()
}

func extractFilesystemPath(toolName string, params map[string]interface{}) string {
	if len(params) == 0 {
		return ""
	}
	key := "file_path"
	switch toolName {
	case "edit_file", "delete_file":
		key = "path"
	}
	path, _ := params[key].(string)
	return path
}

// SetClassifier installs (or replaces) the AI risk classifier consulted in
// ModeAuto. Passing nil disables ModeAuto's auto-approve path (it falls back
// to default-mode behaviour).
func (m *Manager) SetClassifier(c Classifier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.classifier = c
}

// Classifier returns the currently installed classifier (may be nil).
func (m *Manager) Classifier() Classifier {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.classifier
}

// SetFailClosed controls the classifier error/timeout fallback.
// true (default) means fail-closed: classifier errors cause ShouldConfirm=true.
// false means fail-open: classifier errors cause ShouldConfirm=false.
// Fail-open is intended only for audit/observation deployments.
func (m *Manager) SetFailClosed(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failClosed = v
}

// SetTranscript stores a compact projection of the conversation history.
// The transcript is forwarded to the classifier in every ClassifyRequest so
// multi-turn threat patterns can be detected.
// Safe to call concurrently; the caller retains ownership of messages —
// the Manager takes a snapshot via BuildCompactTranscript.
func (m *Manager) SetTranscript(messages []llm.Message) {
	entries := BuildCompactTranscript(messages, 0)
	m.mu.Lock()
	m.transcript = entries
	m.mu.Unlock()
}

// SetConfidenceThreshold sets the minimum confidence for auto-approval.
func (m *Manager) SetConfidenceThreshold(threshold float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.confidenceThreshold = threshold
}

// GetConfidenceThreshold returns the current confidence threshold.
func (m *Manager) GetConfidenceThreshold() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.confidenceThreshold
}

func (m *Manager) SetDenialTracker(t *DenialTracker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.denialTracker = t
}

func (m *Manager) DenialTrackerLockedOut() bool {
	m.mu.RLock()
	t := m.denialTracker
	m.mu.RUnlock()
	return t != nil && t.LockedOut()
}

func (m *Manager) DenialTrackerSample() []string {
	m.mu.RLock()
	t := m.denialTracker
	m.mu.RUnlock()
	if t == nil {
		return nil
	}
	return t.Sample()
}

// RecordPolicyDeny records a hard policy denial (e.g. ConfirmPolicy=block).
func (m *Manager) RecordPolicyDeny(cmd string) (lockedOut bool) {
	m.mu.RLock()
	t := m.denialTracker
	m.mu.RUnlock()
	if t == nil {
		return false
	}
	return t.RecordDeny(cmd)
}

// PolicyBlockReason returns structured info for BLOCKED_BY_POLICY tool results.
func (m *Manager) PolicyBlockReason(toolName string, params map[string]interface{}) (layer, reason, suggestedNext string) {
	layer = "fallback_confirm"
	reason = "tool requires user confirmation and policy is block"

	if m.DenialTrackerLockedOut() {
		layer = "denial_limit"
		reason = "consecutive deny limit reached"
	} else if r, ok := m.lastClassifierReason(toolName, params); ok {
		layer = "classifier"
		reason = r
	}

	suggestedNext = buildSuggestedNext(toolName, params)
	return layer, reason, suggestedNext
}

func (m *Manager) storeAutoDecision(toolName string, params map[string]interface{}, res *ClassifyResult) {
	if res == nil {
		return
	}
	matchValue := ExtractNormalizedMatchValue(toolName, params)
	m.mu.Lock()
	m.lastAutoDecision = autoDecision{
		ToolName:    toolName,
		MatchValue:  matchValue,
		ShouldBlock: res.ShouldBlock,
		Reason:      res.Reason,
		At:          time.Now(),
	}
	m.mu.Unlock()
}

func (m *Manager) lastClassifierReason(toolName string, params map[string]interface{}) (string, bool) {
	matchValue := ExtractNormalizedMatchValue(toolName, params)
	m.mu.RLock()
	dec := m.lastAutoDecision
	m.mu.RUnlock()
	if dec.ToolName != toolName || dec.MatchValue != matchValue || !dec.ShouldBlock {
		return "", false
	}
	if strings.TrimSpace(dec.Reason) == "" {
		return "classifier blocked the call", true
	}
	return dec.Reason, true
}

func buildSuggestedNext(toolName string, params map[string]interface{}) string {
	const prefix = "this call was rejected before execution; do not retry the same exact command. "

	// Heuristic categorization for suggested-next guidance.
	if isShellToolName(toolName) {
		if cmd, _ := params["command"].(string); strings.Contains(cmd, "|") && (strings.Contains(cmd, "bash") || strings.Contains(cmd, "sh")) {
			return prefix + "Avoid piping network output to a shell. Download to a file, inspect it, then execute a pinned, verified script."
		}
		if cmd, _ := params["command"].(string); strings.Contains(cmd, "eval") {
			return prefix + "Avoid eval. Use direct, explicit commands or a script file checked into the workspace."
		}
		return prefix + "Try a more conservative variant of this call, or ask the workflow author to widen permissions explicitly via allow_rules."
	}
	return prefix + "Ask the workflow author to widen permissions explicitly via allow_rules for this tool invocation."
}
