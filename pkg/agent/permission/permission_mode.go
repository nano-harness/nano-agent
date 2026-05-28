// Package permission implements the permission/approval system for nano-agent tool execution.
// It provides tiered permission modes and a session-scoped allowlist so the user can
// progressively grant trust without restarting the session.
package permission

import (
	"path/filepath"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/middleware"

	"mvdan.cc/sh/v3/syntax"
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

var readOnlyShellCommandNames = map[string]struct{}{
	"ls":       {},
	"cat":      {},
	"head":     {},
	"tail":     {},
	"grep":     {},
	"find":     {},
	"pwd":      {},
	"which":    {},
	"echo":     {},
	"env":      {},
	"printenv": {},
	"stat":     {},
	"file":     {},
	"wc":       {},
	"sort":     {},
	"uniq":     {},
	"less":     {},
	"more":     {},
	"tree":     {},
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

// IsReadOnlyTool is a convenience wrapper for "Plan-mode safe" checks.
func IsReadOnlyTool(toolName string, params map[string]interface{}) bool {
	return IsToolAllowedInPlanMode(toolName, params)
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

func isShellToolName(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "run_shell_command", "bash", "shell":
		return true
	default:
		return false
	}
}

// allowShellFastPath returns true when a ModeAuto shell command can be safely
// auto-approved without consulting the classifier.
func allowShellFastPath(command string, allowlist *SessionAllowlist) bool {
	if strings.TrimSpace(command) == "" {
		return false
	}
	if hasDangerousSyntax(command) {
		return false
	}
	pc, err := middleware.ParseCommand(command)
	if err != nil || pc == nil || len(pc.Statements) == 0 {
		return false
	}

	for _, stmt := range pc.Statements {
		stmt = stripWrappers(stmt)
		if stmt.Command == "" {
			return false
		}
		if isReadOnlyShellStatement(stmt) {
			continue
		}
		if allowlist != nil {
			segment := middleware.RebuildCommand(stmt)
			if segment != "" && allowlist.IsAllowed("run_shell_command", map[string]interface{}{"command": segment}) {
				continue
			}
		}
		return false
	}

	return true
}

func isReadOnlyShellStatement(stmt middleware.Statement) bool {
	base := filepath.Base(stmt.Command)
	base = strings.ToLower(strings.TrimSpace(base))
	if _, ok := readOnlyShellCommandNames[base]; ok {
		return true
	}
	// git is read-only only for certain subcommands.
	if base == "git" && len(stmt.Args) > 0 {
		switch strings.ToLower(strings.TrimSpace(stmt.Args[0])) {
		case "status", "log", "diff", "show":
			return true
		}
	}
	return false
}

func stripWrappers(stmt middleware.Statement) middleware.Statement {
	base := strings.ToLower(filepath.Base(stmt.Command))

	// timeout <duration> <cmd...>  (allow options before duration)
	if base == "timeout" && len(stmt.RawArgs) > 0 {
		args := stmt.RawArgs
		idx := 0
		for idx < len(args) && strings.HasPrefix(args[idx], "-") {
			// timeout -k <d> ... uses a value argument; treat it as part of flags
			if args[idx] == "-k" || args[idx] == "--kill-after" || args[idx] == "-s" || args[idx] == "--signal" {
				if idx+1 < len(args) {
					idx += 2
					continue
				}
			}
			idx++
		}
		// skip duration token (if present) then command
		if idx < len(args) {
			idx++ // duration
		}
		if idx < len(args) {
			return middleware.Statement{
				Command: args[idx],
				Args:    middleware.NormalizeArgs(filepath.Base(args[idx]), args[idx+1:]),
				RawArgs: args[idx+1:],
			}
		}
		return stmt
	}

	// time [options] <cmd...>
	if base == "time" && len(stmt.RawArgs) > 0 {
		args := stmt.RawArgs
		idx := 0
		for idx < len(args) && strings.HasPrefix(args[idx], "-") {
			idx++
		}
		if idx < len(args) {
			return middleware.Statement{
				Command: args[idx],
				Args:    middleware.NormalizeArgs(filepath.Base(args[idx]), args[idx+1:]),
				RawArgs: args[idx+1:],
			}
		}
		return stmt
	}

	// nice [-n N] <cmd...>
	if base == "nice" && len(stmt.RawArgs) > 0 {
		args := stmt.RawArgs
		idx := 0
		if idx < len(args) && (args[idx] == "-n" || args[idx] == "--adjustment") {
			if idx+2 < len(args) {
				idx += 2
			} else {
				return stmt
			}
		}
		for idx < len(args) && strings.HasPrefix(args[idx], "-") {
			idx++
		}
		if idx < len(args) {
			return middleware.Statement{
				Command: args[idx],
				Args:    middleware.NormalizeArgs(filepath.Base(args[idx]), args[idx+1:]),
				RawArgs: args[idx+1:],
			}
		}
		return stmt
	}

	// nohup <cmd...>
	if base == "nohup" && len(stmt.RawArgs) > 0 {
		args := stmt.RawArgs
		return middleware.Statement{
			Command: args[0],
			Args:    middleware.NormalizeArgs(filepath.Base(args[0]), args[1:]),
			RawArgs: args[1:],
		}
	}

	// stdbuf [opts...] <cmd...>
	if base == "stdbuf" && len(stmt.RawArgs) > 0 {
		args := stmt.RawArgs
		idx := 0
		for idx < len(args) && strings.HasPrefix(args[idx], "-") {
			idx++
		}
		if idx < len(args) {
			return middleware.Statement{
				Command: args[idx],
				Args:    middleware.NormalizeArgs(filepath.Base(args[idx]), args[idx+1:]),
				RawArgs: args[idx+1:],
			}
		}
		return stmt
	}

	// xargs <cmd...> (only "bare" form: no -I / no sh -c)
	if base == "xargs" && len(stmt.RawArgs) > 0 {
		args := stmt.RawArgs
		for i := 0; i < len(args); i++ {
			a := args[i]
			if a == "-I" || a == "--replace" || a == "-i" || strings.HasPrefix(a, "-I") {
				return stmt
			}
			if (a == "sh" || a == "bash" || a == "zsh") && i+1 < len(args) && args[i+1] == "-c" {
				return stmt
			}
		}
		return middleware.Statement{
			Command: args[0],
			Args:    middleware.NormalizeArgs(filepath.Base(args[0]), args[1:]),
			RawArgs: args[1:],
		}
	}

	return stmt
}

func hasDangerousSyntax(command string) bool {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	f, err := parser.Parse(strings.NewReader(command), "cmd")
	if err != nil || f == nil {
		// Parse failure: be conservative; disable fast-path so classifier decides.
		return true
	}
	danger := false
	syntax.Walk(f, func(node syntax.Node) bool {
		if node == nil || danger {
			return false
		}
		switch n := node.(type) {
		case *syntax.Redirect:
			danger = true
			return false
		case *syntax.CmdSubst:
			danger = true
			return false
		case *syntax.ProcSubst:
			danger = true
			return false
		case *syntax.CallExpr:
			if callHasDangerousBuiltin(n) {
				danger = true
				return false
			}
		}
		return true
	})
	return danger
}

func callHasDangerousBuiltin(call *syntax.CallExpr) bool {
	if call == nil || len(call.Args) == 0 {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(wordToString(call.Args[0])))
	if name == "eval" {
		return true
	}
	if name == "find" {
		for _, w := range call.Args[1:] {
			arg := strings.TrimSpace(wordToString(w))
			if arg == "-exec" || arg == "-execdir" {
				return true
			}
		}
	}
	return false
}

func wordToString(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var b strings.Builder
	_ = syntax.NewPrinter().Print(&b, w)
	return strings.TrimSpace(b.String())
}

// ApplyModeAutoHardening drops intentionally over-broad allow rules when
// permission_mode=auto is active, to prevent accidental full-trust escalation.
func ApplyModeAutoHardening(rules []string) []string {
	hardeningPatterns := map[string]struct{}{
		"":              {},
		"Bash(*)":       {},
		"Bash(python*)": {},
		"Bash(sh*)":     {},
		"Bash(bash*)":   {},
		"Bash(zsh*)":    {},
		"Agent(*)":      {},
		"Read(*)":       {},
		"Write(*)":      {},
		"Edit(*)":       {},
	}
	var out []string
	for _, r := range rules {
		raw := strings.TrimSpace(r)
		if _, drop := hardeningPatterns[raw]; drop {
			logger.Warnf("permission: dropping over-broad allow_rule %q in ModeAuto", raw)
			continue
		}
		out = append(out, r)
	}
	return out
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
