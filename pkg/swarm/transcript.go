package swarm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TranscriptEntry represents a single entry in an agent's transcript.
type TranscriptEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Role      string                 `json:"role"` // "user", "assistant", "tool_use", "tool_result"
	Content   string                 `json:"content,omitempty"`
	ToolName  string                 `json:"tool_name,omitempty"`
	ToolID    string                 `json:"tool_id,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// TranscriptWriter writes agent transcript entries to a JSONL file.
type TranscriptWriter struct {
	path string
	file *os.File
	enc  *json.Encoder
}

// NewTranscriptWriter creates a transcript writer for the given agent.
func NewTranscriptWriter(outputDir, agentID string) (*TranscriptWriter, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create transcript dir: %w", err)
	}
	safeName := strings.ReplaceAll(agentID, "@", "-")
	safeName = strings.ReplaceAll(safeName, "/", "-")
	path := filepath.Join(outputDir, fmt.Sprintf("agent-%s.jsonl", safeName))

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &TranscriptWriter{
		path: path,
		file: f,
		enc:  json.NewEncoder(f),
	}, nil
}

// Write appends an entry to the transcript.
func (w *TranscriptWriter) Write(entry TranscriptEntry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	return w.enc.Encode(entry)
}

// Close closes the transcript file.
func (w *TranscriptWriter) Close() error {
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// Path returns the transcript file path.
func (w *TranscriptWriter) Path() string {
	return w.path
}

// ReadTranscript reads all transcript entries from a file.
func ReadTranscript(path string) ([]TranscriptEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var entries []TranscriptEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry TranscriptEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // Skip malformed entries
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// TranscriptPath returns the expected path for an agent's transcript file.
func TranscriptPath(outputDir, agentID string) string {
	safeName := strings.ReplaceAll(agentID, "@", "-")
	safeName = strings.ReplaceAll(safeName, "/", "-")
	return filepath.Join(outputDir, fmt.Sprintf("agent-%s.jsonl", safeName))
}
