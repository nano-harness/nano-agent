package llm

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// CircuitState represents the state of the circuit breaker
type CircuitState int

const (
	// CircuitClosed is the normal state allowing requests
	CircuitClosed CircuitState = iota
	// CircuitOpen is the tripped state rejecting requests
	CircuitOpen
	// CircuitHalfOpen allows a single probe request to test recovery
	CircuitHalfOpen
)

// String returns the string representation of CircuitState
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig holds configuration for the circuit breaker
type CircuitBreakerConfig struct {
	// MaxRetries is the maximum number of retry attempts per request
	MaxRetries int
	// FailureThreshold is the number of consecutive failures before opening
	FailureThreshold int
	// SuccessThreshold is the number of successes in half-open state to close
	SuccessThreshold int
	// OpenTimeout is how long to wait in open state before transitioning to half-open
	OpenTimeout time.Duration
	// BaseDelay is the initial retry delay
	BaseDelay time.Duration
	// MaxDelay is the maximum retry delay
	MaxDelay time.Duration
	// BackoffFactor is the multiplier for exponential backoff
	BackoffFactor float64
	// JitterFactor adds randomness to prevent thundering herd (0.0 to 1.0)
	JitterFactor float64
}

// DefaultCircuitBreakerConfig returns sensible defaults
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxRetries:       3,
		FailureThreshold: 5,
		SuccessThreshold: 2,
		OpenTimeout:      60 * time.Second,
		BaseDelay:        2 * time.Second,
		MaxDelay:         60 * time.Second,
		BackoffFactor:    2.0,
		JitterFactor:     0.2,
	}
}

// CircuitBreaker implements the circuit breaker pattern for LLM API calls
type CircuitBreaker struct {
	mu                sync.Mutex
	state             CircuitState
	config            CircuitBreakerConfig
	consecutiveFails  int
	halfOpenSuccesses int
	halfOpenFails     int  // tracks consecutive failures specifically in half-open state
	halfOpenProbeUsed bool // limits half-open to a single probe at a time
	lastFailureTime   time.Time
	lastStateChange   time.Time
	totalRequests     int64
	totalFailures     int64
	totalRetries      int64
}

// NewCircuitBreaker creates a new circuit breaker with the given config
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	if cfg.OpenTimeout <= 0 {
		cfg.OpenTimeout = 60 * time.Second
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 2 * time.Second
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 60 * time.Second
	}
	if cfg.BackoffFactor <= 0 {
		cfg.BackoffFactor = 2.0
	}
	if cfg.JitterFactor < 0 {
		cfg.JitterFactor = 0
	} else if cfg.JitterFactor > 1 {
		cfg.JitterFactor = 1
	}

	return &CircuitBreaker{
		state:           CircuitClosed,
		config:          cfg,
		lastStateChange: time.Now(),
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open
var ErrCircuitOpen = errors.New("circuit breaker is open: LLM API rate limit protection active, please wait")

// AllowRequest checks if a request is allowed through the circuit breaker.
// Returns nil if allowed, ErrCircuitOpen if the circuit is open and not yet
// ready for a probe.
func (cb *CircuitBreaker) AllowRequest() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return nil
	case CircuitOpen:
		// Check if enough time has passed to transition to half-open
		if time.Since(cb.lastFailureTime) >= cb.config.OpenTimeout {
			cb.setState(CircuitHalfOpen)
			cb.halfOpenProbeUsed = true // this request is the first probe
			cb.halfOpenFails = 0        // reset half-open failure counter for fresh probing
			logger.Infof("Circuit breaker transitioning to half-open state after %v cooldown", cb.config.OpenTimeout)
			return nil
		}
		remaining := cb.config.OpenTimeout - time.Since(cb.lastFailureTime)
		return fmt.Errorf("%w (retry in %v)", ErrCircuitOpen, remaining.Round(time.Second))
	case CircuitHalfOpen:
		if cb.halfOpenProbeUsed {
			return fmt.Errorf("%w (probe already in-flight)", ErrCircuitOpen)
		}
		cb.halfOpenProbeUsed = true
		return nil
	}
	return nil
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalRequests++

	switch cb.state {
	case CircuitHalfOpen:
		cb.halfOpenSuccesses++
		cb.halfOpenProbeUsed = false // allow next probe
		if cb.halfOpenSuccesses >= cb.config.SuccessThreshold {
			cb.setState(CircuitClosed)
			cb.consecutiveFails = 0
			cb.halfOpenSuccesses = 0
			logger.Infof("Circuit breaker closed after %d consecutive successes in half-open state", cb.config.SuccessThreshold)
		}
	case CircuitClosed:
		cb.consecutiveFails = 0
	}
}

// RecordFailure records a failed request and potentially opens the circuit
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalRequests++
	cb.totalFailures++
	cb.consecutiveFails++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		if cb.consecutiveFails >= cb.config.FailureThreshold {
			cb.setState(CircuitOpen)
			logger.Warnf("Circuit breaker opened after %d consecutive failures", cb.consecutiveFails)
		}
	case CircuitHalfOpen:
		// Gradual recovery: allow multiple probe attempts instead of immediate re-open
		// Only re-open after 2 consecutive failures in half-open state
		cb.halfOpenFails++
		cb.halfOpenSuccesses = 0
		cb.halfOpenProbeUsed = false
		if cb.halfOpenFails >= 2 {
			cb.setState(CircuitOpen)
			logger.Warnf("Circuit breaker re-opened after %d consecutive failures in half-open state", cb.halfOpenFails)
		} else {
			logger.Warnf("Circuit breaker half-open probe failed (%d/2), allowing another probe", cb.halfOpenFails)
		}
	}
}

