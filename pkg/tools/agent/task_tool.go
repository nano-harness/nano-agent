package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// TaskTool implements the task dispatching tool for parallel sub-agent execution
type TaskTool struct {
	cfg       *config.Config
	mainAgent *agent.Agent
}

// NewTaskTool creates a new task dispatching tool
func NewTaskTool(cfg *config.Config, mainAgent *agent.Agent) *TaskTool {
	return &TaskTool{
		cfg:       cfg,
		mainAgent: mainAgent,
	}
}

func (t *TaskTool) Name() string {
	return "task"
}

func (t *TaskTool) Description() string {
	return "Dispatch one or more sub-agents in parallel for independent research/exploration/modification tasks"
}

func (t *TaskTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryBuild // Agent orchestration fits Build category
}

func (t *TaskTool) Schema() *interfaces.ToolSchema {
	// For complex nested schemas with array of objects, we rely on Parameters() method
	// This is a simplified schema for tool discovery/documentation
	tasksArrayProp := interfaces.NewArrayProperty("Batch mode: array of tasks to dispatch in parallel", "object")
	descProp := interfaces.NewStringProperty("Single-task mode: Short description")
	promptProp := interfaces.NewStringProperty("Single-task mode: Complete prompt")
	typeProp := interfaces.NewStringProperty("Single-task mode: Sub-agent type")

	return interfaces.CreateSchema(
		t.Description(),
		map[string]*interfaces.PropertySchema{
			"tasks":         tasksArrayProp,
			"description":   descProp,
			"prompt":        promptProp,
			"subagent_type": typeProp,
		},
		[]string{}, // No required fields - either tasks or (description+prompt+subagent_type)
	)
}

func (t *TaskTool) Parameters() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tasks": map[string]interface{}{
				"type":        "array",
				"description": "Batch mode: array of tasks to dispatch in parallel. Prefer this for multiple independent tasks.",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Short description of this task (for logging/UI, 3-10 words)",
						},
						"prompt": map[string]interface{}{
							"type":        "string",
							"description": "Complete, self-contained prompt for the sub-agent. MUST include all context (file paths, constraints, exact info needed) as sub-agent has ZERO access to your conversation history.",
						},
						"subagent_type": map[string]interface{}{
							"type":        "string",
							"description": "Sub-agent type: expert name (from Available Experts) or built-in type (explore/plan/execute/verify)",
						},
					},
					"required": []string{"description", "prompt", "subagent_type"},
				},
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Single-task mode: Short description (3-10 words). Only used if 'tasks' array is not provided.",
			},
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "Single-task mode: Complete prompt. Only used if 'tasks' array is not provided.",
			},
			"subagent_type": map[string]interface{}{
				"type":        "string",
				"description": "Single-task mode: Sub-agent type. Only used if 'tasks' array is not provided.",
			},
		},
	}

	data, _ := json.Marshal(schema)
	return data
}

func (t *TaskTool) RequiresConfirmation() bool {
	return false
}

func (t *TaskTool) ConcurrencySafe() bool {
	return true
}

