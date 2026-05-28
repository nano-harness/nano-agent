package agent

import (
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

func TestFormatGoalTranscriptBudgeted_Empty(t *testing.T) {
	cfg := newGoalTranscriptConfig(config.GoalConfig{})
	text, stats, truncated := formatGoalTranscriptBudgeted(nil, cfg)

	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}
	if stats.OriginalMessages != 0 {
		t.Errorf("expected 0 original messages, got %d", stats.OriginalMessages)
	}
	if truncated {
		t.Error("expected no truncation for empty input")
	}
}

func TestFormatGoalTranscriptBudgeted_RecentWindow(t *testing.T) {
	cfg := newGoalTranscriptConfig(config.GoalConfig{
		EvaluatorRecentMessages:     5,
		EvaluatorTranscriptMaxBytes: 10000,
		EvaluatorToolArgsMaxBytes:   512,
	})

	messages := []llm.Message{
		{Role: "system", Content: "You are a helpful assistant"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
		{Role: "user", Content: "How are you?"},
		{Role: "assistant", Content: "I'm doing well, thanks!"},
	}

	text, stats, truncated := formatGoalTranscriptBudgeted(messages, cfg)

	if truncated {
		t.Error("expected no truncation for small messages")
	}
	if stats.OriginalMessages != 5 {
		t.Errorf("expected 5 original messages, got %d", stats.OriginalMessages)
	}
	if stats.PreservedMessages != 4 { // system message filtered out
		t.Errorf("expected 4 preserved messages, got %d", stats.PreservedMessages)
	}
	if !strings.Contains(text, "Hello") {
		t.Error("expected text to contain user message")
	}
}

func TestFormatGoalTranscriptBudgeted_FarWindow(t *testing.T) {
	cfg := newGoalTranscriptConfig(config.GoalConfig{
		EvaluatorRecentMessages:     2,
		EvaluatorTranscriptMaxBytes: 10000,
		EvaluatorToolArgsMaxBytes:   512,
	})

	messages := []llm.Message{
		{Role: "user", Content: "Message 1"},
		{Role: "assistant", Content: "Response 1"},
		{Role: "user", Content: "Message 2"},
		{Role: "assistant", Content: "Response 2"},
		{Role: "user", Content: "Message 3"},
		{Role: "assistant", Content: "Response 3"},
	}

	_, stats, truncated := formatGoalTranscriptBudgeted(messages, cfg)

	if truncated {
		t.Error("expected no truncation with sufficient budget")
	}
	if stats.PreservedMessages != 6 {
		t.Errorf("expected 6 preserved messages, got %d", stats.PreservedMessages)
	}
	// First 4 messages should be in skeleton form (far window)
	// Last 2 messages should be in standard form (recent window)
}

func TestFormatGoalTranscriptBudgeted_BudgetExceeded(t *testing.T) {
	cfg := newGoalTranscriptConfig(config.GoalConfig{
		EvaluatorRecentMessages:     10,
		EvaluatorTranscriptMaxBytes: 200, // Very small budget
		EvaluatorToolArgsMaxBytes:   512,
	})

	// Create many messages with large content
	messages := make([]llm.Message, 20)
	for i := 0; i < 20; i++ {
		messages[i] = llm.Message{
			Role:    "user",
			Content: strings.Repeat("x", 100),
		}
	}

	text, stats, truncated := formatGoalTranscriptBudgeted(messages, cfg)

	if !truncated {
		t.Error("expected truncation with small budget")
	}
	if stats.SkippedMessages == 0 {
		t.Error("expected some skipped messages")
	}
	if stats.FinalBytes > cfg.transcriptMaxBytes+500 { // Allow some overhead for formatting
		t.Errorf("final bytes %d exceeds budget %d with overhead", stats.FinalBytes, cfg.transcriptMaxBytes)
	}
	if !strings.Contains(text, "truncated") {
		t.Error("expected truncation message in output")
	}
}

func TestFormatGoalTranscriptBudgeted_ToolCalls(t *testing.T) {
	cfg := newGoalTranscriptConfig(config.GoalConfig{
		EvaluatorRecentMessages:     5,
		EvaluatorTranscriptMaxBytes: 10000,
		EvaluatorToolArgsMaxBytes:   512,
	})

	messages := []llm.Message{
		{Role: "user", Content: "Run a command"},
		{
			Role:    "assistant",
			Content: "I'll run that for you",
			ToolCalls: []tools.ToolCall{
				{
					ID:   "call_1",
					Name: "bash",
					Arguments: map[string]any{
						"command": "ls -la",
					},
				},
			},
		},
		{
			Role:       "tool",
			ToolCallID: "call_1",
			Content:    "total 128\ndrwxr-xr-x 10 user user 4096 ...",
		},
	}

	text, stats, truncated := formatGoalTranscriptBudgeted(messages, cfg)

	if truncated {
		t.Error("expected no truncation for tool call messages")
	}
	if !strings.Contains(text, "bash") {
		t.Error("expected tool name in output")
	}
	if !strings.Contains(text, "call_1") {
		t.Error("expected tool call ID in output")
	}
	if stats.PreservedMessages != 3 {
		t.Errorf("expected 3 preserved messages, got %d", stats.PreservedMessages)
	}
}

func TestFormatGoalTranscriptBudgeted_LargeToolResult(t *testing.T) {
	cfg := newGoalTranscriptConfig(config.GoalConfig{
		EvaluatorRecentMessages:     5,
		EvaluatorTranscriptMaxBytes: 10000,
		EvaluatorToolArgsMaxBytes:   512,
	})

	// Create a 100 KB tool result
	largeContent := strings.Repeat("x", 100*1024)

	messages := []llm.Message{
		{Role: "user", Content: "Read file"},
		{
			Role: "assistant",
			ToolCalls: []tools.ToolCall{
				{ID: "call_1", Name: "read", Arguments: map[string]any{"path": "/file"}},
			},
		},
		{
			Role:       "tool",
			ToolCallID: "call_1",
			Content:    largeContent,
		},
	}

	_, stats, _ := formatGoalTranscriptBudgeted(messages, cfg)

	// Should apply SmartTruncate to tool result
	if stats.FinalBytes >= 100*1024 {
		t.Errorf("expected tool result to be truncated, got %d bytes", stats.FinalBytes)
	}
	// Note: truncated flag tracks budget exhaustion, not individual message truncation
	if stats.PreservedMessages != 3 {
		t.Errorf("expected 3 preserved messages, got %d", stats.PreservedMessages)
	}
}

func TestFormatGoalTranscriptBudgeted_ToolArgsMaxBytes(t *testing.T) {
	cfg := newGoalTranscriptConfig(config.GoalConfig{
		EvaluatorRecentMessages:     5,
		EvaluatorTranscriptMaxBytes: 10000,
		EvaluatorToolArgsMaxBytes:   50, // Very small
	})

	messages := []llm.Message{
		{
			Role: "assistant",
			ToolCalls: []tools.ToolCall{
				{
					ID:   "call_1",
					Name: "bash",
					Arguments: map[string]any{
						"command":     "very long command here",
						"description": "this is a long description that will be truncated",
					},
				},
			},
		},
	}

	text, _, _ := formatGoalTranscriptBudgeted(messages, cfg)

	if !strings.Contains(text, "truncated") {
		t.Error("expected tool args to be truncated")
	}
}

func TestFormatGoalTranscriptBudgeted_ZeroBudget(t *testing.T) {
	cfg := newGoalTranscriptConfig(config.GoalConfig{
		EvaluatorTranscriptMaxBytes: 0, // No limit
		EvaluatorRecentMessages:     5,
		EvaluatorToolArgsMaxBytes:   512,
	})

	messages := []llm.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
	}

	// With zero budget, default to 32 KiB
	if cfg.transcriptMaxBytes != 32*1024 {
		t.Errorf("expected default budget 32 KiB, got %d", cfg.transcriptMaxBytes)
	}

	text, stats, truncated := formatGoalTranscriptBudgeted(messages, cfg)

	if truncated {
		t.Error("expected no truncation with default budget for small messages")
	}
	if stats.PreservedMessages != 2 {
		t.Errorf("expected 2 preserved messages, got %d", stats.PreservedMessages)
	}
	if text == "" {
		t.Error("expected non-empty text")
	}
}

