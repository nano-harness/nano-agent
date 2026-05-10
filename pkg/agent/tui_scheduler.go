package agent

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/cron"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/tools/builtin"
)

var chineseRoutinePrefixPattern = regexp.MustCompile(`^每(\d+)(分钟|分|小时|时)`) //nolint:gochecknoglobals
var chineseRoutineExactPattern = regexp.MustCompile(`^每(\d+)(分钟|分|小时|时)$`) //nolint:gochecknoglobals

// TaskInfo summarises a scheduled task for display in the TUI.
type TaskInfo struct {
	ID       string
	CronExpr string
	Command  string
	Source   string
}

// TUIScheduler wraps the cron Scheduler and StateStore for TUI-mode scheduling.
// It persists tasks via the StateStore so they survive restarts.
type TUIScheduler struct {
	scheduler   *cron.Scheduler
	stateStore  *config.StateStore
	executeTask func(string) error
	mu          sync.RWMutex
	pausedTasks map[string]TaskInfo
}

// NewTUISchedulerFromScheduler wraps an existing cron.Scheduler as a
// TUIScheduler. Use this when the cron.Scheduler is owned by an Engine so that
// there is only one scheduler instance for both the Engine and the TUI.
func NewTUISchedulerFromScheduler(s *cron.Scheduler, stateStore *config.StateStore) *TUIScheduler {
	return &TUIScheduler{
		scheduler:   s,
		stateStore:  stateStore,
		pausedTasks: make(map[string]TaskInfo),
	}
}

// NewTUIScheduler creates a TUIScheduler.
// executeTask is called with the command string on each tick.
func NewTUIScheduler(stateStore *config.StateStore, executeTask func(string) error) *TUIScheduler {
	ts := &TUIScheduler{
		stateStore:  stateStore,
		executeTask: executeTask,
		pausedTasks: make(map[string]TaskInfo),
	}
	ts.scheduler = cron.New(executeTask)
	if stateStore != nil {
		ts.scheduler.SetStateStore(stateStore)
	}
	return ts
}

// Start starts the underlying cron runner and reloads persisted tasks.
func (ts *TUIScheduler) Start() error {
	ts.scheduler.Start()
	if err := ts.scheduler.LoadPersistedTasks(); err != nil {
		logger.Warnf("TUIScheduler: could not reload persisted tasks: %v", err)
	}
	return nil
}

// Stop stops the cron runner.
func (ts *TUIScheduler) Stop() {
	ts.scheduler.Stop()
}

// ScheduleLoop creates a repeating task from a compact interval string (e.g. "5m", "2h").
func (ts *TUIScheduler) ScheduleLoop(interval, command string, maxRuns int) (*cron.Task, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Use ParseNaturalSchedule helper to derive cron expression from interval
	cronExpr, err := builtin.ParseNaturalSchedule("every " + expandInterval(interval))
	if err != nil {
		// Interval couldn't be parsed as natural language — treat it as a raw
		// cron expression and let the cron library validate it.
		cronExpr = interval
	}

	return ts.scheduler.ScheduleTaskWithSource(cronExpr, command, "tui", maxRuns)
}

// ScheduleCron creates a task from a standard cron expression.
func (ts *TUIScheduler) ScheduleCron(cronExpr, command string) (*cron.Task, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.scheduler.ScheduleTaskWithSource(cronExpr, command, "tui", 0)
}

// CancelTask cancels a task by ID.
func (ts *TUIScheduler) CancelTask(taskID string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.scheduler.RemoveTask(taskID)
}

// RemoveTask is a TUI-friendly alias for CancelTask; both remove the task from
// the live scheduler and persisted state.
func (ts *TUIScheduler) RemoveTask(taskID string) error {
	return ts.CancelTask(taskID)
}

// PauseTask removes a task from the live scheduler and keeps an in-memory copy
// that ResumeTask can re-add. robfig/cron does not support per-entry pause.
func (ts *TUIScheduler) PauseTask(taskID string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, t := range ts.scheduler.ListTasks() {
		if t.ID != taskID {
			continue
		}
		ts.pausedTasks[taskID] = TaskInfo{
			ID:       t.ID,
			CronExpr: t.CronExpr,
			Command:  t.Command,
			Source:   t.Source,
		}
		return ts.scheduler.RemoveTask(taskID)
	}
	return fmt.Errorf("task not found: %s", taskID)
}

