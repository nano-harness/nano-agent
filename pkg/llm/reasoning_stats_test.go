package llm

import (
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
)

func TestReasoningStatistics(t *testing.T) {
	t.Run("ReasoningStatsTracking", func(t *testing.T) {
		stats := NewTokenStats()

		// Test setting reasoning enabled
		stats.SetReasoningEnabled(true, "medium")
		if !stats.ReasoningEnabled {
			t.Error("Expected reasoning to be enabled")
		}
		if stats.ReasoningEffort != "medium" {
			t.Errorf("Expected effort 'medium', got '%s'", stats.ReasoningEffort)
		}

		// Test setting reasoning tokens
		stats.SetReasoningTokens(150)
		if stats.ReasoningTokens != 150 {
			t.Errorf("Expected 150 reasoning tokens, got %d", stats.ReasoningTokens)
		}

		// Test setting fallback
		stats.SetReasoningFallback(true)
		if !stats.ReasoningFallback {
			t.Error("Expected reasoning fallback to be true")
		}

		// Test latency calculation
		time.Sleep(10 * time.Millisecond) // Small delay for latency
		latency := stats.GetReasoningLatency()
		if latency <= 0 {
			t.Error("Expected positive latency")
		}
	})

	t.Run("ReasoningStatsInEvent", func(t *testing.T) {
		stats := NewTokenStats()

		// Set up reasoning statistics
		stats.SetReasoningEnabled(true, "high")
		stats.SetReasoningTokens(200)
		stats.SetReasoningFallback(false)

		// Get event and verify reasoning fields
		eventStats := stats.GetEvent()

		if !eventStats.ReasoningEnabled {
			t.Error("Expected reasoning enabled in event")
		}
		if eventStats.ReasoningEffort != "high" {
			t.Errorf("Expected effort 'high', got '%s'", eventStats.ReasoningEffort)
		}
		if eventStats.ReasoningTokens != 200 {
			t.Errorf("Expected 200 reasoning tokens, got %d", eventStats.ReasoningTokens)
		}
		if eventStats.ReasoningFallback {
			t.Error("Expected reasoning fallback to be false")
		}
		if eventStats.ReasoningLatency < 0 {
			t.Error("Expected non-negative latency")
		}
	})

	t.Run("ReasoningStatsDefaults", func(t *testing.T) {
		stats := NewTokenStats()

		// Verify default values
		if stats.ReasoningEnabled {
			t.Error("Expected reasoning to be disabled by default")
		}
		if stats.ReasoningTokens != 0 {
			t.Errorf("Expected 0 reasoning tokens by default, got %d", stats.ReasoningTokens)
		}
		if stats.ReasoningEffort != "" {
			t.Errorf("Expected empty effort by default, got '%s'", stats.ReasoningEffort)
		}
		if stats.ReasoningFallback {
			t.Error("Expected reasoning fallback to be false by default")
		}

		// Verify event has correct defaults
		eventStats := stats.GetEvent()
		if eventStats.ReasoningEnabled {
			t.Error("Expected reasoning disabled in event by default")
		}
		if eventStats.ReasoningLatency != 0 {
			t.Errorf("Expected 0 latency when reasoning not started, got %d", eventStats.ReasoningLatency)
		}
	})
}

func TestReasoningEventStructure(t *testing.T) {
	// Test that event.TokenStats has all required reasoning fields
	stats := &event.TokenStats{
		ReasoningEnabled:  true,
		ReasoningTokens:   100,
		ReasoningEffort:   "low",
		ReasoningFallback: false,
		ReasoningLatency:  50,
	}

	if !stats.ReasoningEnabled {
		t.Error("ReasoningEnabled field not working")
	}
	if stats.ReasoningTokens != 100 {
		t.Error("ReasoningTokens field not working")
	}
	if stats.ReasoningEffort != "low" {
		t.Error("ReasoningEffort field not working")
	}
	if stats.ReasoningFallback {
		t.Error("ReasoningFallback field not working")
	}
	if stats.ReasoningLatency != 50 {
		t.Error("ReasoningLatency field not working")
	}
}
