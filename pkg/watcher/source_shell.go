package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// NewShellSourceForTest creates a ShellSource with the given command.
// Exported for use in tests only; production code should use newSource via the
// Watcher.
func NewShellSourceForTest(command string) *ShellSource {
	return &ShellSource{command: command}
}

// ShellSource polls a shell command for events. The command is expected to
// write a JSON array of objects to stdout, one per detected event. Each object
// may contain arbitrary string fields used as template variables in the rule's
// command template.
//
// If the command produces no output or exits with a non-zero status, the poll
// is treated as "no new events".
type ShellSource struct {
	command string
}

// Poll runs the shell command and parses its stdout as a JSON array of event
// payloads. The since parameter is passed to the command via the environment
// variable WATCHER_SINCE (RFC3339 format).
func (s *ShellSource) Poll(ctx context.Context, filter string, since time.Time) ([]WatchEvent, time.Time, error) {
	if s.command == "" {
		return nil, time.Now(), fmt.Errorf("shell source: no command configured")
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", s.command) //nolint:gosec
	// Always seed the environment from the current process so PATH and other
	// essential variables are available to the polling command.
	cmd.Env = cmd.Environ()
	if !since.IsZero() {
		cmd.Env = append(cmd.Env, "WATCHER_SINCE="+since.UTC().Format(time.RFC3339))
	}
	if filter != "" {
		cmd.Env = append(cmd.Env, "WATCHER_FILTER="+filter)
	}

	out, err := cmd.Output()
	now := time.Now()
	if err != nil {
		// Non-zero exit is not necessarily an error for polling commands.
		return nil, now, nil
	}

	output := strings.TrimSpace(string(out))
	if output == "" || output == "[]" || output == "null" {
		return nil, now, nil
	}

	// Try to parse as JSON array.
	var payloads []map[string]string
	if jsonErr := json.Unmarshal([]byte(output), &payloads); jsonErr != nil {
		// Not JSON – treat each non-empty line as a single "custom" event with
		// the line content stored in {{.OUTPUT}}.
		var events []WatchEvent
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				events = append(events, WatchEvent{
					Source:  "shell",
					Type:    "custom",
					Payload: map[string]string{"OUTPUT": line},
				})
			}
		}
		return events, now, nil
	}

	events := make([]WatchEvent, 0, len(payloads))
	for _, p := range payloads {
		events = append(events, WatchEvent{
			Source:  "shell",
			Type:    "custom",
			Payload: p,
		})
	}
	return events, now, nil
}
