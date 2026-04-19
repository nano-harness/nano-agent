package permission

import (
	"strings"
	"sync"
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
func (s *SessionAllowlist) IsAllowed(toolName string, params map[string]interface{}) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
