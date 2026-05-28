package toolruntime

import (
	"context"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/middleware"
)

// HookDispatcher executes pre/post tool use hooks.
type HookDispatcher interface {
	Execute(ctx context.Context, event middleware.HookEvent, toolName string, params map[string]interface{}) (*middleware.Decision, error)
}

// ToolRequest describes a single tool invocation to be executed by the runtime.
type ToolRequest struct {
	ID         string
	Name       string
	Parameters map[string]interface{}
	SessionID  string
}

// ToolExecutionResult contains the outcome of tool execution with recovery.
type ToolExecutionResult struct {
	Result        *interfaces.ToolResult
	Error         error
	Attempts      int
	TotalTime     time.Duration
	ErrorCategory string
	RecoveryInfo  map[string]interface{}
}

// RecoveryExecutor executes a tool with retry/recovery semantics.
type RecoveryExecutor interface {
	ExecuteToolWithRecovery(ctx context.Context, req ToolRequest) *ToolExecutionResult
}

// ExecuteWithHooks runs a tool through recovery and dispatches pre/post hooks.
// This consolidates the hook + recovery pattern previously scattered in the scheduler.
// The scheduler retains queuing, concurrency, cancel, and event dispatch responsibilities.
func (r *Runtime) ExecuteWithHooks(
	ctx context.Context,
	req ToolRequest,
	hooks HookDispatcher,
	recovery RecoveryExecutor,
) *ToolExecutionResult {
	// Pre-tool hook
	if hooks != nil {
		preParams := makeHookParams(req)
		_, err := hooks.Execute(ctx, middleware.HookPreToolUse, req.Name, preParams)
		if err != nil {
			logger.Warnf("PreToolUse hook execution error for tool %s: %v", req.Name, err)
		}
	}

	// Execute with recovery
	var execResult *ToolExecutionResult
	if recovery != nil {
		execResult = recovery.ExecuteToolWithRecovery(ctx, req)
	} else {
		// Direct execution without recovery
		result, err := r.Execute(ctx, req.Name, req.Parameters)
		execResult = &ToolExecutionResult{
			Result:   result,
			Error:    err,
			Attempts: 1,
		}
	}

	// Post-tool hook
	if hooks != nil {
		hookEvent := middleware.HookPostToolUse
		if execResult.Error != nil {
			hookEvent = middleware.HookPostToolUseFailure
		}
		postParams := makeHookParams(req)
		if execResult.Result != nil {
			postParams["_result"] = execResult.Result
		}
		if execResult.Error != nil {
			postParams["_error"] = execResult.Error.Error()
		}
		_, err := hooks.Execute(ctx, hookEvent, req.Name, postParams)
		if err != nil {
			logger.Warnf("PostToolUse hook execution error for tool %s: %v", req.Name, err)
		}
	}

	return execResult
}

func makeHookParams(req ToolRequest) map[string]interface{} {
	params := make(map[string]interface{}, len(req.Parameters)+3)
	for k, v := range req.Parameters {
		params[k] = v
	}
	params["input"] = req.Parameters
	params["tool_use_id"] = req.ID
	if req.SessionID != "" {
		params["session_id"] = req.SessionID
	}
	return params
}
