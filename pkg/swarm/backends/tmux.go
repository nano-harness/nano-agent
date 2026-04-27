package backends

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// TmuxBackend implements Backend using tmux
type TmuxBackend struct{}

// NewTmuxBackend creates a new TmuxBackend
func NewTmuxBackend() *TmuxBackend {
	return &TmuxBackend{}
}

// SpawnPane creates a new tmux pane and runs the given command
func (t *TmuxBackend) SpawnPane(cmd []string, color string) (string, int, error) {
	if len(cmd) == 0 {
		return "", 0, fmt.Errorf("command cannot be empty")
	}

	// Join command parts
	cmdStr := strings.Join(cmd, " ")

	// Split the current pane and get the new pane ID
	// -P: print pane info, -F: format string, -d: don't switch to new pane
	splitCmd := exec.Command("tmux", "split-window", "-P", "-F", "#{pane_id}", "-d", cmdStr)
	output, err := splitCmd.Output()
	if err != nil {
		return "", 0, fmt.Errorf("failed to split tmux pane: %w", err)
	}

	paneID := strings.TrimSpace(string(output))
	if paneID == "" {
		return "", 0, fmt.Errorf("tmux did not return a pane ID")
	}

	// Get the process ID of the command running in the pane
	pidCmd := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{pane_pid}")
	pidOutput, err := pidCmd.Output()
	if err != nil {
		return paneID, 0, fmt.Errorf("failed to get pane PID: %w", err)
	}

	pidStr := strings.TrimSpace(string(pidOutput))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return paneID, 0, fmt.Errorf("failed to parse PID %q: %w", pidStr, err)
	}

	// TODO: Apply color to pane if supported
	_ = color

	return paneID, pid, nil
}

// KillPane terminates a tmux pane
func (t *TmuxBackend) KillPane(paneID string) error {
	if paneID == "" {
		return fmt.Errorf("pane ID cannot be empty")
	}

	cmd := exec.Command("tmux", "kill-pane", "-t", paneID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to kill tmux pane %s: %w", paneID, err)
	}

	return nil
}
