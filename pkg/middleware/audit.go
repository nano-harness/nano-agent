package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"gopkg.in/natefinch/lumberjack.v2"
)

// AuditEntry is a single JSON Lines audit log entry.
type AuditEntry struct {
	SchemaVersion    string                 `json:"schema_version"`
	Timestamp        string                 `json:"ts"`
	Tool             string                 `json:"tool"`
	Params           map[string]interface{} `json:"params,omitempty"`
	Success          bool                   `json:"success"`
	Error            string                 `json:"error,omitempty"`
	DurationMs       int64                  `json:"duration_ms"`
	SessionID        string                 `json:"session_id,omitempty"`
	SecurityDecision *AuditDecision         `json:"security_decision,omitempty"`
}

// AuditDecision is the stable JSON representation of a security decision.
type AuditDecision struct {
	Action         string                 `json:"action"`
	Reason         string                 `json:"reason,omitempty"`
	Rule           string                 `json:"rule,omitempty"`
	Layer          int                    `json:"layer,omitempty"`
	Confidence     float64                `json:"confidence,omitempty"`
	Suggestions    []string               `json:"suggestions,omitempty"`
	AutoWhitelist  bool                   `json:"auto_whitelist,omitempty"`
	ModifiedParams map[string]interface{} `json:"modified_params,omitempty"`
}

const AuditSchemaVersion = "1"

// AuditSchema describes the stable JSONL audit entry schema.
func AuditSchema() map[string]interface{} {
	return map[string]interface{}{
		"schema_version": AuditSchemaVersion,
		"type":           "object",
		"required":       []string{"schema_version", "ts", "tool", "success", "duration_ms"},
		"properties": map[string]interface{}{
			"schema_version": "string",
			"ts":             "string",
			"tool":           "string",
			"params":         "object",
			"success":        "boolean",
			"error":          "string",
			"duration_ms":    "integer",
			"session_id":     "string",
			"security_decision": map[string]interface{}{
				"type":     "object",
				"required": []string{"action"},
				"properties": map[string]string{
					"action":          "string",
					"reason":          "string",
					"rule":            "string",
					"layer":           "integer",
					"confidence":      "number",
					"suggestions":     "array",
					"auto_whitelist":  "boolean",
					"modified_params": "object",
				},
			},
		},
	}
}

// AuditOptions configures local JSONL audit log rotation.
type AuditOptions struct {
	Path       string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

// DefaultAuditOptions returns the default local audit log and rotation settings.
func DefaultAuditOptions() AuditOptions {
	return AuditOptions{
		MaxSizeMB:  100,
		MaxBackups: 3,
		MaxAgeDays: 28,
		Compress:   true,
	}
}

// AuditMiddleware writes rotated JSON Lines audit entries to ~/.nano/audit.jsonl.
type AuditMiddleware struct {
	path string
	mu   sync.Mutex
	w    io.WriteCloser
}

var sensitiveAuditKeyMarkers = []string{
	"password", "passwd", "secret", "api_key", "apikey", "token",
	"authorization", "cookie", "private_key", "client_secret",
}

// NewAuditMiddleware creates an AuditMiddleware that writes to the given file path.
// If path is empty, it defaults to ~/.nano/audit.jsonl.
func NewAuditMiddleware(path string) (*AuditMiddleware, error) {
	options := DefaultAuditOptions()
	options.Path = path
	return NewAuditMiddlewareWithOptions(options)
}

// NewAuditMiddlewareFromConfig creates an AuditMiddleware from middleware config.
func NewAuditMiddlewareFromConfig(cfg *config.MiddlewareConfig) (*AuditMiddleware, error) {
	options := DefaultAuditOptions()
	if cfg != nil {
		options.Path = cfg.AuditLogPath
		if cfg.AuditMaxSizeMB > 0 {
			options.MaxSizeMB = cfg.AuditMaxSizeMB
		}
		if cfg.AuditMaxBackups >= 0 {
			options.MaxBackups = cfg.AuditMaxBackups
		}
		if cfg.AuditMaxAgeDays >= 0 {
			options.MaxAgeDays = cfg.AuditMaxAgeDays
		}
		options.Compress = cfg.AuditCompress
	}
	return NewAuditMiddlewareWithOptions(options)
}

// NewAuditMiddlewareWithOptions creates an AuditMiddleware with explicit rotation settings.
func NewAuditMiddlewareWithOptions(options AuditOptions) (*AuditMiddleware, error) {
	path := options.Path
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
	if options.MaxSizeMB <= 0 {
		options.MaxSizeMB = 100
	}
	if options.MaxBackups < 0 {
		options.MaxBackups = 0
	}
	if options.MaxAgeDays < 0 {
		options.MaxAgeDays = 0
	}
	writer := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    options.MaxSizeMB,
		MaxBackups: options.MaxBackups,
		MaxAge:     options.MaxAgeDays,
		Compress:   options.Compress,
	}
	return &AuditMiddleware{path: path, w: writer}, nil
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
		SchemaVersion: AuditSchemaVersion,
		Timestamp:     start.UTC().Format(time.RFC3339),
		Tool:          tool.Name(),
		DurationMs:    elapsed.Milliseconds(),
	}

	// Sanitize params (omit large content fields and redact sensitive values).
	sanitized := make(map[string]interface{})
	for k, v := range params {
		if k == "content" || k == "file_content" {
			if s, ok := v.(string); ok {
				sanitized[k] = fmt.Sprintf("<omitted %d bytes>", len(s))
				continue
			}
		}
		if isSensitiveAuditKey(k) {
			sanitized[k] = "[REDACTED]"
			continue
		}
		sanitized[k] = sanitizeAuditValue(v)
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
	if decision, ok := GetSecurityDecision(ctx); ok {
		entry.SecurityDecision = auditDecisionFromSecurityDecision(decision)
	}

	m.writeEntry(entry)
	return result, err
}

func auditDecisionFromSecurityDecision(decision *Decision) *AuditDecision {
	if decision == nil {
		return nil
	}
	var modifiedParams map[string]interface{}
	if len(decision.ModifiedParams) > 0 {
		if sanitized, ok := sanitizeAuditValue(copyDecisionParams(decision.ModifiedParams)).(map[string]interface{}); ok {
			modifiedParams = sanitized
		}
	}
	return &AuditDecision{
		Action:         decision.Action.String(),
		Reason:         decision.Reason,
		Rule:           decision.Rule,
		Layer:          decision.Layer,
		Confidence:     decision.Confidence,
		Suggestions:    append([]string(nil), decision.Suggestions...), // Defensive copy against later mutations.
		AutoWhitelist:  decision.AutoWhitelist,
		ModifiedParams: modifiedParams,
	}
}

func sanitizeAuditValue(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, v := range x {
			if isSensitiveAuditKey(k) {
				out[k] = "[REDACTED]"
			} else {
				out[k] = sanitizeAuditValue(v)
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, item := range x {
			out[i] = sanitizeAuditValue(item)
		}
		return out
	default:
		return v
	}
}

func isSensitiveAuditKey(k string) bool {
	lower := strings.ToLower(k)
	for _, marker := range sensitiveAuditKeyMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (m *AuditMiddleware) writeEntry(entry AuditEntry) {
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')
	m.mu.Lock()
	_, _ = m.w.Write(line)
	m.mu.Unlock()
}

// Close flushes and closes the audit log file.
func (m *AuditMiddleware) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.w == nil {
		return nil
	}
	return m.w.Close()
}

type sessionIDKey struct{}

// WithSessionID stores the session ID in the context for audit logging.
func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, id)
}
