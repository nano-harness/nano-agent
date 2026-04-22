package cron

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskLogEntry_ExtendedFields(t *testing.T) {
	entry := TaskLogEntry{
		TaskID:     "test-task-id",
		Command:    "test command",
		StartedAt:  time.Now(),
		FinishedAt: time.Now().Add(5 * time.Second),
		Success:    false,
		Error:      "test error",

		// M2 fields
		SessionID:     "test-session-123",
		Source:        "cli",
		EventsPath:    "/tmp/events.jsonl",
		DurationMs:    5000,
		ToolCallCount: 10,
		TokenUsage:    2000,
		SchemaVersion: 2,

		// M3 fields
		FailureStage:   "llm_call",
		FailedToolName: "bash",
	}

	// Verify JSON marshaling includes all fields
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Check M2 fields are present
	if decoded["session_id"] != "test-session-123" {
		t.Errorf("session_id = %v, want %q", decoded["session_id"], "test-session-123")
	}
	if decoded["source"] != "cli" {
		t.Errorf("source = %v, want %q", decoded["source"], "cli")
	}
	if decoded["events_path"] != "/tmp/events.jsonl" {
		t.Errorf("events_path = %v, want %q", decoded["events_path"], "/tmp/events.jsonl")
	}
	if decoded["tool_call_count"] != float64(10) {
		t.Errorf("tool_call_count = %v, want 10", decoded["tool_call_count"])
	}
	if decoded["token_usage"] != float64(2000) {
		t.Errorf("token_usage = %v, want 2000", decoded["token_usage"])
	}
	if decoded["schema_version"] != float64(2) {
		t.Errorf("schema_version = %v, want 2", decoded["schema_version"])
	}

	// Check M3 fields are present
	if decoded["failure_stage"] != "llm_call" {
		t.Errorf("failure_stage = %v, want %q", decoded["failure_stage"], "llm_call")
	}
	if decoded["failed_tool_name"] != "bash" {
		t.Errorf("failed_tool_name = %v, want %q", decoded["failed_tool_name"], "bash")
	}
}

func TestTaskLogEntry_BackwardCompatibility(t *testing.T) {
	// Entry with only basic fields (pre-M2)
	entry := TaskLogEntry{
		TaskID:     "old-task",
		Command:    "old command",
		StartedAt:  time.Now(),
		FinishedAt: time.Now().Add(1 * time.Second),
		Success:    true,
	}

	// Should marshal without extended fields
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Extended fields should not be present (omitempty)
	if _, ok := decoded["session_id"]; ok {
		t.Error("session_id should be omitted for empty value")
	}
	if _, ok := decoded["events_path"]; ok {
		t.Error("events_path should be omitted for empty value")
	}
	if _, ok := decoded["failure_stage"]; ok {
		t.Error("failure_stage should be omitted for empty value")
	}
}

func TestTaskLog_AppendAndQuery(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.jsonl")

	tl := NewTaskLog(logPath)

	// Append entries with extended fields
	now := time.Now()
	entries := []TaskLogEntry{
		{
			TaskID:        "task-1",
			Command:       "cmd1",
			StartedAt:     now,
			FinishedAt:    now.Add(1 * time.Second),
			Success:       true,
			SessionID:     "session-1",
			Source:        "cli",
			ToolCallCount: 5,
			TokenUsage:    500,
			SchemaVersion: 2,
		},
		{
			TaskID:         "task-2",
			Command:        "cmd2",
			StartedAt:      now.Add(2 * time.Second),
			FinishedAt:     now.Add(3 * time.Second),
			Success:        false,
			Error:          "test error",
			SessionID:      "session-2",
			Source:         "agent",
			ToolCallCount:  3,
			TokenUsage:     300,
			FailureStage:   "tool_exec",
			FailedToolName: "bash",
			SchemaVersion:  2,
		},
	}

	for _, e := range entries {
		if err := tl.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// Query and verify
	retrieved, err := tl.Query()
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(retrieved) != 2 {
		t.Fatalf("Query returned %d entries, want 2", len(retrieved))
	}

	// Verify first entry
	if retrieved[0].TaskID != "task-1" {
		t.Errorf("retrieved[0].TaskID = %q, want %q", retrieved[0].TaskID, "task-1")
	}
	if retrieved[0].SessionID != "session-1" {
		t.Errorf("retrieved[0].SessionID = %q, want %q", retrieved[0].SessionID, "session-1")
	}
	if retrieved[0].ToolCallCount != 5 {
		t.Errorf("retrieved[0].ToolCallCount = %d, want 5", retrieved[0].ToolCallCount)
	}

	// Verify second entry
	if retrieved[1].TaskID != "task-2" {
		t.Errorf("retrieved[1].TaskID = %q, want %q", retrieved[1].TaskID, "task-2")
	}
	if retrieved[1].FailureStage != "tool_exec" {
		t.Errorf("retrieved[1].FailureStage = %q, want %q", retrieved[1].FailureStage, "tool_exec")
	}
	if retrieved[1].FailedToolName != "bash" {
		t.Errorf("retrieved[1].FailedToolName = %q, want %q", retrieved[1].FailedToolName, "bash")
	}
}

func TestTaskLog_Cleanup(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "cleanup.jsonl")

	tl := NewTaskLog(logPath)

	// Add old and new entries
	now := time.Now()
	oldEntry := TaskLogEntry{
		TaskID:    "old-task",
		Command:   "old",
		StartedAt: now.Add(-40 * 24 * time.Hour), // 40 days ago
		Success:   true,
	}
	newEntry := TaskLogEntry{
		TaskID:    "new-task",
		Command:   "new",
		StartedAt: now.Add(-5 * 24 * time.Hour), // 5 days ago
		Success:   true,
	}

	if err := tl.Append(oldEntry); err != nil {
		t.Fatalf("Append old: %v", err)
	}
	if err := tl.Append(newEntry); err != nil {
		t.Fatalf("Append new: %v", err)
	}

	// Cleanup entries older than 30 days
	if err := tl.Cleanup(30 * 24 * time.Hour); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	// Verify only new entry remains
	entries, err := tl.Query()
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("After cleanup, got %d entries, want 1", len(entries))
	}
	if entries[0].TaskID != "new-task" {
		t.Errorf("After cleanup, TaskID = %q, want %q", entries[0].TaskID, "new-task")
	}
}
