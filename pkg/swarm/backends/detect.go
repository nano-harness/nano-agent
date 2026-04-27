// Package backends provides terminal multiplexer integrations for subprocess teammates
package backends

import (
	"fmt"
	"os"
	"runtime"
)

// Backend represents a terminal multiplexer backend for spawning subprocess teammates
type Backend interface {
	// SpawnPane creates a new pane and runs the given command
	// Returns: paneID, processID, error
	SpawnPane(cmd []string, color string) (string, int, error)

	// KillPane terminates a pane by its ID
	KillPane(paneID string) error
}

// DetectBackend detects and returns the available backend
// Priority: tmux > iTerm2 > error
func DetectBackend() (Backend, error) {
	// Check if running in tmux
	if os.Getenv("TMUX") != "" {
		return NewTmuxBackend(), nil
	}

	// Check if running on macOS with iTerm2
	if runtime.GOOS == "darwin" {
		if isITerm2Available() {
			return NewITerm2Backend(), nil
		}
	}

	return nil, fmt.Errorf("no suitable backend found (not in tmux, iTerm2 not available); use in_process spawn instead")
}

// isITerm2Available checks if iTerm2 is running
func isITerm2Available() bool {
	// Check if iTerm2 process is running
	// For now, just return false - full implementation would use pgrep or osascript
	return false
}
