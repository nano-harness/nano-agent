//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ExpertLoadingSuite tests loading experts from markdown files and YAML config.
// This suite validates:
// - Markdown expert parsing (frontmatter + system prompt)
// - User and project expert directory scanning
// - Expert override priority (builtin > project > user)
// - YAML sub-agent conversion to expert format
// - Security checks (symlink escapes, file size limits)
type ExpertLoadingSuite struct {
	suite.Suite
	tempDir string
}

func TestExpertLoadingSuite(t *testing.T) {
	suite.Run(t, new(ExpertLoadingSuite))
}

func (s *ExpertLoadingSuite) SetupTest() {
	s.tempDir = s.T().TempDir()
}

// TestLoadMarkdownExpert_BasicParsing verifies basic markdown expert parsing.
func (s *ExpertLoadingSuite) TestLoadMarkdownExpert_BasicParsing() {
	// Create test expert file
	expertContent := `---
name: test-expert
description: A test expert for e2e testing
when_to_use: Use when testing
model: ""
temperature: 0.5
max_turns: 15
max_time_minutes: 5
allowed_tools:
  - read_file
  - glob
output_name: report
---

You are a test expert. Your role is to help with testing.

## Guidelines

1. Always be helpful
2. Provide clear answers

## Output Format

Return results in structured format.
`

	expertDir := filepath.Join(s.tempDir, ".nano", "agents")
	err := os.MkdirAll(expertDir, 0755)
	s.Require().NoError(err)

	expertPath := filepath.Join(expertDir, "test-expert.md")
	err = os.WriteFile(expertPath, []byte(expertContent), 0644)
	s.Require().NoError(err)

	// Load experts
	registry := agent.NewExpertRegistry()
	err = agent.LoadMarkdownExperts(registry, s.tempDir)
	s.NoError(err)

	// Verify expert was loaded
	expert, exists := registry.Get("test-expert")
	s.True(exists, "Expert should be loaded")
	s.NotNil(expert)

	// Verify parsed fields
	s.Equal("test-expert", expert.Name)
	s.Equal("A test expert for e2e testing", expert.Description)
	s.Equal("project", expert.Source) // From project directory
	s.Equal(0.5, expert.Temperature)
	s.Equal(15, expert.MaxTurns)
	s.Equal(5, expert.MaxTimeMinutes)
	s.Equal("report", expert.OutputName)
	s.Equal([]string{"read_file", "glob"}, expert.AllowedTools)

	// Verify system prompt (body content)
	s.Contains(expert.SystemPrompt, "You are a test expert")
	s.Contains(expert.SystemPrompt, "## Guidelines")
	s.Contains(expert.SystemPrompt, "## Output Format")
}

// TestLoadMarkdownExpert_Defaults verifies default value application.
func (s *ExpertLoadingSuite) TestLoadMarkdownExpert_Defaults() {
	// Minimal frontmatter with missing optional fields
	expertContent := `---
name: minimal-expert
description: Minimal expert definition
---

System prompt content.
`

	expertDir := filepath.Join(s.tempDir, ".nano", "agents")
	err := os.MkdirAll(expertDir, 0755)
	s.Require().NoError(err)

	expertPath := filepath.Join(expertDir, "minimal.md")
	err = os.WriteFile(expertPath, []byte(expertContent), 0644)
	s.Require().NoError(err)

	registry := agent.NewExpertRegistry()
	err = agent.LoadMarkdownExperts(registry, s.tempDir)
	s.NoError(err)

	expert, exists := registry.Get("minimal-expert")
	s.True(exists)

	// Verify defaults
	s.Equal(20, expert.MaxTurns, "Should use default max_turns")
	s.Equal(10, expert.MaxTimeMinutes, "Should use default max_time")
	s.Equal("result", expert.OutputName, "Should use default output_name")
	s.Equal([]string{"*"}, expert.AllowedTools, "Should use default allowed_tools")
}

