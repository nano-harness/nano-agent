package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// AuditEntry is a single JSON Lines audit log entry.
type AuditEntry struct {
	Timestamp  string                 `json:"ts"`
	Tool       string                 `json:"tool"`
	Params     map[string]interface{} `json:"params,omitempty"`
	Success    bool                   `json:"success"`
	Error      string                 `json:"error,omitempty"`
	DurationMs int64                  `json:"duration_ms"`
	SessionID  string                 `json:"session_id,omitempty"`
}

// AuditMiddleware writes JSON Lines audit entries to ~/.nano/audit.jsonl.
type AuditMiddleware struct {
	path string
	mu   sync.Mutex
	f    *os.File
}

// NewAuditMiddleware creates an AuditMiddleware that writes to the given file path.
// If path is empty, it defaults to ~/.nano/audit.jsonl.
func NewAuditMiddleware(path string) (*AuditMiddleware, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("audit: cannot determine home dir: %w", err)
		}
		path = filepath.Join(home, ".nano", "audit.jsonl")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("audit: cannot create log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("audit: cannot open log file: %w", err)
	}
	return &AuditMiddleware{path: path, f: f}, nil
}

// MustNewAuditMiddleware is like NewAuditMiddleware but panics on error.
// Suitable for use in test helpers.
func MustNewAuditMiddleware(path string) *AuditMiddleware {
	m, err := NewAuditMiddleware(path)
	if err != nil {
		panic(err)
	}
	return m
}

func (m *AuditMiddleware) Name() string { return "audit" }

func (m *AuditMiddleware) Execute(
	ctx context.Context,
	tool interfaces.Tool,
	params map[string]interface{},
	next MiddlewareFunc,
) (*interfaces.ToolResult, error) {
	start := time.Now()
	result, err := next(ctx, tool, params)
	elapsed := time.Since(start)

	entry := AuditEntry{
		Timestamp:  start.UTC().Format(time.RFC3339),
		Tool:       tool.Name(),
		DurationMs: elapsed.Milliseconds(),
	}

	// Sanitize params (omit large content fields).
	sanitized := make(map[string]interface{})
	for k, v := range params {
		if k == "content" || k == "file_content" {
			if s, ok := v.(string); ok {
				sanitized[k] = fmt.Sprintf("<omitted %d bytes>", len(s))
				continue
			}
		}
		sanitized[k] = v
	}
	entry.Params = sanitized

	if err != nil {
		entry.Error = err.Error()
	} else if result != nil {
		entry.Success = result.Success
		if !result.Success {
			entry.Error = result.Error
		}
	}

	// Extract session ID from context if present.
	if sid, ok := ctx.Value(sessionIDKey{}).(string); ok {
		entry.SessionID = sid
	}

	m.writeEntry(entry)
	return result, err
}

func (m *AuditMiddleware) writeEntry(entry AuditEntry) {
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')
	m.mu.Lock()
	_, _ = m.f.Write(line)
	m.mu.Unlock()
}

// Close flushes and closes the audit log file.
func (m *AuditMiddleware) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.f.Close()
}

type sessionIDKey struct{}

// WithSessionID stores the session ID in the context for audit logging.
func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, id)
}
