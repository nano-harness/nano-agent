// Package openspec provides OpenSpec tool implementations for the nano-agent toolbox.
package openspec

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/openspec"
)

// StatusTool provides the opsx_status tool for querying OpenSpec change status.
type StatusTool struct {
	engine *openspec.WorkflowEngine
}

// NewStatusTool creates a new StatusTool.
func NewStatusTool(engine *openspec.WorkflowEngine) *StatusTool {
	return &StatusTool{engine: engine}
}

func (t *StatusTool) Name() string { return "opsx_status" }
func (t *StatusTool) Description() string {
	return "Get the status of OpenSpec changes, including artifact completion and task progress."
}
func (t *StatusTool) RequiresConfirmation() bool        { return false }
func (t *StatusTool) Category() interfaces.ToolCategory { return interfaces.CategoryOpenSpec }
func (t *StatusTool) ConcurrencySafe() bool             { return true }

func (t *StatusTool) Schema() *interfaces.ToolSchema {
	return &interfaces.ToolSchema{
		Type: "object",
		Properties: map[string]*interfaces.PropertySchema{
			"change_name": {
				Type:        "string",
				Description: "Name of the change to check status for. If empty, lists all active changes.",
			},
		},
		Required: []string{},
	}
}

func (t *StatusTool) Execute(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	changeName, _ := params["change_name"].(string)

	cmd := &openspec.Command{
		Type:       openspec.CommandStatus,
		ChangeName: changeName,
	}

	result, err := t.engine.HandleCommand(cmd)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &interfaces.ToolResult{
		Success:     true,
		Data:        result.StatusMessage,
		LLMContent:  result.StatusMessage,
		UserContent: result.StatusMessage,
	}, nil
}

// ReadArtifactTool provides the opsx_read_artifact tool for reading OpenSpec artifacts.
type ReadArtifactTool struct {
	engine *openspec.WorkflowEngine
}

// NewReadArtifactTool creates a new ReadArtifactTool.
func NewReadArtifactTool(engine *openspec.WorkflowEngine) *ReadArtifactTool {
	return &ReadArtifactTool{engine: engine}
}

func (t *ReadArtifactTool) Name() string { return "opsx_read_artifact" }
func (t *ReadArtifactTool) Description() string {
	return "Read the content of an OpenSpec artifact (proposal, specs, design, or tasks) from a change."
}
func (t *ReadArtifactTool) RequiresConfirmation() bool        { return false }
func (t *ReadArtifactTool) Category() interfaces.ToolCategory { return interfaces.CategoryOpenSpec }
func (t *ReadArtifactTool) ConcurrencySafe() bool             { return true }

func (t *ReadArtifactTool) Schema() *interfaces.ToolSchema {
	return &interfaces.ToolSchema{
		Type: "object",
		Properties: map[string]*interfaces.PropertySchema{
			"change_name": {
				Type:        "string",
				Description: "Name of the change.",
			},
			"artifact_id": {
				Type:        "string",
				Description: "Artifact type to read: proposal, specs, design, or tasks.",
				Enum:        []string{"proposal", "specs", "design", "tasks"},
			},
		},
		Required: []string{"change_name", "artifact_id"},
	}
}

func (t *ReadArtifactTool) Execute(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	changeName, _ := params["change_name"].(string)
	artifactID, _ := params["artifact_id"].(string)

	if changeName == "" || artifactID == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "change_name and artifact_id are required",
		}, nil
	}

	content, err := t.engine.Manager().ReadArtifact(changeName, artifactID)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to read artifact: %v", err),
		}, nil
	}

	return &interfaces.ToolResult{
		Success:     true,
		Data:        content,
		LLMContent:  content,
		UserContent: fmt.Sprintf("Read %s artifact from %s (%d bytes)", artifactID, changeName, len(content)),
	}, nil
}

// WriteArtifactTool provides the opsx_write_artifact tool for writing OpenSpec artifacts.
type WriteArtifactTool struct {
	engine *openspec.WorkflowEngine
}

// NewWriteArtifactTool creates a new WriteArtifactTool.
func NewWriteArtifactTool(engine *openspec.WorkflowEngine) *WriteArtifactTool {
	return &WriteArtifactTool{engine: engine}
}

func (t *WriteArtifactTool) Name() string { return "opsx_write_artifact" }
func (t *WriteArtifactTool) Description() string {
	return "Write content to an OpenSpec artifact file (proposal, specs, design, or tasks) within a change."
}
func (t *WriteArtifactTool) RequiresConfirmation() bool        { return false }
func (t *WriteArtifactTool) Category() interfaces.ToolCategory { return interfaces.CategoryOpenSpec }
func (t *WriteArtifactTool) ConcurrencySafe() bool             { return false }

func (t *WriteArtifactTool) Schema() *interfaces.ToolSchema {
	return &interfaces.ToolSchema{
		Type: "object",
		Properties: map[string]*interfaces.PropertySchema{
			"change_name": {
				Type:        "string",
				Description: "Name of the change.",
			},
			"artifact_id": {
				Type:        "string",
				Description: "Artifact type to write: proposal, specs, design, or tasks.",
				Enum:        []string{"proposal", "specs", "design", "tasks"},
			},
			"content": {
				Type:        "string",
				Description: "Markdown content to write to the artifact file.",
			},
		},
		Required: []string{"change_name", "artifact_id", "content"},
	}
}

