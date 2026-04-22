package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

type contextKey int

const (
	forkDepthKey           contextKey = iota
	defaultMaxForkDepth               = 3
	defaultMaxConcurrent              = 5
)

// ForkConfig contains configuration for forking a child agent.
type ForkConfig struct {
	AgentType    AgentType
	Task         string
	SystemPrompt string
	Timeout      time.Duration // Timeout for fork execution (default: 5 minutes)
	Description  string        // Short description for event stream identification
	WorkerID     string        // Optional caller-provided worker ID (takes priority over auto-generated)
}

const DefaultForkTimeout = 5 * time.Minute

// ForkResult contains the result of a forked agent run.
type ForkResult struct {
	AgentType AgentType
	Task      string
	Output    string
	Error     error
	// Extended result fields for better observability
	ToolCalls  []*ToolCallInfo // List of tool calls made during execution (reuses existing ToolCallInfo type from tool_scheduler.go)
	TokensUsed TokenUsageInfo  // Token usage statistics
	Duration   time.Duration   // Total execution duration
}

// TokenUsageInfo contains token usage statistics for the fork
type TokenUsageInfo struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// ForkManager manages forked child agents.
type ForkManager struct {
	parentAgent   *Agent
	maxDepth      int
	maxConcurrent int
}

// NewForkManager creates a new ForkManager backed by the given parent agent.
// The max fork depth is read from cfg.Advanced.Fork.MaxDepth when set; otherwise
// it defaults to defaultMaxForkDepth.
// The max concurrent is read from cfg.Advanced.Fork.MaxConcurrent when set; otherwise
// it defaults to defaultMaxConcurrent (5).
func NewForkManager(parent *Agent) *ForkManager {
	maxDepth := defaultMaxForkDepth
	maxConcurrent := defaultMaxConcurrent
	if parent != nil && parent.config != nil &&
		parent.config.Advanced != nil && parent.config.Advanced.Fork != nil {
		if parent.config.Advanced.Fork.MaxDepth > 0 {
			maxDepth = parent.config.Advanced.Fork.MaxDepth
		}
		if parent.config.Advanced.Fork.MaxConcurrent > 0 {
			maxConcurrent = parent.config.Advanced.Fork.MaxConcurrent
		}
	}
	return &ForkManager{
		parentAgent:   parent,
		maxDepth:      maxDepth,
		maxConcurrent: maxConcurrent,
	}
}

// Fork creates and runs a child agent for the given config.
func (fm *ForkManager) Fork(ctx context.Context, config ForkConfig) (*ForkResult, error) {
	return fm.forkWithHandler(ctx, config, nil)
}

