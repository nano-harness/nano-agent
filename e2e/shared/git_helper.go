//go:build e2e

package shared

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// InitTestRepo initializes a git repository in a temporary directory and creates a baseline commit.
// This is used for binary mode tests which require a git context to generate patches.
// Returns the absolute path to the repository root.
func InitTestRepo(t *testing.T) string {
	t.Helper()

	repoPath := t.TempDir()

	// Initialize git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = repoPath
	err := cmd.Run()
	require.NoError(t, err, "failed to init git repo")

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = repoPath
	err = cmd.Run()
	require.NoError(t, err, "failed to configure git user.name")

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = repoPath
	err = cmd.Run()
	require.NoError(t, err, "failed to configure git user.email")

	// Create a baseline file
	baselineFile := filepath.Join(repoPath, "README.md")
	err = os.WriteFile(baselineFile, []byte("# Test Repository\n\nBaseline content.\n"), 0644)
	require.NoError(t, err, "failed to create baseline file")

	// Add and commit the baseline
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoPath
	err = cmd.Run()
	require.NoError(t, err, "failed to git add")

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = repoPath
	err = cmd.Run()
	require.NoError(t, err, "failed to create initial commit")

	return repoPath
}