// RecordRetry increments the retry counter
func (cb *CircuitBreaker) RecordRetry() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.totalRetries++
}

// State returns the current circuit breaker state
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Stats returns circuit breaker statistics
func (cb *CircuitBreaker) Stats() map[string]interface{} {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return map[string]interface{}{
		"state":             cb.state.String(),
		"consecutive_fails": cb.consecutiveFails,
		"total_requests":    cb.totalRequests,
		"total_failures":    cb.totalFailures,
		"total_retries":     cb.totalRetries,
		"last_failure_time": cb.lastFailureTime,
		"last_state_change": cb.lastStateChange,
	}
}

func (cb *CircuitBreaker) setState(s CircuitState) {
	cb.state = s
	cb.lastStateChange = time.Now()
}

// CalculateRetryDelay returns the delay before the next retry attempt,
// using exponential backoff with jitter.
func (cb *CircuitBreaker) CalculateRetryDelay(attempt int, httpStatus int) time.Duration {
	cb.mu.Lock()
	cfg := cb.config
	cb.mu.Unlock()

	delay := time.Duration(float64(cfg.BaseDelay) * math.Pow(cfg.BackoffFactor, float64(attempt)))

	// Rate limit errors get extra delay
	if httpStatus == http.StatusTooManyRequests {
		delay = delay * 2
	}

	// Cap at max delay
	if delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}

	// Add jitter to prevent thundering herd
	if cfg.JitterFactor > 0 {
		jitter := time.Duration(float64(delay) * cfg.JitterFactor * rand.Float64()) //nolint:gosec
		delay += jitter
	}

	return delay
}

// IsRetryableError checks if an error from an LLM API call should be retried.
// It returns true for rate-limit (429), server errors (500, 502, 503, 504),
// and transient network/timeout errors.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())

	// Never retry authentication/authorization/quota errors
	nonRetryable := []string{
		"unauthorized",
		"invalid api key",
		"authentication",
		"insufficient_quota",
		"exceeded your current quota",
		"billing",
		"permission denied",
		"context canceled",
		"model not found",
		"invalid model",
	}
	for _, s := range nonRetryable {
		if strings.Contains(errMsg, s) {
			return false
		}
	}

	// Use ExtractHTTPStatus for reliable status code checks (avoids false positives from substring matching)
	if httpStatus := ExtractHTTPStatus(err); httpStatus > 0 {
		switch httpStatus {
		case http.StatusTooManyRequests: // 429
			return true
		case http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout: // 500, 502, 503, 504
			return true
		}
	}

	// Retry rate-limit errors identified by text
	if strings.Contains(errMsg, "too many requests") || strings.Contains(errMsg, "rate limit") {
		return true
	}

	// Retry server errors identified by text
	if strings.Contains(errMsg, "internal server error") || strings.Contains(errMsg, "bad gateway") ||
		strings.Contains(errMsg, "service unavailable") || strings.Contains(errMsg, "gateway timeout") {
		return true
	}

	// Retry network/timeout errors
	if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline exceeded") ||
		strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "no such host") || strings.Contains(errMsg, "network") {
		return true
	}

	return false
}

// ExtractHTTPStatus extracts the HTTP status code from an error.
// It tries to parse the status code from the error message produced by
// the OpenAI Go SDK (format: METHOD "URL": STATUS STATUS_TEXT BODY).
func ExtractHTTPStatus(err error) int {
	if err == nil {
		return 0
	}

	msg := err.Error()

	// The OpenAI SDK error format is: POST "url": 429 Too Many Requests {...}
	// Try to extract status code after the URL closing quote
	if idx := strings.Index(msg, "\": "); idx >= 0 {
		rest := msg[idx+3:]
		var code int
		if _, scanErr := fmt.Sscanf(rest, "%d", &code); scanErr == nil && code >= 100 && code < 600 {
			return code
		}
	}

	// Fallback: look for HTTP status codes at word boundaries to avoid matching
	// digits embedded in other tokens (e.g. error code "1500").
	// We check that the digit sequence is preceded and followed by a non-digit.
	statusCodes := []int{429, 500, 502, 503, 504}
	for _, code := range statusCodes {
		codeStr := fmt.Sprintf("%d", code)
		idx := 0
		for {
			pos := strings.Index(msg[idx:], codeStr)
			if pos < 0 {
				break
			}
			absPos := idx + pos
			startOK := absPos == 0 || !isDigit(msg[absPos-1])
			endPos := absPos + len(codeStr)
			endOK := endPos >= len(msg) || !isDigit(msg[endPos])
			if startOK && endOK {
				return code
			}
			idx = absPos + len(codeStr)
		}
	}

	return 0
}

// isDigit returns true if the byte is an ASCII digit.
func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
