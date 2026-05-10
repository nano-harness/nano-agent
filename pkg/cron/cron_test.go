package cron

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	s := New(nil)
	if s == nil {
		t.Fatal("New returned nil")
	}
}

func TestScheduleAndRemoveTask(t *testing.T) {
	s := New(nil)
	s.Start()
	defer s.Stop()

	task, err := s.ScheduleTask("* * * * * *", "echo hello")
	if err != nil {
		t.Fatalf("ScheduleTask: %v", err)
	}
	if task.ID == "" {
		t.Error("task.ID should not be empty")
	}
	if task.CronExpr != "* * * * * *" {
		t.Errorf("CronExpr = %q, want %q", task.CronExpr, "* * * * * *")
	}
	if task.Command != "echo hello" {
		t.Errorf("Command = %q, want %q", task.Command, "echo hello")
	}
	if task.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}

	if err := s.RemoveTask(task.ID); err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}
}

func TestRemoveTask_NotFound(t *testing.T) {
	s := New(nil)
	err := s.RemoveTask("no-such-id")
	if err == nil {
		t.Error("expected error for missing task ID")
	}
}

func TestListTasks(t *testing.T) {
	s := New(nil)
	s.Start()
	defer s.Stop()

	t1, _ := s.ScheduleTask("* * * * * *", "cmd1")
	t2, _ := s.ScheduleTask("0 * * * * *", "cmd2")
	defer s.RemoveTask(t1.ID) //nolint:errcheck
	defer s.RemoveTask(t2.ID) //nolint:errcheck

	tasks := s.ListTasks()
	if len(tasks) != 2 {
		t.Fatalf("ListTasks len = %d, want 2", len(tasks))
	}
}

