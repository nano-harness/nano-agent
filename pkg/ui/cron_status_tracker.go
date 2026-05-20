package ui

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
)

type CronTaskState struct {
	TaskID    string
	Command   string
	StartedAt int64
}

type CronStatusTracker struct {
	mu               sync.RWMutex
	running          map[string]CronTaskState
	onChange         func()
	scheduledCountFn func() int
}

func NewCronStatusTracker() *CronStatusTracker {
	return &CronStatusTracker{
		running: make(map[string]CronTaskState),
	}
}

func (t *CronStatusTracker) Handle(ev event.StreamEvent) {
	if t == nil {
		return
	}

	taskID := metadataString(ev.Metadata, "task_id")
	if taskID == "" {
		taskID = ev.TaskID
	}
	if taskID == "" {
		return
	}

	changed := false
	t.mu.Lock()
	switch ev.Type {
	case event.EventTypeCronTaskStarted:
		if _, exists := t.running[taskID]; !exists {
			t.running[taskID] = CronTaskState{
				TaskID:    taskID,
				Command:   metadataString(ev.Metadata, "task_command"),
				StartedAt: ev.Timestamp,
			}
			changed = true
		}
	case event.EventTypeCronTaskFinished:
		if _, exists := t.running[taskID]; exists {
			delete(t.running, taskID)
			changed = true
		}
	}
	onChange := t.onChange
	t.mu.Unlock()

	if changed && onChange != nil {
		onChange()
	}
}

func (t *CronStatusTracker) Count() int {
	if t == nil {
		return 0
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.running)
}

func (t *CronStatusTracker) Snapshot() []CronTaskState {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]CronTaskState, 0, len(t.running))
	for _, state := range t.running {
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt == out[j].StartedAt {
			return out[i].TaskID < out[j].TaskID
		}
		return out[i].StartedAt < out[j].StartedAt
	})
	return out
}

func (t *CronStatusTracker) FormatIndicator() string {
	running := t.Count()
	scheduled := t.ScheduledCount()
	if scheduled == 0 && running == 0 {
		return ""
	}
	if running == 0 {
		return fmt.Sprintf("⏰ %d scheduled", scheduled)
	}
	return fmt.Sprintf("⏰ %d scheduled, %d running", scheduled, running)
}

// FormatDetails returns a multi-line listing of currently running cron
// tasks (taskID, command, elapsed since started). Empty string when none.
// Used by the /routines status slash command in the TUI.
func (t *CronStatusTracker) FormatDetails() string {
	snap := t.Snapshot()
	if len(snap) == 0 {
		return "ℹ️  当前没有正在运行的定时任务。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Running routines (%d):\n", len(snap))
	nowSec := time.Now().Unix()
	for _, s := range snap {
		elapsed := "-"
		if s.StartedAt > 0 {
			d := time.Duration(nowSec-s.StartedAt) * time.Second
			if d < 0 {
				d = 0
			}
			elapsed = d.String()
		}
		cmd := s.Command
		if cmd == "" {
			cmd = "-"
		}
		fmt.Fprintf(&b, "  - %s  elapsed=%s  cmd=%q\n", s.TaskID, elapsed, cmd)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (t *CronStatusTracker) SetOnChange(fn func()) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onChange = fn
}

func (t *CronStatusTracker) SetScheduledCountFn(fn func() int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.scheduledCountFn = fn
}

func (t *CronStatusTracker) ScheduledCount() int {
	if t == nil {
		return 0
	}
	t.mu.RLock()
	fn := t.scheduledCountFn
	t.mu.RUnlock()
	if fn == nil {
		return 0
	}
	return fn()
}

func (t *CronStatusTracker) TriggerChange() {
	if t == nil {
		return
	}
	t.mu.RLock()
	fn := t.onChange
	t.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

func metadataString(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	v, _ := metadata[key].(string)
	return v
}
