package builtin

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestManageScheduleTool_List_Empty(t *testing.T) {
	tool := NewManageScheduleTool(nil, nil, nil)
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

func TestManageScheduleTool_Create_NaturalLanguage(t *testing.T) {
	dir := t.TempDir()
	ss := config.NewStateStore(dir + "/state.json")
	_ = ss.Load()

	tool := NewManageScheduleTool(nil, ss, nil) // auto-confirm

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

func TestManageScheduleTool_Create_Cron(t *testing.T) {
	dir := t.TempDir()
	ss := config.NewStateStore(dir + "/state.json")
	_ = ss.Load()

	tool := NewManageScheduleTool(nil, ss, nil)

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

func TestManageScheduleTool_Create_NilSchedulerAndStore(t *testing.T) {
	tool := NewManageScheduleTool(nil, nil, nil)

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

func TestManageScheduleTool_Create_Cancelled(t *testing.T) {
	tool := NewManageScheduleTool(nil, nil, func(_ string) bool { return false })

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

func TestManageScheduleTool_Create_MissingCommand(t *testing.T) {
	tool := NewManageScheduleTool(nil, nil, nil)
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":   "create",
		"schedule": "every hour",
	})
	if err == nil {
		t.Error("expected error for missing command")
	}
}

func TestManageScheduleTool_Delete(t *testing.T) {
	dir := t.TempDir()
	ss := config.NewStateStore(dir + "/state.json")
	_ = ss.Load()
	ss.AddTask(config.PersistedTask{ID: "task-1", CronExpr: "* * * * *", Command: "x"})
	_ = ss.Save()

	tool := NewManageScheduleTool(nil, ss, nil)
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

func TestManageScheduleTool_UnknownAction(t *testing.T) {
	tool := NewManageScheduleTool(nil, nil, nil)
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "unknown",
	})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}