func TestScheduleTask_InvalidExpr(t *testing.T) {
	s := New(nil)
	_, err := s.ScheduleTask("not-a-cron", "echo hi")
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestScheduleTask_ExecuteCallback(t *testing.T) {
	var counter int64
	exec := func(cmd string) error {
		atomic.AddInt64(&counter, 1)
		return nil
	}

	s := New(exec)
	s.Start()
	defer s.Stop()

	// Schedule every second; wait up to 3 seconds for at least one execution.
	task, err := s.ScheduleTask("* * * * * *", "ping")
	if err != nil {
		t.Fatalf("ScheduleTask: %v", err)
	}
	defer s.RemoveTask(task.ID) //nolint:errcheck

	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("task was not executed within 3 seconds")
		default:
			if atomic.LoadInt64(&counter) > 0 {
				return // success
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func TestScheduleTask_CallbackError(t *testing.T) {
	// Executor that always errors – scheduler should not crash.
	exec := func(cmd string) error {
		return errors.New("exec failed")
	}

	s := New(exec)
	s.Start()
	defer s.Stop()

	task, err := s.ScheduleTask("* * * * * *", "failing-cmd")
	if err != nil {
		t.Fatalf("ScheduleTask: %v", err)
	}
	defer s.RemoveTask(task.ID) //nolint:errcheck

	// Let the scheduler attempt at least one run without panicking.
	time.Sleep(1500 * time.Millisecond)
}

func TestStartIdempotent(t *testing.T) {
	s := New(nil)
	s.Start()
	s.Start() // second Start should be a no-op
	s.Stop()
}

func TestStopIdempotent(t *testing.T) {
	s := New(nil)
	s.Stop() // Stop without Start should not panic
}

func TestSetExecuteTaskRich(t *testing.T) {
	var (
		mu              sync.Mutex
		executed        bool
		capturedTaskID  string
		capturedCommand string
	)

	richExec := func(command, taskID string) (TaskExecutionMetadata, error) {
		mu.Lock()
		defer mu.Unlock()
		executed = true
		capturedCommand = command
		capturedTaskID = taskID
		return TaskExecutionMetadata{
			SessionID:     "test-session",
			ToolCallCount: 5,
			TokenUsage:    1000,
		}, nil
	}

	s := New(nil)
	s.SetExecuteTaskRich(richExec)
	s.Start()
	defer s.Stop()

	task, err := s.ScheduleTask("* * * * * *", "test command")
	if err != nil {
		t.Fatalf("ScheduleTask: %v", err)
	}
	defer s.RemoveTask(task.ID) //nolint:errcheck

	// Wait for execution
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("task was not executed within 3 seconds")
		default:
			mu.Lock()
			done := executed
			cmd := capturedCommand
			taskID := capturedTaskID
			mu.Unlock()

			if done {
				if cmd != "test command" {
					t.Errorf("capturedCommand = %q, want %q", cmd, "test command")
				}
				if taskID != task.ID {
					t.Errorf("capturedTaskID = %q, want %q", taskID, task.ID)
				}
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func TestScheduleTaskWithMaxRuns(t *testing.T) {
	var counter atomic.Int64

	exec := func(cmd string) error {
		counter.Add(1)
		return nil
	}

	s := New(exec)
	s.Start()
	defer s.Stop()

	// Schedule a task that should run only twice
	task, err := s.ScheduleTaskWithSource("* * * * * *", "test", "test-source", 2)
	if err != nil {
		t.Fatalf("ScheduleTaskWithSource: %v", err)
	}
	defer s.RemoveTask(task.ID) //nolint:errcheck

	// Wait for at least 3 seconds to ensure it only runs twice
	time.Sleep(3500 * time.Millisecond)

	count := counter.Load()
	if count > 2 {
		t.Errorf("task ran %d times, expected at most 2", count)
	}
}

func TestRunTaskNow_Success(t *testing.T) {
	var counter atomic.Int64
	exec := func(cmd string) error {
		counter.Add(1)
		return nil
	}

	s := New(exec)
	s.Start()
	defer s.Stop()

	// Schedule a task with a cron expression that won't fire soon (every hour at minute 0)
	task, err := s.ScheduleTask("0 0 * * * *", "test command")
	if err != nil {
		t.Fatalf("ScheduleTask: %v", err)
	}
	defer s.RemoveTask(task.ID) //nolint:errcheck

	// Immediately run the task
	if err := s.RunTaskNow(task.ID); err != nil {
		t.Fatalf("RunTaskNow: %v", err)
	}

	// Wait a bit to ensure execution completes
	time.Sleep(500 * time.Millisecond)

	// Verify the task was executed
	if counter.Load() == 0 {
		t.Error("task was not executed by RunTaskNow")
	}
}

func TestRunTaskNow_NotFound(t *testing.T) {
	s := New(nil)
	s.Start()
	defer s.Stop()

	err := s.RunTaskNow("nonexistent-id")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestRunTaskNow_DoesNotCountMaxRuns(t *testing.T) {
	var counter atomic.Int64
	exec := func(cmd string) error {
		counter.Add(1)
		return nil
	}

	s := New(exec)
	s.Start()
	defer s.Stop()

	// Schedule a task with max_runs=2
	task, err := s.ScheduleTaskWithSource("0 0 * * * *", "test", "test-source", 2)
	if err != nil {
		t.Fatalf("ScheduleTaskWithSource: %v", err)
	}

	// Run manually 5 times
	for i := 0; i < 5; i++ {
		if err := s.RunTaskNow(task.ID); err != nil {
			t.Fatalf("RunTaskNow iteration %d: %v", i, err)
		}
	}

	// Wait to ensure all executions complete
	time.Sleep(500 * time.Millisecond)

	// Task should still be in the list (not removed despite max_runs=2)
	tasks := s.ListTasks()
	found := false
	for _, tsk := range tasks {
		if tsk.ID == task.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("task was removed despite RunTaskNow not counting toward max_runs")
	}

	// Clean up
	_ = s.RemoveTask(task.ID)
}

func TestRunTaskNow_LogsManualSource(t *testing.T) {
	var counter atomic.Int64
	exec := func(cmd string) error {
		counter.Add(1)
		return nil
	}

	s := New(exec)

	// Set up task log
	logFile := t.TempDir() + "/task_log.jsonl"
	tl := NewTaskLog(logFile)

	s.SetTaskLog(tl)
	s.Start()
	defer s.Stop()

	// Schedule a task
	task, err := s.ScheduleTaskWithSource("0 0 * * * *", "test", "original-source", 0)
	if err != nil {
		t.Fatalf("ScheduleTaskWithSource: %v", err)
	}
	defer s.RemoveTask(task.ID) //nolint:errcheck

	// Run manually
	if err := s.RunTaskNow(task.ID); err != nil {
		t.Fatalf("RunTaskNow: %v", err)
	}

	// Wait for execution to complete
	time.Sleep(500 * time.Millisecond)

	// Query the log
	entries, err := tl.Query()
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no log entries found")
	}

	// Find the entry for our task
	var found bool
	for _, entry := range entries {
		if entry.TaskID == task.ID && entry.Source == "manual-trigger" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log entry with source 'manual-trigger' not found")
	}
}
