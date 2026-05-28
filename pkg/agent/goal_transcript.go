package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
)

// goalTranscriptConfig holds configuration for evaluator transcript formatting.
type goalTranscriptConfig struct {
	transcriptMaxBytes int
	recentMessages     int
	toolArgsMaxBytes   int
}

// newGoalTranscriptConfig creates a config from GoalConfig with defaults.
func newGoalTranscriptConfig(cfg config.GoalConfig) goalTranscriptConfig {
	c := goalTranscriptConfig{
		transcriptMaxBytes: cfg.EvaluatorTranscriptMaxBytes,
		recentMessages:     cfg.EvaluatorRecentMessages,
		toolArgsMaxBytes:   cfg.EvaluatorToolArgsMaxBytes,
	}
	// Apply defaults when zero
	if c.transcriptMaxBytes == 0 {
		c.transcriptMaxBytes = 32 * 1024 // 32 KiB
	}
	if c.recentMessages == 0 {
		c.recentMessages = 12
	}
	if c.toolArgsMaxBytes == 0 {
		c.toolArgsMaxBytes = 512
	}
	return c
}

// TranscriptStats holds statistics about transcript formatting.
type TranscriptStats struct {
	OriginalMessages  int `json:"original_messages"`
	PreservedMessages int `json:"preserved_messages"`
	OriginalBytes     int `json:"original_bytes"`
	FinalBytes        int `json:"final_bytes"`
	SkippedMessages   int `json:"skipped_messages"`
}

// formatGoalTranscriptBudgeted formats messages with budget constraints.
// Returns the formatted text, stats, and whether truncation occurred.
func formatGoalTranscriptBudgeted(messages []llm.Message, cfg goalTranscriptConfig) (string, TranscriptStats, bool) {
	stats := TranscriptStats{
		OriginalMessages: len(messages),
	}

	// Filter out system messages and estimate original size
	var nonSystemMessages []llm.Message
	for _, msg := range messages {
		if msg.Role != "system" {
			nonSystemMessages = append(nonSystemMessages, msg)
			// Rough size estimate
			stats.OriginalBytes += len(msg.Content)
			for _, tc := range msg.ToolCalls {
				stats.OriginalBytes += len(tc.Name)
				if argsBytes, err := json.Marshal(tc.Arguments); err == nil {
					stats.OriginalBytes += len(argsBytes)
				}
			}
		}
	}

	if len(nonSystemMessages) == 0 {
		return "", stats, false
	}

	// Determine recent window and far window
	recentStart := len(nonSystemMessages) - cfg.recentMessages
	if recentStart < 0 {
		recentStart = 0
	}

	var b strings.Builder
	budgetRemaining := cfg.transcriptMaxBytes
	truncated := false

	// Format far window messages (skeleton only)
	for i := 0; i < recentStart; i++ {
		msg := nonSystemMessages[i]
		skeleton := formatMessageSkeleton(msg)

		if budgetRemaining <= 0 {
			// Budget exhausted
			skipped := len(nonSystemMessages) - i
			stats.SkippedMessages = skipped
			truncatedBytes := 0
			for j := i; j < len(nonSystemMessages); j++ {
				truncatedBytes += len(nonSystemMessages[j].Content)
			}
			fmt.Fprintf(&b, "...[transcript truncated: skipped %d messages, ~%d bytes]\n", skipped, truncatedBytes)
			truncated = true
			break
		}

		if len(skeleton) > budgetRemaining {
			// Can't fit even skeleton
			skipped := len(nonSystemMessages) - i
			stats.SkippedMessages = skipped
			fmt.Fprintf(&b, "...[transcript truncated: skipped %d messages]\n", skipped)
			truncated = true
			break
		}

		b.WriteString(skeleton)
		budgetRemaining -= len(skeleton)
		stats.PreservedMessages++
	}

	// Format recent window messages (full detail with smart truncation)
	if !truncated {
		perMessageBudget := cfg.transcriptMaxBytes / cfg.recentMessages
		if perMessageBudget < 1024 {
			perMessageBudget = 1024 // minimum 1KB per recent message
		}

		for i := recentStart; i < len(nonSystemMessages); i++ {
			msg := nonSystemMessages[i]
			formatted := formatMessageStandard(msg, cfg.toolArgsMaxBytes, perMessageBudget)

			if budgetRemaining <= 0 {
				skipped := len(nonSystemMessages) - i
				stats.SkippedMessages = skipped
				fmt.Fprintf(&b, "...[transcript truncated: skipped %d recent messages]\n", skipped)
				truncated = true
				break
			}

			if len(formatted) > budgetRemaining {
				// Truncate this message to fit
				cutAt := budgetRemaining
				if cutAt > 200 {
					cutAt = 200
				}
				if cutAt > 0 && cutAt < len(formatted) {
					b.WriteString(formatted[:cutAt])
					b.WriteString(fmt.Sprintf("...[message truncated %d bytes]\n", len(formatted)-cutAt))
				}
				truncated = true
				stats.PreservedMessages++
				break
			}

			b.WriteString(formatted)
			budgetRemaining -= len(formatted)
			stats.PreservedMessages++
		}
	}

	result := strings.TrimSpace(b.String())
	stats.FinalBytes = len(result)

	return result, stats, truncated
}

