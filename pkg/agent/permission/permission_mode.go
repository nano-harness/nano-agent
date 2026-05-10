// Package permission implements the permission/approval system for nano-agent tool execution.
// It provides tiered permission modes and a session-scoped allowlist so the user can
// progressively grant trust without restarting the session.
package permission

import (
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// PermissionMode represents the global permission level for tool execution.
type PermissionMode string

const (
	// ModeDefault requires user confirmation for every tool that declares
	// RequiresConfirmation() == true.
	ModeDefault PermissionMode = "default"

	// ModeAcceptEdits automatically approves filesystem-write tools (write_file,
	// edit_file, delete_file, patch_file) while still asking for shell commands.
	ModeAcceptEdits PermissionMode = "acceptEdits"

	// ModePlan restricts execution to read-only tools only, preventing any
	// side effects. Used for analysis and planning phases.
	ModePlan PermissionMode = "plan"

	// ModeAuto delegates the confirm-or-allow decision to an AI classifier.
	// When no classifier is wired, ModeAuto behaves like ModeDefault.
	ModeAuto PermissionMode = "auto"

	// ModeYOLO skips ALL permission checks – every tool executes immediately.
	ModeYOLO PermissionMode = "yolo"
)

// IsValidMode reports whether mode is one of the supported permission modes.
func IsValidMode(mode PermissionMode) bool {
	switch mode {
	case ModeDefault, ModeAcceptEdits, ModePlan, ModeAuto, ModeYOLO:
		return true
	default:
		return false
	}
}

// editCategories lists the tool categories that are considered "file edits" for
// the AcceptEdits mode.
var editCategories = map[interfaces.ToolCategory]bool{
	interfaces.CategoryFileSystem: true,
}

// IsEditTool returns true when the tool should be auto-approved in AcceptEdits mode.
func IsEditTool(t interfaces.Tool) bool {
	return editCategories[t.Category()]
}

// readOnlyToolNames lists tools that are allowed in Plan mode.
// These tools perform read-only operations without side effects.
var readOnlyToolNames = map[string]bool{
	// File system read operations
	"read_file":      true,
	"list_directory": true,
	"search_files":   true,
	"file_grep":      true,
	"glob_files":     true,

	// Code analysis
	"codebase_search": true,
	"search_code":     true,
	"view_code":       true,

	// Web operations (read-only)
	"web_search": true,
	"web_fetch":  true,

	// Planning tools
	"create_plan":  true,
	"analyze_task": true,

	// Memory/context queries
	"search_memory": true,
	"list_memories": true,

	// MCP tools (most are read-only)
	"mcp_list_tools":     true,
	"mcp_list_resources": true,
}

// readOnlyShellCommands lists shell command prefixes that are considered read-only.
var readOnlyShellCommands = []string{
	"ls", "cat", "head", "tail", "grep", "find", "git status", "git log",
	"git diff", "git show", "pwd", "which", "echo", "env", "printenv",
	"stat", "file", "wc", "sort", "uniq", "less", "more", "tree",
}

// IsToolAllowedInPlanMode checks if a tool can be executed in Plan mode.
func IsToolAllowedInPlanMode(toolName string, params map[string]interface{}) bool {
	// Check if tool is in the read-only whitelist
	if readOnlyToolNames[toolName] {
		return true
	}

	// Special handling for shell commands - only allow read-only commands
	if toolName == "run_shell_command" || toolName == "bash" {
		if cmd, ok := params["command"].(string); ok {
			return isReadOnlyShellCommand(cmd)
		}
		// Block if we can't determine the command
		return false
	}

	// Block all other tools by default
	return false
}

// isReadOnlyShellCommand checks if a shell command is read-only.
func isReadOnlyShellCommand(cmd string) bool {
	// Trim whitespace and convert to lowercase for comparison
	cmd = strings.TrimSpace(strings.ToLower(cmd))

	// Check against known read-only command prefixes
	for _, prefix := range readOnlyShellCommands {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}

	return false
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
