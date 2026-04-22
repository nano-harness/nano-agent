package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TaskLogEntry records a single task execution.
type TaskLogEntry struct {
	TaskID     string    `json:"task_id"`
	Command    string    `json:"command"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`

	// M2 extended fields (all omitempty for backward compatibility)
	SessionID     string `json:"session_id,omitempty"`      // Session ID for this execution
	Source        string `json:"source,omitempty"`          // Source: cli/tui/agent/daemon
	EventsPath    string `json:"events_path,omitempty"`     // Path to detailed event log file
	DurationMs    int64  `json:"duration_ms,omitempty"`     // Duration in milliseconds
	ToolCallCount int    `json:"tool_call_count,omitempty"` // Number of tool calls made
	TokenUsage    int64  `json:"token_usage,omitempty"`     // Total tokens used
	SchemaVersion int    `json:"schema_version,omitempty"`  // Schema version (2 for M2+)

	// M3 extended fields
	FailureStage   string `json:"failure_stage,omitempty"`    // Failure stage classification
	FailedToolName string `json:"failed_tool_name,omitempty"` // Name of tool that failed
	FailureMessage string `json:"failure_message,omitempty"`  // Structured failure message
}

// MarshalJSON implements json.Marshaler for TaskLogEntry.
func (e *TaskLogEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(*e)
}

// TaskLog persists task execution history to a JSONL file.
// Each line in the file is a JSON-encoded TaskLogEntry.
type TaskLog struct {
	mu   sync.Mutex
	path string
}

// NewTaskLog creates a TaskLog backed by the given file path.
// The parent directory is created on the first Append call.
func NewTaskLog(path string) *TaskLog {
	return &TaskLog{path: path}
}

// DefaultTaskLogPath returns the default path for the task log.
func DefaultTaskLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".nano", "task_log.jsonl"), nil
}

// Append records a task execution to the log file.
func (tl *TaskLog) Append(entry TaskLogEntry) error {
	if tl.path == "" {
		return nil
	}

	tl.mu.Lock()
	defer tl.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(tl.path), 0o755); err != nil {
		return fmt.Errorf("create task log dir: %w", err)
	}

	f, err := os.OpenFile(tl.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open task log: %w", err)
	}
	defer func() { _ = f.Close() }()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal task log entry: %w", err)
	}
	_, err = fmt.Fprintf(f, "%s\n", data)
	return err
}

// Query reads all log entries from the file and returns them sorted by StartedAt
// (most recent last). An empty or non-existent file returns an empty slice.
func (tl *TaskLog) Query() ([]TaskLogEntry, error) {
	if tl.path == "" {
		return nil, nil
	}

	tl.mu.Lock()
	defer tl.mu.Unlock()

	data, err := os.ReadFile(tl.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read task log: %w", err)
	}

	var entries []TaskLogEntry
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		var e TaskLogEntry
		if jsonErr := json.Unmarshal([]byte(line), &e); jsonErr == nil {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

// Cleanup removes log entries older than the given duration.
func (tl *TaskLog) Cleanup(maxAge time.Duration) error {
	if tl.path == "" || maxAge <= 0 {
		return nil
	}

	entries, err := tl.Query()
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-maxAge)
	var kept []TaskLogEntry
	for _, e := range entries {
		if e.StartedAt.After(cutoff) {
			kept = append(kept, e)
		}
	}

	if len(kept) == len(entries) {
		return nil // nothing to prune
	}

	tl.mu.Lock()
	defer tl.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(tl.path), 0o755); err != nil {
		return fmt.Errorf("create task log dir: %w", err)
	}

	tmpPath := tl.path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open tmp task log: %w", err)
	}
	// writeFailed tracks whether we must discard the tmp file.
	writeFailed := false
	defer func() {
		_ = f.Close()
		if writeFailed {
			_ = os.Remove(tmpPath)
		}
	}()
	for _, e := range kept {
		data, marshalErr := json.Marshal(e)
		if marshalErr != nil {
			writeFailed = true
			return fmt.Errorf("marshal task log entry: %w", marshalErr)
		}
		if _, writeErr := fmt.Fprintf(f, "%s\n", data); writeErr != nil {
			writeFailed = true
			return fmt.Errorf("write task log entry: %w", writeErr)
		}
	}
	if closeErr := f.Close(); closeErr != nil {
		writeFailed = true
		return fmt.Errorf("close tmp task log: %w", closeErr)
	}
	return os.Rename(tmpPath, tl.path)
}

// splitLines splits a string into lines, discarding empty trailing lines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
