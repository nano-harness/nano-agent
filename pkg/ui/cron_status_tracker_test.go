package ui

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
)

func cronStarted(taskID string) event.StreamEvent {
	return event.StreamEvent{
		Type:      event.EventTypeCronTaskStarted,
		Timestamp: 100,
		TaskID:    taskID,
		Metadata: map[string]interface{}{
			"task_id":      taskID,
			"task_command": "echo hi",
		},
	}
}

func cronFinished(taskID string) event.StreamEvent {
	return event.StreamEvent{
		Type:   event.EventTypeCronTaskFinished,
		TaskID: taskID,
		Metadata: map[string]interface{}{
			"task_id": taskID,
		},
	}
}

func TestCronStatusTracker_StartedThenFinished(t *testing.T) {
	tracker := NewCronStatusTracker()
	tracker.Handle(cronStarted("a"))
	if got := tracker.Count(); got != 1 {
		t.Fatalf("Count() = %d, want 1", got)
	}
	if got := tracker.FormatIndicator(); got != "⏰ 1 running" {
		t.Fatalf("FormatIndicator() = %q", got)
	}

	tracker.Handle(cronFinished("a"))
	if got := tracker.Count(); got != 0 {
		t.Fatalf("Count() = %d, want 0", got)
	}
	if got := tracker.FormatIndicator(); got != "" {
		t.Fatalf("FormatIndicator() = %q, want empty", got)
	}
}

func TestCronStatusTracker_DedupSameTaskID(t *testing.T) {
	tracker := NewCronStatusTracker()
	tracker.Handle(cronStarted("a"))
	tracker.Handle(cronStarted("a"))
	if got := tracker.Count(); got != 1 {
		t.Fatalf("Count() = %d, want 1", got)
	}
}

func TestCronStatusTracker_MultipleTasks(t *testing.T) {
	tracker := NewCronStatusTracker()
	tracker.Handle(cronStarted("a"))
	tracker.Handle(cronStarted("b"))
	if got := tracker.FormatIndicator(); got != "⏰ 2 running" {
		t.Fatalf("FormatIndicator() = %q", got)
	}
}

func TestCronStatusTracker_FormatDetails(t *testing.T) {
	tracker := NewCronStatusTracker()
	if got := tracker.FormatDetails(); !strings.Contains(got, "当前没有正在运行") {
		t.Fatalf("FormatDetails() = %q, want empty-state message", got)
	}

	tracker.Handle(event.StreamEvent{
		Type:      event.EventTypeCronTaskStarted,
		Timestamp: time.Now().Unix() - 2,
		TaskID:    "task-a",
		Metadata: map[string]interface{}{
			"task_id":      "task-a",
			"task_command": "echo hi",
		},
	})
	got := tracker.FormatDetails()
	for _, want := range []string{"Running routines (1):", "task-a", "elapsed=", `cmd="echo hi"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatDetails() = %q, want to contain %q", got, want)
		}
	}
}

func TestCronStatusTracker_OnChangeFiresOncePerTransition(t *testing.T) {
	tracker := NewCronStatusTracker()
	var calls int32
	tracker.SetOnChange(func() {
		atomic.AddInt32(&calls, 1)
	})

	tracker.Handle(cronStarted("a"))
	tracker.Handle(cronStarted("a"))
	tracker.Handle(cronFinished("a"))
	tracker.Handle(cronFinished("a"))

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("onChange calls = %d, want 2", got)
	}
}

func TestCronStatusTracker_FinishedWithoutStartedNoUnderflow(t *testing.T) {
	tracker := NewCronStatusTracker()
	tracker.Handle(cronFinished("missing"))
	if got := tracker.Count(); got != 0 {
		t.Fatalf("Count() = %d, want 0", got)
	}
}

func TestCronStatusTracker_ConcurrentSafe(t *testing.T) {
	tracker := NewCronStatusTracker()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tracker.Handle(cronStarted(fmt.Sprintf("task-%d", i)))
		}(i)
	}
	wg.Wait()
	if got := tracker.Count(); got != 100 {
		t.Fatalf("Count() = %d, want 100", got)
	}

	for _, state := range tracker.Snapshot() {
		wg.Add(1)
		go func(taskID string) {
			defer wg.Done()
			tracker.Handle(cronFinished(taskID))
		}(state.TaskID)
	}
	wg.Wait()
	if got := tracker.Count(); got != 0 {
		t.Fatalf("Count() = %d, want 0", got)
	}
}

func TestCronStatusTracker_SnapshotIsImmutable(t *testing.T) {
	tracker := NewCronStatusTracker()
	tracker.Handle(cronStarted("a"))
	snap := tracker.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("Snapshot length = %d, want 1", len(snap))
	}
	snap[0].TaskID = "mutated"
	if got := tracker.Snapshot()[0].TaskID; got != "a" {
		t.Fatalf("internal state mutated: got %q", got)
	}
}
