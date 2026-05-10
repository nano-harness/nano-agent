package agent

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/skill"
)

var _ interfaces.ToolGate = (*ProgressiveDisclosure)(nil)

type mockTool struct {
	name     string
	desc     string
	category string
}

func (m *mockTool) Name() string          { return m.name }
func (m *mockTool) Description() string   { return m.desc }
func (m *mockTool) Category() interface{} { return m.category }

// We need to satisfy the interfaces.Tool interface.
// Instead of a full mock, we can test via ProgressiveDisclosure directly.

func TestProgressiveDisclosure_BuildDirectory_Empty(t *testing.T) {
	pd := NewProgressiveDisclosure(5, 5)
	if got := pd.BuildToolDirectory(); got != "" {
		t.Errorf("expected empty directory, got: %q", got)
	}
	if got := pd.BuildSkillDirectory(); got != "" {
		t.Errorf("expected empty skill directory, got: %q", got)
	}
}

func TestProgressiveDisclosure_IndexSkills(t *testing.T) {
	pd := NewProgressiveDisclosure(5, 5)

	skills := []skill.Skill{
		{
			SkillMetadata: skill.SkillMetadata{
				Name:        "test-skill",
				Description: "A test skill for demonstration. It does testing.",
				Scope:       skill.ScopePersonal,
			},
			Instructions: "Do testing.",
		},
	}

	pd.IndexSkills(skills, func(name string) bool { return name == "test-skill" })

	dir := pd.BuildSkillDirectory()
	if dir == "" {
		t.Error("expected non-empty skill directory")
	}
	if !containsStr(dir, "test-skill") {
		t.Error("expected skill name in directory")
	}
}

func TestProgressiveDisclosure_MarkExpanded_Eviction(t *testing.T) {
	pd := NewProgressiveDisclosure(3, 5) // max 3 expanded tools

	pd.MarkExpanded("tool-a")
	pd.MarkExpanded("tool-b")
	pd.MarkExpanded("tool-c")
	pd.MarkExpanded("tool-d") // should evict tool-a

	if pd.expandedTools["tool-a"] {
		t.Error("tool-a should have been evicted")
	}
	if !pd.expandedTools["tool-d"] {
		t.Error("tool-d should be in expanded set")
	}
	if len(pd.expandedTools) > 3 {
		t.Errorf("should have at most 3 expanded tools, got %d", len(pd.expandedTools))
	}
}

func TestProgressiveDisclosure_SearchTools(t *testing.T) {
	pd := NewProgressiveDisclosure(5, 5)

	pd.toolSummaries["read_file"] = &ToolSummary{
		Name:        "read_file",
		Description: "Read a file from the filesystem.",
		Category:    "filesystem",
	}
	pd.toolSummaries["run_shell"] = &ToolSummary{
		Name:        "run_shell",
		Description: "Execute a shell command.",
		Category:    "shell",
	}

	results := pd.SearchTools("file")
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'file', got %d", len(results))
	}
	if results[0].Name != "read_file" {
		t.Errorf("expected 'read_file', got %q", results[0].Name)
	}

	// Search by category
	results = pd.SearchTools("shell")
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'shell', got %d", len(results))
	}
}

func TestProgressiveDisclosure_SearchSkills(t *testing.T) {
	pd := NewProgressiveDisclosure(5, 5)
	pd.skillSummaries["go-review"] = &SkillSummary{
		Name:        "go-review",
		Description: "Code review for Go projects.",
	}
	pd.skillSummaries["rust-dev"] = &SkillSummary{
		Name:        "rust-dev",
		Description: "Rust development assistance.",
	}

	results := pd.SearchSkills("go")
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'go', got %d", len(results))
	}
}

func TestProgressiveDisclosure_GetTool(t *testing.T) {
	pd := NewProgressiveDisclosure(5, 5)
	pd.toolSummaries["test-tool"] = &ToolSummary{Name: "test-tool", Description: "test"}

	ts, ok := pd.GetTool("test-tool")
	if !ok || ts.Name != "test-tool" {
		t.Error("expected to find test-tool")
	}

	_, ok = pd.GetTool("nonexistent")
	if ok {
		t.Error("expected false for nonexistent tool")
	}
}

func TestFirstSentence(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello world. This is more.", "Hello world."},
		{"Short", "Short"},
		{"No punctuation here", "No punctuation here"},
	}
	for _, tc := range tests {
		got := firstSentence(tc.input)
		if got != tc.expected {
			t.Errorf("firstSentence(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && len(s) >= len(sub) &&
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}()
}
