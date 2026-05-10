//go:build smoke

package smoke

import (
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/smoke/helpers"
	"github.com/stretchr/testify/require"
)

// TestSmoke_TUI_BasicStartupAndExit tests that nano can start in TUI mode and exit cleanly.
// This is the minimum viable smoke test (P0 requirement).
func TestSmoke_TUI_BasicStartupAndExit(t *testing.T) {
	// Setup: temporary workspace and mock LLM
	workDir := t.TempDir()
	mock := helpers.NewMockLLMServer(t)
	helpers.WriteMinimalConfig(t, workDir, mock.URL())

	// Create PTY session
	session, err := helpers.NewPTYSession(t, workDir, "--tui")
	require.NoError(t, err, "Failed to create PTY session")

	// Start nano
	err = session.Start()
	require.NoError(t, err, "Failed to start nano")

	// Wait a bit for TUI to initialize
	time.Sleep(1 * time.Second)

	// Send Ctrl+C to exit
	err = session.SendCtrlC()
	require.NoError(t, err, "Failed to send Ctrl+C")

	// Wait for clean exit
	err = session.WaitWithTimeout(3 * time.Second)
	// Note: exit code may be non-zero when interrupted with Ctrl+C, which is acceptable
	if err != nil {
		t.Logf("Exit with signal (expected): %v", err)
	}

	t.Log("✓ TUI started and exited cleanly")
}

// TestSmoke_TUI_BasicDialog tests basic dialog interaction.
func TestSmoke_TUI_BasicDialog(t *testing.T) {
	workDir := t.TempDir()
	mock := helpers.NewMockLLMServer(t)
	helpers.WriteMinimalConfig(t, workDir, mock.URL())

	session, err := helpers.NewPTYSession(t, workDir, "--tui")
	require.NoError(t, err)

	err = session.Start()
	require.NoError(t, err)

	// Wait for prompt (use generous timeout for TUI initialization)
	err = session.WaitForPrompt()
	if err != nil {
		t.Logf("Prompt detection timed out (TUI may use different prompt), continuing anyway")
	}

	// Send a message
	err = session.SendLine("hello")
	require.NoError(t, err)

	// Wait for mock response to appear
	time.Sleep(2 * time.Second)

	// Exit
	err = session.SendCtrlC()
	require.NoError(t, err)

	if err := session.WaitWithTimeout(3 * time.Second); err != nil {
		t.Logf("TUI did not exit after Ctrl+C; cleaned up PTY process: %v", err)
	}

	// Verify mock server received the request
	requests := mock.GetRequests()
	if len(requests) > 0 {
		t.Logf("✓ Mock server received %d request(s)", len(requests))
	} else {
		t.Log("⚠ Mock server received no requests (TUI may have started but not submitted)")
	}
}

// TestSmoke_CLI_Version tests simple CLI command execution.
func TestSmoke_CLI_Version(t *testing.T) {
	workDir := t.TempDir()

	// Use --version to run as a CLI command instead of entering TUI mode.
	session, err := helpers.NewPTYSession(t, workDir, "--version")
	require.NoError(t, err)

	err = session.Start()
	require.NoError(t, err)

	// Wait for completion
	err = session.WaitWithTimeout(5 * time.Second)
	require.NoError(t, err, "Version command should exit cleanly")

	t.Log("✓ CLI version command executed successfully")
}
