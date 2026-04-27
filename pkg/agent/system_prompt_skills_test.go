package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/skill"
)

func TestBuildSkillsMetadataSectionIncludesSourcePaths(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".nano", "skills")
	for _, name := range []string{"alpha-skill", "beta-skill"} {
		skillDir := filepath.Join(skillsDir, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: " + name + " description\n---\n# Instructions\nDo something.\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mgr := skill.NewManager(dir, "", ".nano/skills", 0, 0, 0, false)
	if err := mgr.Discover(); err != nil {
		t.Fatalf("skill.Discover() failed: %v", err)
	}

	spb := newTestSystemPromptBuilder()
	spb.SetSkillManager(mgr)
	section := spb.buildSkillsMetadataSection()

	for _, want := range []string{
		"| Skill | Description | Scope | File Path |",
		"File Path",
		"read or modify a skill's documentation",
		"do NOT search for the file first",
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("skills metadata section missing %q:\n%s", want, section)
		}
	}

	for _, m := range mgr.ListMetadata() {
		if m.SourcePath == "" {
			t.Fatalf("skill %q has empty SourcePath", m.Name)
		}
		if !strings.Contains(section, m.SourcePath) {
			t.Errorf("skills metadata section missing source path %q", m.SourcePath)
		}
	}
}
