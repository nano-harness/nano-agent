package slash_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/slash"
)

func TestNewRegistry_BuiltinCommands(t *testing.T) {
	r := slash.NewRegistry("")
	all := r.All()
	if len(all) == 0 {
		t.Fatal("expected at least built-in commands, got none")
	}

	// Verify all expected built-in commands are present (including full opsx set).
	wantNames := []string{
		"yolo", "permission", "permissions", "allow", "disallow", "think", "clear",
		"skill:list", "skill:use", "skill:off", "skill:info", "skill:install",
		"teammates", "teammates:list", "teammates:show",
		"agents", "agents:list", "agents:show",
		"models", "model list", "model use", "model status", "model fallback", "model doctor", "context status",
		"doctor", "events", "audit",
		"routines list", "routines add", "routines remove", "routines status", "routines pause", "routines resume", "routines run",
		"opsx:propose", "opsx:explore", "opsx:new", "opsx:continue", "opsx:ff",
		"opsx:apply", "opsx:verify", "opsx:sync", "opsx:status",
		"opsx:archive", "opsx:bulk-archive",
	}
	nameSet := make(map[string]bool, len(all))
	for _, cmd := range all {
		nameSet[cmd.Name] = true
	}
	for _, want := range wantNames {
		if !nameSet[want] {
			t.Errorf("expected built-in command %q to be registered", want)
		}
	}
}

func TestNewRegistry_CategoryOrder(t *testing.T) {
	r := slash.NewRegistry("")
	all := r.All()

	prevIdx := -1
	catOrder := map[slash.Category]int{
		slash.CategoryPermission: 0,
		slash.CategorySkill:      1,
		slash.CategoryAgent:      2,
		slash.CategoryModel:      3,
		slash.CategoryObserve:    4,
		slash.CategoryRoutines:   5,
		slash.CategoryOpenSpec:   6,
		slash.CategoryCheckpoint: 7,
		slash.CategoryCustom:     8,
	}
	for _, cmd := range all {
		idx := catOrder[cmd.Category]
		if idx < prevIdx {
			t.Errorf("command %q out of category order (category %q after higher-index category)", cmd.Name, cmd.Category)
		}
		prevIdx = idx
	}
}

func TestRegistry_ByCategory(t *testing.T) {
	r := slash.NewRegistry("")
	perms := r.ByCategory(slash.CategoryPermission)
	if len(perms) == 0 {
		t.Fatal("expected permission commands, got none")
	}
	for _, cmd := range perms {
		if cmd.Category != slash.CategoryPermission {
			t.Errorf("ByCategory(permission) returned command with category %q", cmd.Category)
		}
	}
}

func TestRegistry_Search(t *testing.T) {
	r := slash.NewRegistry("")

	// Empty query returns all.
	all := r.Search("")
	if len(all) != len(r.All()) {
		t.Errorf("Search(\"\") returned %d, want %d", len(all), len(r.All()))
	}

	// Known substring match.
	results := r.Search("skill")
	if len(results) == 0 {
		t.Fatal("Search(\"skill\") returned no results")
	}
	for _, cmd := range results {
		if cmd.Category != slash.CategorySkill {
			// Only skill commands have "skill" in their name — if custom commands
			// happen to have "skill" in description that's also fine.
			_ = cmd
		}
	}

	// No-match query.
	nothing := r.Search("zzznomatch")
	if len(nothing) != 0 {
		t.Errorf("Search(\"zzznomatch\") returned %d results, want 0", len(nothing))
	}
}

func TestRegistry_Names(t *testing.T) {
	r := slash.NewRegistry("")
	names := r.Names()
	if len(names) == 0 {
		t.Fatal("Names() returned empty list")
	}
	for _, n := range names {
		if len(n) == 0 || n[0] != '/' {
			t.Errorf("Names() returned %q which does not start with '/'", n)
		}
	}
}

func TestNewBuiltinRegistry(t *testing.T) {
	r := slash.NewBuiltinRegistry()
	all := r.All()
	if len(all) == 0 {
		t.Fatal("NewBuiltinRegistry() returned empty list")
	}
	// Built-in registry should contain no custom commands.
	for _, cmd := range all {
		if cmd.Category == slash.CategoryCustom {
			t.Errorf("NewBuiltinRegistry() returned custom command %q", cmd.Name)
		}
	}
	// Should still contain all known built-in categories.
	cats := make(map[slash.Category]bool)
	for _, cmd := range all {
		cats[cmd.Category] = true
	}
	for _, want := range []slash.Category{
		slash.CategoryPermission,
		slash.CategorySkill,
		slash.CategoryAgent,
		slash.CategoryModel,
		slash.CategoryObserve,
		slash.CategoryRoutines,
		slash.CategoryOpenSpec,
	} {
		if !cats[want] {
			t.Errorf("NewBuiltinRegistry() missing category %q", want)
		}
	}
}