// taskSpec represents a single task specification
type taskSpec struct {
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`        // New name
	SubAgentType string `json:"subagent_type"` // New name
	// Legacy field names for backward compatibility
	Task      string `json:"task"`       // Legacy: same as Prompt
	AgentType string `json:"agent_type"` // Legacy: same as SubAgentType
}

// normalize ensures Prompt and SubAgentType are populated from legacy fields if needed
func (ts *taskSpec) normalize() {
	if ts.Prompt == "" && ts.Task != "" {
		ts.Prompt = ts.Task
	}
	if ts.SubAgentType == "" && ts.AgentType != "" {
		ts.SubAgentType = ts.AgentType
	}
}

func (t *TaskTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// Check if forkManager is available
	forkManager := t.mainAgent.GetForkManager()
	if forkManager == nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "fork manager not available (sub-agent cannot spawn further sub-agents)",
		}, nil
	}

	expertRegistry := t.mainAgent.GetExpertRegistry()

	// Parse parameters - check for batch mode first
	var tasks []taskSpec
	if tasksArray, ok := params["tasks"]; ok {
		// Batch mode
		tasksJSON, err := json.Marshal(tasksArray)
		if err != nil {
			return &interfaces.ToolResult{
				Success: false,
				Error:   fmt.Sprintf("failed to parse tasks array: %v", err),
			}, nil
		}
		if err := json.Unmarshal(tasksJSON, &tasks); err != nil {
			return &interfaces.ToolResult{
				Success: false,
				Error:   fmt.Sprintf("failed to decode tasks: %v", err),
			}, nil
		}
		// Normalize each task to handle legacy field names
		for i := range tasks {
			tasks[i].normalize()
		}
	} else {
		// Single-task mode - wrap into array
		// Support both new names (prompt/subagent_type) and legacy names (task/agent_type)
		desc, _ := params["description"].(string)

		prompt, _ := params["prompt"].(string)
		if prompt == "" {
			// Fallback to legacy "task" parameter
			prompt, _ = params["task"].(string)
		}

		subagentType, _ := params["subagent_type"].(string)
		if subagentType == "" {
			// Fallback to legacy "agent_type" parameter
			subagentType, _ = params["agent_type"].(string)
		}

		if prompt == "" || subagentType == "" {
			return &interfaces.ToolResult{
				Success: false,
				Error:   "missing required parameters: prompt/task and subagent_type/agent_type are required",
			}, nil
		}

		tasks = []taskSpec{{
			Description:  desc,
			Prompt:       prompt,
			SubAgentType: subagentType,
		}}
	}

	if len(tasks) == 0 {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "no tasks provided",
		}, nil
	}

	// Build fork configs
	toolCallID := agent.ToolCallIDFromContext(ctx)
	configs := make([]agent.ForkConfig, len(tasks))
	for i, task := range tasks {
		// Resolve subagent_type to AgentType or Expert system prompt
		var agentType agent.AgentType
		var systemPrompt string

		// First try expert registry
		if expertRegistry != nil {
			if expert, found := expertRegistry.Get(task.SubAgentType); found {
				agentType = agent.AgentTypeExecute // Experts always use execute type
				systemPrompt = expert.SystemPrompt
				logger.Infof("Task %d: using expert %q", i, expert.Name)
			}
		}

		// Fallback to built-in agent types
		if systemPrompt == "" {
			switch task.SubAgentType {
			case "explore":
				agentType = agent.AgentTypeExplore
			case "plan":
				agentType = agent.AgentTypePlan
			case "execute":
				agentType = agent.AgentTypeExecute
			case "verify":
				agentType = agent.AgentTypeVerify
			default:
				// Unknown subagent type - record error but don't fail entire batch
				configs[i] = agent.ForkConfig{
					Description: task.Description,
					Task:        fmt.Sprintf("ERROR: unknown subagent_type %q", task.SubAgentType),
					AgentType:   agent.AgentTypeExecute,
				}
				continue
			}
		}

		// Generate a stable-but-unique WorkerID. When the task originates from a
		// tool call, include the tool call ID so duplicate descriptions across
		// separate task() invocations do not collide.
		var workerID string
		if toolCallID != "" {
			if task.Description != "" {
				workerID = fmt.Sprintf("subagent-%s-%d-%s", toolCallID, i, task.Description)
			} else {
				workerID = fmt.Sprintf("subagent-%s-%d", toolCallID, i)
			}
		} else if task.Description != "" {
			// Use description as WorkerID component for readability when there is
			// no tool-call-scoped identifier available.
			if len(tasks) == 1 {
				workerID = fmt.Sprintf("subagent-%s", task.Description)
			} else {
				workerID = fmt.Sprintf("subagent-%d-%s", i, task.Description)
			}
		} else {
			// Fallback for programmatic tests without description or tool_call_id.
			workerID = fmt.Sprintf("subagent-%d", i)
		}

		configs[i] = agent.ForkConfig{
			Description:  task.Description,
			Task:         task.Prompt,
			AgentType:    agentType,
			SystemPrompt: systemPrompt,
			WorkerID:     workerID,
		}
	}

	// Execute in parallel via ForkBatch - pass parent event handler for sub-agent event bubbling
	parentEventHandler := t.mainAgent.GetEventHandler()
	results, err := forkManager.ForkBatch(ctx, configs, parentEventHandler)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("fork batch failed: %v", err),
		}, nil
	}

	// Aggregate results into markdown
	var output strings.Builder
	hasErrors := false

	if len(results) == 1 {
		// Single task - simpler output format
		result := results[0]
		if result.Error != nil {
			output.WriteString(fmt.Sprintf("**Error**: %v\n\n", result.Error))
			hasErrors = true
		}
		output.WriteString(result.Output)

		// Add token stats
		if result.TokensUsed.TotalTokens > 0 {
			output.WriteString(fmt.Sprintf("\n\n*Tokens: %d input, %d output (%d total) · Duration: %v*",
				result.TokensUsed.InputTokens,
				result.TokensUsed.OutputTokens,
				result.TokensUsed.TotalTokens,
				result.Duration))
		}
	} else {
		// Multiple tasks - structured output
		for i, result := range results {
			desc := tasks[i].Description
			if desc == "" {
				desc = fmt.Sprintf("Task %d", i+1)
			}

			output.WriteString(fmt.Sprintf("## Task %d: %s\n\n", i+1, desc))

			if result.Error != nil {
				output.WriteString(fmt.Sprintf("**Error**: %v\n\n", result.Error))
				hasErrors = true
				continue
			}

			output.WriteString(result.Output)
			output.WriteString("\n\n")

			// Add token stats for each task
			if result.TokensUsed.TotalTokens > 0 {
				output.WriteString(fmt.Sprintf("*Tokens: %d input, %d output (%d total) · Duration: %v*\n\n",
					result.TokensUsed.InputTokens,
					result.TokensUsed.OutputTokens,
					result.TokensUsed.TotalTokens,
					result.Duration))
			}

			output.WriteString("---\n\n")
		}
	}

	return &interfaces.ToolResult{
		Success:     !hasErrors,
		LLMContent:  output.String(),
		UserContent: output.String(),
		Data:        output.String(),
	}, nil
}
