// Package permission implements the permission/approval system for nano-agent tool execution.
// It provides tiered permission modes and a session-scoped allowlist so the user can
// progressively grant trust without restarting the session.
package permission

import "github.com/nano-harness/nano-agent/pkg/interfaces"

// PermissionMode represents the global permission level for tool execution.
type PermissionMode string

const (
	// ModeDefault requires user confirmation for every tool that declares
	// RequiresConfirmation() == true.
	ModeDefault PermissionMode = "default"

	// ModeAcceptEdits automatically approves filesystem-write tools (write_file,
	// edit_file, delete_file, patch_file) while still asking for shell commands.
	ModeAcceptEdits PermissionMode = "acceptEdits"

	// ModeYOLO skips ALL permission checks – every tool executes immediately.
	ModeYOLO PermissionMode = "yolo"
)

// editCategories lists the tool categories that are considered "file edits" for
// the AcceptEdits mode.
var editCategories = map[interfaces.ToolCategory]bool{
	interfaces.CategoryFileSystem: true,
}

// IsEditTool returns true when the tool should be auto-approved in AcceptEdits mode.
func IsEditTool(t interfaces.Tool) bool {
	return editCategories[t.Category()]
}

// PermissionRule represents a single allowlist entry.  It mirrors the
// "ToolName(specifier)" syntax used by Claude Code.
type PermissionRule struct {
	// ToolName is the tool pattern, e.g. "write_file", "file_*", "*".
	ToolName string
	// Specifier is the optional parameter pattern, e.g. "git *", "*.go".
	// Empty means "match any parameters".
	Specifier string
	// RawPattern is the original string that was parsed, e.g. "Bash(git *)".
	RawPattern string
}
