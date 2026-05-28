package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/middleware"
	"github.com/nano-harness/nano-agent/pkg/tools"
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
	ctx = context.WithValue(ctx, interfaces.TurnContextKey{}, interfaces.TurnContext{SessionID: t.SessionID})
	logger.Infof("Starting turn execution: %s", t.ID)
	t.StartTime = time.Now()

	// Dispatch UserPromptSubmit hook
	if t.hookEngine != nil {
		hookParams := map[string]interface{}{
			"user_input": t.UserInput,
			"turn_id":    t.ID,
			"session_id": t.SessionID,
		}
		hookDecision, err := t.hookEngine.Execute(ctx, middleware.HookUserPromptSubmit, "user_prompt", hookParams)
		if err != nil {
			logger.Warnf("UserPromptSubmit hook execution error: %v", err)
		}
		if hookDecision != nil {
			switch hookDecision.Action {
			case middleware.ActionBlock:
				// Hook blocked the user prompt
				logger.Warnf("User prompt blocked by hook: %s", hookDecision.Reason)
				return fmt.Errorf("user prompt blocked by hook: %s", hookDecision.Reason)
			case middleware.ActionConfirm:
				// Hook requires confirmation (though this is unusual for user input)
				logger.Infof("User prompt requires confirmation per hook: %s", hookDecision.Reason)
			case middleware.ActionAllow:
				// Hook allows - apply any modifications to the user input
				if modifiedInput, ok := hookDecision.ModifiedParams["user_input"].(string); ok && modifiedInput != "" {
					logger.Infof("User input modified by hook: %s -> %s", t.UserInput, modifiedInput)
					t.UserInput = modifiedInput
				}
			}
		}
	}

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

	e.prepare(ctx, t.agentConfig)
	var turnErr error
	goalContinue := false
	for {
		if t.shouldTerminate() {
			logger.Infof("Turn termination condition met")
			break
		}
		// K1.1: Check early stop based on completion criteria (consecutive errors, etc.)
		if stop, reason := t.ShouldStop(); stop {
			logger.Infof("Early stop triggered: %s", reason)
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

	if t.goalContext != nil && t.goalContext.IsActive() {
		goalErr := e.evaluateGoal(ctx)
		if goalErr == ErrContinueRequested {
			turnErr = ErrContinueRequested
			goalContinue = true
		} else if goalErr != nil {
			logger.Warnf("Goal evaluation error: %v", goalErr)
		}
	}

	// M1F: Stop / StopFailure hooks fire at turn termination. Hooks may signal
	// that the agent should keep going by returning ActionBlock; in that case
	// the executor returns ErrContinueRequested so a higher layer can decide.
	if t.hookEngine != nil && !goalContinue {
		stopEvent := middleware.HookStop
		status := "success"
		if turnErr != nil {
			stopEvent = middleware.HookStopFailure
			status = "failed"
		}
		stopParams := map[string]interface{}{
			"turn_id":          t.ID,
			"session_id":       t.SessionID,
			"transcript_path":  "",
			"cwd":              t.WorkingDir,
			"hook_event_name":  "Stop",
			"stop_hook_active": false,
			"iteration":        0,
			"status":           status,
			"iterations":       t.CompletionCriteria.CurrentIteration,
		}
		if t.transcript != nil {
			stopParams["transcript_path"] = t.transcript.Path()
		}
		if t.ralphContext != nil {
			stopParams["stop_hook_active"] = t.ralphContext.IsActive()
			stopParams["iteration"] = t.ralphContext.Iteration()
		}
		if dec, herr := t.hookEngine.Execute(ctx, stopEvent, "turn_stop", stopParams); herr != nil {
			logger.Warnf("Stop hook execution error: %v", herr)
		} else if dec != nil {
			if msg, ok := dec.AuditMetadata["systemMessage"].(string); ok && msg != "" && t.eventHandler != nil {
				t.eventHandler(event.NewStreamEvent(event.EventTypeWarning, "agent_turn").WithContent(msg))
			}
			if dec.Action == middleware.ActionBlock {
				if t.ralphContext != nil && t.ralphContext.IsActive() {
					logger.Warnf("Stop hook returned block while stop_hook_active=true for session %s iteration %d; ignoring to prevent infinite loop", t.SessionID, t.ralphContext.Iteration())
				} else {
					logger.Infof("Stop hook requested continuation: %s", dec.Reason)
					t.continuationReason = dec.Reason
					turnErr = ErrContinueRequested
				}
			}
		}
	}

	e.emitter.executorState("closing", map[string]interface{}{
		"iterations": t.CompletionCriteria.CurrentIteration,
	})
	t.close()
	return turnErr
}

func (e *turnExecutor) evaluateGoal(ctx context.Context) error {
	t := e.turn
	condition := t.goalContext.Condition()
	if t.goalEvaluator == nil {
		// Provide GoalConfig to evaluator
		goalCfg := config.GoalConfig{}
		if t.agentConfig != nil && t.agentConfig.Goal != nil {
			goalCfg = *t.agentConfig.Goal
		}
		t.goalEvaluator = NewGoalEvaluator(t.LLMClient, goalCfg)
	}
	result, err := t.goalEvaluator.Evaluate(ctx, condition, t.Messages)
	if err != nil {
		return err
	}

	// Emit event if transcript was truncated
	if result.TranscriptStats != nil && t.eventHandler != nil {
		payload := map[string]interface{}{
			"original_messages":  result.TranscriptStats.OriginalMessages,
			"preserved_messages": result.TranscriptStats.PreservedMessages,
			"original_bytes":     result.TranscriptStats.OriginalBytes,
			"final_bytes":        result.TranscriptStats.FinalBytes,
			"skipped_messages":   result.TranscriptStats.SkippedMessages,
		}
		t.eventHandler(event.StreamEvent{
			Type:    event.EventTypeGoalEvaluatorTruncated,
			Source:  "goal_evaluator",
			Payload: payload,
		})
	}

	t.goalContext.MarkEvaluated(result.TokensUsed, result.Reason)

	// Fail-closed: if evaluator consistently fails to parse, stop the goal
	if result.EvaluatorParseFailed {
		t.goalContext.IncrementParseFailures()
		if t.goalContext.ConsecutiveParseFailures() >= 3 {
			t.goalContext.MarkStopped("evaluator_parse_failure")
			if t.eventHandler != nil {
				t.eventHandler(event.NewStreamEvent(event.EventTypeWarning, "agent_turn").
					WithContent("⚠️ /goal stopped: evaluator returned invalid JSON 3 consecutive times"))
			}
			return nil
		}
		t.continuationReason = result.Reason
		return ErrContinueRequested
	}
	t.goalContext.ResetParseFailures()

	if result.Met {
		t.goalContext.MarkAchieved(result.Reason)
		if t.eventHandler != nil {
			t.eventHandler(event.NewStreamEvent(event.EventTypeWarning, "agent_turn").
				WithContent("✅ /goal achieved: " + result.Reason))
		}
		return nil
	}
	if t.goalContext.TurnsEvaluated() >= t.goalContext.MaxTurns() {
		reason := fmt.Sprintf("/goal max turns reached: %s", result.Reason)
		t.goalContext.MarkStopped(reason)
		if t.eventHandler != nil {
			t.eventHandler(event.NewStreamEvent(event.EventTypeWarning, "agent_turn").WithContent(reason))
		}
		return nil
	}
	t.continuationReason = result.Reason
	return ErrContinueRequested
}

// K1.3: evaluateSatisfaction evaluates user satisfaction and marks it if threshold is met
func (e *turnExecutor) evaluateSatisfaction(ctx context.Context) error {
	t := e.turn
	// Skip satisfaction evaluation if already marked or if task is already completed
	if t.CompletionCriteria != nil && (t.CompletionCriteria.UserSatisfied || t.CompletionCriteria.TaskCompleted) {
		return nil
	}

	// Only evaluate satisfaction after at least 2 iterations to have meaningful conversation history
	if t.CompletionCriteria == nil || t.CompletionCriteria.CurrentIteration < 2 {
		return nil
	}

	// Initialize evaluator if needed
	if t.goalEvaluator == nil {
		goalCfg := config.GoalConfig{}
		if t.agentConfig != nil && t.agentConfig.Goal != nil {
			goalCfg = *t.agentConfig.Goal
		}
		t.goalEvaluator = NewGoalEvaluator(t.LLMClient, goalCfg)
	}

	// Evaluate satisfaction
	result, err := t.goalEvaluator.EvaluateSatisfaction(ctx, t.UserInput, t.Messages)
	if err != nil {
		logger.Warnf("Satisfaction evaluation failed: %v", err)
		return nil // Don't fail the turn on evaluation errors
	}

	// Update satisfaction score only if evaluator parsed successfully
	if t.CompletionCriteria != nil {
		if result.EvaluatorParseFailed {
			logger.Warnf("satisfaction evaluator parse failed; keeping previous score (raw: %s)", result.Reason)
		} else {
			t.CompletionCriteria.LLMSatisfactionScore = result.Score
		}
	}

	// Mark user satisfied if score is >= satisfactionThreshold
	satisfactionThreshold := 0.7 // Default threshold
	if t.agentConfig != nil && t.agentConfig.Turn != nil && t.agentConfig.Turn.SatisfactionThreshold > 0 {
		satisfactionThreshold = t.agentConfig.Turn.SatisfactionThreshold
	}
	if result.Score >= satisfactionThreshold {
		t.MarkUserSatisfied(result.Reason)
		logger.Infof("User satisfaction threshold met: score=%.2f, reason=%s", result.Score, result.Reason)
	} else {
		logger.Debugf("User satisfaction score: %.2f (threshold=%.2f), reason=%s", result.Score, satisfactionThreshold, result.Reason)
	}

	return nil
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

	// K1.2: Heuristic - detect completion signals in LLM response text
	if len(toolCalls) == 0 {
		responseLower := strings.ToLower(response)
		if strings.Contains(responseLower, "task complete") ||
			strings.Contains(responseLower, "task is complete") ||
			strings.Contains(responseLower, "task finished") ||
			strings.Contains(responseLower, "task is finished") ||
			strings.Contains(responseLower, "task done") ||
			strings.Contains(responseLower, "successfully completed") {
			logger.Infof("Detected completion signal in LLM response")
			t.UpdateCompletionStatus(true, "")
		}
	}

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
		t.CompletionCriteria.CurrentIteration++
		logger.Debugf("Turn error recorded: %s (consecutive: %d, total: %d)",
			err.Error(), t.CompletionCriteria.ConsecutiveErrors, t.CompletionCriteria.ErrorCount)
	} else {
		t.updateConsecutiveErrorsFromToolResults(toolResults)
		// Increment iteration counter (error counting already handled by updateConsecutiveErrorsFromToolResults)
		t.CompletionCriteria.CurrentIteration++
	}
	t.recordTokenGain()

	// K1.3: Evaluate user satisfaction periodically
	if err := e.evaluateSatisfaction(ctx); err != nil {
		logger.Warnf("Satisfaction evaluation error: %v", err)
	}

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
	remainingTools, toolResults := e.handleTaskDoneTools(toolsToExecute)
	if len(remainingTools) == 0 {
		return toolResults
	}
	executedResults, err := t.executeToolCallsInParallel(ctx, remainingTools)
	if err != nil {
		logger.Errorf("Tool execution infrastructure error: %v", err)
		executedResults = t.buildFallbackResults(remainingTools, err)
		t.CompletionCriteria.ErrorCount++
	}
	for id, result := range executedResults {
		toolResults[id] = result
	}
	return toolResults
}

