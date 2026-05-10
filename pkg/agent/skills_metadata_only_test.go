package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/skill"
)

func writePromptSkill(t *testing.T, baseDir, name, description, body string) {
	t.Helper()
	dir := filepath.Join(baseDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %q\n---\n\n%s\n", name, description, body)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newActiveSkillPromptBuilder(t *testing.T, body string) *SystemPromptBuilder {
	t.Helper()
	workDir := t.TempDir()
	skillsDir := filepath.Join(workDir, "skills")
	writePromptSkill(t, skillsDir, "skill-one", "first active skill", body)
	writePromptSkill(t, skillsDir, "skill-two", "second active skill", body)
	mgr := skill.NewManager(workDir, "", skillsDir, 0, 0, 5, true)
	if err := mgr.Discover(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"skill-one", "skill-two"} {
		if err := mgr.ActivateSkill(name); err != nil {
			t.Fatal(err)
		}
	}
	spb := NewSystemPromptBuilder(workDir, []interfaces.Tool{
		promptTestTool{name: "discover_tools", description: "discover tools", category: interfaces.CategoryAgent},
	}, nil, &config.Config{IsSubAgent: true})
	spb.SetSkillManager(mgr)
	return spb
}

func TestActiveSkillBodyNotInjectedIntoSystemPrompt(t *testing.T) {
	spb := newActiveSkillPromptBuilder(t, "# Body\n\nPhase 1\nSymptom-00\n```go\nfmt.Println()\n```")
	section := spb.buildActiveSkillsSection()
	for _, forbidden := range []string{"Phase 1", "Symptom-00", "```go"} {
		if strings.Contains(section, forbidden) {
			t.Fatalf("active skill body marker %q was injected: %q", forbidden, section)
		}
	}
	if !strings.Contains(section, "skill-one") || !strings.Contains(section, "first active skill") {
		t.Fatalf("active skill metadata missing: %q", section)
	}
}

func TestUnifiedSystemPromptSizeBudget(t *testing.T) {
	spb := newActiveSkillPromptBuilder(t, strings.Repeat("Phase 1 Symptom-00 ```go```\n", 600))
	prompt := spb.BuildEnhancedSystemPrompt(context.Background(), nil)
	if got := len([]rune(prompt)); got >= 30000 {
		t.Fatalf("prompt size = %d runes, want < 30000", got)
	}
}

func TestUnifiedSystemPromptKeepsCriticalInstructions(t *testing.T) {
	spb := newActiveSkillPromptBuilder(t, "body")
	prompt := spb.BuildEnhancedSystemPrompt(context.Background(), nil)
	for _, want := range []string{
		"Before acting on an active skill, you MUST call `discover_skills`",
		"Before using non-core tools, you MUST call `discover_tools` first",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing critical instruction %q", want)
		}
	}
}
