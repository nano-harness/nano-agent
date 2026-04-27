package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

type turnExecutor struct {
	turn    *Turn
	emitter turnEventEmitter
}

func newTurnExecutor(turn *Turn) *turnExecutor {
	return &turnExecutor{
		turn:    turn,
		emitter: turn.events(),
	}
}

func (e *turnExecutor) Execute(ctx context.Context) error {
	t := e.turn
	logger.Infof("Starting turn execution: %s", t.ID)
	t.StartTime = time.Now()

	if t.agentConfig != nil && t.agentConfig.Turn != nil && t.agentConfig.Turn.MaxDuration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.agentConfig.Turn.MaxDuration)
		defer cancel()
	}

	t.ctx = ctx
	if t.Toolbox != nil {
		t.Toolbox.ResetReadFileState()
	}
	if t.agent != nil {
		t.agent.SetCurrentTurnID(t.ID)
	}

	e.prepare(ctx, config.Get())
	var turnErr error
	for {
		if t.shouldTerminate() {
			logger.Infof("Turn termination condition met")
			break
		}
		done, err := e.runIteration(ctx)
		if err != nil {
			return err
		}
		if done {
			break
		}
	}

	if err := t.saveConversationMemory(); err != nil {
		logger.Warnf("Failed to save conversation memory: %v", err)
	}
	e.emitter.executorState("closing", map[string]interface{}{
		"iterations": t.CompletionCriteria.CurrentIteration,
	})
	t.close()
	return turnErr
}

func (e *turnExecutor) prepare(ctx context.Context, cfg *config.Config) {
	e.turn.preprocessInput(ctx, cfg)
	e.emitter.plannerPlanSnapshot([]map[string]interface{}{
		{"id": "understand", "title": "理解需求", "status": "pending"},
		{"id": "execute", "title": "执行与调用工具", "status": "pending"},
		{"id": "synthesize", "title": "整理输出", "status": "pending"},
	})
	e.emitter.executorState("running", nil)
}

func (e *turnExecutor) runIteration(ctx context.Context) (bool, error) {
	t := e.turn
	e.emitter.plannerDecision("request_llm", map[string]interface{}{
		"iteration": t.CompletionCriteria.CurrentIteration + 1,
	})

	response, toolCalls, retryableLLMFailure, err := t.requestLLMWithRetry(ctx, 3)
	if err != nil {
		return false, err
	}
	if retryableLLMFailure {
		return false, nil
	}

	decision := "no_tool_calls_detected"
	if len(toolCalls) > 0 {
		decision = "tool_calls_detected"
	}
	e.emitter.plannerDecision(decision, map[string]interface{}{
		"response_chars":   len(response),
		"tool_calls_count": len(toolCalls),
	})

	done, err := e.completeIfNoTools(ctx, response, len(toolCalls))
	if done || err != nil {
		return done, err
	}

	toolsToExecute := make([]ToolToExecute, len(toolCalls))
	for i, tc := range toolCalls {
		toolsToExecute[i] = ToolToExecute{
			ID:         tc.ID,
			Name:       tc.Name,
			Parameters: tc.Arguments,
		}
	}

	toolResults := e.executeTools(ctx, toolsToExecute)
	if err := t.addToolResultsToContext(toolResults); err != nil {
		logger.Errorf("Failed to add tool results to context: %v", err)
		t.CompletionCriteria.ErrorCount++
		t.CompletionCriteria.ConsecutiveErrors++
	} else {
		t.updateConsecutiveErrorsFromToolResults(toolResults)
	}
	t.recordTokenGain()
	e.keepRunningForUnreadMailbox(ctx)
	if t.isComplete() {
		logger.Infof("Turn completion criteria met")
		return true, nil
	}
	return false, nil
}

func (e *turnExecutor) completeIfNoTools(ctx context.Context, response string, toolCallCount int) (bool, error) {
	t := e.turn
	if toolCallCount > 0 || t.CompletionCriteria == nil || t.CompletionCriteria.TaskCompleted {
		return false, nil
	}
	if t.CompletionCriteria.LoopDetectionEnabled && t.hasSimilarContent() {
		logger.Infof("Similar-content loop detected: terminating turn early")
		t.emitLoopDetectedEvent("similar_content",
			fmt.Sprintf("LLM output repeated %d times without meaningful change",
				t.CompletionCriteria.MaxSimilarContent))
		return true, nil
	}
	hasUnreadMessages, count, err := t.hasUnreadMailboxMessages(ctx)
	if err == nil && hasUnreadMessages {
		logger.Infof("Continuing turn despite no tool calls: %d mailbox messages need processing", count)
	}
	if !hasUnreadMessages {
		t.MarkTaskCompleted("natural-completion: model returned text without tool calls")
		return true, nil
	}
	_ = response
	return false, nil
}

func (e *turnExecutor) executeTools(ctx context.Context, toolsToExecute []ToolToExecute) map[string]*interfaces.ToolResult {
	t := e.turn
	e.emitter.executorSchedule(toolsToExecute)
	if len(toolsToExecute) == 0 {
		return make(map[string]*interfaces.ToolResult)
	}
	toolResults, err := t.executeToolCallsInParallel(ctx, toolsToExecute)
	if err != nil {
		logger.Errorf("Tool execution infrastructure error: %v", err)
		toolResults = t.buildFallbackResults(toolsToExecute, err)
		t.CompletionCriteria.ErrorCount++
	}
	if toolResults == nil {
		toolResults = make(map[string]*interfaces.ToolResult)
	}
	return toolResults
}

func (e *turnExecutor) keepRunningForUnreadMailbox(ctx context.Context) {
	t := e.turn
	if t.CompletionCriteria == nil {
		return
	}
	hasUnreadMessages, count, err := t.hasUnreadMailboxMessages(ctx)
	logger.Infof("DEBUG: Checking mailbox after tool execution: count=%d, err=%v, TaskCompleted=%v", count, err, t.CompletionCriteria.TaskCompleted)
	if err == nil && hasUnreadMessages && t.CompletionCriteria.TaskCompleted {
		logger.Infof("Continuing turn despite completion: %d mailbox messages need processing", count)
		t.CompletionCriteria.TaskCompleted = false
	}
}
