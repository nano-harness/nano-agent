package backends

import (
	"fmt"
	"os/exec"
	"strings"
)

// ITerm2Backend implements Backend using iTerm2 via AppleScript
type ITerm2Backend struct{}

// NewITerm2Backend creates a new ITerm2Backend
func NewITerm2Backend() *ITerm2Backend {
	return &ITerm2Backend{}
}

// SpawnPane creates a new iTerm2 pane and runs the given command
func (i *ITerm2Backend) SpawnPane(cmd []string, color string) (string, int, error) {
	if len(cmd) == 0 {
		return "", 0, fmt.Errorf("command cannot be empty")
	}

	// Join command parts
	cmdStr := strings.Join(cmd, " ")

	// Create AppleScript to split pane and run command
	script := fmt.Sprintf(`
tell application "iTerm"
    tell current window
        tell current session
            split vertically with default profile
            tell second session
                write text "%s"
                get tty
            end tell
        end tell
    end tell
end tell
`, cmdStr)

	// Execute AppleScript
	osascriptCmd := exec.Command("osascript", "-e", script)
	output, err := osascriptCmd.Output()
	if err != nil {
		return "", 0, fmt.Errorf("failed to execute AppleScript: %w", err)
	}

	tty := strings.TrimSpace(string(output))
	if tty == "" {
		return "", 0, fmt.Errorf("iTerm2 did not return a TTY")
	}

	// For iTerm2, we use the TTY as the "pane ID"
	// Getting the actual PID is more complex, so we return 0 for now
	// TODO: Implement PID retrieval for iTerm2
	return tty, 0, nil
}

// KillPane terminates an iTerm2 pane
func (i *ITerm2Backend) KillPane(paneID string) error {
	if paneID == "" {
		return fmt.Errorf("pane ID (TTY) cannot be empty")
	}

	// Create AppleScript to close the session with the given TTY
	script := fmt.Sprintf(`
tell application "iTerm"
    tell current window
        repeat with aSession in sessions
            if tty of aSession is "%s" then
                close aSession
                exit repeat
            end if
        end repeat
    end tell
end tell
`, paneID)

	osascriptCmd := exec.Command("osascript", "-e", script)
	if err := osascriptCmd.Run(); err != nil {
		return fmt.Errorf("failed to kill iTerm2 pane %s: %w", paneID, err)
	}

	return nil
}
