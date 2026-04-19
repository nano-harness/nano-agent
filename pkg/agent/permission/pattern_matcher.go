package permission

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/middleware"
)

// ParseRule parses a raw rule string into a PermissionRule.
//
// Supported formats:
//
//	"write_file"          → ToolName="write_file", Specifier=""
//	"Bash(git *)"         → ToolName="run_shell_command", Specifier="git *"
//	"run_shell_command(npm run *)" → ToolName="run_shell_command", Specifier="npm run *"
//	"file_*"              → ToolName="file_*", Specifier=""
//
// The tool name "Bash" is treated as an alias for "run_shell_command" to match
// the Claude Code convention.
func ParseRule(raw string) PermissionRule {
	raw = strings.TrimSpace(raw)
	rule := PermissionRule{RawPattern: raw}

	openParen := strings.Index(raw, "(")
	if openParen == -1 {
		// No specifier.
		rule.ToolName = normaliseName(raw)
		return rule
	}

	closeParen := strings.LastIndex(raw, ")")
	if closeParen <= openParen {
		// Malformed – treat whole string as tool name.
		rule.ToolName = normaliseName(raw)
		return rule
	}

	rule.ToolName = normaliseName(raw[:openParen])
	rule.Specifier = strings.TrimSpace(raw[openParen+1 : closeParen])
	return rule
}

// normaliseName maps friendly aliases to canonical tool names.
func normaliseName(name string) string {
	name = strings.TrimSpace(name)
	switch strings.ToLower(name) {
	case "bash", "shell":
		return "run_shell_command"
	default:
		return name
	}
}

// MatchToolName reports whether the (possibly wildcarded) pattern matches the
// concrete tool name.  Pattern follows filepath.Match glob rules.
// An empty pattern never matches; use "*" to match all tools.
func MatchToolName(pattern, toolName string) bool {
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	// Fast-path exact match.
	if pattern == toolName {
		return true
	}
	matched, err := filepath.Match(pattern, toolName)
	return err == nil && matched
}