// TestLoadMarkdownExpert_InvalidName verifies name validation.
func (s *ExpertLoadingSuite) TestLoadMarkdownExpert_InvalidName() {
	testCases := []struct {
		name      string
		expertName string
		shouldLoad bool
	}{
		{
			name:       "uppercase name",
			expertName: "InvalidExpert",
			shouldLoad: false,
		},
		{
			name:       "starts with digit",
			expertName: "1expert",
			shouldLoad: false,
		},
		{
			name:       "contains underscore",
			expertName: "expert_name",
			shouldLoad: false,
		},
		{
			name:       "valid kebab-case",
			expertName: "valid-expert",
			shouldLoad: true,
		},
	}

	for _, tc := range testCases {
		s.T().Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			expertDir := filepath.Join(tempDir, ".nano", "agents")
			err := os.MkdirAll(expertDir, 0755)
			require.NoError(t, err)

			content := "---\nname: " + tc.expertName + "\ndescription: Test\n---\n\nSystem prompt."
			expertPath := filepath.Join(expertDir, "test.md")
			err = os.WriteFile(expertPath, []byte(content), 0644)
			require.NoError(t, err)

			registry := agent.NewExpertRegistry()
			err = agent.LoadMarkdownExperts(registry, tempDir)
			require.NoError(t, err) // Loading should not error

			_, exists := registry.Get(tc.expertName)
			if tc.shouldLoad {
				require.True(t, exists, "Expert with valid name should load")
			} else {
				require.False(t, exists, "Expert with invalid name should not load")
			}
		})
	}
}

// TestLoadMarkdownExpert_MissingFrontmatter verifies error handling.
func (s *ExpertLoadingSuite) TestLoadMarkdownExpert_MissingFrontmatter() {
	// File without frontmatter
	expertContent := `# Expert Without Frontmatter

This file has no YAML frontmatter.
`

	expertDir := filepath.Join(s.tempDir, ".nano", "agents")
	err := os.MkdirAll(expertDir, 0755)
	s.Require().NoError(err)

	expertPath := filepath.Join(expertDir, "no-frontmatter.md")
	err = os.WriteFile(expertPath, []byte(expertContent), 0644)
	s.Require().NoError(err)

	registry := agent.NewExpertRegistry()
	err = agent.LoadMarkdownExperts(registry, s.tempDir)
	s.NoError(err, "Load should not fail for invalid files")

	// Registry should be empty (file was skipped)
	s.Equal(0, registry.Count())
}

// TestLoadMarkdownExpert_FileSizeLimit verifies file size enforcement.
func (s *ExpertLoadingSuite) TestLoadMarkdownExpert_FileSizeLimit() {
	expertDir := filepath.Join(s.tempDir, ".nano", "agents")
	err := os.MkdirAll(expertDir, 0755)
	s.Require().NoError(err)

	// Create a file larger than MaxExpertFileSize (1MB)
	largeFrontmatter := "---\nname: large-expert\ndescription: Test\n---\n\n"
	largeContent := make([]byte, 1024*1024+1000) // 1MB + 1KB
	copy(largeContent, largeFrontmatter)
	for i := len(largeFrontmatter); i < len(largeContent); i++ {
		largeContent[i] = 'A'
	}

	expertPath := filepath.Join(expertDir, "large.md")
	err = os.WriteFile(expertPath, largeContent, 0644)
	s.Require().NoError(err)

	registry := agent.NewExpertRegistry()
	err = agent.LoadMarkdownExperts(registry, s.tempDir)
	s.NoError(err, "Load should not fail")

	// File should be skipped due to size limit
	_, exists := registry.Get("large-expert")
	s.False(exists, "Large file should be skipped")
}

// TestLoadMarkdownExpert_SymlinkSecurity verifies symlink escape prevention.
func (s *ExpertLoadingSuite) TestLoadMarkdownExpert_SymlinkSecurity() {
	// Skip on Windows (symlink support varies)
	if os.Getenv("GOOS") == "windows" {
		s.T().Skip("Skipping symlink test on Windows")
	}

	expertDir := filepath.Join(s.tempDir, ".nano", "agents")
	err := os.MkdirAll(expertDir, 0755)
	s.Require().NoError(err)

	// Create a file outside the agents directory
	outsideDir := filepath.Join(s.tempDir, "outside")
	err = os.MkdirAll(outsideDir, 0755)
	s.Require().NoError(err)

	outsideFile := filepath.Join(outsideDir, "malicious.md")
	maliciousContent := "---\nname: malicious\ndescription: Should not load\n---\n\nMalicious content."
	err = os.WriteFile(outsideFile, []byte(maliciousContent), 0644)
	s.Require().NoError(err)

	// Create symlink from agents/ to outside file
	symlinkPath := filepath.Join(expertDir, "link.md")
	err = os.Symlink(outsideFile, symlinkPath)
	if err != nil {
		s.T().Skipf("Cannot create symlink: %v", err)
		return
	}

	registry := agent.NewExpertRegistry()
	err = agent.LoadMarkdownExperts(registry, s.tempDir)
	s.NoError(err)

	// Symlink escape should be prevented
	_, exists := registry.Get("malicious")
	s.False(exists, "Expert from symlink escape should not load")
}

