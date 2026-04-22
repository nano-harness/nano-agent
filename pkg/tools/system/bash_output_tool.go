package system

import (
	"context"
	"fmt"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// BashOutputTool retrieves output from background tasks
type BashOutputTool struct {
	bgManager *BackgroundTaskManager
}

// NewBashOutputTool creates a new bash output tool
func NewBashOutputTool(bgManager *BackgroundTaskManager) *BashOutputTool {
	return &BashOutputTool{bgManager: bgManager}
}

func (t *BashOutputTool) Name() string {
	return "bash_output"
}

func (t *BashOutputTool) Description() string {
	return "Retrieve output from a background shell task"
}

func (t *BashOutputTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryShell
}

func (t *BashOutputTool) RequiresConfirmation() bool {
	return false
}

func (t *BashOutputTool) ConcurrencySafe() bool {
	return true // Reading output is safe to do concurrently
}

func (t *BashOutputTool) Schema() *interfaces.ToolSchema {
	taskIDProp := interfaces.NewStringProperty("Task ID of the background task")
	taskIDProp.Examples = []string{"a1b2c3d4", "9f8e7d6c"}
	taskIDProp.Usage = "Task ID returned when starting a background task"

	fromOffsetProp := interfaces.NewNumberProperty("Byte offset to start reading from (default: 0)")
	fromOffsetProp.Examples = []string{"0", "1024", "4096"}
	fromOffsetProp.Usage = "Use to read incrementally. Track offset between calls to get only new output."

	blockProp := interfaces.NewBooleanProperty("Block and wait for new output (default: false)")
	blockProp.Examples = []string{"true", "false"}
	blockProp.Usage = "If true, waits up to 10 seconds for new output. Useful for monitoring running tasks."

	maxLinesProp := interfaces.NewNumberProperty("Maximum number of lines to return (default: 200)")
	maxLinesProp.Examples = []string{"50", "100", "200", "500"}
	maxLinesProp.Usage = "Returns last N lines of output. Set to 0 for all output."

	return interfaces.CreateSchema(
		"Retrieve output from a background shell task",
		map[string]*interfaces.PropertySchema{
			"task_id":     taskIDProp,
			"from_offset": fromOffsetProp,
			"block":       blockProp,
			"max_lines":   maxLinesProp,
		},
		[]string{"task_id"},
	)
}

func (t *BashOutputTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	taskID, ok := params["task_id"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "task_id parameter is required",
			UserContent: "❌ task_id parameter is required",
			LLMContent:  "bash_output failed: task_id parameter is required",
		}, nil
	}

	fromOffset := int64(0)
	if offsetParam, ok := params["from_offset"].(float64); ok {
		fromOffset = int64(offsetParam)
	}

	block := false
	if blockParam, ok := params["block"].(bool); ok {
		block = blockParam
	}

	maxLines := 200
	if linesParam, ok := params["max_lines"].(float64); ok {
		maxLines = int(linesParam)
	}

	// Determine block timeout
	blockTimeout := time.Duration(0)
	if block {
		blockTimeout = 10 * time.Second // 10 seconds
	}

	content, newOffset, status, err := t.bgManager.ReadOutput(taskID, fromOffset, blockTimeout, maxLines)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("Failed to read output: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to read task output: %v", err),
			LLMContent:  fmt.Sprintf("bash_output failed: %v", err),
		}, nil
	}

	header := fmt.Sprintf("[Task %s | Status: %s | Offset: %d]\n", taskID, status, newOffset)
	output := header + content

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: output,
		LLMContent:  output,
		Metadata: map[string]interface{}{
			"task_id":    taskID,
			"status":     string(status),
			"new_offset": newOffset,
			"lines":      len(splitLines(content)),
		},
	}, nil
}
