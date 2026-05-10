package system

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// ListBackgroundTool lists background tasks
type ListBackgroundTool struct {
	bgManager *BackgroundTaskManager
}

// NewListBackgroundTool creates a new list background tool
func NewListBackgroundTool(bgManager *BackgroundTaskManager) *ListBackgroundTool {
	return &ListBackgroundTool{bgManager: bgManager}
}

func (t *ListBackgroundTool) Name() string {
	return "list_background_tasks"
}

func (t *ListBackgroundTool) Description() string {
	return "List all background shell tasks for the current session"
}

func (t *ListBackgroundTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryShell
}

func (t *ListBackgroundTool) RequiresConfirmation() bool {
	return false
}

func (t *ListBackgroundTool) ConcurrencySafe() bool {
	return true // Listing is safe to do concurrently
}

func (t *ListBackgroundTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"List all background shell tasks for the current session",
		map[string]*interfaces.PropertySchema{},
		[]string{},
	)
}

func (t *ListBackgroundTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// Get session ID from Turn context (if available).
	sessionID := "default"
	if tc, ok := ctx.Value(interfaces.TurnContextKey{}).(interfaces.TurnContext); ok && tc.SessionID != "" {
		sessionID = tc.SessionID
	}

	tasks := t.bgManager.List(sessionID)

	if len(tasks) == 0 {
		content := "No background tasks found for this session."
		return &interfaces.ToolResult{
			Success:     true,
			UserContent: content,
			LLMContent:  content,
			Metadata: map[string]interface{}{
				"task_count": 0,
			},
		}, nil
	}

	// Format as table
	var output strings.Builder
	output.WriteString(fmt.Sprintf("Background Tasks (%d):\n", len(tasks)))
	output.WriteString("─────────────────────────────────────────────────────────────────────\n")
	output.WriteString(fmt.Sprintf("%-10s %-12s %-8s %-20s %s\n", "TASK_ID", "STATUS", "EXIT", "STARTED", "COMMAND"))
	output.WriteString("─────────────────────────────────────────────────────────────────────\n")

	for _, task := range tasks {
		status, taskExitCode, finishedAt := task.snapshot()
		exitCode := "-"
		if finishedAt != nil {
			exitCode = fmt.Sprintf("%d", taskExitCode)
		}

		started := task.StartedAt.Format("01-02 15:04:05")

		// Truncate command if too long
		cmd := task.Command
		if len(cmd) > 40 {
			cmd = cmd[:37] + "..."
		}

		output.WriteString(fmt.Sprintf("%-10s %-12s %-8s %-20s %s\n",
			task.ID, status, exitCode, started, cmd))
	}

	content := output.String()

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: content,
		LLMContent:  content,
		Metadata: map[string]interface{}{
			"task_count": len(tasks),
			"session_id": sessionID,
		},
	}, nil
}
