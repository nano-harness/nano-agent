package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackgroundTaskManager_Spawn(t *testing.T) {
	// Create temporary directory for test
	tempDir := t.TempDir()
	manager := NewBackgroundTaskManager(tempDir)

	sessionID := "test-session"
	command := "echo hello && sleep 0.1 && echo world"

	// Spawn background task
	task, err := manager.Spawn(context.Background(), sessionID, command, "/tmp")
	if err != nil {
		t.Fatalf("Failed to spawn task: %v", err)
	}

	if task.ID == "" {
		t.Error("Task ID should not be empty")
	}
	if status := task.GetStatus(); status != BgStatusRunning {
		t.Errorf("Expected status %s, got %s", BgStatusRunning, status)
	}
	if task.SessionID != sessionID {
		t.Errorf("Expected sessionID %s, got %s", sessionID, task.SessionID)
	}
	if task.Command != command {
		t.Errorf("Expected command %s, got %s", command, task.Command)
	}

	// Wait for task to complete
	time.Sleep(300 * time.Millisecond)

	// Read output
	output, _, status, err := manager.ReadOutput(task.ID, 0, 0, 0)
	if err != nil {
		t.Fatalf("Failed to read output: %v", err)
	}

	if status != BgStatusCompleted {
		t.Errorf("Expected status %s, got %s", BgStatusCompleted, status)
	}

	if !strings.Contains(output, "hello") || !strings.Contains(output, "world") {
		t.Errorf("Expected output to contain 'hello' and 'world', got: %s", output)
	}
}

func TestBackgroundTaskManager_ReadOutput_Incremental(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewBackgroundTaskManager(tempDir)

	sessionID := "test-session"
	command := "echo line1 && sleep 0.1 && echo line2 && sleep 0.1 && echo line3"

	task, err := manager.Spawn(context.Background(), sessionID, command, "/tmp")
	if err != nil {
		t.Fatalf("Failed to spawn task: %v", err)
	}

	// Read first chunk
	time.Sleep(150 * time.Millisecond)
	output1, offset1, _, err := manager.ReadOutput(task.ID, 0, 0, 0)
	if err != nil {
		t.Fatalf("Failed to read output: %v", err)
	}

	if !strings.Contains(output1, "line1") {
		t.Errorf("Expected first chunk to contain 'line1', got: %s", output1)
	}

	// Read second chunk from offset
	time.Sleep(150 * time.Millisecond)
	output2, offset2, _, err := manager.ReadOutput(task.ID, offset1, 0, 0)
	if err != nil {
		t.Fatalf("Failed to read output from offset: %v", err)
	}

	if offset2 <= offset1 {
		t.Errorf("Expected new offset %d to be greater than previous %d", offset2, offset1)
	}

	if strings.Contains(output2, "line1") {
		t.Errorf("Second chunk should not contain 'line1', got: %s", output2)
	}
}

func TestBackgroundTaskManager_Kill(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewBackgroundTaskManager(tempDir)

	sessionID := "test-session"
	command := "sleep 60"

	task, err := manager.Spawn(context.Background(), sessionID, command, "/tmp")
	if err != nil {
		t.Fatalf("Failed to spawn task: %v", err)
	}

	// Verify task is running
	time.Sleep(100 * time.Millisecond)
	_, _, status, _ := manager.ReadOutput(task.ID, 0, 0, 0)
	if status != BgStatusRunning {
		t.Errorf("Expected status %s before kill, got %s", BgStatusRunning, status)
	}

	// Kill the task
	err = manager.Kill(task.ID)
	if err != nil {
		t.Fatalf("Failed to kill task: %v", err)
	}

	// Wait for kill to complete
	time.Sleep(200 * time.Millisecond)

	// Verify task is killed
	_, _, status, _ = manager.ReadOutput(task.ID, 0, 0, 0)
	if status != BgStatusKilled {
		t.Errorf("Expected status %s after kill, got %s", BgStatusKilled, status)
	}
}

func TestBackgroundTaskManager_List(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewBackgroundTaskManager(tempDir)

	session1 := "session1"
	session2 := "session2"

	// Spawn tasks in different sessions
	task1, _ := manager.Spawn(context.Background(), session1, "echo test1", "/tmp")
	task2, _ := manager.Spawn(context.Background(), session1, "echo test2", "/tmp")
	task3, _ := manager.Spawn(context.Background(), session2, "echo test3", "/tmp")

	// List tasks for session1
	tasks1 := manager.List(session1)
	if len(tasks1) != 2 {
		t.Errorf("Expected 2 tasks for session1, got %d", len(tasks1))
	}

	// Verify task IDs
	foundIDs := map[string]bool{}
	for _, task := range tasks1 {
		foundIDs[task.ID] = true
	}

	if !foundIDs[task1.ID] || !foundIDs[task2.ID] {
		t.Errorf("Expected to find task1 and task2 in session1 list")
	}

	// List tasks for session2
	tasks2 := manager.List(session2)
	if len(tasks2) != 1 {
		t.Errorf("Expected 1 task for session2, got %d", len(tasks2))
	}

	if tasks2[0].ID != task3.ID {
		t.Errorf("Expected task3 in session2 list")
	}
}