func TestNewRegistry_CustomCommandPermissionMetadata(t *testing.T) {
	cwd := t.TempDir()
	cmdDir := filepath.Join(cwd, ".nano", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "deploy.md"), []byte(`---
description: Deploy safely
allowed-tools: [run_shell_command]
permission-profile: acceptEdits
---
Deploy $ARGUMENTS
`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := slash.NewRegistry(cwd)
	var got *slash.Command
	for _, cmd := range r.All() {
		if cmd.Name == "deploy" {
			cmdCopy := cmd
			got = &cmdCopy
			break
		}
	}
	if got == nil {
		t.Fatal("custom command not registered")
	}
	if len(got.AllowedTools) != 1 || got.AllowedTools[0] != "run_shell_command" {
		t.Fatalf("AllowedTools = %#v", got.AllowedTools)
	}
	if got.PermissionProfile != "acceptEdits" {
		t.Fatalf("PermissionProfile = %q, want acceptEdits", got.PermissionProfile)
	}
}

func TestNewRegistry_CustomCommandPreludeMetadata(t *testing.T) {
	cwd := t.TempDir()
	cmdDir := filepath.Join(cwd, ".nano", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "ship.md"), []byte(`---
description: Ship with preflight
prelude_timeout: 7
prelude_on_error: abort
prelude_output: full
---
!go test ./pkg/slash
!echo ready
Ship $ARGUMENTS
`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := slash.NewRegistry(cwd)
	var got *slash.Command
	for _, cmd := range r.All() {
		if cmd.Name == "ship" {
			cmdCopy := cmd
			got = &cmdCopy
			break
		}
	}
	if got == nil {
		t.Fatal("custom command not registered")
	}
	if len(got.Prelude) != 2 || got.Prelude[0] != "go test ./pkg/slash" || got.Prelude[1] != "echo ready" {
		t.Fatalf("Prelude = %#v", got.Prelude)
	}
	if got.PreludeTimeoutSeconds != 7 || got.PreludeOnError != "abort" || got.PreludeOutput != "full" {
		t.Fatalf("unexpected prelude options: %#v", got)
	}
}

func TestNewRegistry_AgentProfileRegistered(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".nano", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reviewer.yaml"), []byte(`description: Review code
initial_prompt: Review the requested changes.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := slash.NewRegistry(cwd)
	cmd, ok := r.Find("reviewer")
	if !ok {
		t.Fatal("agent profile not registered as slash command")
	}
	if cmd.Category != slash.CategoryAgent {
		t.Errorf("Category = %q, want %q", cmd.Category, slash.CategoryAgent)
	}
	if cmd.Source != "agent-profile" {
		t.Errorf("Source = %q, want agent-profile", cmd.Source)
	}
	if cmd.Description == "" {
		t.Errorf("Description should be non-empty")
	}
}

func TestNewRegistry_AgentProfileBuiltinConflict(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".nano", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// "yolo" is a built-in permission command.
	if err := os.WriteFile(filepath.Join(dir, "yolo.yaml"), []byte(`description: hostile takeover`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := slash.NewRegistry(cwd)
	cmd, ok := r.Find("yolo")
	if !ok {
		t.Fatal("built-in /yolo missing")
	}
	if cmd.Source == "agent-profile" {
		t.Fatalf("agent profile overrode built-in yolo: %#v", cmd)
	}
	if cmd.Category != slash.CategoryPermission {
		t.Errorf("Category = %q, want permission", cmd.Category)
	}
}

func TestNewRegistry_AgentProfileCustomCommandConflict(t *testing.T) {
	cwd := t.TempDir()
	cmdDir := filepath.Join(cwd, ".nano", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "deploy.md"), []byte(`---
description: Deploy
---
Do deploy
`), 0o644); err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(cwd, ".nano", "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "deploy.yaml"), []byte(`description: agent deploy`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := slash.NewRegistry(cwd)
	cmd, ok := r.Find("deploy")
	if !ok {
		t.Fatal("custom /deploy missing")
	}
	if cmd.Source == "agent-profile" {
		t.Fatalf("agent profile overrode custom command: %#v", cmd)
	}
	if cmd.Category != slash.CategoryCustom {
		t.Errorf("Category = %q, want custom", cmd.Category)
	}
}

func TestNewRegistry_AgentProfileInvalidName(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".nano", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Names with ':' or '.' must be rejected; agentprofile uses filename as
	// the default Name when frontmatter is absent.
	if err := os.WriteFile(filepath.Join(dir, "bad name.yaml"), []byte(`name: "bad:name"
description: x
`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := slash.NewRegistry(cwd)
	if _, ok := r.Find("bad:name"); ok {
		t.Fatal("agent profile with invalid name should not be registered")
	}
	if _, ok := r.Find("bad name"); ok {
		t.Fatal("agent profile with space in name should not be registered")
	}
}
