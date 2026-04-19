package agent

import (
	"context"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// ForkTool allows the main agent to fork a child agent for a sub-task.
type ForkTool struct {
	forkManager *ForkManager
}

// NewForkTool creates a new ForkTool.
func NewForkTool(fm *ForkManager) *ForkTool {
	return &ForkTool{forkManager: fm}
}

// Name returns the tool name.
func (t *ForkTool) Name() string { return "fork" }

// Description returns the tool description.
func (t *ForkTool) Description() string {
	return "Fork a child agent to execute a sub-task. Choose agent_type based on the task: " +
		"'explore' for read-only codebase exploration, 'plan' for structured planning, " +
		"'verify' for adversarial review, 'execute' (default) for general tasks with full tool access."
}

// Category returns the tool category.
func (t *ForkTool) Category() interfaces.ToolCategory { return interfaces.CategoryAgent }

// RequiresConfirmation returns false.
func (t *ForkTool) RequiresConfirmation() bool { return false }

// ConcurrencySafe returns false.
func (t *ForkTool) ConcurrencySafe() bool { return false }

// Schema returns the JSON schema.
func (t *ForkTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"Fork a child agent to execute a sub-task",
		map[string]*interfaces.PropertySchema{
			"task": {
				Type:        "string",
				Description: "The task for the child agent to execute",
			},
			"agent_type": {
				Type:        "string",
				Description: "Type of agent: explore, plan, verify, execute (default: execute)",
				Enum:        []string{"explore", "plan", "verify", "execute"},
				Default:     "execute",
			},
		},
		[]string{"task"},
	)
}

// Execute runs the fork tool.
func (t *ForkTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	task, _ := params["task"].(string)
	if task == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "task parameter is required",
		}, nil
	}

	agentTypeStr, _ := params["agent_type"].(string)
	if agentTypeStr == "" {
		agentTypeStr = "execute"
	}
	agentType := AgentType(agentTypeStr)

	result, err := t.forkManager.Fork(ctx, ForkConfig{
		AgentType: agentType,
		Task:      task,
	})
	if err != nil {
		errMsg := fmt.Sprintf("fork failed: %v", err)
		if result != nil && result.Output != "" {
			errMsg += "\nPartial output:\n" + result.Output
		}
		return &interfaces.ToolResult{
			Success:     false,
			Error:       errMsg,
			LLMContent:  errMsg,
			UserContent: errMsg,
		}, nil
	}

	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  result.Output,
		UserContent: result.Output,
	}, nil
}