// formatMessageSkeleton formats a message as a compact skeleton (for far window).
func formatMessageSkeleton(msg llm.Message) string {
	var b strings.Builder
	role := msg.Role
	if role == "" {
		role = "unknown"
	}

	// Basic role and content length
	contentLen := len(msg.Content)
	if contentLen > 0 {
		// Show first 200 bytes of content
		preview := msg.Content
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		fmt.Fprintf(&b, "[%s ~%db] %s\n", role, contentLen, preview)
	} else {
		fmt.Fprintf(&b, "[%s ~%db]\n", role, contentLen)
	}

	// Tool calls: just name and args hash
	if len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			argsHash := hashToolArgs(tc.Arguments)
			fmt.Fprintf(&b, "  [tool_call] %s args:%s\n", tc.Name, argsHash)
		}
	}

	// Tool result: just ID and length
	if msg.ToolCallID != "" {
		fmt.Fprintf(&b, "  [tool_result %s ~%db]\n", msg.ToolCallID, len(msg.Content))
	}

	return b.String()
}

// formatMessageStandard formats a message with full detail (for recent window).
func formatMessageStandard(msg llm.Message, toolArgsMaxBytes, toolResultMaxBytes int) string {
	var b strings.Builder
	role := msg.Role
	if role == "" {
		role = "unknown"
	}

	// Role and content
	if msg.ToolCallID == "" {
		// Regular message
		fmt.Fprintf(&b, "[%s] %s\n", role, msg.Content)
	}

	// Tool calls with JSON-serialized arguments
	if len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			args := summarizeToolCallArgs(tc.Arguments, toolArgsMaxBytes)
			fmt.Fprintf(&b, "[assistant tool_call] %s %s\n", tc.Name, args)
		}
	}

	// Tool result with smart truncation
	if msg.ToolCallID != "" {
		content := msg.Content
		// Apply SmartTruncateToolResult based on tool name
		// We don't have tool name here, so we'll extract it from ToolCallID or use generic truncation
		if len(content) > toolResultMaxBytes {
			content = SmartTruncateToolResult("", content, toolResultMaxBytes)
		}
		fmt.Fprintf(&b, "[tool_result %s] %s\n", msg.ToolCallID, content)
	}

	return b.String()
}

// summarizeToolCallArgs serializes tool arguments with a byte limit.
func summarizeToolCallArgs(args map[string]any, maxBytes int) string {
	if len(args) == 0 {
		return "{}"
	}

	// Try JSON serialization
	jsonBytes, err := json.Marshal(args)
	if err != nil {
		// Fallback to fmt.Sprint
		s := fmt.Sprint(args)
		if len(s) > maxBytes {
			return s[:maxBytes] + fmt.Sprintf("...[truncated %d bytes]", len(s)-maxBytes)
		}
		return s
	}

	jsonStr := string(jsonBytes)
	if len(jsonStr) <= maxBytes {
		return jsonStr
	}

	// Truncate
	return jsonStr[:maxBytes] + fmt.Sprintf("...[truncated %d bytes]", len(jsonStr)-maxBytes)
}

// hashToolArgs creates a short hash of tool arguments for skeleton representation.
func hashToolArgs(args map[string]any) string {
	if len(args) == 0 {
		return "empty"
	}
	// Try to serialize
	jsonBytes, err := json.Marshal(args)
	if err != nil {
		return "error"
	}
	hash := sha256.Sum256(jsonBytes)
	return fmt.Sprintf("%x", hash[:4]) // First 8 hex chars
}