// TestLoadMarkdownExpert_NonMarkdownFiles verifies that only .md files are loaded.
func (s *ExpertLoadingSuite) TestLoadMarkdownExpert_NonMarkdownFiles() {
	expertDir := filepath.Join(s.tempDir, ".nano", "agents")
	err := os.MkdirAll(expertDir, 0755)
	s.Require().NoError(err)

	// Create various non-.md files
	txtFile := filepath.Join(expertDir, "readme.txt")
	err = os.WriteFile(txtFile, []byte("This is a text file"), 0644)
	s.Require().NoError(err)

	yamlFile := filepath.Join(expertDir, "config.yaml")
	err = os.WriteFile(yamlFile, []byte("key: value"), 0644)
	s.Require().NoError(err)

	// Create valid .md file
	mdFile := filepath.Join(expertDir, "valid.md")
	validContent := "---\nname: valid\ndescription: Valid expert\n---\n\nSystem prompt."
	err = os.WriteFile(mdFile, []byte(validContent), 0644)
	s.Require().NoError(err)

	registry := agent.NewExpertRegistry()
	err = agent.LoadMarkdownExperts(registry, s.tempDir)
	s.NoError(err)

	// Only .md file should be loaded
	s.Equal(1, registry.Count(), "Only markdown files should be loaded")
	_, exists := registry.Get("valid")
	s.True(exists)
}

// TestLoadMarkdownExpert_NestedDirectories verifies that nested dirs are ignored.
func (s *ExpertLoadingSuite) TestLoadMarkdownExpert_NestedDirectories() {
	expertDir := filepath.Join(s.tempDir, ".nano", "agents")
	nestedDir := filepath.Join(expertDir, "nested")
	err := os.MkdirAll(nestedDir, 0755)
	s.Require().NoError(err)

	// Create expert in nested directory (should be ignored)
	nestedExpert := filepath.Join(nestedDir, "nested-expert.md")
	content := "---\nname: nested\ndescription: Nested expert\n---\n\nPrompt."
	err = os.WriteFile(nestedExpert, []byte(content), 0644)
	s.Require().NoError(err)

	// Create expert in root agents/ directory
	rootExpert := filepath.Join(expertDir, "root-expert.md")
	rootContent := "---\nname: root\ndescription: Root expert\n---\n\nPrompt."
	err = os.WriteFile(rootExpert, []byte(rootContent), 0644)
	s.Require().NoError(err)

	registry := agent.NewExpertRegistry()
	err = agent.LoadMarkdownExperts(registry, s.tempDir)
	s.NoError(err)

	// Only root expert should load (nested directories are skipped)
	s.Equal(1, registry.Count())
	_, exists := registry.Get("root")
	s.True(exists)
	_, exists = registry.Get("nested")
	s.False(exists, "Nested directory experts should not load")
}

// TestLoadMarkdownExpert_NonexistentDirectory verifies graceful handling.
func (s *ExpertLoadingSuite) TestLoadMarkdownExpert_NonexistentDirectory() {
	// workDir points to directory without .nano/agents/
	registry := agent.NewExpertRegistry()
	err := agent.LoadMarkdownExperts(registry, s.tempDir)

	s.NoError(err, "Should handle missing directory gracefully")
	s.Equal(0, registry.Count())
}

