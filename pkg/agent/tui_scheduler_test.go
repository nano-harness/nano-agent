package agent

import (
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestTUIScheduler_StartStop(t *testing.T) {
	ts := NewTUIScheduler(nil, func(cmd string) error { return nil })
	if err := ts.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ts.Stop()
}

func TestTUIScheduler_ScheduleLoop(t *testing.T) {
	executed := make(chan string, 1)
	ts := NewTUIScheduler(nil, func(cmd string) error {
		executed <- cmd
		return nil
	})
	if err := ts.Start(); err != nil {
		t.Fatal(err)
	}
	defer ts.Stop()

	// Schedule a very frequent task using a direct cron expression
	task, err := ts.ScheduleCron("* * * * * *", "ping") // every second (6-field with seconds)
	if err != nil {
		t.Fatalf("ScheduleCron: %v", err)
	}

	if task.ID == "" {
		t.Error("expected non-empty task ID")
	}

	// Wait for execution (up to 3 seconds)
	select {
	case cmd := <-executed:
		if cmd != "ping" {
			t.Errorf("expected 'ping', got %q", cmd)
		}
	case <-time.After(3 * time.Second):
		t.Error("task did not execute within 3 seconds")
	}
}

func TestTUIScheduler_CancelTask(t *testing.T) {
	ts := NewTUIScheduler(nil, func(cmd string) error { return nil })
	if err := ts.Start(); err != nil {
		t.Fatal(err)
	}
	defer ts.Stop()

	task, err := ts.ScheduleCron("0 0 0 1 1 *", "never") // Jan 1st at midnight
	if err != nil {
		t.Fatalf("ScheduleCron: %v", err)
	}

	if err := ts.CancelTask(task.ID); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}

	tasks := ts.ListTasks()
	for _, tt := range tasks {
		if tt.ID == task.ID {
			t.Error("task should have been removed")
		}
	}
}

func TestTUIScheduler_ListTasks(t *testing.T) {
	ts := NewTUIScheduler(nil, func(cmd string) error { return nil })
	if err := ts.Start(); err != nil {
		t.Fatal(err)
	}
	defer ts.Stop()

	_, err := ts.ScheduleCron("0 0 0 1 1 *", "task1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ts.ScheduleCron("0 0 0 1 1 *", "task2")
	if err != nil {
		t.Fatal(err)
	}

	tasks := ts.ListTasks()
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTUIScheduler_WithStateStore(t *testing.T) {
	dir := t.TempDir()
	ss := config.NewStateStore(dir + "/state.json")
	if err := ss.Load(); err != nil {
		t.Fatal(err)
	}

	ts := NewTUIScheduler(ss, func(cmd string) error { return nil })
	if err := ts.Start(); err != nil {
		t.Fatal(err)
	}
	defer ts.Stop()

	// Schedule a task — it should be persisted
	_, err := ts.ScheduleCron("0 0 0 1 1 *", "persist-me")
	if err != nil {
		t.Fatal(err)
	}

	tasks := ss.GetTasks()
	if len(tasks) != 1 {
		t.Errorf("expected 1 persisted task, got %d", len(tasks))
	}
	if tasks[0].Command != "persist-me" {
		t.Errorf("expected command 'persist-me', got %q", tasks[0].Command)
	}
}

func TestTUIScheduler_AddPauseResumeRemove(t *testing.T) {
	ts := NewTUIScheduler(nil, func(cmd string) error { return nil })
	if err := ts.Start(); err != nil {
		t.Fatal(err)
	}
	defer ts.Stop()

	id, err := ts.AddRoutineFromDescription("每5分钟运行 echo hello")
	if err != nil {
		t.Fatalf("AddRoutineFromDescription: %v", err)
	}
	if id == "" {
		t.Fatal("expected task id")
	}
	if len(ts.ListTasks()) != 1 {
		t.Fatalf("expected one task after add")
	}
	if err := ts.PauseTask(id); err != nil {
		t.Fatalf("PauseTask: %v", err)
	}
	if len(ts.ListTasks()) != 0 {
		t.Fatalf("expected no live tasks after pause")
	}
	if err := ts.ResumeTask(id); err != nil {
		t.Fatalf("ResumeTask: %v", err)
	}
	tasks := ts.ListTasks()
	if len(tasks) != 1 || tasks[0].Command != "echo hello" {
		t.Fatalf("expected resumed task, got %+v", tasks)
	}
	if err := ts.RemoveTask(tasks[0].ID); err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}
}