// MatchSpecifier reports whether the specifier pattern matches value.
// An empty specifier matches everything.  The match is case-sensitive.
//
// Matching strategy (applied in order, first hit wins):
//  1. filepath.Match glob (exact path, e.g. "*.go" matches "main.go")
//  2. Basename glob: if the specifier contains no path separator, also try
//     filepath.Match against filepath.Base(value) so that "*.go" matches
//     "pkg/sub/main.go" as well.
//  3. Prefix match for space-terminated "word *" patterns so that
//     "git *" matches "git status" but NOT "github" – the literal prefix
//     (including any trailing space before the "*") must be present.
func MatchSpecifier(specifier, value string) bool {
	if specifier == "" {
		return true
	}
	// 1. Full filepath.Match glob.
	matched, err := filepath.Match(specifier, value)
	if err == nil && matched {
		return true
	}
	// 2. Basename match for patterns without a path separator.
	//    Check whether value contains a separator before calling filepath.Base to
	//    avoid a redundant call when value has no path component.
	if !strings.ContainsAny(specifier, "/\\") && strings.ContainsAny(value, "/\\") {
		base := filepath.Base(value)
		matched, err = filepath.Match(specifier, base)
		if err == nil && matched {
			return true
		}
	}
	// 3. Prefix match for "token *" patterns (space before "*" is intentional
	//    and is NOT stripped, so "git *" requires the value to start with "git "
	//    and will NOT match "github").
	if strings.HasSuffix(specifier, "*") {
		prefix := strings.TrimSuffix(specifier, "*")
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// paramKeys maps a canonical tool name to the parameter key that should be
// extracted for specifier matching.
var paramKeys = map[string]string{
	"run_shell_command": "command",
	"write_file":        "file_path",
	"read_file":         "file_path",
	"edit_file":         "file_path",
	"delete_file":       "file_path",
	"patch_file":        "file_path",
}

// ExtractMatchValue returns the string value from params that should be compared
// against a specifier.  Returns "" when no suitable key is found.
func ExtractMatchValue(toolName string, params map[string]interface{}) string {
	key, ok := paramKeys[toolName]
	if !ok {
		return ""
	}
	if params == nil {
		return ""
	}
	if v, ok := params[key]; ok {
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return ""
}

// BuildAllowlistRule creates a PermissionRule from a concrete tool invocation.
// When a relevant parameter value is available a specifier is included so the
// rule is as narrow as possible.  It returns the first rule produced by
// BuildAllowlistRules so that simple callers work unchanged.
func BuildAllowlistRule(toolName string, params map[string]interface{}) PermissionRule {
	rules := BuildAllowlistRules(toolName, params)
	return rules[0]
}

// BuildAllowlistRules creates per-subcommand PermissionRules from a tool
// invocation.  For compound commands such as "mkdir -p /tmp && curl -L
// https://..." it generates one prefix rule per distinct sub-command:
//
//	run_shell_command(mkdir *)
//	run_shell_command(curl *)
//
// For simple commands with arguments it generates a single prefix rule, e.g.
//
//	run_shell_command(git *)
//
// For simple commands with no arguments it generates an exact specifier, e.g.
//
//	run_shell_command(ls)
//
// Leading env-variable assignments (e.g. "FOO=1 git status") are stripped so
// that the rule is keyed on the actual command name, not the assignment.
//
// Non-shell tools (no relevant parameter) get a plain tool-name rule.
func BuildAllowlistRules(toolName string, params map[string]interface{}) []PermissionRule {
	value := ExtractMatchValue(toolName, params)
	if value == "" {
		return []PermissionRule{ParseRule(toolName)}
	}
	pc, err := middleware.ParseCommand(value)
	if err != nil {
		// If parsing fails, fall back to a single prefix rule derived from the raw value.
		prefix := extractCommandPrefix(value)
		return []PermissionRule{ParseRule(toolName + "(" + prefix + "*)")}
	}
	if len(pc.Statements) <= 1 {
		// Single statement (or zero statements for degenerate input like bare ";").
		if len(pc.Statements) == 1 {
			return []PermissionRule{buildRuleFromStatement(toolName, pc.Statements[0], value)}
		}
		// Zero statements – fall back to the raw-value prefix heuristic.
		prefix := extractCommandPrefix(value)
		return []PermissionRule{ParseRule(toolName + "(" + prefix + "*)")}
	}
	// Compound command – one rule per distinct sub-command.
	seen := make(map[string]bool)
	var rules []PermissionRule
	for _, stmt := range pc.Statements {
		if stmt.Command == "" {
			continue
		}
		cmdName := filepath.Base(stmt.Command)
		key := cmdName
		if seen[key] {
			continue
		}
		seen[key] = true
		rules = append(rules, buildRuleFromStatement(toolName, stmt, ""))
	}
	if len(rules) == 0 {
		prefix := extractCommandPrefix(value)
		return []PermissionRule{ParseRule(toolName + "(" + prefix + "*)")}
	}
	return rules
}

// buildRuleFromStatement creates a single PermissionRule for a parsed Statement.
// It emits an exact specifier for zero-arg commands (e.g. "ls") and a prefix
// specifier for commands with arguments (e.g. "git *").
//
// stmt.Command is empty for env-assignment-only input (e.g. "FOO=1" with no
// command word).  In that case the raw value is used as a fallback.
func buildRuleFromStatement(toolName string, stmt middleware.Statement, fallbackRaw string) PermissionRule {
	if stmt.Command == "" {
		prefix := extractCommandPrefix(fallbackRaw)
		return ParseRule(toolName + "(" + prefix + "*)")
	}
	cmdName := filepath.Base(stmt.Command)
	if len(stmt.RawArgs) == 0 {
		// No arguments: emit exact specifier so "ls" matches "ls" (not "lsof").
		return ParseRule(toolName + "(" + cmdName + ")")
	}
	// Has arguments: emit prefix specifier so "git *" matches any git sub-command.
	return ParseRule(toolName + "(" + cmdName + " *)")
}

// ExtractNormalizedMatchValue returns the match value for specifier matching.
// For run_shell_command it normalizes single-statement commands by stripping
// leading env-variable assignments (e.g. "FOO=1 git status" → "git status")
// so that prefix rules generated by BuildAllowlistRules can match correctly.
// Compound commands and parse failures are returned unchanged.
func ExtractNormalizedMatchValue(toolName string, params map[string]interface{}) string {
	value := ExtractMatchValue(toolName, params)
	if toolName != "run_shell_command" || value == "" {
		return value
	}
	pc, err := middleware.ParseCommand(value)
	if err != nil || len(pc.Statements) != 1 {
		return value
	}
	if rebuilt := middleware.RebuildCommand(pc.Statements[0]); rebuilt != "" {
		return rebuilt
	}
	return value
}

// extractCommandPrefix returns "cmdname " for use as a fallback allowlist prefix pattern.
func extractCommandPrefix(command string) string {
	cmd := strings.TrimSpace(command)
	if idx := strings.IndexByte(cmd, ' '); idx >= 0 {
		cmd = cmd[:idx]
	}
	cmd = filepath.Base(cmd)
	return cmd + " "
}
