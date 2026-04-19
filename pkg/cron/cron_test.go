package cron

import (
	"errors"
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