func TestSummarizeToolCallArgs_Empty(t *testing.T) {
	result := summarizeToolCallArgs(map[string]any{}, 512)
	if result != "{}" {
		t.Errorf("expected empty object, got %q", result)
	}
}

func TestSummarizeToolCallArgs_Truncation(t *testing.T) {
	args := map[string]any{
		"key1": strings.Repeat("x", 1000),
		"key2": "value2",
	}
	result := summarizeToolCallArgs(args, 50)
	if len(result) > 100 { // 50 + "...[truncated N bytes]"
		t.Errorf("expected truncation, got length %d", len(result))
	}
	if !strings.Contains(result, "truncated") {
		t.Error("expected truncation message")
	}
}

func TestHashToolArgs_Empty(t *testing.T) {
	hash := hashToolArgs(map[string]any{})
	if hash != "empty" {
		t.Errorf("expected 'empty', got %q", hash)
	}
}

func TestHashToolArgs_Consistent(t *testing.T) {
	args := map[string]any{"key": "value"}
	hash1 := hashToolArgs(args)
	hash2 := hashToolArgs(args)
	if hash1 != hash2 {
		t.Error("expected consistent hash")
	}
	if len(hash1) != 8 {
		t.Errorf("expected 8-char hash, got %d chars", len(hash1))
	}
}

func TestNewGoalTranscriptConfig_Defaults(t *testing.T) {
	cfg := newGoalTranscriptConfig(config.GoalConfig{})

	if cfg.transcriptMaxBytes != 32*1024 {
		t.Errorf("expected default 32 KiB, got %d", cfg.transcriptMaxBytes)
	}
	if cfg.recentMessages != 12 {
		t.Errorf("expected default 12 recent messages, got %d", cfg.recentMessages)
	}
	if cfg.toolArgsMaxBytes != 512 {
		t.Errorf("expected default 512 bytes for tool args, got %d", cfg.toolArgsMaxBytes)
	}
}

func TestNewGoalTranscriptConfig_CustomValues(t *testing.T) {
	cfg := newGoalTranscriptConfig(config.GoalConfig{
		EvaluatorTranscriptMaxBytes: 16 * 1024,
		EvaluatorRecentMessages:     5,
		EvaluatorToolArgsMaxBytes:   256,
	})

	if cfg.transcriptMaxBytes != 16*1024 {
		t.Errorf("expected 16 KiB, got %d", cfg.transcriptMaxBytes)
	}
	if cfg.recentMessages != 5 {
		t.Errorf("expected 5 recent messages, got %d", cfg.recentMessages)
	}
	if cfg.toolArgsMaxBytes != 256 {
		t.Errorf("expected 256 bytes for tool args, got %d", cfg.toolArgsMaxBytes)
	}
}
