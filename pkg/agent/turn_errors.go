package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// LLMErrorType represents the category of LLM errors
type LLMErrorType int

// ErrContinueRequested is returned when a Stop/StopFailure hook signals that
// the agent should keep working. Higher layers can interpret this to decide
// whether to launch another turn instead of finalising the session.
var ErrContinueRequested = errors.New("turn continuation requested by hook")

const (
	// LLMErrorTransient represents temporary network or connectivity issues
	LLMErrorTransient LLMErrorType = iota
	// LLMErrorPermanent represents configuration or authentication failures
	LLMErrorPermanent
	// LLMErrorRateLimit represents rate limiting errors
	LLMErrorRateLimit
)

// String returns the string representation of the error type
func (t LLMErrorType) String() string {
	switch t {
	case LLMErrorTransient:
		return "transient"
	case LLMErrorPermanent:
		return "permanent"
	case LLMErrorRateLimit:
		return "rate_limit"
	default:
		return "unknown"
	}
}

// ClassifyLLMError analyzes an error and returns its classification
func ClassifyLLMError(err error) LLMErrorType {
	if err == nil {
		return LLMErrorTransient
	}

	errStr := strings.ToLower(err.Error())

	// Check for rate limit errors (429 status code or rate limit keywords)
	rateLimitIndicators := []string{
		"429",
		"rate limit",
		"rate_limit",
		"too many requests",
		"quota exceeded",
		"request limit",
	}
	for _, indicator := range rateLimitIndicators {
		if strings.Contains(errStr, indicator) {
			return LLMErrorRateLimit
		}
	}

	// Check for permanent errors (authentication, authorization, invalid config)
	permanentIndicators := []string{
		"400",
		"401",
		"403",
		"bad request",
		"invalid api key",
		"invalid_api_key",
		"authentication failed",
		"unauthorized",
		"forbidden",
		"invalid credentials",
		"api key not found",
		"invalid model",
		"model not found",
		"invalid configuration",
	}
	for _, indicator := range permanentIndicators {
		if strings.Contains(errStr, indicator) {
			return LLMErrorPermanent
		}
	}

	// Check for context-specific permanent errors
	if errors.Is(err, context.Canceled) {
		return LLMErrorPermanent
	}

	// Default to transient for all other errors (conservative approach)
	// This includes: timeouts, connection resets, network errors, etc.
	return LLMErrorTransient
}

// WrapLLMError wraps an error with additional context about its type
func WrapLLMError(err error, errType LLMErrorType) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s LLM error: %w", errType.String(), err)
}
