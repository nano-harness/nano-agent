package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/cron"
)

func TestManageRoutineTool_List_Empty(t *testing.T) {
	tool := NewManageRoutineTool(nil, nil, nil)
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Errorf("expected success: %s", result.LLMContent)
	}
}

func TestManageRoutineTool_Create_NaturalLanguage(t *testing.T) {
	dir := t.TempDir()
	ss := config.NewStateStore(dir + "/state.json")
	_ = ss.Load()

	tool := NewManageRoutineTool(nil, ss, nil) // auto-confirm

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":   "create",
		"command":  "run tests",
		"schedule": "every 5 minutes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Errorf("expected success: %s", result.LLMContent)
	}

	tasks := ss.GetTasks()
	if len(tasks) != 1 {
		t.Errorf("expected 1 persisted task, got %d", len(tasks))
	}
	if tasks[0].CronExpr != "0 */5 * * * *" {
		t.Errorf("expected cron 0 */5 * * * *, got %q", tasks[0].CronExpr)
	}
}

func TestManageRoutineTool_Create_Cron(t *testing.T) {
	dir := t.TempDir()
	ss := config.NewStateStore(dir + "/state.json")
	_ = ss.Load()

	tool := NewManageRoutineTool(nil, ss, nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":   "create",
		"command":  "check status",
		"schedule": "0 9 * * *",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Errorf("expected success: %s", result.LLMContent)
	}
	tasks := ss.GetTasks()
	if len(tasks) != 1 {
		t.Errorf("expected 1 persisted task, got %d", len(tasks))
	}
	// 5-field "0 9 * * *" should be normalised to 6-field "0 0 9 * * *"
	if tasks[0].CronExpr != "0 0 9 * * *" {
		t.Errorf("expected cron 0 0 9 * * *, got %q", tasks[0].CronExpr)
	}
}

func TestManageRoutineTool_Create_NilSchedulerAndStore(t *testing.T) {
	tool := NewManageRoutineTool(nil, nil, nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":   "create",
		"command":  "check status",
		"schedule": "every hour",
	})
	if err != nil {
		t.Fatal(err)
	}
	// When both scheduler and stateStore are nil, task cannot be created.
	if result.Success {
		t.Error("expected failure when both scheduler and stateStore are nil")
	}
}

func TestManageRoutineTool_Create_Cancelled(t *testing.T) {
	tool := NewManageRoutineTool(nil, nil, func(_ string) bool { return false })

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":   "create",
		"command":  "run tests",
		"schedule": "every hour",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Error("expected failure when user cancels")
	}
}

func TestManageRoutineTool_Create_MissingCommand(t *testing.T) {
	tool := NewManageRoutineTool(nil, nil, nil)
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":   "create",
		"schedule": "every hour",
	})
	if err == nil {
		t.Error("expected error for missing command")
	}
}

func TestManageRoutineTool_Delete(t *testing.T) {
	dir := t.TempDir()
	ss := config.NewStateStore(dir + "/state.json")
	_ = ss.Load()
	ss.AddTask(config.PersistedTask{ID: "task-1", CronExpr: "* * * * *", Command: "x"})
	_ = ss.Save()

	tool := NewManageRoutineTool(nil, ss, nil)
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":  "delete",
		"task_id": "task-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Errorf("expected success: %s", result.LLMContent)
	}
	if len(ss.GetTasks()) != 0 {
		t.Error("expected task to be deleted from state store")
	}
}

func TestManageRoutineTool_UnknownAction(t *testing.T) {
	tool := NewManageRoutineTool(nil, nil, nil)
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "unknown",
	})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestManageRoutineTool_RunAction_Success(t *testing.T) {
	dir := t.TempDir()
	ss := config.NewStateStore(dir + "/state.json")
	_ = ss.Load()

	// Create a cron scheduler
	sched := cron.New(nil)
	sched.Start()
	defer sched.Stop()

	tool := NewManageRoutineTool(sched, ss, nil)

	// First create a task
	createResult, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":   "create",
		"command":  "test command",
		"schedule": "0 0 * * * *",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !createResult.Success {
		t.Errorf("expected success: %s", createResult.LLMContent)
	}

	// Get the task ID
	taskID, ok := createResult.Data.(map[string]interface{})["task_id"].(string)
	if !ok {
		t.Fatal("expected task_id in createResult.Data")
	}

	// Run the task
	runResult, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":  "run",
		"task_id": taskID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !runResult.Success {
		t.Errorf("expected success: %s", runResult.LLMContent)
	}
	runData, ok := runResult.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected map in runResult.Data")
	}
	if runData["trigger"] != "manual" {
		t.Errorf("expected trigger='manual', got %v", runData["trigger"])
	}
}

func TestManageRoutineTool_RunAction_MissingTaskID(t *testing.T) {
	tool := NewManageRoutineTool(nil, nil, nil)
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "run",
	})
	if err == nil {
		t.Error("expected error for missing task_id")
	}
}

func TestManageRoutineTool_RunAction_TaskNotFound(t *testing.T) {
	sched := cron.New(nil)
	sched.Start()
	defer sched.Stop()

	tool := NewManageRoutineTool(sched, nil, nil)
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":  "run",
		"task_id": "nonexistent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Error("expected failure for nonexistent task")
	}
	if !strings.Contains(result.LLMContent, "not found") {
		t.Errorf("expected 'not found' in message, got %q", result.LLMContent)
	}
}

func TestManageRoutineTool_RunAction_NoScheduler(t *testing.T) {
	tool := NewManageRoutineTool(nil, nil, nil)
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":  "run",
		"task_id": "any-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Error("expected failure when scheduler is nil")
	}
	if !strings.Contains(result.LLMContent, "no live scheduler") {
		t.Errorf("expected 'no live scheduler' in message, got %q", result.LLMContent)
	}
}