func (t *WriteArtifactTool) Execute(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	changeName, _ := params["change_name"].(string)
	artifactID, _ := params["artifact_id"].(string)
	content, _ := params["content"].(string)

	if changeName == "" || artifactID == "" || content == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "change_name, artifact_id, and content are required",
		}, nil
	}

	if err := t.engine.Manager().WriteArtifact(changeName, artifactID, content); err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to write artifact: %v", err),
		}, nil
	}

	return &interfaces.ToolResult{
		Success:     true,
		Data:        fmt.Sprintf("Written %s artifact to %s (%d bytes)", artifactID, changeName, len(content)),
		LLMContent:  fmt.Sprintf("Successfully wrote %s for change %s", artifactID, changeName),
		UserContent: fmt.Sprintf("✓ Wrote %s artifact (%d bytes)", artifactID, len(content)),
	}, nil
}

// UpdateTaskTool provides the opsx_update_task tool for marking tasks complete/incomplete.
type UpdateTaskTool struct {
	engine *openspec.WorkflowEngine
}

// NewUpdateTaskTool creates a new UpdateTaskTool.
func NewUpdateTaskTool(engine *openspec.WorkflowEngine) *UpdateTaskTool {
	return &UpdateTaskTool{engine: engine}
}

func (t *UpdateTaskTool) Name() string { return "opsx_update_task" }
func (t *UpdateTaskTool) Description() string {
	return "Mark an OpenSpec task as complete or incomplete by updating the checkbox in tasks.md."
}
func (t *UpdateTaskTool) RequiresConfirmation() bool        { return false }
func (t *UpdateTaskTool) Category() interfaces.ToolCategory { return interfaces.CategoryOpenSpec }
func (t *UpdateTaskTool) ConcurrencySafe() bool             { return false }

func (t *UpdateTaskTool) Schema() *interfaces.ToolSchema {
	return &interfaces.ToolSchema{
		Type: "object",
		Properties: map[string]*interfaces.PropertySchema{
			"change_name": {
				Type:        "string",
				Description: "Name of the change.",
			},
			"task_id": {
				Type:        "string",
				Description: "Task ID to update (e.g., '1.1', '2.3').",
			},
			"complete": {
				Type:        "boolean",
				Description: "Set to true to mark complete, false to mark incomplete.",
			},
		},
		Required: []string{"change_name", "task_id", "complete"},
	}
}

func (t *UpdateTaskTool) Execute(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	changeName, _ := params["change_name"].(string)
	taskID, _ := params["task_id"].(string)
	complete, _ := params["complete"].(bool)

	if changeName == "" || taskID == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "change_name and task_id are required",
		}, nil
	}

	content, err := t.engine.Manager().ReadArtifact(changeName, "tasks")
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to read tasks.md: %v", err),
		}, nil
	}

	newStatus := openspec.TaskStatusPending
	if complete {
		newStatus = openspec.TaskStatusComplete
	}

	updated := openspec.UpdateTaskStatus(content, taskID, newStatus)
	if err := t.engine.Manager().WriteArtifact(changeName, "tasks", updated); err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to write tasks.md: %v", err),
		}, nil
	}

	statusStr := "incomplete"
	if complete {
		statusStr = "complete"
	}

	msg := fmt.Sprintf("Task %s marked as %s in %s", taskID, statusStr, changeName)
	return &interfaces.ToolResult{
		Success:     true,
		Data:        msg,
		LLMContent:  msg,
		UserContent: fmt.Sprintf("✓ Task %s → %s", taskID, statusStr),
	}, nil
}

// ListChangesTool provides the opsx_list_changes tool for listing active changes.
type ListChangesTool struct {
	engine *openspec.WorkflowEngine
}

// NewListChangesTool creates a new ListChangesTool.
func NewListChangesTool(engine *openspec.WorkflowEngine) *ListChangesTool {
	return &ListChangesTool{engine: engine}
}

func (t *ListChangesTool) Name() string { return "opsx_list_changes" }
func (t *ListChangesTool) Description() string {
	return "List all active (non-archived) OpenSpec changes."
}
func (t *ListChangesTool) RequiresConfirmation() bool        { return false }
func (t *ListChangesTool) Category() interfaces.ToolCategory { return interfaces.CategoryOpenSpec }
func (t *ListChangesTool) ConcurrencySafe() bool             { return true }

func (t *ListChangesTool) Schema() *interfaces.ToolSchema {
	return &interfaces.ToolSchema{
		Type:       "object",
		Properties: map[string]*interfaces.PropertySchema{},
		Required:   []string{},
	}
}

func (t *ListChangesTool) Execute(_ context.Context, _ map[string]interface{}) (*interfaces.ToolResult, error) {
	changes, err := t.engine.Manager().ListChanges()
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to list changes: %v", err),
		}, nil
	}

	if len(changes) == 0 {
		return &interfaces.ToolResult{
			Success:     true,
			Data:        "No active changes",
			LLMContent:  "No active OpenSpec changes found.",
			UserContent: "No active changes",
		}, nil
	}

	msg := fmt.Sprintf("Active changes: %s", strings.Join(changes, ", "))
	return &interfaces.ToolResult{
		Success:     true,
		Data:        changes,
		LLMContent:  msg,
		UserContent: msg,
	}, nil
}

// RegisterOpenSpecTools registers all OpenSpec tools with the given registry.
func RegisterOpenSpecTools(registry interfaces.ToolRegistry, engine *openspec.WorkflowEngine) {
	tools := []interfaces.Tool{
		NewStatusTool(engine),
		NewReadArtifactTool(engine),
		NewWriteArtifactTool(engine),
		NewUpdateTaskTool(engine),
		NewListChangesTool(engine),
	}

	for _, tool := range tools {
		if err := registry.Register(tool); err != nil {
			logger.Warnf("Failed to register OpenSpec tool %s: %v", tool.Name(), err)
		}
	}
}
