package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/llm"
)

// TranscriptEntry is one JSONL record in a ralph-loop compatible transcript.
type TranscriptEntry struct {
	Type           string                 `json:"type"`
	UUID           string                 `json:"uuid"`
	ParentUUID     string                 `json:"parentUuid,omitempty"`
	Timestamp      time.Time              `json:"timestamp"`
	SessionID      string                 `json:"sessionId"`
	Cwd            string                 `json:"cwd"`
	Version        string                 `json:"version"`
	TurnID         string                 `json:"turnId,omitempty"`
	RalphIteration int                    `json:"ralphIteration,omitempty"`
	Message        map[string]interface{} `json:"message,omitempty"`
	ToolUseResult  map[string]interface{} `json:"toolUseResult,omitempty"`
	IsMeta         bool                   `json:"isMeta,omitempty"`
}

type TranscriptWriter struct {
	path string
	mu   sync.Mutex
	f    *os.File
}

func NewTranscriptWriter(sessionID string) (*TranscriptWriter, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Join(home, ".nano-agent", "sessions")
	sessionDir := filepath.Clean(filepath.Join(baseDir, sanitizeSessionIDForPath(sessionID)))
	if sessionDir != baseDir && !strings.HasPrefix(sessionDir, baseDir+string(os.PathSeparator)) {
		return nil, fmt.Errorf("invalid transcript session path")
	}
	path := filepath.Join(sessionDir, "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &TranscriptWriter{path: path, f: f}, nil
}

func (w *TranscriptWriter) Append(entry TranscriptEntry) error {
	if w == nil || w.f == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if entry.UUID == "" {
		entry.UUID = generateTranscriptUUID()
	}
	if entry.Version == "" {
		entry.Version = "nano-agent"
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := w.f.Write(append(data, '\n')); err != nil {
		return err
	}
	return w.f.Sync()
}

func (w *TranscriptWriter) Path() string {
	if w == nil {
		return ""
	}
	return w.path
}

func (w *TranscriptWriter) Close() error {
	if w == nil || w.f == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.f.Close()
	w.f = nil
	return err
}

func transcriptEntryForMessage(sessionID, cwd, turnID string, iteration int, typ string, msg llm.Message) TranscriptEntry {
	message := map[string]interface{}{
		"role":    msg.Role,
		"content": msg.Content,
	}
	if len(msg.ToolCalls) > 0 {
		message["tool_calls"] = msg.ToolCalls
	}
	if msg.ToolCallID != "" {
		message["tool_call_id"] = msg.ToolCallID
	}
	if len(msg.ReasoningBlocks) > 0 {
		message["reasoning_blocks"] = msg.ReasoningBlocks
		message["reasoning"] = llm.BlocksToText(msg.ReasoningBlocks)
	} else if msg.Reasoning != "" {
		message["reasoning"] = msg.Reasoning
	}
	return TranscriptEntry{
		Type:           typ,
		SessionID:      sessionID,
		Cwd:            cwd,
		TurnID:         turnID,
		RalphIteration: iteration,
		Message:        message,
	}
}

func generateTranscriptUUID() string {
	return "tr_" + generateSessionID()
}

func sanitizeSessionIDForPath(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range sessionID {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	clean := b.String()
	if clean == "" || clean == "." || clean == ".." {
		return "default"
	}
	return clean
}
