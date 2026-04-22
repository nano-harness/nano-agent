package system

import (
	"context"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// KillBashTool terminates background tasks
type KillBashTool struct {
	bgManager *BackgroundTaskManager
}

// NewKillBashTool creates a new kill bash tool
func NewKillBashTool(bgManager *BackgroundTaskManager) *KillBashTool {
	return &KillBashTool{bgManager: bgManager}
}

func (t *KillBashTool) Name() string {
	return "kill_bash"
}

func (t *KillBashTool) Description() string {
	return "Terminate a background shell task"
}

func (t *KillBashTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryShell
}

func (t *KillBashTool) RequiresConfirmation() bool {
	return false
}

func (t *KillBashTool) ConcurrencySafe() bool {
	return false // Killing processes is not safe to do concurrently
}

func (t *KillBashTool) Schema() *interfaces.ToolSchema {
	taskIDProp := interfaces.NewStringProperty("Task ID of the background task to terminate")
	taskIDProp.Examples = []string{"a1b2c3d4", "9f8e7d6c"}
	taskIDProp.Usage = "Task ID returned when starting a background task. Sends SIGTERM, then SIGKILL after 5s grace period."

	return interfaces.CreateSchema(
		"Terminate a background shell task",
		map[string]*interfaces.PropertySchema{
			"task_id": taskIDProp,
		},
		[]string{"task_id"},
	)
}

func (t *KillBashTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	taskID, ok := params["task_id"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "task_id parameter is required",
			UserContent: "❌ task_id parameter is required",
			LLMContent:  "kill_bash failed: task_id parameter is required",
		}, nil
	}

	err := t.bgManager.Kill(taskID)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("Failed to kill task: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to kill task: %v", err),
			LLMContent:  fmt.Sprintf("kill_bash failed: %v", err),
		}, nil
	}

	content := fmt.Sprintf("✅ Background task %s terminated (SIGTERM sent, SIGKILL after 5s if needed)", taskID)

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: content,
		LLMContent:  content,
		Metadata: map[string]interface{}{
			"task_id": taskID,
		},
	}, nil
}
