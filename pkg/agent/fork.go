package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

type contextKey int

const (
	forkDepthKey        contextKey = iota
	defaultMaxForkDepth            = 3
)

// ForkConfig contains configuration for forking a child agent.
type ForkConfig struct {
	AgentType    AgentType
	Task         string
	SystemPrompt string
}

// ForkResult contains the result of a forked agent run.
type ForkResult struct {
	AgentType AgentType
	Task      string
	Output    string
	Error     error
}

// ForkManager manages forked child agents.
type ForkManager struct {
	parentAgent *Agent
	maxDepth    int
}

// NewForkManager creates a new ForkManager backed by the given parent agent.
// The max fork depth is read from cfg.Advanced.Fork.MaxDepth when set; otherwise
// it defaults to defaultMaxForkDepth.
func NewForkManager(parent *Agent) *ForkManager {
	maxDepth := defaultMaxForkDepth
	if parent != nil && parent.config != nil &&
		parent.config.Advanced != nil && parent.config.Advanced.Fork != nil &&
		parent.config.Advanced.Fork.MaxDepth > 0 {
		maxDepth = parent.config.Advanced.Fork.MaxDepth
	}
	return &ForkManager{
		parentAgent: parent,
		maxDepth:    maxDepth,
	}
}

// Fork creates and runs a child agent for the given config.
func (fm *ForkManager) Fork(ctx context.Context, config ForkConfig) (*ForkResult, error) {
	depth := currentForkDepth(ctx)
	if depth >= fm.maxDepth {
		return nil, fmt.Errorf("fork depth limit (%d) reached", fm.maxDepth)
	}
	childCtx := withForkDepth(ctx, depth+1)

	// Build child config
	childCfg := *fm.parentAgent.config
	childCfg.IsSubAgent = true

	// Apply agent-type system prompt if no custom one provided
	if config.SystemPrompt != "" {
		childCfg.CustomSystemPrompt = config.SystemPrompt
	} else {
		typeCfg := GetAgentTypeConfig(config.AgentType)
		if typeCfg != nil && typeCfg.SystemPrompt != nil {
			prompt := typeCfg.SystemPrompt(fm.parentAgent.GetWorkingDirectory())
			if prompt != "" {
				childCfg.CustomSystemPrompt = prompt
			}
		}
	}

	// Pass the parent's approval handler so tools that require confirmation
	// are not silently auto-approved in fork children.
	childAgent, err := New(&childCfg, fm.parentAgent.toolScheduler.GetApprovalHandler())
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
	collectEvent := func(ev event.StreamEvent) {
		if ev.Type == event.EventTypeContent && ev.Content != "" {
			buf.WriteString(ev.Content)
		}
	}

	if err := childAgent.ProcessStream(childCtx, config.Task, collectEvent); err != nil {
		return &ForkResult{
			AgentType: config.AgentType,
			Task:      config.Task,
			Output:    buf.String(),
			Error:     err,
		}, err
	}

	return &ForkResult{
		AgentType: config.AgentType,
		Task:      config.Task,
		Output:    buf.String(),
	}, nil
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
