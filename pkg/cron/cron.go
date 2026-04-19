// Package cron manages recurring scheduled tasks backed by github.com/robfig/cron.
// This package supersedes pkg/scheduler.
package cron

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/google/uuid"
	cronlib "github.com/robfig/cron/v3"
)

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

// Scheduler manages scheduled tasks.
type Scheduler struct {
	cron        *cronlib.Cron
	tasks       map[string]*Task
	mu          sync.RWMutex
	executeTask func(command string) error
	stateStore  *config.StateStore
	taskLog     *TaskLog
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

// SetExecuteTask replaces the function called whenever a scheduled task fires.
// Use this to wire in a live executor after initial construction.
func (s *Scheduler) SetExecuteTask(fn func(command string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executeTask = fn
}

// Start starts the underlying cron runner.
func (s *Scheduler) Start() {
	s.cron.Start()
	logger.Info("cron scheduler started")
}

// Stop stops the cron runner.
func (s *Scheduler) Stop() {
	s.cron.Stop()
	logger.Info("cron scheduler stopped")
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
			// Snapshot mutable fields under RLock to prevent data races when
			// SetExecuteTask / SetTaskLog are called concurrently.
			s.mu.RLock()
			execFn := s.executeTask
			tl := s.taskLog
			s.mu.RUnlock()

			logger.Infof("cron: executing persisted task %s: %s", taskID, cmd)
			if execFn != nil {
				started := time.Now()
				execErr := execFn(cmd)
				if tl != nil {
					entry := TaskLogEntry{
						TaskID:     taskID,
						Command:    cmd,
						StartedAt:  started,
						FinishedAt: time.Now(),
						Success:    execErr == nil,
					}
					if execErr != nil {
						entry.Error = execErr.Error()
					}
					_ = tl.Append(entry)
				}
				if execErr != nil {
					logger.Errorf("cron: persisted task %s failed: %v", taskID, execErr)
				}
			}
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
		// Snapshot mutable fields under RLock to prevent data races when
		// SetExecuteTask / SetTaskLog are called concurrently.
		s.mu.RLock()
		execFn := s.executeTask
		tl := s.taskLog
		s.mu.RUnlock()

		logger.Infof("cron: executing task %s: %s", taskID, command)
		if execFn != nil {
			started := time.Now()
			err := execFn(command)
			if tl != nil {
				entry := TaskLogEntry{
					TaskID:     taskID,
					Command:    command,
					StartedAt:  started,
					FinishedAt: time.Now(),
					Success:    err == nil,
				}
				if err != nil {
					entry.Error = err.Error()
				}
				_ = tl.Append(entry)
			}
			if err != nil {
				logger.Errorf("cron: task %s failed: %v", taskID, err)
			}
		}
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
