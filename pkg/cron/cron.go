// Package cron manages recurring scheduled tasks backed by github.com/robfig/cron.
// This package supersedes pkg/scheduler.
package cron

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/google/uuid"
	cronlib "github.com/robfig/cron/v3"
)

// ErrTaskNotFound is returned when a task ID is not found in the scheduler.
var ErrTaskNotFound = errors.New("cron: task not found")

// Task represents a scheduled task.
type Task struct {
	ID        string          `json:"id"`
	CronExpr  string          `json:"cron_expression"`
	Command   string          `json:"command"`
	CreatedAt time.Time       `json:"created_at"`
	EntryID   cronlib.EntryID `json:"-"`
	Source    string          `json:"source,omitempty"`
	MaxRuns   int             `json:"max_runs,omitempty"`
}

// TaskExecutionMetadata contains rich information about a task execution.
type TaskExecutionMetadata struct {
	SessionID     string
	EventsPath    string
	ToolCallCount int
	TokenUsage    int64
	FailureStage  string
	FailedTool    string
}

// Scheduler manages scheduled tasks.
type Scheduler struct {
	cron        *cronlib.Cron
	tasks       map[string]*Task
	mu          sync.RWMutex
	executeTask func(command string) error
	// executeTaskRich is an optional rich executor that returns metadata
	executeTaskRich func(command, taskID string) (TaskExecutionMetadata, error)
	stateStore      *config.StateStore
	taskLog         *TaskLog

	// Cleanup goroutine management
	stopCleanup     chan struct{}
	cleanupRunning  bool
	retentionDays   int
	cleanupInterval time.Duration
}

// New creates a new Scheduler.
// executeTask is called with the command string each time a task fires.
func New(executeTask func(command string) error) *Scheduler {
	return &Scheduler{
		cron:        cronlib.New(cronlib.WithSeconds()),
		tasks:       make(map[string]*Task),
		executeTask: executeTask,
	}
}

// SetStateStore attaches a StateStore so tasks are persisted on every change.
func (s *Scheduler) SetStateStore(ss *config.StateStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stateStore = ss
}

// SetTaskLog attaches a TaskLog so every task execution is recorded.
func (s *Scheduler) SetTaskLog(tl *TaskLog) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskLog = tl
}

// SetLogRetention configures automatic log cleanup behavior.
// retentionDays: number of days to retain logs (0 = no cleanup)
// interval: how often to run cleanup
func (s *Scheduler) SetLogRetention(retentionDays int, interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retentionDays = retentionDays
	s.cleanupInterval = interval
}

// runLogCleanup is the background goroutine that periodically cleans old logs
// and event files from cron executions.
func (s *Scheduler) runLogCleanup() {
	if s.cleanupInterval == 0 {
		s.cleanupInterval = 24 * time.Hour
	}
	if s.retentionDays == 0 {
		s.retentionDays = 30
	}

	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCleanup:
			return
		case <-ticker.C:
			s.mu.RLock()
			tl := s.taskLog
			retDays := s.retentionDays
			s.mu.RUnlock()

			// Clean up task logs
			if tl != nil && retDays > 0 {
				maxAge := time.Duration(retDays) * 24 * time.Hour
				if err := tl.Cleanup(maxAge); err != nil {
					logger.Warnf("cron: log cleanup failed: %v", err)
				} else {
					logger.Debugf("cron: log cleanup completed (retention: %d days)", retDays)
				}
			}

			// Clean up old event files
			if err := s.cleanupOldEventFiles(retDays); err != nil {
				logger.Warnf("cron: event file cleanup failed: %v", err)
			}
		}
	}
}

