package middleware

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// BackoffStrategy defines the delay calculation strategy for retries.
type BackoffStrategy int

const (
	BackoffFixed       BackoffStrategy = iota // Constant delay
	BackoffLinear                             // Delay grows linearly
	BackoffExponential                        // Delay doubles each retry
)

// BackoffConfig configures the retry backoff behavior.
type BackoffConfig struct {
	Strategy     BackoffStrategy
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64 // Used for Linear and Exponential
	JitterRatio  float64 // 0.0 = no jitter, 1.0 = full jitter
	MaxAttempts  int
}

// DefaultBackoffConfig returns a sensible default backoff configuration.
func DefaultBackoffConfig() BackoffConfig {
	return BackoffConfig{
		Strategy:     BackoffExponential,
		InitialDelay: 200 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		JitterRatio:  0.2,
		MaxAttempts:  3,
	}
}

// ComputeDelay calculates the retry delay for the given attempt number (1-based).
func (c BackoffConfig) ComputeDelay(attempt int) time.Duration {
	var base time.Duration
	switch c.Strategy {
	case BackoffFixed:
		base = c.InitialDelay
	case BackoffLinear:
		base = time.Duration(float64(c.InitialDelay) * float64(attempt) * c.Multiplier)
	case BackoffExponential:
		base = time.Duration(float64(c.InitialDelay) * math.Pow(c.Multiplier, float64(attempt-1)))
	default:
		base = c.InitialDelay
	}
	if base > c.MaxDelay {
		base = c.MaxDelay
	}
	if c.JitterRatio > 0 {
		jitter := time.Duration(float64(base) * c.JitterRatio * rand.Float64()) //nolint:gosec
		base += jitter
	}
	return base
}

// isRetryable reports whether an error or tool result warrants a retry.
func isRetryable(result *interfaces.ToolResult, err error) bool {
	if err != nil {
		// Do not retry context cancellations.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		return true
	}
	if result != nil && !result.Success {
		// Retry transient tool failures (not security blocks).
		if result.Error != "" {
			return true
		}
	}
	return false
}

// ResilienceMiddleware retries tool execution using the configured backoff strategy.
type ResilienceMiddleware struct {
	config BackoffConfig
}

// NewResilienceMiddleware creates a ResilienceMiddleware with the given backoff config.
func NewResilienceMiddleware(cfg BackoffConfig) *ResilienceMiddleware {
	return &ResilienceMiddleware{config: cfg}
}

func (m *ResilienceMiddleware) Name() string { return "resilience" }

func (m *ResilienceMiddleware) Execute(
	ctx context.Context,
	tool interfaces.Tool,
	params map[string]interface{},
	next MiddlewareFunc,
) (*interfaces.ToolResult, error) {
	var lastResult *interfaces.ToolResult
	var lastErr error

	for attempt := 1; attempt <= m.config.MaxAttempts; attempt++ {
		result, err := next(ctx, tool, params)
		if !isRetryable(result, err) {
			return result, err
		}
		lastResult = result
		lastErr = err

		if attempt < m.config.MaxAttempts {
			delay := m.config.ComputeDelay(attempt)
			select {
			case <-ctx.Done():
				return lastResult, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return lastResult, lastErr
}