func (e *turnExecutor) handleTaskDoneTools(toolsToExecute []ToolToExecute) ([]ToolToExecute, map[string]*interfaces.ToolResult) {
	t := e.turn
	results := make(map[string]*interfaces.ToolResult)
	remaining := make([]ToolToExecute, 0, len(toolsToExecute))
	for _, tool := range toolsToExecute {
		if strings.TrimSpace(tool.Name) != "task_done" {
			remaining = append(remaining, tool)
			continue
		}
		status, summary := parseTaskDoneArguments(tool.Parameters)
		success := status == "" || strings.EqualFold(status, "success") || strings.EqualFold(status, "completed")
		if t.eventHandler != nil {
			t.eventHandler(event.StreamEvent{
				Type:   event.EventTypeToolUse,
				Source: "agent_turn",
				ToolUse: &event.ToolUse{
					ID:         tool.ID,
					ToolName:   tool.Name,
					Parameters: tool.Parameters,
					Status:     "executing",
				},
			})
			t.eventHandler(event.StreamEvent{
				Type: event.EventTypeToolResult,
				ToolResult: &tools.ToolResult{
					ID:      tool.ID,
					Content: "task_done acknowledged",
				},
			})
		}
		reason := summary
		if reason == "" {
			reason = "task_done"
		}
		t.MarkTaskCompleted(reason)
		if t.eventHandler != nil {
			t.eventHandler(event.StreamEvent{
				Type:   event.EventTypeToolUse,
				Source: "agent_turn",
				ToolUse: &event.ToolUse{
					ID:       tool.ID,
					ToolName: tool.Name,
					Status:   "success",
					Result:   reason,
				},
			})
		}
		results[tool.ID] = &interfaces.ToolResult{
			Success:     success,
			LLMContent:  "Task completion signal received: " + reason,
			UserContent: reason,
			Metadata: map[string]interface{}{
				"tool_name": "task_done",
				"status":    status,
			},
		}
	}
	return remaining, results
}

func parseTaskDoneArguments(params map[string]interface{}) (status string, summary string) {
	if v, ok := params["status"].(string); ok {
		status = v
	}
	if v, ok := params["summary"].(string); ok {
		summary = v
	}
	if summary == "" {
		if v, ok := params["message"].(string); ok {
			summary = v
		}
	}
	// Compatibility layer: mock LLMs and provider adapters may preserve raw
	// function-call argument JSON under __raw when the provider-native payload
	// cannot be safely decoded into the standard parameter map before this
	// parser runs. Decode that raw JSON here so task_done remains an internal
	// completion signal even without a registered concrete tool implementation.
	if len(params) == 1 {
		if raw, ok := params["__raw"].(string); ok {
			var decoded map[string]interface{}
			if json.Unmarshal([]byte(raw), &decoded) == nil {
				return parseTaskDoneArguments(decoded)
			}
		}
	}
	return status, summary
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