// SetExecuteTask replaces the function called whenever a scheduled task fires.
// Use this to wire in a live executor after initial construction.
func (s *Scheduler) SetExecuteTask(fn func(command string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executeTask = fn
}

// SetExecuteTaskRich sets a rich executor that returns detailed metadata.
// If set, this takes precedence over SetExecuteTask.
func (s *Scheduler) SetExecuteTaskRich(fn func(command, taskID string) (TaskExecutionMetadata, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executeTaskRich = fn
}

// runTaskOnce executes a task once. This is the shared execution path used by both
// scheduled cron triggers and manual RunTaskNow calls.
// isManual: true for manual triggers (doesn't count toward max_runs), false for cron triggers.
// source: the source string to log (e.g. "manual-trigger" for manual runs, or task.Source for cron).
func (s *Scheduler) runTaskOnce(taskID, command, source string, isManual bool) {
	// Snapshot mutable fields under RLock to prevent data races when
	// SetExecuteTask / SetTaskLog are called concurrently.
	s.mu.RLock()
	execFn := s.executeTask
	execRichFn := s.executeTaskRich
	tl := s.taskLog
	s.mu.RUnlock()

	logger.Infof("cron: executing task %s: %s", taskID, command)
	started := time.Now()
	var execErr error
	var meta TaskExecutionMetadata

	// Use rich executor if available, otherwise fall back to simple executor
	if execRichFn != nil {
		meta, execErr = execRichFn(command, taskID)
	} else if execFn != nil {
		execErr = execFn(command)
	}

	if tl != nil {
		finished := time.Now()
		entry := TaskLogEntry{
			TaskID:        taskID,
			Command:       command,
			StartedAt:     started,
			FinishedAt:    finished,
			Success:       execErr == nil,
			Source:        source,
			SessionID:     meta.SessionID,
			EventsPath:    meta.EventsPath,
			DurationMs:    finished.Sub(started).Milliseconds(),
			ToolCallCount: meta.ToolCallCount,
			TokenUsage:    meta.TokenUsage,
			SchemaVersion: 2,
		}
		if execErr != nil {
			entry.Error = execErr.Error()
			entry.FailureStage = meta.FailureStage
			entry.FailedToolName = meta.FailedTool
		}
		_ = tl.Append(entry)
	}
	if execErr != nil {
		logger.Errorf("cron: task %s failed: %v", taskID, execErr)
	}
}

// Start starts the underlying cron runner and log cleanup goroutine.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cron.Start()
	logger.Info("cron scheduler started")

	// Start cleanup goroutine if not already running
	if !s.cleanupRunning && s.taskLog != nil {
		s.stopCleanup = make(chan struct{})
		s.cleanupRunning = true
		go s.runLogCleanup()
		logger.Debug("cron: log cleanup goroutine started")
	}
}

// Stop stops the cron runner and cleanup goroutine.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cron.Stop()
	logger.Info("cron scheduler stopped")

	// Stop cleanup goroutine if running
	if s.cleanupRunning {
		close(s.stopCleanup)
		s.cleanupRunning = false
		logger.Debug("cron: log cleanup goroutine stopped")
	}
}

// LoadPersistedTasks loads tasks from the StateStore and schedules them.
// Call this after Start().
func (s *Scheduler) LoadPersistedTasks() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stateStore == nil {
		return nil
	}

	tasks := s.stateStore.GetTasks()
	for _, pt := range tasks {
		// Explicitly copy loop-variable fields into locals so the closure
		// captures fresh values for each iteration.
		taskID := pt.ID
		cmd := pt.Command
		cronExpr := pt.CronExpr
		maxRuns := pt.MaxRuns
		var runCount atomic.Int64

		entryID, err := s.cron.AddFunc(cronExpr, func() {
			if maxRuns > 0 {
				n := runCount.Add(1)
				if n > int64(maxRuns) {
					return // already past limit; removal goroutine is pending
				}
				if n == int64(maxRuns) {
					// Last allowed run — remove asynchronously to avoid deadlock.
					go func() {
						s.mu.Lock()
						if t, ok := s.tasks[taskID]; ok {
							s.cron.Remove(t.EntryID)
							delete(s.tasks, taskID)
						}
						s.mu.Unlock()
						s.removePersistedTask(taskID)
					}()
				}
			}
			// Get the source under RLock
			s.mu.RLock()
			taskSource := pt.Source
			s.mu.RUnlock()

			// Use shared execution path
			s.runTaskOnce(taskID, cmd, taskSource, false)
		})
		if err != nil {
			logger.Warnf("cron: could not reload persisted task %s (%q): %v", taskID, cronExpr, err)
			continue
		}

		t := &Task{
			ID:       taskID,
			CronExpr: cronExpr,
			Command:  cmd,
			Source:   pt.Source,
			MaxRuns:  maxRuns,
			EntryID:  entryID,
		}
		if pt.CreatedAt != "" {
			if parsed, err := time.Parse(time.RFC3339, pt.CreatedAt); err == nil {
				t.CreatedAt = parsed
			}
		}
		s.tasks[taskID] = t
		logger.Infof("cron: reloaded persisted task %s (%s)", taskID, cronExpr)
	}
	return nil
}

// addPersistedTask adds a single task to the state store without touching tasks
// owned by other sources. Must NOT be called with s.mu held.
func (s *Scheduler) addPersistedTask(t *Task) {
	if s.stateStore == nil {
		return
	}
	s.stateStore.AddTask(config.PersistedTask{
		ID:        t.ID,
		CronExpr:  t.CronExpr,
		Command:   t.Command,
		Source:    t.Source,
		MaxRuns:   t.MaxRuns,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
	})
	if err := s.stateStore.Save(); err != nil {
		logger.Infof("failed to persist cron task %s: %v", t.ID, err)
	}
}

