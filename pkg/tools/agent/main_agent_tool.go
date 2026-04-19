// Package agent provides tools for interacting with agents
package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// MainAgentTool wraps the main agent functionality as a tool
type MainAgentTool struct {
	name        string
	description string
	config      *config.Config
	agent       *agent.Agent
	mutex       sync.RWMutex
}

// NewMainAgentTool creates a new main agent tool
func NewMainAgentTool(cfg *config.Config, agentInstance *agent.Agent) *MainAgentTool {
	return &MainAgentTool{
		name:        "main_agent",
		description: "Execute tasks using the main agent with full capabilities",
		config:      cfg,
		agent:       agentInstance,
	}
}

// Name returns the tool name
func (t *MainAgentTool) Name() string {
	return t.name
}

// Description returns the tool description
func (t *MainAgentTool) Description() string {
	return t.description
}

// Category returns the tool category
func (t *MainAgentTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryBuild // Using existing category for now
}

// RequiresConfirmation returns false as main agent execution doesn't require confirmation
func (t *MainAgentTool) RequiresConfirmation() bool {
	return false
}

// ConcurrencySafe returns false: agent execution has arbitrary side effects.
func (t *MainAgentTool) ConcurrencySafe() bool { return false }

// Schema returns the tool schema
func (t *MainAgentTool) Schema() *interfaces.ToolSchema {
	taskProp := interfaces.NewStringProperty("The task or request to execute using the main agent")
	taskProp.Examples = []string{"Analyze the codebase structure", "Generate a README file", "Fix the bug in main.go"}
	taskProp.Usage = "Describe the task you want the main agent to perform"

	contextProp := interfaces.NewStringProperty("Additional context or constraints for the task execution")
	contextProp.Examples = []string{"Focus on security aspects", "Use TypeScript", "Follow existing code patterns"}
	contextProp.Usage = "Optional context to guide the agent's execution"

	streamProp := interfaces.NewBooleanProperty("Whether to stream the response (default: false)")
	streamProp.Examples = []string{"false", "true"}
	streamProp.Usage = "Enable streaming for real-time output"

	return interfaces.CreateSchema(
		t.description,
		map[string]*interfaces.PropertySchema{
			"task":    taskProp,
			"context": contextProp,
			"stream":  streamProp,
		},
		[]string{"task"},
	)
}

// Execute runs the main agent with the provided task
func (t *MainAgentTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	// Extract parameters
	task, ok := params["task"].(string)
	if !ok || task == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "task parameter is required and must be a string",
			UserContent: "❌ 任务参数是必需的且必须是字符串",
			LLMContent:  "main_agent tool failed: missing or invalid task parameter",
		}, nil
	}

	// Optional context parameter
	contextStr, _ := params["context"].(string)
	if contextStr != "" {
		task = fmt.Sprintf("%s\n\n上下文: %s", task, contextStr)
	}

	// Optional stream parameter
	stream, _ := params["stream"].(bool)

	logger.Infof("MainAgentTool executing task: %s", task)

	if t.agent == nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "main agent not initialized",
			UserContent: "❌ 主agent未初始化",
			LLMContent:  "main_agent tool failed: agent not initialized",
		}, nil
	}

	// Collect output from agent execution
	var output strings.Builder
	var hasError bool
	var errorMsg string

	// Event handler to capture agent output
	eventHandler := func(e event.StreamEvent) {
		switch e.Type {
		case event.EventTypeContent:
			output.WriteString(e.Content)
		case event.EventTypeError:
			hasError = true
			errorMsg = e.Content
			fmt.Fprintf(&output, "错误: %s\n", e.Content)
		case event.EventTypeToolUse:
			if e.ToolUse != nil {
				fmt.Fprintf(&output, "🔧 使用工具: %s\n", e.ToolUse.ToolName)
			}
		case event.EventTypeToolResult:
			if e.ToolResult != nil {
				fmt.Fprintf(&output, "✅ 工具结果: %s\n", e.ToolResult.Content)
			}
		}
	}

	// Execute the task using the main agent
	err := t.agent.ProcessStream(ctx, task, eventHandler)

	result := output.String()

	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			Data:        result,
			UserContent: fmt.Sprintf("❌ 主agent执行失败: %v\n输出:\n%s", err, result),
			LLMContent:  fmt.Sprintf("main_agent execution failed: %v. Output: %s", err, result),
			Metadata: map[string]interface{}{
				"task":        task,
				"has_context": contextStr != "",
				"stream":      stream,
				"error":       err.Error(),
			},
		}, nil
	}

	if hasError {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       errorMsg,
			Data:        result,
			UserContent: fmt.Sprintf("⚠️ 主agent执行完成但有错误:\n%s", result),
			LLMContent:  fmt.Sprintf("main_agent execution completed with errors. Output: %s", result),
			Metadata: map[string]interface{}{
				"task":        task,
				"has_context": contextStr != "",
				"stream":      stream,
				"has_error":   true,
			},
		}, nil
	}

	return &interfaces.ToolResult{
		Success:     true,
		Data:        result,
		UserContent: fmt.Sprintf("✅ 主agent执行成功:\n%s", result),
		LLMContent:  fmt.Sprintf("main_agent execution successful. Output: %s", result),
		Metadata: map[string]interface{}{
			"task":        task,
			"has_context": contextStr != "",
			"stream":      stream,
		},
	}, nil
}

// SetAgent updates the agent instance (useful for dynamic reconfiguration)
func (t *MainAgentTool) SetAgent(agentInstance *agent.Agent) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.agent = agentInstance
}

// GetAgent returns the current agent instance
func (t *MainAgentTool) GetAgent() *agent.Agent {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.agent
}