// ResumeTask re-adds a task previously paused by PauseTask. The resumed live
// task receives a fresh scheduler ID because the underlying scheduler generates
// IDs and has no per-entry pause/resume primitive.
func (ts *TUIScheduler) ResumeTask(taskID string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	task, ok := ts.pausedTasks[taskID]
	if !ok {
		return fmt.Errorf("paused task not found: %s", taskID)
	}
	if _, err := ts.scheduler.ScheduleTaskWithSource(task.CronExpr, task.Command, task.Source, 0); err != nil {
		return err
	}
	delete(ts.pausedTasks, taskID)
	return nil
}

// AddRoutineFromDescription parses a TUI description and schedules it.
func (ts *TUIScheduler) AddRoutineFromDescription(description string) (string, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return "", fmt.Errorf("description is required")
	}
	schedule, command, err := parseRoutineDescription(description)
	if err != nil {
		return "", err
	}
	cronExpr, err := builtin.ParseNaturalSchedule(schedule)
	if err != nil {
		cronExpr = schedule
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	task, err := ts.scheduler.ScheduleTaskWithSource(cronExpr, command, "tui", 0)
	if err != nil {
		return "", err
	}
	return task.ID, nil
}

// ListTasks returns all currently scheduled tasks.
func (ts *TUIScheduler) ListTasks() []TaskInfo {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	tasks := ts.scheduler.ListTasks()
	out := make([]TaskInfo, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, TaskInfo{
			ID:       t.ID,
			CronExpr: t.CronExpr,
			Command:  t.Command,
			Source:   t.Source,
		})
	}
	return out
}

// FormatTasks returns a human-readable listing of all currently
// registered routines (cron tasks loaded into the in-memory scheduler).
// Returns an empty-state message when no tasks are registered.
func (ts *TUIScheduler) FormatTasks() string {
	tasks := ts.ListTasks()
	if len(tasks) == 0 {
		return "ℹ️  暂无已加载的定时任务。使用 `nano routines add ...` 添加。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Routines (%d, in-memory):\n", len(tasks))
	for _, t := range tasks {
		src := t.Source
		if src == "" {
			src = "-"
		}
		fmt.Fprintf(&b, "  - %s  cron=%q  source=%s  cmd=%q\n",
			t.ID, t.CronExpr, src, t.Command)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Scheduler returns the underlying cron.Scheduler for direct use in tools.
func (ts *TUIScheduler) Scheduler() *cron.Scheduler {
	return ts.scheduler
}

// expandInterval converts compact suffixes to words for ParseNaturalSchedule.
// e.g. "5m" → "5 minutes", "2h" → "2 hours"
func expandInterval(s string) string {
	if len(s) < 2 {
		return s
	}
	unit := s[len(s)-1]
	num := s[:len(s)-1]
	switch unit {
	case 'm':
		return fmt.Sprintf("%s minutes", num)
	case 'h':
		return fmt.Sprintf("%s hours", num)
	default:
		return s
	}
}

func parseRoutineDescription(description string) (schedule, command string, err error) {
	if parts := strings.SplitN(description, " -- ", 2); len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
	}
	if parts := strings.SplitN(description, " run ", 2); len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
	}
	if parts := strings.SplitN(description, " 运行 ", 2); len(parts) == 2 {
		return normalizeChineseSchedule(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1]), nil
	}
	if strings.HasPrefix(description, "每") {
		if match := chineseRoutinePrefixPattern.FindStringSubmatch(description); len(match) == 3 {
			rest := strings.TrimSpace(strings.TrimPrefix(description, match[0]))
			rest = strings.TrimPrefix(rest, "运行")
			rest = strings.TrimPrefix(rest, "执行")
			rest = strings.TrimSpace(rest)
			if rest != "" {
				unit := "minutes"
				if match[2] == "小时" || match[2] == "时" {
					unit = "hours"
				}
				return fmt.Sprintf("every %s %s", match[1], unit), rest, nil
			}
		}
	}
	fields := strings.Fields(description)
	if len(fields) >= 7 {
		candidate := strings.Join(fields[:6], " ")
		if _, parseErr := builtin.ParseNaturalSchedule(candidate); parseErr == nil {
			return candidate, strings.Join(fields[6:], " "), nil
		}
	}
	return "", "", fmt.Errorf("could not parse routine description; use '<schedule> run <command>'")
}

func normalizeChineseSchedule(schedule string) string {
	if match := chineseRoutineExactPattern.FindStringSubmatch(schedule); len(match) == 3 {
		unit := "minutes"
		if match[2] == "小时" || match[2] == "时" {
			unit = "hours"
		}
		return fmt.Sprintf("every %s %s", match[1], unit)
	}
	return schedule
}
