package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
	turnpolicy "github.com/nano-harness/nano-agent/pkg/policy"
)

// attemptLLMRecovery attempts multi-layer recovery for LLM API failures
// Returns true if recovery was successful and the request should be retried
func (t *Turn) attemptLLMRecovery(ctx context.Context, err error, attempt int) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())

	// Recovery Path 1: prompt_too_long → compress context and retry
	if strings.Contains(errMsg, "prompt_too_long") ||
		strings.Contains(errMsg, "context_length") ||
		strings.Contains(errMsg, "maximum context length") ||
		strings.Contains(errMsg, "context length exceeded") {
		logger.Infof("Attempting recovery from context length error via compression (attempt %d)", attempt)
		compErr := t.CompressMessages(ctx, true)
		if compErr == nil {
			logger.Infof("Context compression succeeded, retrying LLM request")
			return true
		}
		logger.Warnf("Context compression failed: %v", compErr)
	}

	// max_tokens related API errors are not recoverable by appending more prompt
	// content; doing so increases context size without changing the token limit.
	if strings.Contains(errMsg, "max_tokens") ||
		strings.Contains(errMsg, "maximum tokens") ||
		strings.Contains(errMsg, "output length") {
		logger.Warnf("Max tokens/output length error is not recoverable via retry in attemptLLMRecovery (attempt %d); reduce context or adjust token limits", attempt)
		return false
	}

	return false
}

// shouldTerminate checks if the turn should terminate based on completion criteria
func (t *Turn) shouldTerminate() bool {
	return t.turnPolicyDecision().Action == turnpolicy.ActionTerminate
}

func (t *Turn) turnPolicyDecision() turnpolicy.Decision {
	if t.agent != nil {
		if pm := t.agent.GetPermissionManager(); pm != nil && pm.DenialTrackerLockedOut() {
			t.SetTerminationInfo(
				"classifier_lockout",
				"classifier_lockout",
				"consecutive deny limit reached",
			)
			return turnpolicy.NewDecision(turnpolicy.ActionTerminate, "classifier_lockout").
				WithMetadata("reason", "classifier_lockout")
		}
	}

	var state turnpolicy.TurnState
	if t.ctx != nil {
		state.ContextErr = t.ctx.Err()
	}
	if t.CompletionCriteria != nil {
		state.TaskCompleted = t.CompletionCriteria.TaskCompleted
		state.ConsecutiveErrors = t.CompletionCriteria.ConsecutiveErrors
		state.ErrorThreshold = t.CompletionCriteria.ErrorThreshold
		state.DiminishingReturnsWindow = t.CompletionCriteria.DiminishingReturnsWindow
		state.SimilarContentThreshold = t.CompletionCriteria.MaxSimilarContent
		if t.CompletionCriteria.DiminishingReturnsEnabled {
			state.DiminishingReturns = t.hasDiminishingReturns()
		}
		if t.CompletionCriteria.LoopDetectionEnabled {
			state.SimilarContentLoop = t.hasSimilarContent()
		}
	}

	decision := turnpolicy.NewTurnPolicy().Evaluate(state)
	if decision.Action != turnpolicy.ActionTerminate {
		return decision
	}

	// Set structured termination metadata based on the decision reason
	reason := decision.Metadata["reason"]
	switch reason {
	case "context_done":
		logger.Infof("Turn context cancelled/timed out: %v", state.ContextErr)
		fingerprint := "context_canceled"
		if state.ContextErr == context.DeadlineExceeded {
			fingerprint = "context_timeout"
		}
		t.SetTerminationInfo("context_done", fingerprint, fmt.Sprintf("context: %v", state.ContextErr))
	case "error_threshold":
		logger.Infof("Too many consecutive errors (%d >= %d): terminating turn",
			state.ConsecutiveErrors, state.ErrorThreshold)
		t.SetTerminationInfo("error_threshold",
			fmt.Sprintf("errors:%d_consecutive", state.ConsecutiveErrors),
			fmt.Sprintf("too many consecutive errors (%d >= %d)", state.ConsecutiveErrors, state.ErrorThreshold))
	case "diminishing_returns":
		logger.Infof("Diminishing-returns detected: stopping turn after %d consecutive low-gain iterations",
			state.DiminishingReturnsWindow)
		t.SetTerminationInfo("diminishing_returns",
			fmt.Sprintf("dr_no_progress:%drounds", state.DiminishingReturnsWindow),
			fmt.Sprintf("diminishing returns after %d low-gain iterations", state.DiminishingReturnsWindow))
	case "similar_content_loop":
		logger.Infof("Loop detected: %d consecutive similar LLM responses (threshold %d)",
			len(t.CompletionCriteria.ContentSimilarityHistory), t.CompletionCriteria.MaxSimilarContent)
		t.SetTerminationInfo("similar_content_loop",
			"loop:similar_content",
			fmt.Sprintf("LLM output repeated %d times without meaningful change", t.CompletionCriteria.MaxSimilarContent))
		t.emitLoopDetectedEvent("similar_content",
			fmt.Sprintf("LLM output repeated %d times without meaningful change", t.CompletionCriteria.MaxSimilarContent))
	default:
		// Fallback for unknown termination reasons
		t.SetTerminationInfo("unknown", "unknown_termination", fmt.Sprintf("reason: %v", reason))
	}

	return decision
}

