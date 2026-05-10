package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── SessionStore ────────────────────────────────────────────────────────────

func TestSessionStore_AddAndRecent(t *testing.T) {
	db := filepath.Join(t.TempDir(), "sessions.db")
	ss, err := newSessionStore(db)
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	defer ss.Close()

	_ = ss.Add("sess1", "user", "hello world")
	_ = ss.Add("sess1", "assistant", "hi there")

	entries, err := ss.Recent("sess1", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	// Recent returns chronological order (oldest first).
	if entries[0].Role != "user" {
		t.Errorf("entries[0].Role = %q, want user", entries[0].Role)
	}
	if entries[1].Role != "assistant" {
		t.Errorf("entries[1].Role = %q, want assistant", entries[1].Role)
	}
}

func TestSessionStore_Search(t *testing.T) {
	db := filepath.Join(t.TempDir(), "sessions.db")
	ss, err := newSessionStore(db)
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	defer ss.Close()

	_ = ss.Add("s1", "user", "the quick brown fox")
	_ = ss.Add("s1", "user", "lazy dog")
	_ = ss.Add("s2", "user", "unrelated content")

	results, err := ss.Search("quick", "s1", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	if !strings.Contains(results[0].Content, "quick") {
		t.Errorf("unexpected content: %q", results[0].Content)
	}
}

func TestSessionStore_DeleteSession(t *testing.T) {
	db := filepath.Join(t.TempDir(), "sessions.db")
	ss, err := newSessionStore(db)
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	defer ss.Close()

	_ = ss.Add("sess", "user", "to be deleted")
	if err := ss.DeleteSession("sess"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	entries, _ := ss.Recent("sess", 10)
	if len(entries) != 0 {
		t.Errorf("expected empty session after delete, got %d", len(entries))
	}
}

func TestSessionStore_ListSessions(t *testing.T) {
	db := filepath.Join(t.TempDir(), "sessions.db")
	ss, err := newSessionStore(db)
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	defer ss.Close()

	_ = ss.Add("alpha", "user", "msg")
	_ = ss.Add("beta", "user", "msg")
	_ = ss.Add("alpha", "user", "msg2")

	ids, err := ss.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("len = %d, want 2", len(ids))
	}
}

// ─── KnowledgeStore ──────────────────────────────────────────────────────────

func TestKnowledgeStore_SetAndGet(t *testing.T) {
	db := filepath.Join(t.TempDir(), "knowledge.db")
	ks, err := newKnowledgeStore(db)
	if err != nil {
		t.Fatalf("newKnowledgeStore: %v", err)
	}
	defer ks.Close()

	if err := ks.Set("lang", "Go 1.22", "go,version"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	entry, err := ks.Get("lang")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry == nil {
		t.Fatal("Get returned nil")
	}
	if entry.Value != "Go 1.22" {
		t.Errorf("Value = %q, want %q", entry.Value, "Go 1.22")
	}
	if entry.Tags != "go,version" {
		t.Errorf("Tags = %q, want %q", entry.Tags, "go,version")
	}
}

func TestKnowledgeStore_GetMissing(t *testing.T) {
	db := filepath.Join(t.TempDir(), "knowledge.db")
	ks, err := newKnowledgeStore(db)
	if err != nil {
		t.Fatalf("newKnowledgeStore: %v", err)
	}
	defer ks.Close()

	entry, err := ks.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry != nil {
		t.Error("expected nil for missing key")
	}
}

func TestKnowledgeStore_Upsert(t *testing.T) {
	db := filepath.Join(t.TempDir(), "knowledge.db")
	ks, err := newKnowledgeStore(db)
	if err != nil {
		t.Fatalf("newKnowledgeStore: %v", err)
	}
	defer ks.Close()

	_ = ks.Set("key", "old", "")
	_ = ks.Set("key", "new", "updated")

	entry, _ := ks.Get("key")
	if entry.Value != "new" {
		t.Errorf("upsert: Value = %q, want %q", entry.Value, "new")
	}
}

func TestKnowledgeStore_Delete(t *testing.T) {
	db := filepath.Join(t.TempDir(), "knowledge.db")
	ks, err := newKnowledgeStore(db)
	if err != nil {
		t.Fatalf("newKnowledgeStore: %v", err)
	}
	defer ks.Close()

	_ = ks.Set("toDelete", "val", "")
	if err := ks.Delete("toDelete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	entry, _ := ks.Get("toDelete")
	if entry != nil {
		t.Error("expected nil after delete")
	}
}

func TestKnowledgeStore_List(t *testing.T) {
	db := filepath.Join(t.TempDir(), "knowledge.db")
	ks, err := newKnowledgeStore(db)
	if err != nil {
		t.Fatalf("newKnowledgeStore: %v", err)
	}
	defer ks.Close()

	_ = ks.Set("k1", "v1", "")
	_ = ks.Set("k2", "v2", "")

	entries, err := ks.List(10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("len = %d, want 2", len(entries))
	}
}

// ─── ProjectMemory ───────────────────────────────────────────────────────────

func TestProjectMemory_AppendAndRead(t *testing.T) {
	dir := t.TempDir()
	pm := NewProjectMemory(dir)

	if err := pm.Append("first entry"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := pm.Append("second entry"); err != nil {
		t.Fatalf("Append: %v", err)
	}

	content, err := pm.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(content, "first entry") {
		t.Error("expected 'first entry' in content")
	}
	if !strings.Contains(content, "second entry") {
		t.Error("expected 'second entry' in content")
	}
}

func TestProjectMemory_ReadMissing(t *testing.T) {
	pm := NewProjectMemory(t.TempDir())
	content, err := pm.Read()
	if err != nil {
		t.Fatalf("Read on missing file: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty string, got %q", content)
	}
}

func TestProjectMemory_WriteAndReadRules(t *testing.T) {
	dir := t.TempDir()
	pm := NewProjectMemory(dir)

	if err := pm.WriteRule("style", "# Style\nUse tabs."); err != nil {
		t.Fatalf("WriteRule: %v", err)
	}
	if err := pm.WriteRule("naming", "# Naming\nCamelCase."); err != nil {
		t.Fatalf("WriteRule: %v", err)
	}

	rules, err := pm.ReadRules()
	if err != nil {
		t.Fatalf("ReadRules: %v", err)
	}
	if !strings.Contains(rules, "style") {
		t.Error("expected 'style' in rules")
	}
	if !strings.Contains(rules, "naming") {
		t.Error("expected 'naming' in rules")
	}
}

func TestProjectMemory_Summary(t *testing.T) {
	dir := t.TempDir()
	pm := NewProjectMemory(dir)
	_ = pm.Append("project note")

	summary := pm.Summary()
	if !strings.Contains(summary, "project note") {
		t.Errorf("Summary should contain project note, got: %q", summary)
	}
}

// ─── Manager ────────────────────────────────────────────────────────────────

func TestManager_NewManager(t *testing.T) {
	dataDir := t.TempDir()
	m := NewManager(t.TempDir(), dataDir, true)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	defer m.Close()

	if !m.enabled {
		t.Error("expected manager to be enabled")
	}
}

func TestManager_Disabled(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir(), false)
	defer m.Close()

	err := m.SaveMemory(context.Background(), nil, "", "", nil)
	if err == nil {
		t.Error("expected error when memory disabled")
	}
}

func TestManager_ForSession(t *testing.T) {
	dataDir := t.TempDir()
	m := NewManager(t.TempDir(), dataDir, true)
	defer m.Close()

	v := m.ForSession("test-session")
	if v == nil {
		t.Fatal("ForSession returned nil")
	}
	if v.SessionID() != "test-session" {
		t.Errorf("SessionID = %q, want %q", v.SessionID(), "test-session")
	}
}

func TestManager_SetKnowledge(t *testing.T) {
	dataDir := t.TempDir()
	m := NewManager(t.TempDir(), dataDir, true)
	defer m.Close()

	if err := m.SetKnowledge("key1", "value1", "tag"); err != nil {
		t.Fatalf("SetKnowledge: %v", err)
	}
}

func TestManager_GetListDeleteKnowledge(t *testing.T) {
	dataDir := t.TempDir()
	m := NewManager(t.TempDir(), dataDir, true)
	defer m.Close()

	if err := m.SetKnowledge("key1", "value1", "tag"); err != nil {
		t.Fatalf("SetKnowledge: %v", err)
	}

	entry, err := m.GetKnowledge("key1")
	if err != nil {
		t.Fatalf("GetKnowledge: %v", err)
	}
	if entry.Value != "value1" {
		t.Fatalf("GetKnowledge value = %q, want value1", entry.Value)
	}

	entries, err := m.ListKnowledge(10)
	if err != nil {
		t.Fatalf("ListKnowledge: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ListKnowledge len = %d, want 1", len(entries))
	}

	if err := m.DeleteKnowledge("key1"); err != nil {
		t.Fatalf("DeleteKnowledge: %v", err)
	}
	if entry, err := m.GetKnowledge("key1"); err != nil || entry != nil {
		t.Fatalf("GetKnowledge after delete = (%v, %v), want nil entry", entry, err)
	}
}

func TestManager_GetMemoryTools(t *testing.T) {
	dataDir := t.TempDir()
	m := NewManager(t.TempDir(), dataDir, true)
	defer m.Close()

	tools := m.GetMemoryTools()
	if len(tools) != 1 {
		t.Fatalf("GetMemoryTools len = %d, want 1", len(tools))
	}
	if tools[0].Name() != "memory" {
		t.Errorf("tool name = %q, want %q", tools[0].Name(), "memory")
	}
}

func TestManager_GetMemoryStats(t *testing.T) {
	dataDir := t.TempDir()
	m := NewManager(t.TempDir(), dataDir, true)
	defer m.Close()

	stats := m.GetMemoryStats()
	if stats.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

// ─── SessionView ─────────────────────────────────────────────────────────────

func TestSessionView_AddAndRecent(t *testing.T) {
	dataDir := t.TempDir()
	m := NewManager(t.TempDir(), dataDir, true)
	defer m.Close()

	v := m.ForSession("s1")
	if err := v.Add("user", "hello"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := v.Add("assistant", "world"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entries, err := v.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
}

func TestSessionView_Clear(t *testing.T) {
	dataDir := t.TempDir()
	m := NewManager(t.TempDir(), dataDir, true)
	defer m.Close()

	v := m.ForSession("clearMe")
	_ = v.Add("user", "to be cleared")
	if err := v.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	entries, _ := v.Recent(10)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(entries))
	}
}

// ─── LocalMemoryTool ─────────────────────────────────────────────────────────

func TestLocalMemoryTool_Add(t *testing.T) {
	dataDir := t.TempDir()
	m := NewManager(t.TempDir(), dataDir, true)
	defer m.Close()

	tool := NewLocalMemoryTool(m)
	ctx := ContextWithSessionID(context.Background(), "tool-test")

	result, err := tool.Execute(ctx, map[string]interface{}{
		"action":  "add",
		"content": "user prefers brevity",
		"role":    "user",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}
}

func TestLocalMemoryTool_AddMissingContent(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir(), true)
	defer m.Close()

	tool := NewLocalMemoryTool(m)
	result, _ := tool.Execute(context.Background(), map[string]interface{}{"action": "add"})
	if result.Success {
		t.Error("expected failure when content missing")
	}
}

func TestLocalMemoryTool_Get(t *testing.T) {
	dataDir := t.TempDir()
	m := NewManager(t.TempDir(), dataDir, true)
	defer m.Close()

	tool := NewLocalMemoryTool(m)
	ctx := ContextWithSessionID(context.Background(), "get-test")

	_, _ = tool.Execute(ctx, map[string]interface{}{"action": "add", "content": "memory entry"})

	result, err := tool.Execute(ctx, map[string]interface{}{
		"action": "get",
		"limit":  float64(10),
	})
	if err != nil {
		t.Fatalf("Execute get: %v", err)
	}
	if !result.Success {
		t.Fatalf("get failed: %s", result.Error)
	}
	if !strings.Contains(result.LLMContent, "memory entry") {
		t.Errorf("expected content in output, got: %q", result.LLMContent)
	}
}

func TestLocalMemoryTool_UnknownAction(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir(), true)
	defer m.Close()

	tool := NewLocalMemoryTool(m)
	result, _ := tool.Execute(context.Background(), map[string]interface{}{"action": "delete"})
	if result.Success {
		t.Error("expected failure for unknown action")
	}
}

func TestLocalMemoryTool_Schema(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir(), true)
	defer m.Close()

	tool := NewLocalMemoryTool(m)
	schema := tool.Schema()
	if schema == nil {
		t.Fatal("Schema returned nil")
	}
	if _, ok := schema.Properties["action"]; !ok {
		t.Error("Schema missing 'action' property")
	}
}

// ─── ContextWithSessionID ────────────────────────────────────────────────────

func TestContextWithSessionID(t *testing.T) {
	ctx := ContextWithSessionID(context.Background(), "my-session")
	id := sessionIDFromContext(ctx)
	if id != "my-session" {
		t.Errorf("sessionIDFromContext = %q, want %q", id, "my-session")
	}
}

func TestSessionIDFromContext_Default(t *testing.T) {
	id := sessionIDFromContext(context.Background())
	if id != "default" {
		t.Errorf("default id = %q, want %q", id, "default")
	}
}

// ─── NewManagerFromAPIKey ─────────────────────────────────────────────────────

func TestNewManagerFromAPIKey_BackwardCompat(t *testing.T) {
	// Should not panic; ignores all API key params.
	m := NewManagerFromAPIKey("url", "key", "user", "agent", "extra", true)
	if m == nil {
		t.Fatal("NewManagerFromAPIKey returned nil")
	}
	m.Close()
}

// ─── ProjectMemory with no rules dir ─────────────────────────────────────────

func TestProjectMemory_ReadRulesMissing(t *testing.T) {
	pm := NewProjectMemory(t.TempDir())
	rules, err := pm.ReadRules()
	if err != nil {
		t.Fatalf("ReadRules on missing dir: %v", err)
	}
	if rules != "" {
		t.Errorf("expected empty string, got %q", rules)
	}
}

func TestProjectMemory_SummaryEmpty(t *testing.T) {
	pm := NewProjectMemory(t.TempDir())
	if pm.Summary() != "" {
		t.Error("expected empty summary when no memory files exist")
	}
}

// cleanup helper
func init() {
	// Ensure os package is used (for TestTokenStore_FilePermissions analogue if needed)
	_ = os.DevNull
}
