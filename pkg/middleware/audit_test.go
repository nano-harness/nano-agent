package middleware

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

type auditTestTool struct{}

func (auditTestTool) Name() string                   { return "audit_test" }
func (auditTestTool) Description() string            { return "test tool" }
func (auditTestTool) Schema() *interfaces.ToolSchema { return nil }
func (auditTestTool) RequiresConfirmation() bool     { return false }
func (auditTestTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryDiagnostics
}
func (auditTestTool) ConcurrencySafe() bool { return true }
func (auditTestTool) Execute(context.Context, map[string]interface{}) (*interfaces.ToolResult, error) {
	return &interfaces.ToolResult{Success: true}, nil
}

func TestAuditMiddlewareRedactsSensitiveParams(t *testing.T) {
	path := t.TempDir() + "/audit.jsonl"
	m := MustNewAuditMiddleware(path)
	defer func() { _ = m.Close() }()

	_, err := m.Execute(context.Background(), auditTestTool{}, map[string]interface{}{
		"api_key": "secret",
		"nested": map[string]interface{}{
			"access_token": "token",
			"safe":         "value",
		},
	}, func(ctx context.Context, tool interfaces.Tool, params map[string]interface{}) (*interfaces.ToolResult, error) {
		return tool.Execute(ctx, params)
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	_ = m.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	var entry AuditEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("unmarshal audit entry: %v", err)
	}
	if entry.Params["api_key"] != "[REDACTED]" {
		t.Fatalf("api_key not redacted: %#v", entry.Params["api_key"])
	}
	if entry.SchemaVersion != AuditSchemaVersion {
		t.Fatalf("schema version = %q, want %q", entry.SchemaVersion, AuditSchemaVersion)
	}
	nested, ok := entry.Params["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("nested params missing: %#v", entry.Params["nested"])
	}
	if nested["access_token"] != "[REDACTED]" || nested["safe"] != "value" {
		t.Fatalf("nested redaction failed: %#v", nested)
	}
}

func TestAuditSchemaIncludesDecisionFields(t *testing.T) {
	schema := AuditSchema()
	if schema["schema_version"] != AuditSchemaVersion {
		t.Fatalf("schema version = %#v", schema["schema_version"])
	}
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing properties: %#v", schema["properties"])
	}
	if _, ok := properties["security_decision"]; !ok {
		t.Fatalf("schema missing security_decision: %#v", properties)
	}
}

func TestAuditMiddlewareRecordsSecurityDecision(t *testing.T) {
	path := t.TempDir() + "/audit.jsonl"
	m := MustNewAuditMiddleware(path)
	defer func() { _ = m.Close() }()

	ctx := WithSecurityDecision(context.Background(), &Decision{
		Action:        ActionBlock,
		Reason:        "blocked by hook",
		Rule:          "deny-rm",
		Layer:         LayerHook,
		Confidence:    0.9,
		Suggestions:   []string{"use trash"},
		AutoWhitelist: true,
	})

	_, err := m.Execute(ctx, auditTestTool{}, map[string]interface{}{"command": "rm -rf tmp"}, func(ctx context.Context, tool interfaces.Tool, params map[string]interface{}) (*interfaces.ToolResult, error) {
		return tool.Execute(ctx, params)
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	_ = m.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	var entry AuditEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("unmarshal audit entry: %v", err)
	}
	if entry.SecurityDecision == nil {
		t.Fatal("expected security decision in audit entry")
	}
	if entry.SecurityDecision.Action != "block" || entry.SecurityDecision.Layer != LayerHook || entry.SecurityDecision.Rule != "deny-rm" {
		t.Fatalf("unexpected security decision: %#v", entry.SecurityDecision)
	}
	if len(entry.SecurityDecision.Suggestions) != 1 || entry.SecurityDecision.Suggestions[0] != "use trash" {
		t.Fatalf("unexpected suggestions: %#v", entry.SecurityDecision.Suggestions)
	}
}

func TestAuditMiddlewareRotatesLocalJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	m, err := NewAuditMiddlewareWithOptions(AuditOptions{
		Path:       path,
		MaxSizeMB:  1,
		MaxBackups: 2,
		MaxAgeDays: 1,
		Compress:   false,
	})
	if err != nil {
		t.Fatalf("NewAuditMiddlewareWithOptions: %v", err)
	}
	defer func() { _ = m.Close() }()

	blob := strings.Repeat("x", 64*1024)
	for i := 0; i < 40; i++ {
		_, err := m.Execute(context.Background(), auditTestTool{}, map[string]interface{}{
			"iteration": i,
			"blob":      blob,
		}, func(ctx context.Context, tool interfaces.Tool, params map[string]interface{}) (*interfaces.ToolResult, error) {
			return tool.Execute(ctx, params)
		})
		if err != nil {
			t.Fatalf("Execute %d returned error: %v", i, err)
		}
	}
	if err := m.Close(); err != nil {
		t.Fatalf("close audit middleware: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "audit*.jsonl*"))
	if err != nil {
		t.Fatalf("glob audit logs: %v", err)
	}
	if len(matches) < 2 {
		t.Fatalf("expected rotated audit logs, got %v", matches)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("current audit log missing after rotation: %v", err)
	}
}

func TestNewAuditMiddlewareFromConfigUsesRotationSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	m, err := NewAuditMiddlewareFromConfig(&config.MiddlewareConfig{
		AuditLogPath:    path,
		AuditMaxSizeMB:  1,
		AuditMaxBackups: 1,
		AuditMaxAgeDays: 7,
		AuditCompress:   false,
	})
	if err != nil {
		t.Fatalf("NewAuditMiddlewareFromConfig: %v", err)
	}
	defer func() { _ = m.Close() }()

	_, err = m.Execute(context.Background(), auditTestTool{}, map[string]interface{}{"safe": "value"}, func(ctx context.Context, tool interfaces.Tool, params map[string]interface{}) (*interfaces.ToolResult, error) {
		return tool.Execute(ctx, params)
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("close audit middleware: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("configured audit path missing: %v", err)
	}
}