// TestYAMLSubAgentConversion_Basic verifies YAML sub-agent to expert conversion.
func (s *ExpertLoadingSuite) TestYAMLSubAgentConversion_Basic() {
	subAgents := []agent.SubAgentConfig{
		{
			AgentName:    "coder",
			SystemPrompt: "You are a coding assistant.",
			WhenToUse:    "Use when coding",
			AllowedTools: []string{"read_file", "write_file"},
			Enabled:      true,
		},
	}

	registry := agent.NewExpertRegistry()
	err := agent.LoadYAMLSubAgentsAsExperts(registry, subAgents)
	s.NoError(err)

	expert, exists := registry.Get("coder")
	s.True(exists)
	s.Equal("coder", expert.Name)
	s.Equal("coder", expert.DisplayName)
	s.Equal("Use when coding", expert.Description)
	s.Equal("yaml", expert.Source)
	s.Equal("You are a coding assistant.", expert.SystemPrompt)
	s.Equal([]string{"read_file", "write_file"}, expert.AllowedTools)
}

// TestYAMLSubAgentConversion_KebabCase verifies name conversion.
func (s *ExpertLoadingSuite) TestYAMLSubAgentConversion_KebabCase() {
	testCases := []struct {
		input    string
		expected string
	}{
		{"coder", "coder"},
		{"myAgent", "my-agent"},
		{"my_agent", "my-agent"},
		{"MyAgent", "my-agent"},
		{"API_Helper", "api-helper"},
	}

	for _, tc := range testCases {
		s.T().Run(tc.input, func(t *testing.T) {
			subAgents := []agent.SubAgentConfig{
				{
					AgentName:    tc.input,
					SystemPrompt: "Test",
					Enabled:      true,
				},
			}

			registry := agent.NewExpertRegistry()
			err := agent.LoadYAMLSubAgentsAsExperts(registry, subAgents)
			require.NoError(t, err)

			_, exists := registry.Get(tc.expected)
			require.True(t, exists, "Expert should be loaded with kebab-case name")
		})
	}
}

// TestYAMLSubAgentConversion_DisabledAgent verifies disabled agents are skipped.
func (s *ExpertLoadingSuite) TestYAMLSubAgentConversion_DisabledAgent() {
	subAgents := []agent.SubAgentConfig{
		{
			AgentName:    "enabled-agent",
			SystemPrompt: "Enabled",
			Enabled:      true,
		},
		{
			AgentName:    "disabled-agent",
			SystemPrompt: "Disabled",
			Enabled:      false,
		},
	}

	registry := agent.NewExpertRegistry()
	err := agent.LoadYAMLSubAgentsAsExperts(registry, subAgents)
	s.NoError(err)

	s.Equal(1, registry.Count(), "Only enabled agent should load")
	_, exists := registry.Get("enabled-agent")
	s.True(exists)
	_, exists = registry.Get("disabled-agent")
	s.False(exists, "Disabled agent should not load")
}

// TestYAMLSubAgentConversion_DefaultAllowedTools verifies default tool access.
func (s *ExpertLoadingSuite) TestYAMLSubAgentConversion_DefaultAllowedTools() {
	subAgents := []agent.SubAgentConfig{
		{
			AgentName:    "full-access",
			SystemPrompt: "Test",
			AllowedTools: []string{}, // Empty should default to ["*"]
			Enabled:      true,
		},
	}

	registry := agent.NewExpertRegistry()
	err := agent.LoadYAMLSubAgentsAsExperts(registry, subAgents)
	s.NoError(err)

	expert, exists := registry.Get("full-access")
	s.True(exists)
	s.Equal([]string{"*"}, expert.AllowedTools, "Empty allowed_tools should default to [\"*\"]")
}

// TestYAMLSubAgentConversion_NonASCIIName verifies non-ASCII rejection.
func (s *ExpertLoadingSuite) TestYAMLSubAgentConversion_NonASCIIName() {
	subAgents := []agent.SubAgentConfig{
		{
			AgentName:    "穿越小说家", // Non-ASCII name
			SystemPrompt: "Test",
			Enabled:      true,
		},
	}

	registry := agent.NewExpertRegistry()
	err := agent.LoadYAMLSubAgentsAsExperts(registry, subAgents)
	s.NoError(err, "Load should not error")

	// Non-ASCII name should be skipped
	s.Equal(0, registry.Count(), "Non-ASCII expert should not load")
}
