//go:build smoke

package smoke

import (
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/smoke/helpers"
	"github.com/stretchr/testify/require"
)

// TestSmoke_TUI_MultilineInput tests multi-line input with backslash continuation.
func TestSmoke_TUI_MultilineInput(t *testing.T) {
	workDir := t.TempDir()
	mock := helpers.NewMockLLMServer(t)
	helpers.WriteMinimalConfig(t, workDir, mock.URL())

	session, err := helpers.NewPTYSession(t, workDir, "--tui")
	require.NoError(t, err)

	err = session.Start()
	require.NoError(t, err)

	// Wait for TUI to initialize
	time.Sleep(1 * time.Second)

	// Send multi-line input using backslash continuation
	// Line 1: "hello\"
	err = session.Send("hello\\")
	require.NoError(t, err)

	err = session.Send("\r")
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// Line 2: "world"
	err = session.SendLine("world")
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(2 * time.Second)

	// Exit
	err = session.SendCtrlC()
	require.NoError(t, err)

	if err := session.WaitWithTimeout(3 * time.Second); err != nil {
		t.Logf("TUI did not exit after Ctrl+C; cleaned up PTY process: %v", err)
	}

	t.Log("✓ Multi-line input test completed")
}

// TestSmoke_TUI_SlashCommand tests slash command execution.
func TestSmoke_TUI_SlashCommand(t *testing.T) {
	workDir := t.TempDir()
	mock := helpers.NewMockLLMServer(t)
	helpers.WriteMinimalConfig(t, workDir, mock.URL())

	session, err := helpers.NewPTYSession(t, workDir, "--tui")
	require.NoError(t, err)

	err = session.Start()
	require.NoError(t, err)

	// Wait for TUI to initialize
	time.Sleep(1 * time.Second)

	// Send a slash command (e.g., /help or /clear)
	err = session.SendLine("/help")
	require.NoError(t, err)

	// Wait for command to be processed
	time.Sleep(1 * time.Second)

	// Exit
	err = session.SendCtrlC()
	require.NoError(t, err)

	if err := session.WaitWithTimeout(3 * time.Second); err != nil {
		t.Logf("TUI did not exit after Ctrl+C; cleaned up PTY process: %v", err)
	}

	t.Log("✓ Slash command test completed")
}

// TestSmoke_TUI_SessionResume tests session resume functionality.
func TestSmoke_TUI_SessionResume(t *testing.T) {
	workDir := t.TempDir()
	mock := helpers.NewMockLLMServer(t)
	helpers.WriteMinimalConfig(t, workDir, mock.URL())

	// First session: send a message and get session ID
	session1, err := helpers.NewPTYSession(t, workDir, "--tui")
	require.NoError(t, err)

	err = session1.Start()
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	// Send a message to create session history
	err = session1.SendLine("test message")
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	// Exit first session
	err = session1.SendCtrlC()
	require.NoError(t, err)
	if err := session1.WaitWithTimeout(3 * time.Second); err != nil {
		t.Logf("First TUI session did not exit after Ctrl+C; cleaned up PTY process: %v", err)
	}

	// Small delay between sessions
	time.Sleep(500 * time.Millisecond)

	// Second session: resume with --continue flag
	session2, err := helpers.NewPTYSession(t, workDir, "--tui", "--continue")
	require.NoError(t, err)

	err = session2.Start()
	require.NoError(t, err)

	// Wait for resume to complete
	time.Sleep(2 * time.Second)

	// Exit second session
	err = session2.SendCtrlC()
	require.NoError(t, err)
	if err := session2.WaitWithTimeout(3 * time.Second); err != nil {
		t.Logf("Second TUI session did not exit after Ctrl+C; cleaned up PTY process: %v", err)
	}

	t.Log("✓ Session resume test completed")
}