// emitLoopDetectedEvent fires an EventTypeLoopDetected event with context.
func (t *Turn) emitLoopDetectedEvent(loopType, reason string) {
	if t.eventHandler == nil {
		return
	}
	evt := event.NewStreamEvent(event.EventTypeLoopDetected, "agent_turn")
	evt = evt.WithContent(reason)
	evt = evt.WithMetadata("loop_type", loopType)
	evt = evt.WithMetadata("reason", reason)
	if t.CompletionCriteria != nil {
		evt = evt.WithMetadata("iteration", t.CompletionCriteria.CurrentIteration)
	}
	t.eventHandler(evt)
}

// updateContentHistory appends an LLM response snippet and keeps the window trimmed.
// Call this after each requestOpenAIAPI that produced a non-empty text response.
func (t *Turn) updateContentHistory(response string) {
	cc := t.CompletionCriteria
	if cc == nil || !cc.LoopDetectionEnabled || response == "" {
		return
	}

	// Normalise: lowercase, collapse whitespace, trim to 200 chars for comparison
	normalised := strings.ToLower(strings.Join(strings.Fields(response), " "))
	if len(normalised) > 200 {
		normalised = normalised[:200]
	}

	maxHistory := cc.MaxSimilarContent + 1
	if maxHistory < 2 {
		maxHistory = 2
	}
	cc.ContentSimilarityHistory = append(cc.ContentSimilarityHistory, normalised)
	if len(cc.ContentSimilarityHistory) > maxHistory {
		cc.ContentSimilarityHistory = cc.ContentSimilarityHistory[len(cc.ContentSimilarityHistory)-maxHistory:]
	}
}

// hasSimilarContent returns true when the last MaxSimilarContent responses are
// all identical after normalisation.
func (t *Turn) hasSimilarContent() bool {
	cc := t.CompletionCriteria
	threshold := cc.MaxSimilarContent
	if threshold <= 0 {
		threshold = 3
	}
	if len(cc.ContentSimilarityHistory) < threshold {
		return false
	}
	// Check that the last `threshold` entries are identical
	recent := cc.ContentSimilarityHistory[len(cc.ContentSimilarityHistory)-threshold:]
	first := recent[0]
	for _, s := range recent[1:] {
		if s != first {
			return false
		}
	}
	return true
}

// recordTokenGain samples the current context token count and appends the delta
// to the diminishing-returns history ring buffer. Call this after each LLM round-trip.
func (t *Turn) recordTokenGain() {
	cc := t.CompletionCriteria
	if cc == nil || !cc.DiminishingReturnsEnabled {
		return
	}

	currentTokens := t.compressionStrategy.EstimateTokenCount(t.Messages)

	// Skip recording samples during any error recovery: failures often produce
	// short diagnostic messages, causing naturally low token gain that pollutes
	// diminishing-returns history. Still update prevTokens so the delta after
	// recovery is calculated from the current point.
	// Only non-error-recovery "healthy" iterations should contribute to
	// low-gain detection. Failed tool calls often add short diagnostic messages;
	// counting those samples would make recovery paths look like stalled work.
	if cc.ConsecutiveErrors > 0 {
		// Keep the baseline current so the first healthy sample after recovery
		// measures only post-recovery progress, not accumulated error text.
		cc.diminishingReturnsPrevTokens = currentTokens
		return
	}

	delta := currentTokens - cc.diminishingReturnsPrevTokens
	if delta < 0 {
		delta = 0 // after compression the count may drop; treat as zero gain
	}
	cc.diminishingReturnsPrevTokens = currentTokens

	// Keep only the last Window samples
	cc.diminishingReturnsHistory = append(cc.diminishingReturnsHistory, delta)
	window := cc.DiminishingReturnsWindow
	if window <= 0 {
		window = 3
	}
	if len(cc.diminishingReturnsHistory) > window {
		cc.diminishingReturnsHistory = cc.diminishingReturnsHistory[len(cc.diminishingReturnsHistory)-window:]
	}
}

// hasDiminishingReturns returns true when both dimensions of token gain are
// below the threshold:
//   - Dimension 1 (single-point): the most recent iteration's gain < MinGain
//   - Dimension 2 (cumulative): sum of all gains in the window < MinGain
//
// This two-dimension gain check prevents misclassifying steady near-threshold progress as "stuck".
// The latest sample catches immediate progress, while the cumulative window
// catches several smaller-but-meaningful updates across consecutive iterations.
// For example, if MinGain is 500 and the cumulative window threshold is
// 1000, per-iteration gains [450, 480, 490] are each below MinGain, but
// their cumulative gain of 1420 still indicates meaningful progress.
func (t *Turn) hasDiminishingReturns() bool {
	cc := t.CompletionCriteria

	// Skip diminishing returns detection if we're in error recovery mode
	// because error recovery naturally has low token counts
	if cc.ConsecutiveErrors > 0 {
		return false
	}

	window := cc.DiminishingReturnsWindow
	if window <= 0 {
		window = 3
	}
	minGain := cc.DiminishingReturnsMinGain
	if minGain <= 0 {
		minGain = 500
	}

	if len(cc.diminishingReturnsHistory) < window {
		return false // not enough samples yet
	}

	// Dimension 1: Check most recent single-point delta
	lastDelta := cc.diminishingReturnsHistory[len(cc.diminishingReturnsHistory)-1]
	if lastDelta >= minGain {
		return false // Recent iteration shows meaningful progress
	}

	// Dimension 2: Check cumulative delta across the window
	var cumulative int
	for _, gain := range cc.diminishingReturnsHistory {
		cumulative += gain
	}
	if cumulative >= minGain {
		return false // Window shows meaningful cumulative progress
	}

	return true // Both dimensions below threshold → truly stuck
}
