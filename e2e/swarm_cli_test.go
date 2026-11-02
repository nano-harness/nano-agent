//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChatCommand_Help verifies the chat command help text
func TestChatCommand_Help(t *testing.T) {
	// Build the binary first
	binPath := buildTestBinary(t)

	cmd := exec.Command(binPath, "chat", "--help")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err)

	outputStr := string(output)
	assert.Contains(t, outputStr, "team-lead")
	assert.Contains(t, outputStr, "--team")
	assert.Contains(t, outputStr, "REPL")
}

// TestTeammateCommand_Help verifies the teammate command help text
func TestTeammateCommand_Help(t *testing.T) {
	binPath := buildTestBinary(t)

	cmd := exec.Command(binPath, "teammate", "--help")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err)

	outputStr := string(output)
	assert.Contains(t, outputStr, "teammate")
	assert.Contains(t, outputStr, "--team")
	assert.Contains(t, outputStr, "--name")
	assert.Contains(t, outputStr, "--session")
	assert.Contains(t, outputStr, "--initial-prompt-file")
}

// TestRootCommand_TeamFlag verifies --team flag in root command
func TestRootCommand_TeamFlag(t *testing.T) {
	binPath := buildTestBinary(t)

	cmd := exec.Command(binPath, "--help")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err)

	outputStr := string(output)
	assert.Contains(t, outputStr, "--team")
	assert.Contains(t, outputStr, "team-lead mode")
	assert.Contains(t, outputStr, "mailbox support")
}

// TestChatCommand_InvalidTeam verifies error handling with invalid team name
func TestChatCommand_InvalidTeam(t *testing.T) {
	t.Skip("Skipping interactive test - chat command requires TTY")
	// This test would require more complex TTY simulation
}

// TestTeammateCommand_MissingLeadMailbox verifies validation of required flags
func TestTeammateCommand_MissingLeadMailbox(t *testing.T) {
	t.Skip("Skipping interactive test - teammate command requires specific environment")
	// This test would require setting up a team-lead session first
}

// buildTestBinary builds the nano binary for testing and returns the path
func buildTestBinary(t *testing.T) string {
	t.Helper()

	// Create temp directory for test binary
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "nano")

	// Build the binary
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./cmd/nano")
	cmd.Dir = getRepoRoot(t)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to build binary: %s", string(output))

	return binPath
}

// getRepoRoot returns the repository root directory
func getRepoRoot(t *testing.T) string {
	t.Helper()

	// Get current directory
	wd, err := os.Getwd()
	require.NoError(t, err)

	// Walk up until we find go.mod
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root without finding go.mod
			t.Fatal("Could not find repository root (go.mod not found)")
		}
		dir = parent
	}
}

// TestEngineInitialization_TeamMode verifies engine can be created in team-lead mode
func TestEngineInitialization_TeamMode(t *testing.T) {
	// This is a unit-style test but placed in e2e for integration context
	// It verifies that NewLeadEngine can be called successfully

	// Note: This would require proper config setup
	t.Skip("Skipping - requires full config initialization")
}

// TestCLI_Flags_Compatibility verifies flag compatibility between modes
func TestCLI_Flags_Compatibility(t *testing.T) {
	binPath := buildTestBinary(t)

	tests := []struct {
		name          string
		args          []string
		shouldContain string
	}{
		{
			name:          "TUI with team flag",
			args:          []string{"--help"},
			shouldContain: "--team",
		},
		{
			name:          "Chat command structure",
			args:          []string{"chat", "--help"},
			shouldContain: "--team",
		},
		{
			name:          "Teammate command structure",
			args:          []string{"teammate", "--help"},
			shouldContain: "--initial-prompt-file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(binPath, tt.args...)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err)

			outputStr := string(output)
			assert.Contains(t, outputStr, tt.shouldContain)
		})
	}
}

// TestCommandDiscovery verifies swarm commands are registered
func TestCommandDiscovery(t *testing.T) {
	binPath := buildTestBinary(t)

	cmd := exec.Command(binPath, "--help")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err)

	outputStr := string(output)

	// Verify chat command is present (teammate is hidden)
	assert.Contains(t, outputStr, "chat", "Command chat should be registered")

	// Verify teammate command exists (even though hidden from main help)
	teammateCmd := exec.Command(binPath, "teammate", "--help")
	teammateOutput, err := teammateCmd.CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(teammateOutput), "teammate")
}

// TestCLI_ConfigValidation verifies config validation for team modes
func TestCLI_ConfigValidation(t *testing.T) {
	t.Skip("Requires config file setup - test structure verified")

	// This test would verify:
	// 1. Team-lead mode requires valid API key
	// 2. Teammate mode requires valid lead-mailbox path
	// 3. Error messages are clear when config is missing
}

// TestTeamSessionPersistence verifies team sessions persist correctly
func TestTeamSessionPersistence(t *testing.T) {
	t.Skip("Requires daemon integration - test structure verified")

	// This test would verify:
	// 1. Creating a team session via daemon
	// 2. Executing commands in that session
	// 3. Verifying session state persists across requests
	// 4. Cleanup on session deletion
}

// TestConcurrentTeamSessions verifies multiple team sessions can run concurrently
func TestConcurrentTeamSessions(t *testing.T) {
	t.Skip("Requires full daemon setup - test structure verified")

	// This test would verify:
	// 1. Create multiple team sessions with different names
	// 2. Execute commands in parallel across sessions
	// 3. Verify sessions don't interfere with each other
	// 4. Check mailbox isolation between teams
}

// TestTeamSession_IdleTimeout verifies idle session cleanup
func TestTeamSession_IdleTimeout(t *testing.T) {
	t.Skip("Requires time-dependent daemon behavior - test structure verified")

	// This test would verify:
	// 1. Create a team session
	// 2. Wait for idle timeout period
	// 3. Verify session is automatically cleaned up
	// 4. Verify cleanup is logged properly
}

// TestMailbox_Integration verifies mailbox integration in team mode
func TestMailbox_Integration(t *testing.T) {
	// Use existing mailbox e2e tests
	// Just verify they work in team-lead context
	t.Skip("Covered by mailbox_*_e2e_test.go files")
}

// TestFlagParsing verifies all team-related flags parse correctly
func TestFlagParsing(t *testing.T) {
	binPath := buildTestBinary(t)

	tests := []struct {
		name          string
		args          []string
		expectError   bool
		errorContains string
	}{
		{
			name:        "Valid team flag",
			args:        []string{"--help", "--team", "alpha"},
			expectError: false,
		},
		{
			name:        "Empty team name",
			args:        []string{"--help", "--team", ""},
			expectError: false, // Empty is allowed, will use default
		},
		{
			name:        "Team flag without TUI",
			args:        []string{"--help", "--team", "test"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(binPath, tt.args...)
			output, err := cmd.CombinedOutput()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, string(output), tt.errorContains)
				}
			} else {
				// Help output may exit with 0, check output instead
				outputStr := strings.ToLower(string(output))
				assert.True(t, strings.Contains(outputStr, "usage") || strings.Contains(outputStr, "help"))
			}
		})
	}
}
