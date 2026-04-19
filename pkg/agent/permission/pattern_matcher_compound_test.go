package permission_test

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
)

func TestBuildAllowlistRules_SimpleCommand(t *testing.T) {
	params := map[string]interface{}{"command": "git status"}
	rules := permission.BuildAllowlistRules("run_shell_command", params)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d: %v", len(rules), rules)
	}
	r := rules[0]
	if r.ToolName != "run_shell_command" {
		t.Errorf("ToolName = %q, want run_shell_command", r.ToolName)
	}
	if r.Specifier != "git *" {
		t.Errorf("Specifier = %q, want \"git *\"", r.Specifier)
	}
}

func TestBuildAllowlistRules_ZeroArgCommand(t *testing.T) {
	// A bare command with no args must generate an exact specifier, not "ls *".
	// "ls *" would require "ls " as a prefix, so it would NOT match bare "ls".
	params := map[string]interface{}{"command": "ls"}
	rules := permission.BuildAllowlistRules("run_shell_command", params)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d: %v", len(rules), rules)
	}
	r := rules[0]
	if r.Specifier != "ls" {
		t.Errorf("Specifier = %q, want \"ls\"", r.Specifier)
	}
	// The exact specifier must match the bare invocation.
	if !permission.MatchSpecifier(r.Specifier, "ls") {
		t.Errorf("specifier %q should match bare \"ls\"", r.Specifier)
	}
}

func TestBuildAllowlistRules_EnvVarPrefix(t *testing.T) {
	// Leading env-var assignments must be stripped so the rule is keyed on the
	// real command name, not the assignment.
	params := map[string]interface{}{"command": "FOO=1 git status"}
	rules := permission.BuildAllowlistRules("run_shell_command", params)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d: %v", len(rules), rules)
	}
	if rules[0].Specifier != "git *" {
		t.Errorf("Specifier = %q, want \"git *\"", rules[0].Specifier)
	}
}

func TestBuildAllowlistRules_CompoundCommand(t *testing.T) {
	params := map[string]interface{}{"command": "mkdir -p /tmp && curl -L https://example.com"}
	rules := permission.BuildAllowlistRules("run_shell_command", params)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules for compound command, got %d: %v", len(rules), rules)
	}
	specs := map[string]bool{}
	for _, r := range rules {
		specs[r.Specifier] = true
	}
	if !specs["mkdir *"] {
		t.Errorf("expected rule with specifier \"mkdir *\", got %v", specs)
	}
	if !specs["curl *"] {
		t.Errorf("expected rule with specifier \"curl *\", got %v", specs)
	}
}

func TestBuildAllowlistRules_Dedup(t *testing.T) {
	// Duplicate sub-commands should yield a single rule.
	params := map[string]interface{}{"command": "echo a && echo b && echo c"}
	rules := permission.BuildAllowlistRules("run_shell_command", params)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule after dedup, got %d: %v", len(rules), rules)
	}
	if rules[0].Specifier != "echo *" {
		t.Errorf("Specifier = %q, want \"echo *\"", rules[0].Specifier)
	}
}

func TestBuildAllowlistRules_PrefixMatch(t *testing.T) {
	// The generated prefix rule must match variant invocations of the same command.
	params := map[string]interface{}{"command": "git status"}
	rules := permission.BuildAllowlistRules("run_shell_command", params)
	if len(rules) == 0 {
		t.Fatal("expected at least one rule")
	}
	r := rules[0]
	// "git *" should match "git diff", "git log --oneline", etc.
	for _, cmd := range []string{"git diff", "git log --oneline", "git fetch origin"} {
		if !permission.MatchSpecifier(r.Specifier, cmd) {
			t.Errorf("rule specifier %q should match %q", r.Specifier, cmd)
		}
	}
	// Must not match unrelated commands.
	if permission.MatchSpecifier(r.Specifier, "github") {
		t.Errorf("rule specifier %q must not match \"github\"", r.Specifier)
	}
}

func TestBuildAllowlistRules_NoParam(t *testing.T) {
	// Tool with no relevant param → plain tool-name rule.
	rules := permission.BuildAllowlistRules("web_search", map[string]interface{}{"query": "go lang"})
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].ToolName != "web_search" {
		t.Errorf("ToolName = %q, want web_search", rules[0].ToolName)
	}
	if rules[0].Specifier != "" {
		t.Errorf("Specifier = %q, want empty", rules[0].Specifier)
	}
}

func TestBuildAllowlistRule_Compat(t *testing.T) {
	// BuildAllowlistRule (singular) must return the first rule from BuildAllowlistRules.
	params := map[string]interface{}{"command": "npm run build"}
	rule := permission.BuildAllowlistRule("run_shell_command", params)
	rules := permission.BuildAllowlistRules("run_shell_command", params)
	if rule.ToolName != rules[0].ToolName || rule.Specifier != rules[0].Specifier {
		t.Errorf("BuildAllowlistRule mismatch: got %+v, want %+v", rule, rules[0])
	}
}

func TestIsAllowed_EnvVarPrefixNormalization(t *testing.T) {
	// A rule generated from "FOO=1 git status" (specifier "git *") must also
	// match future invocations that carry the same env prefix.
	al := permission.NewSessionAllowlist()
	params := map[string]interface{}{"command": "FOO=1 git status"}
	for _, r := range permission.BuildAllowlistRules("run_shell_command", params) {
		al.AddRule(r)
	}

	// The same env-prefixed command should be auto-allowed.
	if !al.IsAllowed("run_shell_command", map[string]interface{}{"command": "FOO=1 git status"}) {
		t.Error("IsAllowed: env-prefixed command matching the rule should be allowed")
	}
	// A different git sub-command with the same env prefix should also be allowed.
	if !al.IsAllowed("run_shell_command", map[string]interface{}{"command": "FOO=1 git log"}) {
		t.Error("IsAllowed: env-prefixed 'git log' should match 'git *' rule")
	}
	// A completely different command must not be allowed.
	if al.IsAllowed("run_shell_command", map[string]interface{}{"command": "FOO=1 curl evil.com"}) {
		t.Error("IsAllowed: 'curl' should not match 'git *' rule")
	}
}
