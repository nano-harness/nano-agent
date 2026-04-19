package agent

import (
	"fmt"
	"sync"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/cron"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/tools/builtin"
)

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
}

// NewTUISchedulerFromScheduler wraps an existing cron.Scheduler as a
// TUIScheduler. Use this when the cron.Scheduler is owned by an Engine so that
// there is only one scheduler instance for both the Engine and the TUI.
func NewTUISchedulerFromScheduler(s *cron.Scheduler, stateStore *config.StateStore) *TUIScheduler {
	return &TUIScheduler{
		scheduler:  s,
		stateStore: stateStore,
	}
}

// NewTUIScheduler creates a TUIScheduler.
// executeTask is called with the command string on each tick.
func NewTUIScheduler(stateStore *config.StateStore, executeTask func(string) error) *TUIScheduler {
	ts := &TUIScheduler{
		stateStore:  stateStore,
		executeTask: executeTask,
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
