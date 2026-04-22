package builtin

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/cron"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// ManageRoutineTool lets the agent create, delete, list, pause, or resume
// routine tasks (cron-based recurring scheduled tasks) through conversation.
// Task creation requires user confirmation.
type ManageRoutineTool struct {
	scheduler  *cron.Scheduler
	stateStore *config.StateStore
	confirmFn  func(summary string) bool
	// mu guards concurrent access to the scheduler field during SetScheduler/getScheduler.
	mu sync.Mutex
}

// NewManageRoutineTool creates a ManageRoutineTool.
// scheduler and stateStore may be nil in which case the tool operates in
// read-only / no-op mode.
func NewManageRoutineTool(scheduler *cron.Scheduler, ss *config.StateStore, confirmFn func(string) bool) *ManageRoutineTool {
	return &ManageRoutineTool{
		scheduler:  scheduler,
		stateStore: ss,
		confirmFn:  confirmFn,
	}
}

// SetScheduler wires in a live cron.Scheduler so tasks are scheduled immediately.
// This is called after the TUI scheduler is started (typically from SetTUIScheduler).
func (t *ManageRoutineTool) SetScheduler(s *cron.Scheduler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.scheduler = s
}

// Name returns the tool name.
func (t *ManageRoutineTool) Name() string { return "manage_routine" }

// Description returns the tool description.
func (t *ManageRoutineTool) Description() string {
	return "Manage routine tasks (recurring scheduled commands): create, delete, list, status, pause, or resume. " +
		"Schedule supports cron expressions (e.g. '0 9 * * 1-5') or natural language (e.g. 'every 5 minutes', 'daily at 9am'). " +
		"Create requires user confirmation."
}

// Category returns the tool category.
func (t *ManageRoutineTool) Category() interfaces.ToolCategory { return interfaces.CategoryAgent }

// RequiresConfirmation returns false — handled internally.
func (t *ManageRoutineTool) RequiresConfirmation() bool { return false }

// ConcurrencySafe returns false.
func (t *ManageRoutineTool) ConcurrencySafe() bool { return false }

// Schema returns the JSON schema.
func (t *ManageRoutineTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"Manage routine tasks (recurring scheduled commands)",
		map[string]*interfaces.PropertySchema{
			"action": {
				Type:        "string",
				Description: "Action to perform: create, delete, list, pause, resume, or run a task immediately by ID",
				Enum:        []string{"create", "delete", "list", "pause", "resume", "run"},
			},
			"command": {
				Type:        "string",
				Description: "Command or natural language instruction to execute on schedule",
			},
			"schedule": {
				Type:        "string",
				Description: "Cron expression (e.g. '0 * * * *') or natural language (e.g. 'every 5 minutes', 'daily at 9am')",
			},
			"task_id": {
				Type:        "string",
				Description: "Task ID for delete/pause/resume/run actions",
			},
			"max_runs": {
				Type:        "integer",
				Description: "Maximum number of runs (0 = unlimited)",
			},
		},
		[]string{"action"},
	)
}

// Execute runs the routine management action.
func (t *ManageRoutineTool) Execute(_ context.Context, args map[string]interface{}) (*interfaces.ToolResult, error) {
	action, ok := args["action"].(string)
	if !ok || action == "" {
		return nil, fmt.Errorf("action is required")
	}

	switch action {
	case "list":
		return t.listTasks()
	case "create":
		return t.createTask(args)
	case "delete":
		return t.deleteTask(args)
	case "pause", "resume":
		return t.pauseResumeTask(args, action)
	case "run":
		return t.runTaskNow(args)
	default:
		return nil, fmt.Errorf("unknown action %q; valid: create, delete, list, pause, resume, run", action)
	}
}

// getScheduler returns the current scheduler under the lock.
func (t *ManageRoutineTool) getScheduler() *cron.Scheduler {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.scheduler
}

func (t *ManageRoutineTool) listTasks() (*interfaces.ToolResult, error) {
	var lines []string

	sched := t.getScheduler()
	if sched != nil {
		tasks := sched.ListTasks()
		for _, task := range tasks {
			lines = append(lines, fmt.Sprintf("- [%s] %s | schedule: %s | created: %s",
				task.ID[:8], task.Command, task.CronExpr, task.CreatedAt.Format(time.RFC3339)))
		}
	} else if t.stateStore != nil {
		tasks := t.stateStore.GetTasks()
		for _, task := range tasks {
			lines = append(lines, fmt.Sprintf("- [%s] %s | schedule: %s | source: %s",
				task.ID[:8], task.Command, task.CronExpr, task.Source))
		}
	}

	if len(lines) == 0 {
		msg := "No scheduled tasks"
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	}

	content := "Scheduled tasks:\n" + strings.Join(lines, "\n")
	return &interfaces.ToolResult{Success: true, LLMContent: content, UserContent: content}, nil
}

