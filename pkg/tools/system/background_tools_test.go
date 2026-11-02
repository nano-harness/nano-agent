package system

import (
	"context"
	"testing"
	"time"
)

func TestBashOutputTool_Execute(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewBashOutputTool(bgManager)

	// Spawn a background task first
	task, _ := bgManager.Spawn(context.Background(), "test-session", "echo line1 && echo line2 && echo line3", "/tmp")

	// Wait for output
	waitForCompletion(task.ID, bgManager, 500)

	// Test basic output retrieval
	params := map[string]interface{}{
		"task_id":    task.ID,
		"session_id": "test-session",
	}

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Verify metadata
	if taskID, ok := result.Metadata["task_id"].(string); !ok || taskID != task.ID {
		t.Errorf("Expected task_id %s in metadata", task.ID)
	}

	if status, ok := result.Metadata["status"].(string); !ok || status != string(BgStatusCompleted) {
		t.Errorf("Expected status %s, got %v", BgStatusCompleted, status)
	}
}

func TestBashOutputTool_SessionIsolation(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewBashOutputTool(bgManager)

	taskA, _ := bgManager.Spawn(context.Background(), "session-a", "echo hello", "/tmp")
	waitForCompletion(taskA.ID, bgManager, 500)

	// Wrong session_id should be rejected.
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"task_id":    taskA.ID,
		"session_id": "session-b",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Success {
		t.Fatalf("Expected failure for wrong session, got success")
	}
}

func TestBashOutputTool_IncrementalRead(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewBashOutputTool(bgManager)

	task, _ := bgManager.Spawn(context.Background(), "test-session", "echo line1 && sleep 0.3 && echo line2 && sleep 0.3 && echo line3", "/tmp")

	// First read - should get line1
	waitForOutput(task.ID, bgManager, 200)
	result1, _ := tool.Execute(context.Background(), map[string]interface{}{
		"task_id":    task.ID,
		"session_id": "test-session",
	})

	var offset1 int64
	switch v := result1.Metadata["new_offset"].(type) {
	case float64:
		offset1 = int64(v)
	case int64:
		offset1 = v
	default:
		t.Fatalf("Unexpected type for new_offset: %T", v)
	}

	// Second read from offset - should get line2 (and maybe line3)
	waitForOutput(task.ID, bgManager, 400)
	result2, _ := tool.Execute(context.Background(), map[string]interface{}{
		"task_id":     task.ID,
		"session_id":  "test-session",
		"from_offset": float64(offset1),
	})

	var offset2 int64
	switch v := result2.Metadata["new_offset"].(type) {
	case float64:
		offset2 = int64(v)
	case int64:
		offset2 = v
	default:
		t.Fatalf("Unexpected type for new_offset: %T", v)
	}

	if offset2 <= offset1 {
		t.Errorf("Expected offset to increase, got %v -> %v (output1: %q, output2: %q)", offset1, offset2, result1.UserContent, result2.UserContent)
	}
}

func TestBashOutputTool_MaxLines(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewBashOutputTool(bgManager)

	// Generate many lines
	task, _ := bgManager.Spawn(context.Background(), "test-session", "i=1; while [ $i -le 100 ]; do echo line$i; i=$((i+1)); done", "/tmp")
	waitForCompletion(task.ID, bgManager, 1000)

	// Read with max_lines
	result, _ := tool.Execute(context.Background(), map[string]interface{}{
		"task_id":    task.ID,
		"session_id": "test-session",
		"max_lines":  float64(10),
	})

	lines := result.Metadata["lines"].(int)
	if lines != 10 {
		t.Errorf("Expected 10 lines, got %d", lines)
	}
}

func TestBashOutputTool_MissingTaskID(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewBashOutputTool(bgManager)

	result, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Success {
		t.Error("Expected failure when task_id is missing")
	}
}

func TestBashOutputTool_InvalidTaskID(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewBashOutputTool(bgManager)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"task_id": "nonexistent",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Success {
		t.Error("Expected failure with invalid task_id")
	}
}

func TestKillBashTool_Execute(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewKillBashTool(bgManager)

	// Spawn long-running task
	task, _ := bgManager.Spawn(context.Background(), "test-session", "sleep 60", "/tmp")

	// Verify running
	waitForOutput(task.ID, bgManager, 200)
	_, _, status, _ := bgManager.ReadOutput(task.ID, 0, 0, 0)
	if status != BgStatusRunning {
		t.Errorf("Expected status %s, got %s", BgStatusRunning, status)
	}

	// Kill the task
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"task_id":    task.ID,
		"session_id": "test-session",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Verify killed
	waitForCompletion(task.ID, bgManager, 6000)
	_, _, status, _ = bgManager.ReadOutput(task.ID, 0, 0, 0)
	if status != BgStatusKilled {
		t.Errorf("Expected status %s after kill, got %s", BgStatusKilled, status)
	}
}

func TestKillBashTool_SessionIsolation(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewKillBashTool(bgManager)

	taskA, _ := bgManager.Spawn(context.Background(), "session-a", "sleep 5", "/tmp")

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"task_id":    taskA.ID,
		"session_id": "session-b",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Success {
		t.Fatalf("Expected failure for wrong session, got success")
	}

	// Cleanup (correct session).
	_, _ = tool.Execute(context.Background(), map[string]interface{}{
		"task_id":    taskA.ID,
		"session_id": "session-a",
	})
}

func TestKillBashTool_MissingTaskID(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewKillBashTool(bgManager)

	result, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Success {
		t.Error("Expected failure when task_id is missing")
	}
}

func TestKillBashTool_InvalidTaskID(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewKillBashTool(bgManager)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"task_id": "nonexistent",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Success {
		t.Error("Expected failure with invalid task_id")
	}
}

func TestListBackgroundTool_Execute(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewListBackgroundTool(bgManager)

	// Empty list
	result1, _ := tool.Execute(context.Background(), map[string]interface{}{})
	if !result1.Success {
		t.Fatalf("Expected success, got error: %s", result1.Error)
	}

	if count, ok := result1.Metadata["task_count"].(int); !ok || count != 0 {
		t.Errorf("Expected task_count 0, got %v", result1.Metadata["task_count"])
	}

	// Spawn some tasks
	task1, _ := bgManager.Spawn(context.Background(), "default", "echo test1", "/tmp")
	task2, _ := bgManager.Spawn(context.Background(), "default", "sleep 1", "/tmp")

	// List with tasks
	result2, _ := tool.Execute(context.Background(), map[string]interface{}{})
	if !result2.Success {
		t.Fatalf("Expected success, got error: %s", result2.Error)
	}

	if count, ok := result2.Metadata["task_count"].(int); !ok || count != 2 {
		t.Errorf("Expected task_count 2, got %v", result2.Metadata["task_count"])
	}

	// Output should contain task IDs
	output := result2.UserContent
	if !containsString(output, task1.ID) || !containsString(output, task2.ID) {
		t.Errorf("Expected output to contain task IDs, got: %s", output)
	}
}

// Helper functions for tests
func waitForCompletion(taskID string, manager *BackgroundTaskManager, maxWaitMs int) {
	for i := 0; i < maxWaitMs/50; i++ {
		_, _, status, _ := manager.ReadOutput(taskID, 0, 0, 0)
		if status != BgStatusRunning {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitForOutput(taskID string, manager *BackgroundTaskManager, waitMs int) {
	time.Sleep(time.Duration(waitMs) * time.Millisecond)
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
