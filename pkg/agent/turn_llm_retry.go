package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

func (t *Turn) requestLLMWithRetry(ctx context.Context, maxRetries int) (string, []*tools.ToolCall, bool, error) {
	var response string
	var toolCalls []*tools.ToolCall
	var err error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		response, toolCalls, err = t.requestOpenAIAPI(ctx)
		if err == nil {
			return response, toolCalls, false, nil
		}

		// Check for permanent errors - never retry these
		if errType := ClassifyLLMError(err); errType == LLMErrorPermanent {
			logger.Errorf("LLM API request failed with permanent error (no retry): %v", err)
			return "", nil, false, WrapLLMError(err, errType)
		}

		// Attempt LLM recovery paths before giving up
		if recovered := t.attemptLLMRecovery(ctx, err, attempt); recovered {
			continue
		}

		if attempt < maxRetries {
			backoff := time.Duration(attempt) * 2 * time.Second
			logger.Warnf("LLM API request failed (attempt %d/%d): %v — retrying in %v",
				attempt, maxRetries, err, backoff)
			select {
			case <-ctx.Done():
				return "", nil, false, fmt.Errorf("LLM API request cancelled: %w", ctx.Err())
			case <-time.After(backoff):
			}
			continue
		}

		// Last attempt failed - record error but don't terminate immediately
		logger.Errorf("LLM API request failed after %d attempts: %v", maxRetries, err)
		t.CompletionCriteria.ConsecutiveErrors++
		if t.CompletionCriteria.ConsecutiveErrors >= t.CompletionCriteria.ErrorThreshold {
			return "", nil, false, fmt.Errorf("LLM API request failed after exhausting all recovery paths: %w", err)
		}

		backoffDelay := t.llmLoopBackoffDelay()
		logger.Warnf("Waiting %v before retrying turn loop after LLM failure (consecutive errors: %d/%d)",
			backoffDelay, t.CompletionCriteria.ConsecutiveErrors, t.CompletionCriteria.ErrorThreshold)
		select {
		case <-ctx.Done():
			return "", nil, false, ctx.Err()
		case <-time.After(backoffDelay):
		}

		return "", nil, true, nil
	}

	return response, toolCalls, false, err
}

func (t *Turn) llmLoopBackoffDelay() time.Duration {
	maxBackoff := 60 * time.Second
	if cfg := config.Get(); cfg != nil && cfg.Advanced != nil &&
		cfg.Advanced.CircuitBreaker != nil &&
		cfg.Advanced.CircuitBreaker.MaxDelayMs > 0 {
		maxBackoff = time.Duration(cfg.Advanced.CircuitBreaker.MaxDelayMs) * time.Millisecond
	}

	shift := t.CompletionCriteria.ConsecutiveErrors - 1
	if shift < 0 {
		shift = 0
	}
	backoffDelay := time.Duration(5<<shift) * time.Second
	if backoffDelay > maxBackoff {
		backoffDelay = maxBackoff
	}
	return backoffDelay
}
