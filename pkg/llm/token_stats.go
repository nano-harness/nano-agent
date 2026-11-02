package llm

import (
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
)

// TokenStats tracks token usage during LLM operations with enhanced real-time capabilities
type TokenStats struct {
	InputTokens       int
	OutputTokens      int
	RequestSizeBytes  int
	ResponseSizeBytes int
	StartTime         int64
	EndTime           int64

	// Session totals
	SessionInputTokens  int
	SessionOutputTokens int
	SessionTotalTokens  int

	// Real-time tracking
	LastUpdateTime      int64
	TokensPerSecond     float64
	PeakTokensPerSecond float64
	UpdateCount         int
	mu                  sync.RWMutex

	// Streaming state
	IsStreaming         bool
	StreamStartTime     int64
	LastOutputIncrement int

	// Reasoning-specific tracking
	ReasoningEnabled   bool
	ReasoningTokens    int
	ReasoningEffort    string
	ReasoningFallback  bool
	ReasoningStartTime int64
}

// Global session token tracker
var (
	sessionInputTokens  int
	sessionOutputTokens int
	sessionTotalTokens  int
	sessionMu           sync.Mutex
)

// NewTokenStats creates a new TokenStats with current time as start time
func NewTokenStats() *TokenStats {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	return &TokenStats{
		StartTime:           time.Now().UnixNano() / int64(time.Millisecond),
		SessionInputTokens:  sessionInputTokens,
		SessionOutputTokens: sessionOutputTokens,
		SessionTotalTokens:  sessionTotalTokens,
	}
}

// SetInputTokens sets the input token count and initializes streaming
func (ts *TokenStats) SetInputTokens(tokens int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.InputTokens = tokens
	ts.IsStreaming = true
	ts.StreamStartTime = time.Now().UnixNano() / int64(time.Millisecond)
	ts.LastUpdateTime = ts.StreamStartTime
}

// AddOutputTokens adds output tokens incrementally with real-time rate calculation
func (ts *TokenStats) AddOutputTokens(tokens int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if tokens <= 0 {
		return
	}

	ts.OutputTokens += tokens
	ts.LastOutputIncrement = tokens
	ts.UpdateCount++

	currentTime := time.Now().UnixNano() / int64(time.Millisecond)

	// Calculate tokens per second
	if ts.IsStreaming && ts.StreamStartTime > 0 {
		elapsedMs := currentTime - ts.StreamStartTime
		if elapsedMs > 0 {
			ts.TokensPerSecond = float64(ts.OutputTokens) / (float64(elapsedMs) / 1000.0)

			// Update peak rate
			if ts.TokensPerSecond > ts.PeakTokensPerSecond {
				ts.PeakTokensPerSecond = ts.TokensPerSecond
			}
		}
	}

	ts.LastUpdateTime = currentTime
}

// StartStreaming marks the beginning of streaming response
func (ts *TokenStats) StartStreaming() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.IsStreaming = true
	ts.StreamStartTime = time.Now().UnixNano() / int64(time.Millisecond)
	ts.LastUpdateTime = ts.StreamStartTime
	ts.UpdateCount = 0
	ts.TokensPerSecond = 0
}

// StopStreaming marks the end of streaming response
func (ts *TokenStats) StopStreaming() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.IsStreaming = false
}

// GetRealTimeStats returns current streaming statistics
func (ts *TokenStats) GetRealTimeStats() (tokensPerSec float64, peakRate float64, isStreaming bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	return ts.TokensPerSecond, ts.PeakTokensPerSecond, ts.IsStreaming
}

// GetEvent returns the event.TokenStats for event emission with real-time data
func (ts *TokenStats) GetEvent() *event.TokenStats {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Set end time if not already set
	if ts.EndTime == 0 {
		ts.EndTime = time.Now().UnixNano() / int64(time.Millisecond)
	}

	duration := ts.EndTime - ts.StartTime
	if duration < 0 {
		duration = 0
	}

	return &event.TokenStats{
		InputTokens:         ts.InputTokens,
		OutputTokens:        ts.OutputTokens,
		TotalTokens:         ts.InputTokens + ts.OutputTokens,
		RequestSizeBytes:    ts.RequestSizeBytes,
		ResponseSizeBytes:   ts.ResponseSizeBytes,
		StartTime:           ts.StartTime,
		EndTime:             ts.EndTime,
		Duration:            duration,
		SessionInputTokens:  ts.SessionInputTokens,
		SessionOutputTokens: ts.SessionOutputTokens,
		SessionTotalTokens:  ts.SessionTotalTokens,
		// Real-time fields
		TokensPerSecond:     ts.TokensPerSecond,
		PeakTokensPerSecond: ts.PeakTokensPerSecond,
		IsStreaming:         ts.IsStreaming,
		UpdateCount:         ts.UpdateCount,
		// Reasoning fields
		ReasoningEnabled:  ts.ReasoningEnabled,
		ReasoningTokens:   ts.ReasoningTokens,
		ReasoningEffort:   ts.ReasoningEffort,
		ReasoningFallback: ts.ReasoningFallback,
		ReasoningLatency:  ts.GetReasoningLatency(),
	}
}

// Finish marks the token stats as complete and sets end time
func (ts *TokenStats) Finish() {
	ts.EndTime = time.Now().UnixNano() / int64(time.Millisecond)

	// Update session totals
	sessionMu.Lock()
	sessionInputTokens += ts.InputTokens
	sessionOutputTokens += ts.OutputTokens
	sessionTotalTokens += ts.InputTokens + ts.OutputTokens

	// Update the session totals in this instance
	ts.SessionInputTokens = sessionInputTokens
	ts.SessionOutputTokens = sessionOutputTokens
	ts.SessionTotalTokens = sessionTotalTokens
	sessionMu.Unlock()
}

// SetReasoningEnabled marks reasoning as enabled for this request
func (ts *TokenStats) SetReasoningEnabled(enabled bool, effort string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.ReasoningEnabled = enabled
	ts.ReasoningEffort = effort
	if enabled {
		ts.ReasoningStartTime = time.Now().UnixNano() / int64(time.Millisecond)
	}
}

// SetReasoningTokens sets the number of reasoning tokens received
func (ts *TokenStats) SetReasoningTokens(tokens int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.ReasoningTokens = tokens
}

// SetReasoningFallback marks that reasoning fallback was used
func (ts *TokenStats) SetReasoningFallback(fallback bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.ReasoningFallback = fallback
}

// GetReasoningLatency calculates reasoning processing latency
func (ts *TokenStats) GetReasoningLatency() int64 {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if !ts.ReasoningEnabled || ts.ReasoningStartTime == 0 {
		return 0
	}

	currentTime := time.Now().UnixNano() / int64(time.Millisecond)
	return currentTime - ts.ReasoningStartTime
}

// EstimateTokensFromChars provides a rough estimate based on character count
func EstimateTokensFromChars(text string) int {
	// Rough approximation: 1 token ≈ 4 characters
	if text == "" {
		return 0
	}
	return len(text) / 4
}
