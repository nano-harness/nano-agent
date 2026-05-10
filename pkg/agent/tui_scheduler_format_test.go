package agent

import (
	"strings"
	"testing"
)

func TestTUIScheduler_FormatTasks(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		ts := NewTUIScheduler(nil, func(cmd string) error { return nil })
		got := ts.FormatTasks()
		if !strings.Contains(got, "暂无已加载的定时任务") {
			t.Fatalf("FormatTasks() = %q, want empty-state message", got)
		}
	})

	t.Run("non-empty", func(t *testing.T) {
		ts := NewTUIScheduler(nil, func(cmd string) error { return nil })
		task, err := ts.Scheduler().ScheduleTaskWithSource("0 0 0 1 1 *", "echo hi", "test-source", 0)
		if err != nil {
			t.Fatalf("ScheduleTaskWithSource: %v", err)
		}

		got := ts.FormatTasks()
		for _, want := range []string{
			"Routines (1, in-memory):",
			task.ID,
			`cron="0 0 0 1 1 *"`,
			"source=test-source",
			`cmd="echo hi"`,
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("FormatTasks() = %q, want to contain %q", got, want)
			}
		}
	})
}
