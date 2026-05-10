//go:build smoke

package helpers

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// NanoConfig holds test configuration for nano-agent.
type NanoConfig struct {
	BaseURL string
	APIKey  string
}

// WriteMinimalConfig creates a minimal .nano.yaml for testing.
func WriteMinimalConfig(t *testing.T, workDir string, mockURL string) string {
	configPath := filepath.Join(workDir, ".nano.yaml")

	config := fmt.Sprintf(`# Minimal test configuration
openai:
  base_url: "%s"
  api_key: "test-key"
  model: "gpt-4"

# Skip unnecessary features for smoke tests
agent:
  max_turns: 5
  max_context_tokens: 4000

# Auto-approve all tools for testing
auto_approve:
  enabled: true
  patterns:
    - "*"
`, mockURL)

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	return configPath
}

// WriteEmptyGitRepo initializes an empty git repo for tests that need one.
func WriteEmptyGitRepo(t *testing.T, workDir string) {
	// Create a minimal git repo
	gitDir := filepath.Join(workDir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("Failed to create .git dir: %v", err)
	}

	// Write minimal git config
	configPath := filepath.Join(gitDir, "config")
	config := `[core]
	repositoryformatversion = 0
	filemode = true
	bare = false
`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("Failed to write git config: %v", err)
	}
}
