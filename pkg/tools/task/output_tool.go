package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/swarm"
)

// OutputTool reads async agent transcript or blocks until output is available.
type OutputTool struct{}

// NewOutputTool creates a new TaskOutput tool.
func NewOutputTool() *OutputTool {
	return &OutputTool{}
}

func (t *OutputTool) Name() string {
	return "TaskOutput"
}

func (t *OutputTool) Description() string {
	return "Read the output of a background agent. Returns transcript content if available, or blocks briefly waiting for output."
}

func (t *OutputTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryAgent
}

func (t *OutputTool) RequiresConfirmation() bool {
	return false
}

func (t *OutputTool) ConcurrencySafe() bool {
	return true
}

func (t *OutputTool) Schema() *interfaces.ToolSchema {
	agentIDProp := interfaces.NewStringProperty("The agent_id returned by Agent tool when run_in_background=true.")
	waitProp := interfaces.NewBooleanProperty("If true, block up to 30s waiting for output. Default: false.")

	return interfaces.CreateSchema(
		t.Description(),
		map[string]*interfaces.PropertySchema{
			"agent_id": agentIDProp,
			"wait":     waitProp,
		},
		[]string{"agent_id"},
	)
}

func (t *OutputTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	agentID, _ := params["agent_id"].(string)
	if agentID == "" {
		return &interfaces.ToolResult{
			Success:    false,
			Error:      "agent_id is required",
			LLMContent: "TaskOutput failed: agent_id parameter is required.",
		}, nil
	}

	// Try reading transcript directly
	return t.readTranscript(agentID)
}

func (t *OutputTool) readTranscript(agentID string) (*interfaces.ToolResult, error) {
	// Try common output directories
	outputDirs := []string{"/tmp/nano-agent-output", ".nano/output"}
	for _, dir := range outputDirs {
		path := swarm.TranscriptPath(dir, agentID)
		entries, err := swarm.ReadTranscript(path)
		if err == nil && len(entries) > 0 {
			// Format transcript
			var content strings.Builder
			for _, e := range entries {
				if e.Role == "assistant" && e.Content != "" {
					content.WriteString(e.Content)
					content.WriteString("\n")
				}
			}
			return &interfaces.ToolResult{
				Success: true,
				Data:    content.String(),
				Metadata: map[string]interface{}{
					"status":   "completed",
					"agent_id": agentID,
					"entries":  len(entries),
				},
				LLMContent: content.String(),
			}, nil
		}
	}

	return &interfaces.ToolResult{
		Success: false,
		Error:   fmt.Sprintf("no transcript found for agent %s", agentID),
		Metadata: map[string]interface{}{
			"status":   "not_found",
			"agent_id": agentID,
		},
		LLMContent: fmt.Sprintf("No output found for agent %s. It may not have started or the output was cleaned up.", agentID),
	}, nil
}
