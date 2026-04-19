package permission_test

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
)

func TestParseRule_PlainName(t *testing.T) {
	r := permission.ParseRule("write_file")
	if r.ToolName != "write_file" {
		t.Errorf("unexpected ToolName: %s", r.ToolName)
	}
	if r.Specifier != "" {
		t.Errorf("unexpected Specifier: %s", r.Specifier)
	}
	if r.RawPattern != "write_file" {
		t.Errorf("unexpected RawPattern: %s", r.RawPattern)
	}
}

func TestParseRule_BashAlias(t *testing.T) {
	r := permission.ParseRule("Bash(git *)")
	if r.ToolName != "run_shell_command" {
		t.Errorf("expected run_shell_command, got %s", r.ToolName)
	}
	if r.Specifier != "git *" {
		t.Errorf("unexpected Specifier: %q", r.Specifier)
	}
}

func TestParseRule_WithSpecifier(t *testing.T) {
	r := permission.ParseRule("write_file(*.go)")
	if r.ToolName != "write_file" {
		t.Errorf("unexpected ToolName: %s", r.ToolName)
	}
	if r.Specifier != "*.go" {
		t.Errorf("unexpected Specifier: %s", r.Specifier)
	}
}

func TestParseRule_Wildcard(t *testing.T) {
	r := permission.ParseRule("file_*")
	if r.ToolName != "file_*" {
		t.Errorf("unexpected ToolName: %s", r.ToolName)
	}
}

func TestMatchToolName(t *testing.T) {
	cases := []struct {
		pattern, tool string
		want          bool
	}{
		{"*", "anything", true},
		// Empty pattern must NOT match: only "*" is the explicit wildcard.
		{"", "anything", false},
		{"", "", false},
		{"read_file", "read_file", true},
		{"read_file", "write_file", false},
		{"file_*", "file_read", true},
		{"file_*", "file_write", true},
		{"file_*", "run_shell_command", false},
	}
	for _, c := range cases {
		got := permission.MatchToolName(c.pattern, c.tool)
		if got != c.want {
			t.Errorf("MatchToolName(%q, %q) = %v, want %v", c.pattern, c.tool, got, c.want)
		}
	}
}

func TestMatchSpecifier(t *testing.T) {
	cases := []struct {
		spec, value string
		want        bool
	}{
		{"", "anything", true},
		// Shell command prefix patterns: space before * must be preserved.
		{"git *", "git status", true},
		{"git *", "git commit -m foo", true},
		{"git *", "rm -rf /", false},
		// "git *" must NOT match "github" – the literal space in the prefix is required.
		{"git *", "github", false},
		{"*.go", "main.go", true},
		{"*.go", "main.py", false},
		{"npm run *", "npm run build", true},
		{"npm run *", "npm install", false},
		// Basename matching: "*.go" must match nested paths.
		{"*.go", "pkg/main.go", true},
		{"*.go", "pkg/sub/main.go", true},
		{"*.go", "pkg/sub/main.py", false},
	}
	for _, c := range cases {
		got := permission.MatchSpecifier(c.spec, c.value)
		if got != c.want {
			t.Errorf("MatchSpecifier(%q, %q) = %v, want %v", c.spec, c.value, got, c.want)
		}
	}
}

func TestExtractMatchValue(t *testing.T) {
	cases := []struct {
		toolName string
		params   map[string]interface{}
		want     string
	}{
		{"run_shell_command", map[string]interface{}{"command": "git status"}, "git status"},
		{"write_file", map[string]interface{}{"file_path": "main.go"}, "main.go"},
		{"web_search", map[string]interface{}{"query": "golang"}, ""},
		{"run_shell_command", nil, ""},
	}
	for _, c := range cases {
		got := permission.ExtractMatchValue(c.toolName, c.params)
		if got != c.want {
			t.Errorf("ExtractMatchValue(%q, ...) = %q, want %q", c.toolName, got, c.want)
		}
	}
}
