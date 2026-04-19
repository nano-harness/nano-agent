package skill

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// --- Parser Tests ---

func TestParseSkillFile(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: test-skill
description: "A test skill for unit testing"
triggers:
  - "test"
  - "unit test"
globs:
  - "*.go"
auto_invoke: true
priority: 10
---

# Test Skill

These are the instructions for the test skill.

## Steps

1. First step
2. Second step
`
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	skill, err := ParseSkillFile(skillPath)
	if err != nil {
		t.Fatalf("ParseSkillFile failed: %v", err)
	}

	if skill.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", skill.Name)
	}
	if skill.Description != "A test skill for unit testing" {
		t.Errorf("expected description 'A test skill for unit testing', got %q", skill.Description)
	}
	if len(skill.Triggers) != 2 {
		t.Errorf("expected 2 triggers, got %d", len(skill.Triggers))
	}
	if len(skill.Globs) != 1 || skill.Globs[0] != "*.go" {
		t.Errorf("unexpected globs: %v", skill.Globs)
	}
	if !skill.IsAutoInvoke() {
		t.Error("expected auto_invoke to be true")
	}
	if skill.Priority != 10 {
		t.Errorf("expected priority 10, got %d", skill.Priority)
	}
	if skill.Instructions == "" {
		t.Error("expected non-empty instructions")
	}
	if skill.SourcePath != skillPath {
		t.Errorf("expected source path %q, got %q", skillPath, skill.SourcePath)
	}
}

func TestParseMetadataOnly(t *testing.T) {
	dir := t.TempDir()
	content := `---
name: meta-only
description: "Metadata only test"
triggers:
  - "meta"
---

# Instructions that should NOT be loaded
`
	skillPath := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, err := ParseMetadataOnly(skillPath)
	if err != nil {
		t.Fatalf("ParseMetadataOnly failed: %v", err)
	}

	if meta.Name != "meta-only" {
		t.Errorf("expected name 'meta-only', got %q", meta.Name)
	}
	if meta.Description != "Metadata only test" {
		t.Errorf("expected description 'Metadata only test', got %q", meta.Description)
	}
}

func TestParseSkillFile_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	content := `# No frontmatter here
Just plain markdown.
`
	skillPath := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseSkillFile(skillPath)
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}

func TestParseSkillFile_InvalidName(t *testing.T) {
	dir := t.TempDir()
	content := `---
name: "Invalid Name With Spaces"
description: "bad"
---
body
`
	skillPath := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseSkillFile(skillPath)
	if err == nil {
		t.Error("expected error for invalid skill name")
	}
}

func TestParseSkillFile_EmptyName(t *testing.T) {
	dir := t.TempDir()
	content := `---
name: ""
description: "empty name"
---
body
`
	skillPath := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseSkillFile(skillPath)
	if err == nil {
		t.Error("expected error for empty skill name")
	}
}

func TestAutoInvokeDefault(t *testing.T) {
	meta := &SkillMetadata{Name: "test"}
	if !meta.IsAutoInvoke() {
		t.Error("expected default auto_invoke to be true")
	}

	f := false
	meta.AutoInvoke = &f
	if meta.IsAutoInvoke() {
		t.Error("expected auto_invoke to be false when explicitly set")
	}
}

// --- Loader Tests ---

func TestManagerDiscover(t *testing.T) {
	dir := t.TempDir()

	// Create project skills directory
	projSkillsDir := filepath.Join(dir, ".nano", "skills")
	createTestSkill(t, projSkillsDir, "code-review", "code-review", "Review code quality", []string{"review", "code review"})
	createTestSkill(t, projSkillsDir, "deploy", "deploy", "Deployment guide", []string{"deploy", "release"})

	mgr := NewManager(dir, "", filepath.Join(dir, ".nano", "skills"), 0, 0, 0, true)
	if err := mgr.Discover(); err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if mgr.Count() != 2 {
		t.Errorf("expected 2 skills, got %d", mgr.Count())
	}

	s := mgr.GetByName("code-review")
	if s == nil {
		t.Fatal("expected to find 'code-review' skill")
	}
	if s.Description != "Review code quality" {
		t.Errorf("unexpected description: %q", s.Description)
	}
}

func TestManagerProjectOverridesPersonal(t *testing.T) {
	dir := t.TempDir()

	personalDir := filepath.Join(dir, "personal-skills")
	projectDir := filepath.Join(dir, "project-skills")

	createTestSkill(t, personalDir, "my-skill", "my-skill", "Personal version", []string{"test"})
	createTestSkill(t, projectDir, "my-skill", "my-skill", "Project version", []string{"test"})

	mgr := NewManager(dir, personalDir, projectDir, 0, 0, 0, true)
	if err := mgr.Discover(); err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if mgr.Count() != 1 {
		t.Errorf("expected 1 skill (deduplicated), got %d", mgr.Count())
	}

	s := mgr.GetByName("my-skill")
	if s == nil {
		t.Fatal("expected to find 'my-skill'")
	}
	if s.Description != "Project version" {
		t.Errorf("expected project version to override, got %q", s.Description)
	}
	if s.Scope != ScopeProject {
		t.Errorf("expected scope to be project, got %q", s.Scope)
	}
}

func TestManagerMaxSkillsLimit(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "skills")

	// Create 5 skills but set limit to 3
	for i := 0; i < 5; i++ {
		name := "skill" + string(rune('a'+i))
		createTestSkill(t, projDir, name, name, "Skill "+name, nil)
	}

	mgr := NewManager(dir, "", projDir, 0, 3, 0, true)
	if err := mgr.Discover(); err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if mgr.Count() > 3 {
		t.Errorf("expected at most 3 skills, got %d", mgr.Count())
	}
}

func TestManagerActivateDeactivate(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "skills")
	createTestSkill(t, projDir, "test-skill", "test-skill", "Test", nil)

	mgr := NewManager(dir, "", projDir, 0, 0, 2, true)
	if err := mgr.Discover(); err != nil {
		t.Fatal(err)
	}

	// Activate
	if err := mgr.ActivateSkill("test-skill"); err != nil {
		t.Fatalf("ActivateSkill failed: %v", err)
	}
	if !mgr.IsActive("test-skill") {
		t.Error("expected skill to be active")
	}

	active := mgr.GetActiveSkills()
	if len(active) != 1 {
		t.Errorf("expected 1 active skill, got %d", len(active))
	}

	// Deactivate
	mgr.DeactivateSkill("test-skill")
	if mgr.IsActive("test-skill") {
		t.Error("expected skill to be deactivated")
	}
}

func TestManagerActivateNotFound(t *testing.T) {
	mgr := NewManager(".", "", "", 0, 0, 0, true)
	if err := mgr.ActivateSkill("nonexistent"); err == nil {
		t.Error("expected error for nonexistent skill")
	}
}

func TestManagerListSkillNames(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "skills")
	createTestSkill(t, projDir, "alpha", "alpha", "First skill", nil)
	createTestSkill(t, projDir, "beta", "beta", "Second skill", nil)

	mgr := NewManager(dir, "", projDir, 0, 0, 0, true)
	if err := mgr.Discover(); err != nil {
		t.Fatal(err)
	}

	list := mgr.ListSkillNames()
	if list == "" {
		t.Error("expected non-empty list")
	}
}

// --- Matcher Tests ---

func TestMatchTriggers(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "skills")
	createTestSkill(t, projDir, "code-review", "code-review", "Review code", []string{"review", "code review"})
	createTestSkill(t, projDir, "deploy", "deploy", "Deploy app", []string{"deploy", "release"})

	mgr := NewManager(dir, "", projDir, 0, 0, 0, true)
	if err := mgr.Discover(); err != nil {
		t.Fatal(err)
	}

	results := mgr.Match(&MatchContext{
		UserInput: "Please review my code changes",
	}, false)

	if len(results) == 0 {
		t.Fatal("expected at least one match")
	}
	if results[0].Skill.Name != "code-review" {
		t.Errorf("expected 'code-review' to be top match, got %q", results[0].Skill.Name)
	}
}

func TestMatchGlobs(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "skills")
	createTestSkillWithGlobs(t, projDir, "go-lint", "go-lint", "Go linting", nil, []string{"*.go"})

	mgr := NewManager(dir, "", projDir, 0, 0, 0, true)
	if err := mgr.Discover(); err != nil {
		t.Fatal(err)
	}

	results := mgr.Match(&MatchContext{
		UserInput:      "Fix the code",
		MentionedFiles: []string{"main.go", "utils.go"},
	}, false)

	if len(results) == 0 {
		t.Fatal("expected glob match")
	}
	if results[0].Skill.Name != "go-lint" {
		t.Errorf("expected 'go-lint' match, got %q", results[0].Skill.Name)
	}
}

func TestMatchSkipsActiveSkills(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "skills")
	createTestSkill(t, projDir, "review", "review", "Review", []string{"review"})

	mgr := NewManager(dir, "", projDir, 0, 0, 5, true)
	if err := mgr.Discover(); err != nil {
		t.Fatal(err)
	}

	// Activate the skill
	_ = mgr.ActivateSkill("review")

	// Match should skip active skills
	results := mgr.Match(&MatchContext{UserInput: "review code"}, false)
	if len(results) != 0 {
		t.Error("expected no matches for already active skills")
	}
}

func TestMatchNoInput(t *testing.T) {
	mgr := NewManager(".", "", "", 0, 0, 0, true)
	results := mgr.Match(nil, false)
	if results != nil {
		t.Error("expected nil for nil context")
	}
	results = mgr.Match(&MatchContext{UserInput: ""}, false)
	if results != nil {
		t.Error("expected nil for empty input")
	}
}

// --- Install Tests ---

func TestInstallSkillFromURL(t *testing.T) {
	content := `---
name: remote-skill
description: "A remotely installed skill"
triggers:
  - "remote"
---

# Remote Skill

Instructions for the remote skill.
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		w.Write([]byte(content))
	}))
	defer server.Close()

	dir := t.TempDir()
	personalDir := filepath.Join(dir, "personal-skills")

	mgr := NewManager(dir, personalDir, filepath.Join(dir, "project-skills"), 0, 0, 0, true)

	ctx := context.Background()
	installed, err := mgr.InstallSkill(ctx, server.URL+"/SKILL.md")
	if err != nil {
		t.Fatalf("InstallSkill failed: %v", err)
	}

	if installed.Name != "remote-skill" {
		t.Errorf("expected name 'remote-skill', got %q", installed.Name)
	}
	if installed.Description != "A remotely installed skill" {
		t.Errorf("unexpected description: %q", installed.Description)
	}

	// Verify file was written
	skillPath := filepath.Join(personalDir, "remote-skill", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("expected skill file at %q: %v", skillPath, err)
	}

	// Verify it was discovered
	if mgr.Count() != 1 {
		t.Errorf("expected 1 skill after install, got %d", mgr.Count())
	}
	if mgr.GetByName("remote-skill") == nil {
		t.Error("expected to find installed skill by name")
	}
}