// forkWithHandler is the internal fork implementation that allows injecting a custom event handler
func (fm *ForkManager) forkWithHandler(ctx context.Context, config ForkConfig, eventHandler func(event.StreamEvent)) (*ForkResult, error) {
	depth := currentForkDepth(ctx)
	if depth >= fm.maxDepth {
		return nil, fmt.Errorf("fork depth limit (%d) reached", fm.maxDepth)
	}
	childCtx := withForkDepth(ctx, depth+1)

	// Apply timeout
	timeout := config.Timeout
	if timeout == 0 {
		timeout = DefaultForkTimeout
	}
	childCtx, cancel := context.WithTimeout(childCtx, timeout)
	defer cancel()

	// Build child config using deep copy to prevent parent config modification
	childCfg := fm.parentAgent.config.DeepCopy()
	childCfg.IsSubAgent = true

	// Apply agent-type system prompt if no custom one provided
	var basePrompt string
	if config.SystemPrompt != "" {
		basePrompt = config.SystemPrompt
	} else {
		typeCfg := GetAgentTypeConfig(config.AgentType)
		if typeCfg != nil && typeCfg.SystemPrompt != nil {
			prompt := typeCfg.SystemPrompt(fm.parentAgent.GetWorkingDirectory())
			if prompt != "" {
				basePrompt = prompt
			}
		}
	}

	// Append sub-agent constraint block to system prompt
	childCfg.CustomSystemPrompt = basePrompt + subAgentConstraintBlock

	// Replace parent's approval handler with rejector for sub-agents
	childAgent, err := New(childCfg, subAgentApprovalRejector)
	if err != nil {
		return nil, fmt.Errorf("failed to create fork child agent: %w", err)
	}
	defer func() {
		if shutdownErr := childAgent.Shutdown(); shutdownErr != nil {
			logger.Warnf("fork child cleanup failed: %v", shutdownErr)
		}
	}()
	childAgent.isForkChild = true
	childAgent.agentType = config.AgentType

	// Enforce tool allow/deny lists defined for the agent type.
	typeCfg := GetAgentTypeConfig(config.AgentType)
	if typeCfg != nil && len(typeCfg.AllowedTools) > 0 && typeCfg.AllowedTools[0] != "*" {
		childAgent.toolScheduler.SetAllowedTools(typeCfg.AllowedTools)
	}

	var buf strings.Builder
	var toolCalls []*ToolCallInfo
	var totalInputTokens, totalOutputTokens int
	startTime := time.Now()

	collectEvent := func(ev event.StreamEvent) {
		if ev.Type == event.EventTypeContent && ev.Content != "" {
			buf.WriteString(ev.Content)
		}
		// Track tool calls from ToolUse event
		if ev.Type == event.EventTypeToolUse && ev.ToolUse != nil {
			now := time.Now()
			toolCalls = append(toolCalls, &ToolCallInfo{
				ID:         ev.ToolUse.ID,
				Name:       ev.ToolUse.ToolName,
				Parameters: ev.ToolUse.Parameters,
				Status:     StatusSuccess, // Initially success, updated if tool result has error
				StartTime:  &now,
			})
		}
		// Track tool results and update status
		if ev.Type == event.EventTypeToolResult && ev.ToolResult != nil {
			// Find the corresponding tool call by ID and update its status
			for i := len(toolCalls) - 1; i >= 0; i-- {
				if toolCalls[i].ID == ev.ToolResult.ID {
					// Convert tools.ToolResult to interfaces.ToolResult for storage
					toolCalls[i].Result = &interfaces.ToolResult{
						Success:     ev.ToolResult.Error == "",
						Data:        ev.ToolResult.Content,
						Error:       ev.ToolResult.Error,
						LLMContent:  ev.ToolResult.Content,
						UserContent: ev.ToolResult.Content,
					}
					if ev.ToolResult.Error != "" {
						toolCalls[i].Status = StatusError
						toolCalls[i].Error = fmt.Errorf("%s", ev.ToolResult.Error)
					}
					if toolCalls[i].StartTime != nil {
						durationMs := time.Since(*toolCalls[i].StartTime).Milliseconds()
						toolCalls[i].DurationMs = &durationMs
					}
					break
				}
			}
		}
		// Track token usage from TokenStats event
		if ev.TokenStats != nil {
			totalInputTokens += ev.TokenStats.InputTokens
			totalOutputTokens += ev.TokenStats.OutputTokens
		}

		// Forward event to parent handler if provided
		if eventHandler != nil {
			eventHandler(ev)
		}
	}

	processErr := childAgent.ProcessStream(childCtx, config.Task, collectEvent)
	duration := time.Since(startTime)

	result := &ForkResult{
		AgentType: config.AgentType,
		Task:      config.Task,
		Output:    buf.String(),
		Error:     processErr,
		ToolCalls: toolCalls,
		TokensUsed: TokenUsageInfo{
			InputTokens:  totalInputTokens,
			OutputTokens: totalOutputTokens,
			TotalTokens:  totalInputTokens + totalOutputTokens,
		},
		Duration: duration,
	}

	if processErr != nil {
		return result, processErr
	}

	return result, nil
}