func (t *ManageRoutineTool) createTask(args map[string]interface{}) (*interfaces.ToolResult, error) {
	command, _ := args["command"].(string)
	if command == "" {
		return nil, fmt.Errorf("command is required for create action")
	}

	scheduleStr, _ := args["schedule"].(string)
	if scheduleStr == "" {
		return nil, fmt.Errorf("schedule is required for create action")
	}

	// Parse natural language or cron expression
	cronExpr, err := ParseNaturalSchedule(scheduleStr)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Invalid schedule %q: %v", scheduleStr, err),
			UserContent: fmt.Sprintf("Invalid schedule %q: %v", scheduleStr, err),
		}, nil
	}

	maxRuns := 0
	if mr, ok := args["max_runs"].(float64); ok {
		maxRuns = int(mr)
	}

	// Confirmation
	summary := fmt.Sprintf("Create scheduled task:\n  Command: %s\n  Schedule: %s (cron: %s)\n  Max runs: %d",
		command, scheduleStr, cronExpr, maxRuns)
	if t.confirmFn != nil && !t.confirmFn(summary) {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "Task creation cancelled by user",
			UserContent: "Task creation cancelled by user",
		}, nil
	}

	sched := t.getScheduler()
	if sched == nil {
		// No live scheduler — persist to state store only (activates on next start).
		if t.stateStore == nil {
			msg := "Cannot schedule task: no live scheduler and no state store configured"
			return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
		}
		t.stateStore.AddTask(config.PersistedTask{
			ID:       generateTaskID(),
			CronExpr: cronExpr,
			Command:  command,
			Source:   "conversation",
			MaxRuns:  maxRuns,
		})
		if err := t.stateStore.Save(); err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				LLMContent:  fmt.Sprintf("Failed to persist scheduled task: %v", err),
				UserContent: fmt.Sprintf("Failed to persist scheduled task: %v", err),
			}, nil
		}
		msg := fmt.Sprintf("Task scheduled (will activate on next TUI start): %s @ %s", command, cronExpr)
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	}

	task, err := sched.ScheduleTaskWithSource(cronExpr, command, "conversation", maxRuns)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Failed to schedule task: %v", err),
			UserContent: fmt.Sprintf("Failed to schedule task: %v", err),
		}, nil
	}

	msg := fmt.Sprintf("Task %q scheduled.\n  ID: %s\n  Cron: %s\n  Command: %s",
		task.ID[:8], task.ID, cronExpr, command)
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  msg,
		UserContent: msg,
		Data:        map[string]interface{}{"task_id": task.ID, "cron": cronExpr},
	}, nil
}

func (t *ManageRoutineTool) deleteTask(args map[string]interface{}) (*interfaces.ToolResult, error) {
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return nil, fmt.Errorf("task_id is required for delete action")
	}

	sched := t.getScheduler()
	if sched != nil {
		if err := sched.RemoveTask(taskID); err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				LLMContent:  fmt.Sprintf("Failed to delete task %q: %v", taskID, err),
				UserContent: fmt.Sprintf("Failed to delete task %q: %v", taskID, err),
			}, nil
		}
	} else if t.stateStore != nil {
		t.stateStore.RemoveTask(taskID)
		if err := t.stateStore.Save(); err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				LLMContent:  fmt.Sprintf("Failed to persist deletion of task %q: %v", taskID, err),
				UserContent: fmt.Sprintf("Failed to persist deletion of task %q: %v", taskID, err),
			}, nil
		}
	}

	msg := fmt.Sprintf("Task %q deleted", taskID)
	return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
}

func (t *ManageRoutineTool) pauseResumeTask(args map[string]interface{}, action string) (*interfaces.ToolResult, error) {
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return nil, fmt.Errorf("task_id is required for %s action", action)
	}
	// Note: the robfig/cron library does not support pausing individual tasks natively.
	// We acknowledge the intent and advise that pause/resume is achieved via delete+recreate.
	msg := fmt.Sprintf("Task %s is not yet supported in the live scheduler. To pause a task, delete it and recreate it when needed.", action)
	return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
}

func (t *ManageRoutineTool) runTaskNow(args map[string]interface{}) (*interfaces.ToolResult, error) {
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return nil, fmt.Errorf("task_id is required for run action")
	}

	sched := t.getScheduler()
	if sched == nil {
		msg := "Cannot run task: no live scheduler available"
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}

	// Pre-check existence to provide better error messages
	tasks := sched.ListTasks()
	var found bool
	for _, task := range tasks {
		if task.ID == taskID {
			found = true
			break
		}
	}
	if !found {
		msg := fmt.Sprintf("Task %q not found", taskID)
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}

	// Trigger async to avoid blocking the tool call
	go sched.RunTaskNow(taskID) //nolint:errcheck

	msg := fmt.Sprintf("Task %q triggered for immediate execution", taskID)
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  msg,
		UserContent: msg,
		Data:        map[string]interface{}{"trigger": "manual", "task_id": taskID},
	}, nil
}

// generateTaskID generates a simple unique ID using timestamp.
func generateTaskID() string {
	return fmt.Sprintf("conv-%d", time.Now().UnixNano())
}
