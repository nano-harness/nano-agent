package task

import (
	"context"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/swarm"
)

// StopTool cancels an async agent.
type StopTool struct{}

// NewStopTool creates a new TaskStop tool.
func NewStopTool() *StopTool {
	return &StopTool{}
}

func (t *StopTool) Name() string {
	return "TaskStop"
}

func (t *StopTool) Description() string {
	return "Cancel a running background agent."
}

func (t *StopTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryAgent
}

func (t *StopTool) RequiresConfirmation() bool {
	return false
}

func (t *StopTool) ConcurrencySafe() bool {
	return true
}

func (t *StopTool) Schema() *interfaces.ToolSchema {
	agentIDProp := interfaces.NewStringProperty("The agent_id of the background agent to stop.")

	return interfaces.CreateSchema(
		t.Description(),
		map[string]*interfaces.PropertySchema{
			"agent_id": agentIDProp,
		},
		[]string{"agent_id"},
	)
}

func (t *StopTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	agentID, _ := params["agent_id"].(string)
	if agentID == "" {
		return &interfaces.ToolResult{
			Success:    false,
			Error:      "agent_id is required",
			LLMContent: "TaskStop failed: agent_id parameter is required.",
		}, nil
	}

	// Cancel the agent via lifecycle registry
	cancelled := swarm.CancelByAgentID(agentID)
	if !cancelled {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("agent %s not found or already stopped", agentID),
			Metadata: map[string]interface{}{
				"status":   "not_found",
				"agent_id": agentID,
			},
			LLMContent: fmt.Sprintf("Agent %s not found or already stopped.", agentID),
		}, nil
	}

	return &interfaces.ToolResult{
		Success: true,
		Metadata: map[string]interface{}{
			"status":   "stopped",
			"agent_id": agentID,
		},
		LLMContent: fmt.Sprintf("Agent %s has been stopped.", agentID),
	}, nil
}
