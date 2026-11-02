package agent

import (
	"context"

	"github.com/nano-harness/nano-agent/pkg/middleware"
	"github.com/nano-harness/nano-agent/pkg/toolruntime"
)

// recoveryAdapter bridges the agent's ToolRecoveryStrategy to the
// toolruntime.RecoveryExecutor interface, allowing the runtime to execute
// tools with retry/recovery semantics.
type recoveryAdapter struct {
	strategy *ToolRecoveryStrategy
	executor ToolExecutor
}

func (a *recoveryAdapter) ExecuteToolWithRecovery(ctx context.Context, req toolruntime.ToolRequest) *toolruntime.ToolExecutionResult {
	toolToExec := ToolToExecute{
		ID:         req.ID,
		Name:       req.Name,
		Parameters: req.Parameters,
	}
	agentResult := a.strategy.ExecuteWithRecovery(ctx, a.executor, toolToExec)

	return &toolruntime.ToolExecutionResult{
		Result:        agentResult.Result,
		Error:         agentResult.Error,
		Attempts:      agentResult.Attempts,
		TotalTime:     agentResult.TotalTime,
		ErrorCategory: string(agentResult.ErrorCategory),
		RecoveryInfo:  agentResult.RecoveryInfo,
	}
}

// hookEngineAdapter wraps middleware.HookEngine to implement toolruntime.HookDispatcher.
type hookEngineAdapter struct {
	engine *middleware.HookEngine
}

func (a *hookEngineAdapter) Execute(ctx context.Context, event middleware.HookEvent, toolName string, params map[string]interface{}) (*middleware.Decision, error) {
	return a.engine.Execute(ctx, event, toolName, params)
}