func TestInstallSkillEmptyURL(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, filepath.Join(dir, "personal"), "", 0, 0, 0, true)

	_, err := mgr.InstallSkill(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestInstallSkillInvalidScheme(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, filepath.Join(dir, "personal"), "", 0, 0, 0, true)

	_, err := mgr.InstallSkill(context.Background(), "ftp://example.com/SKILL.md")
	if err == nil {
		t.Error("expected error for unsupported URL scheme")
	}
}

func TestInstallSkillHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	dir := t.TempDir()
	mgr := NewManager(dir, filepath.Join(dir, "personal"), "", 0, 0, 0, true)

	_, err := mgr.InstallSkill(context.Background(), server.URL+"/missing.md")
	if err == nil {
		t.Error("expected error for HTTP 404")
	}
}

func TestInstallSkillInvalidContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("This is not a valid SKILL.md file"))
	}))
	defer server.Close()

	dir := t.TempDir()
	mgr := NewManager(dir, filepath.Join(dir, "personal"), "", 0, 0, 0, true)

	_, err := mgr.InstallSkill(context.Background(), server.URL+"/bad.md")
	if err == nil {
		t.Error("expected error for invalid skill content")
	}
}

func TestInstallSkillOverwritesExisting(t *testing.T) {
	content1 := `---
name: my-skill
description: "Version 1"
---
v1 instructions
`
	content2 := `---
name: my-skill
description: "Version 2"
---
v2 instructions
`
	var serverContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(serverContent))
	}))
	defer server.Close()

	dir := t.TempDir()
	personalDir := filepath.Join(dir, "personal-skills")
	mgr := NewManager(dir, personalDir, filepath.Join(dir, "project-skills"), 0, 0, 0, true)

	// Install v1
	serverContent = content1
	sk1, err := mgr.InstallSkill(context.Background(), server.URL+"/SKILL.md")
	if err != nil {
		t.Fatalf("Install v1 failed: %v", err)
	}
	if sk1.Description != "Version 1" {
		t.Errorf("expected v1, got %q", sk1.Description)
	}

	// Install v2 (overwrite)
	serverContent = content2
	sk2, err := mgr.InstallSkill(context.Background(), server.URL+"/SKILL.md")
	if err != nil {
		t.Fatalf("Install v2 failed: %v", err)
	}
	if sk2.Description != "Version 2" {
		t.Errorf("expected v2, got %q", sk2.Description)
	}

	// Only one skill should exist
	if mgr.Count() != 1 {
		t.Errorf("expected 1 skill after overwrite, got %d", mgr.Count())
	}
}