// removePersistedTask removes a single task from the state store by ID.
// Must NOT be called with s.mu held.
func (s *Scheduler) removePersistedTask(taskID string) {
	if s.stateStore == nil {
		return
	}
	s.stateStore.RemoveTask(taskID)
	if err := s.stateStore.Save(); err != nil {
		logger.Infof("failed to remove persisted cron task %s: %v", taskID, err)
	}
}

// ScheduleTask registers a new cron task.
// cronExpr must be a valid cron expression (with optional seconds field).
func (s *Scheduler) ScheduleTask(cronExpr, command string) (*Task, error) {
	return s.scheduleTask(cronExpr, command, "", 0)
}

// ScheduleTaskWithSource registers a new cron task with a source label and optional max runs.
func (s *Scheduler) ScheduleTaskWithSource(cronExpr, command, source string, maxRuns int) (*Task, error) {
	return s.scheduleTask(cronExpr, command, source, maxRuns)
}

func (s *Scheduler) scheduleTask(cronExpr, command, source string, maxRuns int) (*Task, error) {
	s.mu.Lock()

	taskID := uuid.New().String()
	var runCount atomic.Int64

	entryID, err := s.cron.AddFunc(cronExpr, func() {
		if maxRuns > 0 {
			n := runCount.Add(1)
			if n > int64(maxRuns) {
				return // already past limit; removal goroutine is pending
			}
			if n == int64(maxRuns) {
				// Last allowed run — remove asynchronously to avoid deadlock.
				go func() {
					s.mu.Lock()
					if t, ok := s.tasks[taskID]; ok {
						s.cron.Remove(t.EntryID)
						delete(s.tasks, taskID)
					}
					s.mu.Unlock()
					s.removePersistedTask(taskID)
				}()
			}
		}
		// Use shared execution path
		s.runTaskOnce(taskID, command, source, false)
	})
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("cron: invalid expression %q: %w", cronExpr, err)
	}

	t := &Task{
		ID:        taskID,
		CronExpr:  cronExpr,
		Command:   command,
		Source:    source,
		MaxRuns:   maxRuns,
		CreatedAt: time.Now(),
		EntryID:   entryID,
	}
	s.tasks[taskID] = t
	logger.Infof("cron: task %s scheduled (%s)", taskID, cronExpr)
	s.mu.Unlock()

	s.addPersistedTask(t)
	return t, nil
}

// RemoveTask cancels and removes a task by its ID.
func (s *Scheduler) RemoveTask(taskID string) error {
	s.mu.Lock()

	t, ok := s.tasks[taskID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("cron: task not found: %s", taskID)
	}
	s.cron.Remove(t.EntryID)
	delete(s.tasks, taskID)
	logger.Infof("cron: task %s removed", taskID)
	s.mu.Unlock()

	s.removePersistedTask(taskID)
	return nil
}

// ListTasks returns all registered tasks.
func (s *Scheduler) ListTasks() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	return out
}

// RunTaskNow immediately executes a registered task by ID without affecting its
// cron schedule state. Does not count toward max_runs. Logs with source="manual-trigger".
// Returns ErrTaskNotFound if the task ID does not exist.
// Executes synchronously; callers may run this in a goroutine for async behavior.
func (s *Scheduler) RunTaskNow(taskID string) error {
	// Snapshot task data under lock
	s.mu.RLock()
	t, ok := s.tasks[taskID]
	if !ok {
		s.mu.RUnlock()
		return ErrTaskNotFound
	}
	cmd := t.Command
	s.mu.RUnlock()

	// Execute using the shared path with manual trigger flag
	s.runTaskOnce(taskID, cmd, "manual-trigger", true)
	return nil
}

// cleanupOldEventFiles removes old event files based on retention policy.
func (s *Scheduler) cleanupOldEventFiles(retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	eventsDir := filepath.Join(home, ".nano", "cron-events")
	if _, err := os.Stat(eventsDir); os.IsNotExist(err) {
		return nil // No events directory
	}

	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	var deletedCount int

	// Walk through all task directories
	err = filepath.Walk(eventsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Only check .jsonl files
		if !info.IsDir() && filepath.Ext(path) == ".jsonl" {
			if info.ModTime().Before(cutoff) {
				if err := os.Remove(path); err == nil {
					deletedCount++
				}
			}
		}

		return nil
	})

	if deletedCount > 0 {
		logger.Debugf("cron: cleaned up %d old event files", deletedCount)
	}

	return err
}
