package permission

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
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
//  1. YOLO mode → never confirm.
//  2. Session allowlist match → never confirm.
//  3. ContextualConfirmationTool.RequiresConfirmationForParams → confirm when
//     the tool marks the specific parameters as sensitive. Filesystem write,
//     edit, and delete tools define this for protected names such as .env,
//     package manifests, lock files, config files, and paths outside the
//     trusted workspace.
//  4. AcceptEdits mode + non-sensitive edit tool → never confirm.
//  5. Filesystem edit tool path inside trusted workdir → never confirm.
//  6. Tool.RequiresConfirmation → use that.
//  7. Default → no confirmation required.
func (m *Manager) ShouldConfirm(toolName string, params map[string]interface{}, tool interfaces.Tool) bool {
	m.mu.RLock()
	mode := m.mode
	workdir := m.workdir
	classifier := m.classifier
	m.mu.RUnlock()

	// M3-3: NANO_AUTO_ACCEPT environment variable globally short-circuits
	// confirmation prompts. Intended for non-interactive automation contexts
	// where the operator has already accepted full autonomy.
	if v := os.Getenv("NANO_AUTO_ACCEPT"); v == "1" || strings.EqualFold(v, "true") {
		return false
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
		return false
	}

	// M2-1: Auto mode consults an AI classifier. The classifier is given a
	// bounded timeout; on error or timeout we keep the conservative default
	// of asking the user (fail-closed).
	if mode == ModeAuto && classifier != nil {
		ctx, cancel := context.WithTimeout(context.Background(), classifier.Timeout())
		defer cancel()
		result, err := classifier.Classify(ctx, ClassifyRequest{
			ToolName: toolName,
			Params:   params,
			WorkDir:  workdir,
			PermMode: mode,
		})
		if err == nil && result != nil {
			return result.ShouldBlock
		}
		// fall through to default behaviour on error
	}

	if tool == nil {
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
		return false
	}

	// 5. Filesystem edit tools inside the trusted workdir are auto-approved.
	if tool != nil && IsEditTool(tool) && workdir != "" {
		if path := extractFilesystemPath(toolName, params); path != "" {
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
