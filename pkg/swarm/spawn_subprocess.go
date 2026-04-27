package swarm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nano-harness/nano-agent/pkg/swarm/backends"
	"github.com/nano-harness/nano-agent/pkg/team"
)

// SpawnSubprocess spawns a teammate in a separate subprocess (tmux/iTerm2 pane)
func SpawnSubprocess(ctx context.Context, opts SpawnOptions) (*SpawnHandle, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	// Detect available backend
	backend, err := backends.DetectBackend()
	if err != nil {
		return nil, fmt.Errorf("no subprocess backend available: %w (hint: use kind=in_process instead)", err)
	}

	identity := opts.newIdentity()

	// Write initial prompt to a temp file
	promptFile, err := writeTempPromptFile(opts.InitialPrompt)
	if err != nil {
		return nil, fmt.Errorf("failed to write prompt file: %w", err)
	}

	// Get the path to the nano-agent executable
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	// Build command to run in the subprocess
	cmd := []string{
		execPath,
		"teammate",
		"--team", opts.TeamName,
		"--name", opts.Name,
		"--session", identity.sessionID,
		"--initial-prompt-file", promptFile,
	}

	// Spawn the pane
	paneID, pid, err := backend.SpawnPane(cmd, opts.Color)
	if err != nil {
		_ = os.Remove(promptFile)
		return nil, fmt.Errorf("failed to spawn pane: %w", err)
	}

	// Add member to team
	member := opts.newTeamMember(identity.agentID, identity.sessionID, team.KindSubprocess)
	member.TmuxPaneID = paneID
	member.PID = pid
	if err := team.AddMember(opts.TeamName, member); err != nil {
		// Try to kill the pane we just spawned
		_ = backend.KillPane(paneID)
		_ = os.Remove(promptFile)
		return nil, fmt.Errorf("failed to add team member: %w", err)
	}

	return &SpawnHandle{
		AgentID:   identity.agentID,
		SessionID: identity.sessionID,
		Done:      nil, // Subprocess doesn't provide a done channel
	}, nil
}

// writeTempPromptFile writes the initial prompt to a temporary file
func writeTempPromptFile(prompt string) (string, error) {
	tmpFile, err := os.CreateTemp("", "nano-teammate-prompt-*.txt")
	if err != nil {
		return "", err
	}
	defer func() { _ = tmpFile.Close() }()

	if _, err := tmpFile.WriteString(prompt); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

// GetExecutablePath returns the path to the nano-agent binary
// Exported for testing
func GetExecutablePath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", err
	}

	// Resolve symlinks
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", err
	}

	return execPath, nil
}
