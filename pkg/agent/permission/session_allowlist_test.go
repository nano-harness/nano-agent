package permission_test

import (
	"sync"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
)

func TestAllowlist_AddAndIsAllowed(t *testing.T) {
	al := permission.NewSessionAllowlist()

	// Empty allowlist – nothing allowed.
	if al.IsAllowed("read_file", nil) {
		t.Error("empty allowlist should not allow anything")
	}

	// Add plain tool rule.
	al.AddRule(permission.ParseRule("read_file"))
	if !al.IsAllowed("read_file", nil) {
		t.Error("read_file should be allowed after adding rule")
	}
	if al.IsAllowed("write_file", nil) {
		t.Error("write_file should not be allowed")
	}
}

func TestAllowlist_WithSpecifier(t *testing.T) {
	al := permission.NewSessionAllowlist()
	al.AddRule(permission.ParseRule("Bash(git *)"))

	params := map[string]interface{}{"command": "git status"}
	if !al.IsAllowed("run_shell_command", params) {
		t.Error("git status should be allowed")
	}

	params2 := map[string]interface{}{"command": "rm -rf /"}
	if al.IsAllowed("run_shell_command", params2) {
		t.Error("rm -rf / should not be allowed by git * rule")
	}
}

func TestAllowlist_WildcardToolName(t *testing.T) {
	al := permission.NewSessionAllowlist()
	al.AddRule(permission.ParseRule("file_*"))

	if !al.IsAllowed("file_read", nil) {
		t.Error("file_read should match file_* pattern")
	}
	if !al.IsAllowed("file_write", nil) {
		t.Error("file_write should match file_* pattern")
	}
	if al.IsAllowed("run_shell_command", nil) {
		t.Error("run_shell_command should not match file_* pattern")
	}
}

func TestAllowlist_RemoveRule(t *testing.T) {
	al := permission.NewSessionAllowlist()
	al.AddRule(permission.ParseRule("read_file"))
	al.RemoveRule("read_file")

	if al.IsAllowed("read_file", nil) {
		t.Error("read_file should not be allowed after removal")
	}
}

func TestAllowlist_Clear(t *testing.T) {
	al := permission.NewSessionAllowlist()
	al.AddRule(permission.ParseRule("read_file"))
	al.AddRule(permission.ParseRule("write_file"))
	al.Clear()

	if len(al.ListRules()) != 0 {
		t.Error("allowlist should be empty after Clear()")
	}
}

func TestAllowlist_NoDuplicates(t *testing.T) {
	al := permission.NewSessionAllowlist()
	al.AddRule(permission.ParseRule("read_file"))
	al.AddRule(permission.ParseRule("read_file"))

	if len(al.ListRules()) != 1 {
		t.Errorf("expected 1 rule, got %d", len(al.ListRules()))
	}
}

func TestAllowlist_ConcurrentAccess(t *testing.T) {
	al := permission.NewSessionAllowlist()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			al.AddRule(permission.ParseRule("read_file"))
			al.IsAllowed("read_file", nil)
			al.ListRules()
		}()
	}
	wg.Wait()
}

func TestAllowlist_FilePathSpecifier(t *testing.T) {
	al := permission.NewSessionAllowlist()
	al.AddRule(permission.ParseRule("write_file(*.go)"))

	goParams := map[string]interface{}{"file_path": "main.go"}
	if !al.IsAllowed("write_file", goParams) {
		t.Error("writing *.go file should be allowed")
	}

	// Nested path: "*.go" should also match "pkg/sub/main.go" via basename.
	nestedParams := map[string]interface{}{"file_path": "pkg/sub/main.go"}
	if !al.IsAllowed("write_file", nestedParams) {
		t.Error("writing nested *.go file should be allowed via basename match")
	}

	txtParams := map[string]interface{}{"file_path": "README.md"}
	if al.IsAllowed("write_file", txtParams) {
		t.Error("writing README.md should not be allowed by *.go rule")
	}
}

func TestAllowlist_EmptyRuleRejected(t *testing.T) {
	al := permission.NewSessionAllowlist()
	// An empty raw string produces a rule with an empty ToolName.
	al.AddRule(permission.ParseRule(""))
	if len(al.ListRules()) != 0 {
		t.Error("empty rule should be silently rejected")
	}
	// Whitespace-only input should likewise be rejected.
	al.AddRule(permission.ParseRule("   "))
	if len(al.ListRules()) != 0 {
		t.Error("whitespace-only rule should be silently rejected")
	}
}
