package permission

import (
	"strings"
	"sync"

	"github.com/nano-harness/nano-agent/pkg/middleware"
)

// SessionAllowlist is a thread-safe, session-scoped collection of PermissionRules.
// Once a rule is added, matching tool calls no longer need explicit user approval
// for the lifetime of the session.
type SessionAllowlist struct {
	mu    sync.RWMutex
	rules []PermissionRule
}

// NewSessionAllowlist creates an empty SessionAllowlist.
func NewSessionAllowlist() *SessionAllowlist {
	return &SessionAllowlist{}
}

// AddRule appends a new rule to the allowlist.  Duplicate raw patterns and
// rules with an empty ToolName are silently ignored.
func (s *SessionAllowlist) AddRule(rule PermissionRule) {
	if strings.TrimSpace(rule.ToolName) == "" {
		return // reject empty / invalid rules
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rules {
		if r.RawPattern == rule.RawPattern {
			return
		}
	}
	s.rules = append(s.rules, rule)
}

// RemoveRule removes the first rule whose RawPattern equals pattern.
func (s *SessionAllowlist) RemoveRule(pattern string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.rules {
		if r.RawPattern == pattern {
			s.rules = append(s.rules[:i], s.rules[i+1:]...)
			return
		}
	}
}

// IsAllowed reports whether the combination of toolName and params is covered
// by any rule in the allowlist.
//
// For shell tools (run_shell_command / bash / shell), compound commands are
// validated per-sub-command: every individual statement must independently
// match at least one rule, and commands containing dangerous syntax
// (redirections, command substitutions, eval, find -exec, …) are never
// allowed regardless of existing rules.
func (s *SessionAllowlist) IsAllowed(toolName string, params map[string]interface{}) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Shell tools get compound-aware, per-subcommand validation.
	if isShellToolName(toolName) {
		return s.isShellAllowed(toolName, params)
	}

	for _, r := range s.rules {
		if !MatchToolName(r.ToolName, toolName) {
			continue
		}
		// No specifier → tool-level match is sufficient.
		if r.Specifier == "" {
			return true
		}
		// Specifier present → also check extracted parameter value.
		value := ExtractNormalizedMatchValue(toolName, params)
		if MatchSpecifier(r.Specifier, value) {
			return true
		}
	}
	return false
}

// isShellAllowed validates a shell tool invocation against the allowlist with
// full compound-command awareness.  The caller must hold s.mu.RLock.
//
// Rules:
//  1. A tool-level rule (empty specifier) grants blanket permission.
//  2. Commands containing dangerous syntax (redirects, $(), eval, …) are
//     never allowed, regardless of other rules.
//  3. The command is split into individual statements via
//     middleware.ParseCommand; each rebuilt statement must independently match
//     at least one specifier rule.  A parse failure is treated as
//     fail-closed: the command is not allowed.
func (s *SessionAllowlist) isShellAllowed(toolName string, params map[string]interface{}) bool {
	// A tool-level rule (no specifier) grants blanket permission.
	for _, r := range s.rules {
		if MatchToolName(r.ToolName, toolName) && r.Specifier == "" {
			return true
		}
	}

	command := ExtractMatchValue(toolName, params)
	if command == "" {
		return false
	}

	// Never allow commands with dangerous syntax regardless of allowlist rules.
	if hasDangerousSyntax(command) {
		return false
	}

	// Parse into sub-commands; fail-closed on parse error.
	pc, err := middleware.ParseCommand(command)
	if err != nil || pc == nil || len(pc.Statements) == 0 {
		return false
	}

	// A7: use AllStatements() which includes nested commands from `bash -c "..."`
	// so that inner commands are checked against the allowlist too.
	// Previously only top-level pc.Statements were checked, allowing arbitrary
	// inner commands as long as a single Bash(*) rule existed.
	stmts := pc.AllStatements()
	if len(stmts) == 0 {
		return false
	}

	// Every sub-command (including nested) must match at least one specifier rule.
	for _, stmt := range stmts {
		segment := middleware.RebuildCommand(stmt)
		if segment == "" {
			return false
		}
		if !s.isSegmentAllowed(toolName, segment) {
			return false
		}
	}
	return true
}

// isSegmentAllowed checks whether a single rebuilt command segment matches any
// specifier rule in the allowlist for the given tool.
// The caller must hold s.mu.RLock.
func (s *SessionAllowlist) isSegmentAllowed(toolName, segment string) bool {
	for _, r := range s.rules {
		if !MatchToolName(r.ToolName, toolName) {
			continue
		}
		if r.Specifier == "" {
			return true
		}
		if MatchSpecifier(r.Specifier, segment) {
			return true
		}
	}
	return false
}

// ListRules returns a snapshot copy of all rules.
func (s *SessionAllowlist) ListRules() []PermissionRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]PermissionRule, len(s.rules))
	copy(cp, s.rules)
	return cp
}

// Clear removes all rules from the allowlist.
func (s *SessionAllowlist) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = nil
}