func TestBackgroundTaskManager_MaxTasks(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewBackgroundTaskManager(tempDir)

	sessionID := "test-session"

	// Spawn MaxTasksPerSession long-running tasks (sleep so they don't complete immediately)
	var taskIDs []string
	for i := 0; i < MaxTasksPerSession; i++ {
		task, err := manager.Spawn(context.Background(), sessionID, "sleep 30", "/tmp")
		if err != nil {
			t.Fatalf("Failed to spawn task %d: %v", i, err)
		}
		taskIDs = append(taskIDs, task.ID)
	}

	// At max limit, GC should trigger but we should still be able to spawn more
	// (since GC removes completed tasks). With all tasks sleeping, GC won't free any.
	task101, err := manager.Spawn(context.Background(), sessionID, "sleep 1", "/tmp")
	if err != nil {
		t.Fatalf("Failed to spawn task after hitting limit: %v", err)
	}

	// Verify we have at least 100 tasks (GC may have removed some older completed ones)
	tasks := manager.List(sessionID)
	if len(tasks) < 100 {
		t.Errorf("Expected at least 100 tasks, got %d", len(tasks))
	}

	// Kill all running tasks
	for _, task := range tasks {
		manager.Kill(task.ID)
	}
	// Also kill the 101st if it was created
	if task101 != nil {
		manager.Kill(task101.ID)
	}

	// Wait for kills to complete
	time.Sleep(200 * time.Millisecond)
}

func TestBackgroundTaskManager_ExitCode(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewBackgroundTaskManager(tempDir)

	sessionID := "test-session"

	// Test successful command (exit 0)
	task1, _ := manager.Spawn(context.Background(), sessionID, "exit 0", "/tmp")
	time.Sleep(200 * time.Millisecond)

	manager.mu.RLock()
	bgTask1 := manager.tasks[task1.ID]
	manager.mu.RUnlock()

	status1, exitCode1, _ := bgTask1.snapshot()
	if exitCode1 != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode1)
	}
	if status1 != BgStatusCompleted {
		t.Errorf("Expected status %s, got %s", BgStatusCompleted, status1)
	}

	// Test failed command (exit 1)
	task2, _ := manager.Spawn(context.Background(), sessionID, "exit 1", "/tmp")
	time.Sleep(200 * time.Millisecond)

	manager.mu.RLock()
	bgTask2 := manager.tasks[task2.ID]
	manager.mu.RUnlock()

	status2, exitCode2, _ := bgTask2.snapshot()
	if exitCode2 != 1 {
		t.Errorf("Expected exit code 1, got %d", exitCode2)
	}
	if status2 != BgStatusFailed {
		t.Errorf("Expected status %s, got %s", BgStatusFailed, status2)
	}
}

func TestBackgroundTaskManager_LogFileLimit(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewBackgroundTaskManager(tempDir)

	sessionID := "test-session"

	// Generate output larger than max size
	// Print 1KB chunks, 150 times = 150KB > 100KB limit
	command := "for i in {1..150}; do printf '%1024s' | tr ' ' 'x'; done"

	task, err := manager.Spawn(context.Background(), sessionID, command, "/tmp")
	if err != nil {
		t.Fatalf("Failed to spawn task: %v", err)
	}

	// Wait for command to complete
	time.Sleep(500 * time.Millisecond)

	// Check log file size
	logPath := filepath.Join(tempDir, sessionID, task.ID+".log")
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Failed to stat log file: %v", err)
	}

	if info.Size() > MaxBgLogFileBytes {
		t.Errorf("Log file size %d exceeds max %d", info.Size(), MaxBgLogFileBytes)
	}

	// Size should be close to max but not over
	if info.Size() < MaxBgLogFileBytes-1024 {
		t.Logf("Warning: Log file size %d is much smaller than max %d", info.Size(), MaxBgLogFileBytes)
	}
}

func TestBackgroundTaskManager_ReadOutput_MaxLines(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewBackgroundTaskManager(tempDir)

	sessionID := "test-session"
	command := "i=1; while [ $i -le 100 ]; do echo line$i; i=$((i+1)); done"

	task, _ := manager.Spawn(context.Background(), sessionID, command, "/tmp")
	time.Sleep(300 * time.Millisecond)

	// Read with max lines = 10
	output, _, _, err := manager.ReadOutput(task.ID, 0, 0, 10)
	if err != nil {
		t.Fatalf("Failed to read output: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 10 {
		t.Errorf("Expected 10 lines, got %d", len(lines))
	}

	// Should contain last 10 lines (line91-line100)
	if !strings.Contains(output, "line91") || !strings.Contains(output, "line100") {
		t.Errorf("Expected last 10 lines, got: %s", output)
	}

	if strings.Contains(output, "line1") && !strings.Contains(output, "line100") {
		t.Errorf("Should not contain early lines, got: %s", output)
	}
}