// ForkBatch executes multiple fork configs in parallel with a concurrency limit.
// Results are returned in the same order as the input configs.
// Individual fork failures do not block others; errors are captured in each ForkResult.
// The entire batch is cancelled only if the parent context is cancelled.
func (fm *ForkManager) ForkBatch(ctx context.Context, configs []ForkConfig, parentEventHandler func(event.StreamEvent)) ([]*ForkResult, error) {
	if len(configs) == 0 {
		return []*ForkResult{}, nil
	}

	// Create semaphore for concurrency control
	sem := make(chan struct{}, fm.maxConcurrent)
	results := make([]*ForkResult, len(configs))
	var wg sync.WaitGroup

	for i, cfg := range configs {
		wg.Add(1)
		go func(index int, config ForkConfig) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[index] = &ForkResult{
					AgentType: config.AgentType,
					Task:      config.Task,
					Error:     ctx.Err(),
				}
				return
			}

			// Generate worker ID: use caller-provided if available, otherwise fallback
			workerID := config.WorkerID
			if workerID == "" {
				workerID = fmt.Sprintf("subagent-%d-%s", index, config.Description)
				if config.Description == "" {
					workerID = fmt.Sprintf("subagent-%d", index)
				}
			}

			// Wrap event handler with worker ID and drop terminal markers
			wrappedHandler := wrapEventHandlerWithWorkerID(parentEventHandler, workerID, config)

			// Emit ExpertStarted event
			expertName := string(config.AgentType)
			if expertName == "" {
				expertName = "general-purpose"
			}
			startTime := time.Now()
			if wrappedHandler != nil {
				wrappedHandler(event.StreamEvent{
					Type:      event.EventTypeExpertStarted,
					Timestamp: startTime.Unix(),
					Metadata: map[string]interface{}{
						"expert_name":  expertName,
						"display_name": expertName,
						"task":         config.Task,
						"agent_type":   string(config.AgentType),
					},
				})
			}

			// Execute fork
			result, err := fm.forkWithHandler(ctx, config, wrappedHandler)
			if result == nil {
				// If result is nil (shouldn't happen), create an error result
				result = &ForkResult{
					AgentType: config.AgentType,
					Task:      config.Task,
					Error:     err,
				}
			}
			results[index] = result

			// Emit ExpertFinished event
			if wrappedHandler != nil {
				finishedEvent := event.StreamEvent{
					Type: event.EventTypeExpertFinished,
					Metadata: map[string]interface{}{
						"duration_ms": time.Since(startTime).Milliseconds(),
						"success":     result.Error == nil,
						"agent_type":  string(config.AgentType),
					},
				}
				if result.TokensUsed.TotalTokens > 0 {
					finishedEvent.Metadata["tokens_used"] = result.TokensUsed.TotalTokens
					finishedEvent.Metadata["input_tokens"] = result.TokensUsed.InputTokens
					finishedEvent.Metadata["output_tokens"] = result.TokensUsed.OutputTokens
				}
				if result.Error != nil {
					finishedEvent.Error = result.Error.Error()
				}
				wrappedHandler(finishedEvent)
			}
		}(i, cfg)
	}

	wg.Wait()
	return results, nil
}

func currentForkDepth(ctx context.Context) int {
	if v := ctx.Value(forkDepthKey); v != nil {
		if d, ok := v.(int); ok {
			return d
		}
	}
	return 0
}

func withForkDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, forkDepthKey, depth)
}

// Sub-agent constraint block to be appended to all sub-agent system prompts
const subAgentConstraintBlock = `

---

## Sub-Agent Operating Constraints (CRITICAL)

You are a sub-agent dispatched via the parent agent's task tool. You MUST follow these constraints:

1. **NO conversation history**: You have ZERO context beyond the prompt above. Treat the user prompt as your complete and ONLY context.
2. **NO recursion**: You CANNOT dispatch further sub-agents. The task tool is unavailable to you.
3. **NO user interaction**: You CANNOT ask the user questions or request clarification. If the prompt is ambiguous, do your best with the information given and document your assumptions in your final answer.
4. **NO approval-required tools**: Tools requiring user approval (network, dangerous shell, cross-directory writes) will be auto-rejected. Stick to operations within the working directory.
5. **Final answer in last message**: Your last message IS your return value to the parent agent. Make it self-contained, structured, and immediately useful — include file paths, code snippets, key findings, etc. Do not say "I'll continue" or defer.
`

// subAgentApprovalRejector is an approval handler that always denies approval requests from sub-agents
func subAgentApprovalRejector(info *ToolCallInfo) bool {
	logger.Warnf("Sub-agent attempted to call approval-required tool %q (ID: %s) - rejecting", info.Name, info.ID)
	return false
}

// wrapEventHandlerWithWorkerID wraps a parent event handler to inject worker_id and metadata
func wrapEventHandlerWithWorkerID(parent func(event.StreamEvent), workerID string, cfg ForkConfig) func(event.StreamEvent) {
	if parent == nil {
		return nil
	}
	return func(ev event.StreamEvent) {
		// Drop sub-agent terminal markers - these would confuse parent stream termination
		// Sub-agent completion is signaled via ExpertFinished instead
		if ev.Type == event.EventTypeTaskCompletion || ev.Type == event.EventTypeError {
			return
		}

		ev.WorkerID = workerID
		if ev.Metadata == nil {
			ev.Metadata = make(map[string]interface{})
		}
		ev.Metadata["sub_agent_type"] = string(cfg.AgentType)
		ev.Metadata["task_description"] = cfg.Description
		parent(ev)
	}
}
