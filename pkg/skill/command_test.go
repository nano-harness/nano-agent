package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createTempCommand writes a slash command .md file and returns the cwd.
func createTempCommand(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	cmdDir := filepath.Join(dir, ".nano", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, name+".md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

// ─── CommandManager ──────────────────────────────────────────────────────────

func TestNewCommandManager_Empty(t *testing.T) {
	m := NewCommandManager(t.TempDir())
	if m == nil {
		t.Fatal("NewCommandManager returned nil")
	}
	if len(m.List()) != 0 {
		t.Errorf("expected 0 commands in empty dir, got %d", len(m.List()))
	}
}

func TestNewCommandManager_LoadsCommand(t *testing.T) {
	cwd := createTempCommand(t, "greet", `---
description: Say hello
allowed-tools: [run_shell_command]
permission-profile: acceptEdits
---
Hello $ARGUMENTS!
`)
	m := NewCommandManager(cwd)
	cmds := m.List()
	if len(cmds) != 1 {
		t.Fatalf("len = %d, want 1", len(cmds))
	}
	cmd := cmds[0]
	if cmd.Name != "greet" {
		t.Errorf("Name = %q, want %q", cmd.Name, "greet")
	}
	if cmd.Description != "Say hello" {
		t.Errorf("Description = %q, want %q", cmd.Description, "Say hello")
	}
	if len(cmd.AllowedTools) != 1 || cmd.AllowedTools[0] != "run_shell_command" {
		t.Errorf("AllowedTools = %v, want [run_shell_command]", cmd.AllowedTools)
	}
	if cmd.PermissionProfile != "acceptEdits" {
		t.Errorf("PermissionProfile = %q, want acceptEdits", cmd.PermissionProfile)
	}
}

func TestCommandManager_Find(t *testing.T) {
	cwd := createTempCommand(t, "deploy", "Deploy $ARGUMENTS")
	m := NewCommandManager(cwd)

	def, ok := m.Find("deploy")
	if !ok {
		t.Fatal("Find returned false")
	}
	if def.Name != "deploy" {
		t.Errorf("Name = %q", def.Name)
	}
}

func TestCommandManager_FindMissing(t *testing.T) {
	m := NewCommandManager(t.TempDir())
	_, ok := m.Find("nonexistent")
	if ok {
		t.Error("expected Find to return false for missing command")
	}
}

func TestCommandManager_NoDuplicateOnMultipleDirs(t *testing.T) {
	// Both .nano/commands and .claude/commands have the same name; first wins.
	dir := t.TempDir()
	nanoDir := filepath.Join(dir, ".nano", "commands")
	claudeDir := filepath.Join(dir, ".claude", "commands")
	_ = os.MkdirAll(nanoDir, 0o755)
	_ = os.MkdirAll(claudeDir, 0o755)
	_ = os.WriteFile(filepath.Join(nanoDir, "dup.md"), []byte("nano version"), 0o644)
	_ = os.WriteFile(filepath.Join(claudeDir, "dup.md"), []byte("claude version"), 0o644)

	m := NewCommandManager(dir)
	cmds := m.List()
	if len(cmds) != 1 {
		t.Errorf("expected 1 command (no dup), got %d", len(cmds))
	}
}

// ─── RenderCommandBody ───────────────────────────────────────────────────────

func TestRenderCommandBody_Arguments(t *testing.T) {
	body := "Run $ARGUMENTS now"
	got := RenderCommandBody(body, []string{"make", "build"})
	want := "Run make build now"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderCommandBody_Positional(t *testing.T) {
	body := "First: $1, Second: $2"
	got := RenderCommandBody(body, []string{"alpha", "beta"})
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Errorf("positional replacement failed: %q", got)
	}
}

func TestRenderCommandBody_OutOfRange(t *testing.T) {
	body := "$1 and $3"
	got := RenderCommandBody(body, []string{"only-one"})
	if !strings.Contains(got, "only-one") {
		t.Errorf("$1 not substituted: %q", got)
	}
	// $3 out of range → empty
	if strings.Contains(got, "$3") {
		t.Errorf("$3 should be replaced with empty: %q", got)
	}
}

func TestRenderCommandBody_NoArgs(t *testing.T) {
	body := "Hello world"
	got := RenderCommandBody(body, nil)
	if got != "Hello world" {
		t.Errorf("got %q, want %q", got, "Hello world")
	}
}

// ─── RenderCommand ───────────────────────────────────────────────────────────

func TestRenderCommand(t *testing.T) {
	def := &CommandDef{Body: "Deploy to $ARGUMENTS"}
	got := RenderCommand(def, []string{"production"})
	if got != "Deploy to production" {
		t.Errorf("got %q, want %q", got, "Deploy to production")
	}
}

// ─── ExtractCommandPreludes ──────────────────────────────────────────────────

func TestExtractCommandPreludes_WithPreludes(t *testing.T) {
	body := "!git status\n!make build\nActual body text"
	preludes, remaining := ExtractCommandPreludes(body)

	if len(preludes) != 2 {
		t.Fatalf("preludes len = %d, want 2", len(preludes))
	}
	if preludes[0] != "git status" {
		t.Errorf("preludes[0] = %q, want %q", preludes[0], "git status")
	}
	if preludes[1] != "make build" {
		t.Errorf("preludes[1] = %q, want %q", preludes[1], "make build")
	}
	if !strings.Contains(remaining, "Actual body text") {
		t.Errorf("remaining should contain body text: %q", remaining)
	}
}

func TestExtractCommandPreludes_NoPreludes(t *testing.T) {
	body := "Just a regular body"
	preludes, remaining := ExtractCommandPreludes(body)
	if len(preludes) != 0 {
		t.Errorf("expected no preludes, got %v", preludes)
	}
	if remaining != body {
		t.Errorf("remaining = %q, want %q", remaining, body)
	}
}

func TestExtractCommandPreludes_EmptyPrelude(t *testing.T) {
	body := "!\nActual content"
	preludes, _ := ExtractCommandPreludes(body)
	// Empty prelude line should be skipped.
	if len(preludes) != 0 {
		t.Errorf("expected no preludes for empty '!' line, got %v", preludes)
	}
}

// ─── ParseSlashCommand ───────────────────────────────────────────────────────

func TestParseSlashCommand_Valid(t *testing.T) {
	cwd := createTempCommand(t, "greet", "Hello $ARGUMENTS")
	def, rendered, args, ok := ParseSlashCommand(cwd, "/greet world")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if def.Name != "greet" {
		t.Errorf("def.Name = %q", def.Name)
	}
	if !strings.Contains(rendered, "world") {
		t.Errorf("rendered = %q, want 'world' in it", rendered)
	}
	if len(args) != 1 || args[0] != "world" {
		t.Errorf("args = %v, want [world]", args)
	}
}

func TestParseSlashCommand_NotSlash(t *testing.T) {
	_, _, _, ok := ParseSlashCommand(t.TempDir(), "not a slash command")
	if ok {
		t.Error("expected ok=false for non-slash input")
	}
}

func TestParseSlashCommand_UnknownCommand(t *testing.T) {
	_, _, _, ok := ParseSlashCommand(t.TempDir(), "/unknown-command")
	if ok {
		t.Error("expected ok=false for unknown command")
	}
}

func TestParseSlashCommand_WithQuotedArgs(t *testing.T) {
	cwd := createTempCommand(t, "say", "$ARGUMENTS")
	_, rendered, _, ok := ParseSlashCommand(cwd, `/say "hello world"`)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !strings.Contains(rendered, "hello world") {
		t.Errorf("rendered = %q, expected 'hello world'", rendered)
	}
}

// ─── CommandDef defaults ─────────────────────────────────────────────────────

func TestCommandDef_DefaultPreludeTimeout(t *testing.T) {
	// A command without prelude_timeout should default to 30.
	cwd := createTempCommand(t, "simple", "body text")
	m := NewCommandManager(cwd)
	def, _ := m.Find("simple")
	if def.PreludeTimeoutSeconds != 30 {
		t.Errorf("PreludeTimeoutSeconds = %d, want 30", def.PreludeTimeoutSeconds)
	}
}

func TestCommandDef_DefaultPreludeOutput(t *testing.T) {
	cwd := createTempCommand(t, "simple2", "body text")
	m := NewCommandManager(cwd)
	def, _ := m.Find("simple2")
	if def.PreludeOutput != "summary" {
		t.Errorf("PreludeOutput = %q, want %q", def.PreludeOutput, "summary")
	}
}

func TestCommandDef_PreludeOnErrorAbort(t *testing.T) {
	cwd := createTempCommand(t, "aborter", `---
prelude_on_error: abort
---
body`)
	m := NewCommandManager(cwd)
	def, _ := m.Find("aborter")
	if def.PreludeOnError != "abort" {
		t.Errorf("PreludeOnError = %q, want abort", def.PreludeOnError)
	}
}

// ─── splitCommandArgs ────────────────────────────────────────────────────────

func TestSplitCommandArgs_Basic(t *testing.T) {
	got := splitCommandArgs("one two three")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
}

func TestSplitCommandArgs_Quoted(t *testing.T) {
	got := splitCommandArgs(`one "two three" four`)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %v", len(got), got)
	}
	if got[1] != "two three" {
		t.Errorf("got[1] = %q, want %q", got[1], "two three")
	}
}

func TestSplitCommandArgs_Empty(t *testing.T) {
	got := splitCommandArgs("")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}