func TestInstallSkillMaxSkillsReached(t *testing.T) {
	content := `---
name: new-skill
description: "A new skill"
---
instructions
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(content))
	}))
	defer server.Close()

	dir := t.TempDir()
	personalDir := filepath.Join(dir, "personal-skills")
	projDir := filepath.Join(dir, "project-skills")

	// Create 2 existing skills and set max to 2
	createTestSkill(t, projDir, "skilla", "skilla", "Skill A", nil)
	createTestSkill(t, projDir, "skillb", "skillb", "Skill B", nil)

	mgr := NewManager(dir, personalDir, projDir, 0, 2, 0, true)
	if err := mgr.Discover(); err != nil {
		t.Fatal(err)
	}

	_, err := mgr.InstallSkill(context.Background(), server.URL+"/SKILL.md")
	if err == nil {
		t.Error("expected error when max skills reached")
	}
}

// --- Test Helpers ---

func createTestSkill(t *testing.T, baseDir, dirName, skillName, description string, triggers []string) {
	t.Helper()
	createTestSkillWithGlobs(t, baseDir, dirName, skillName, description, triggers, nil)
}

func createTestSkillWithGlobs(t *testing.T, baseDir, dirName, skillName, description string, triggers, globs []string) {
	t.Helper()
	skillDir := filepath.Join(baseDir, dirName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var triggerYAML string
	if len(triggers) > 0 {
		triggerYAML = "triggers:\n"
		for _, tr := range triggers {
			triggerYAML += "  - \"" + tr + "\"\n"
		}
	}

	var globYAML string
	if len(globs) > 0 {
		globYAML = "globs:\n"
		for _, g := range globs {
			globYAML += "  - \"" + g + "\"\n"
		}
	}

	content := "---\nname: " + skillName + "\ndescription: \"" + description + "\"\n" + triggerYAML + globYAML + "---\n\n# " + skillName + " Instructions\n\nDo the thing.\n"

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
