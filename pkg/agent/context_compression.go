package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// maxToolResultBytes is the per-message cap for tool result content before truncation.
const maxToolResultBytes = 4096

// CompressionInfo tracks compression statistics like Gemini CLI
// inspired by google-gemini/gemini-cli ChatCompressionInfo
type CompressionInfo struct {
	OriginalTokens   int     `json:"original_tokens"`
	CompressedTokens int     `json:"compressed_tokens"`
	TokensSaved      int     `json:"tokens_saved"`
	CompressionRatio float64 `json:"compression_ratio"`
	MessagesBefore   int     `json:"messages_before"`
	MessagesAfter    int     `json:"messages_after"`
	TriggeredBy      string  `json:"triggered_by"`
	Summary          string  `json:"summary"`
}

// ContextStatus reports the current transcript/context budget state for a
// session or turn. It is designed for CLI/daemon control surfaces.
type ContextStatus struct {
	MessageCount       int              `json:"message_count"`
	EstimatedTokens    int              `json:"estimated_tokens"`
	MaxTokens          int              `json:"max_tokens"`
	ThresholdTokens    int              `json:"threshold_tokens"`
	ThresholdRatio     float64          `json:"threshold_ratio"`
	PreserveRatio      float64          `json:"preserve_ratio"`
	ShouldCompress     bool             `json:"should_compress"`
	CompressionEnabled bool             `json:"compression_enabled"`
	LastCompression    *CompressionInfo `json:"last_compression,omitempty"`
}

// CompressionStrategy implements advanced context compression based on Gemini CLI patterns
// inspired by google-gemini/gemini-cli
type CompressionStrategy struct {
	thresholdRatio    float64           // 0.7 = compress when 70% of max tokens used
	preserveRatio     float64           // 0.3 = preserve 30% of recent context
	maxTokens         int               // Maximum tokens before compression
	minMessagesToKeep int               // Minimum messages to always preserve
	tokenCounter      *llm.TokenCounter // Accurate token counting
}

// NewCompressionStrategy creates a new compression strategy, dynamically
// calibrated to the configured model's context window via the model registry.
func NewCompressionStrategy() *CompressionStrategy {
	// Create token counter with a default model
	tokenCounter, _ := llm.NewTokenCounter("moonshot-v1") // Use moonshot for more accurate counting

	// Infer profile from configured model name; pass empty string when model is
	// not configured so that InferModelProfile applies its conservative default.
	cfg := config.Get()
	modelName := ""
	if cfg != nil {
		modelName = cfg.Model
	}
	profile := llm.InferModelProfile(modelName)

	// Derive compression parameters from the inferred profile
	maxTokens := int(float64(profile.ContextWindow) * 0.95) // 5% headroom
	thresholdRatio := profile.ThresholdRatio
	preserveRatio := profile.PreserveRatio
	minMessagesToKeep := computeMinKeep(profile.ContextWindow)
	effectiveContextWindow := profile.ContextWindow

	// Override from global config when available (highest priority)
	if cfg != nil {
		// If the user explicitly specified a context window, recompute from it
		if cfg.ContextConfig.ModelContextWindow > 0 {
			overrideProfile := llm.ComputeProfileFromContextWindow(cfg.ContextConfig.ModelContextWindow)
			maxTokens = int(float64(overrideProfile.ContextWindow) * 0.95)
			thresholdRatio = overrideProfile.ThresholdRatio
			preserveRatio = overrideProfile.PreserveRatio
			minMessagesToKeep = computeMinKeep(overrideProfile.ContextWindow)
			effectiveContextWindow = overrideProfile.ContextWindow
		}
		// User explicit max_tokens is the absolute highest priority
		if v := cfg.ContextConfig.MaxTokens; v > 0 {
			maxTokens = v
		}
		if r := cfg.ContextConfig.CompressionRatio; r > 0 && r < 1 {
			preserveRatio = r
			// Map compression ratio to trigger threshold (consistent with llm.StreamingConversation)
			thresholdRatio = 1.0 - r
		}
		if k := cfg.ContextConfig.PreserveRecentTurns; k > 0 {
			minMessagesToKeep = k
		}
	}

	logger.Infof("Compression strategy: model=%s, contextWindow=%d, maxTokens=%d, threshold=%.0f%%, preserve=%.0f%%",
		modelName, effectiveContextWindow, maxTokens, thresholdRatio*100, preserveRatio*100)

	return &CompressionStrategy{
		thresholdRatio:    thresholdRatio,
		preserveRatio:     preserveRatio,
		maxTokens:         maxTokens,
		minMessagesToKeep: minMessagesToKeep,
		tokenCounter:      tokenCounter,
	}
}

// computeMinKeep returns the minimum number of recent messages to always
// preserve during compression, scaled to the model's context window size.
func computeMinKeep(contextWindow int) int {
	switch {
	case contextWindow <= 8_192:
		return 3
	case contextWindow <= 16_384:
		return 4
	case contextWindow <= 131_072:
		return 6
	case contextWindow <= 200_000:
		return 8
	default:
		return 10
	}
}

// NewCompressionStrategyWithConfig creates a new compression strategy with custom config
func NewCompressionStrategyWithConfig(maxTokens int, compressionRatio float64, preserveRecentTurns int) *CompressionStrategy {
	// Create token counter with a default model
	tokenCounter, _ := llm.NewTokenCounter("moonshot-v1")

	// Convert compression ratio to trigger threshold: threshold = 1 - compressionRatio
	threshold := 1.0 - compressionRatio
	if threshold <= 0 {
		threshold = 0.5
	}

	return &CompressionStrategy{
		thresholdRatio:    threshold,           // Trigger threshold derived from ratio
		preserveRatio:     compressionRatio,    // Use configured compression ratio for preservation
		maxTokens:         maxTokens,           // Use configured max tokens
		minMessagesToKeep: preserveRecentTurns, // Use configured recent turns
		tokenCounter:      tokenCounter,
	}
}

// ShouldCompress determines if compression is needed based on token count
func (cs *CompressionStrategy) ShouldCompress(messages []llm.Message, currentTokens int) bool {
	logger.Debugf("Checking compression: %d messages, %d tokens", len(messages), currentTokens)
	if len(messages) <= cs.minMessagesToKeep+1 { // +1 for system message
		logger.Debug("Not compressing: not enough messages")
		return false
	}

	shouldCompress := currentTokens > int(float64(cs.maxTokens)*cs.thresholdRatio)
	logger.Debugf("Should compress: %v (threshold: %d tokens)", shouldCompress, int(float64(cs.maxTokens)*cs.thresholdRatio))
	return shouldCompress
}

// Status estimates the current context budget without mutating messages.
// This is a read-only snapshot that does not trigger logging or nested calls.
func (cs *CompressionStrategy) Status(messages []llm.Message, systemPrompt string, last *CompressionInfo) ContextStatus {
	currentTokens := cs.EstimateTokenCountWithSystemPrompt(messages, systemPrompt)
	thresholdTokens := int(float64(cs.maxTokens) * cs.thresholdRatio)
	// Inline shouldCompress calculation without calling ShouldCompress to avoid nested logging
	shouldCompress := len(messages) > cs.minMessagesToKeep+1 && currentTokens > thresholdTokens
	return ContextStatus{
		MessageCount:       len(messages),
		EstimatedTokens:    currentTokens,
		MaxTokens:          cs.maxTokens,
		ThresholdTokens:    thresholdTokens,
		ThresholdRatio:     cs.thresholdRatio,
		PreserveRatio:      cs.preserveRatio,
		ShouldCompress:     shouldCompress,
		CompressionEnabled: true,
		LastCompression:    last,
	}
}

// SegmentHistory intelligently segments conversation history
// Returns messages to compress and messages to preserve
func (cs *CompressionStrategy) SegmentHistory(messages []llm.Message) (toCompress []llm.Message, toPreserve []llm.Message) {
	logger.Infof("Segmenting history: %d total messages", len(messages))

	if len(messages) <= cs.minMessagesToKeep+1 {
		logger.Infof("Not segmenting: not enough messages (%d <= %d)", len(messages), cs.minMessagesToKeep+1)
		return []llm.Message{}, messages
	}

	// Always preserve system message
	var systemMsg llm.Message
	var hasSystemMsg bool
	startIndex := 0
	if len(messages) > 0 && messages[0].Role == "system" {
		systemMsg = messages[0]
		hasSystemMsg = true
		startIndex = 1
	}

	remainingMessages := messages[startIndex:]
	preserveCount := max(cs.minMessagesToKeep, int(float64(len(remainingMessages))*cs.preserveRatio))

	logger.Debugf("Remaining messages after system: %d, preserve count: %d", len(remainingMessages), preserveCount)

	if len(remainingMessages) <= preserveCount {
		logger.Info("Not segmenting: remaining messages <= preserve count")
		return []llm.Message{}, messages
	}

	// Find intelligent split point
	splitIndex := len(remainingMessages) - preserveCount

	// Ensure we don't split within tool calls/results
	splitIndex = cs.findSmartSplitIndex(remainingMessages, splitIndex)

	toCompress = remainingMessages[:splitIndex]

	// Build toPreserve with system message if present
	if hasSystemMsg {
		toPreserve = append([]llm.Message{systemMsg}, remainingMessages[splitIndex:]...)
	} else {
		toPreserve = remainingMessages[splitIndex:]
	}

	logger.Infof("Segmented history: %d to compress, %d to preserve (system msg: %t)",
		len(toCompress), len(toPreserve), hasSystemMsg)

	return toCompress, toPreserve
}

// findSmartSplitIndex ensures we don't split in the middle of tool interactions
func (cs *CompressionStrategy) findSmartSplitIndex(messages []llm.Message, targetIndex int) int {
	// Look forward to find a safe split point
	for i := targetIndex; i < len(messages); i++ {
		msg := messages[i]

		// Never split within a tool call
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Look for the next user message after this
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Role == "user" || messages[j].Role == "system" {
					return j
				}
			}
		}

		// Safe to split after a user message
		if msg.Role == "user" {
			return i
		}
	}

	// Fallback to original split
	return targetIndex
}

// OutputType categorizes tool output for smart truncation.
type OutputType int

const (
	OutputTypeGeneral       OutputType = iota
	OutputTypeSearchResults            // grep / search tools
	OutputTypeFileContent              // read / cat / view tools
	OutputTypeError                    // error / fail tools
	OutputTypeLog                      // run / exec / shell tools
)

// categorizeToolOutput determines the output type based on tool name.
func categorizeToolOutput(toolName string) OutputType {
	lower := strings.ToLower(toolName)
	switch {
	case strings.Contains(lower, "search") || strings.Contains(lower, "grep"):
		return OutputTypeSearchResults
	case strings.Contains(lower, "read") || strings.Contains(lower, "cat") || strings.Contains(lower, "view"):
		return OutputTypeFileContent
	case strings.Contains(lower, "run") || strings.Contains(lower, "exec") || strings.Contains(lower, "shell"):
		return OutputTypeLog
	case strings.Contains(lower, "error") || strings.Contains(lower, "fail"):
		return OutputTypeError
	default:
		return OutputTypeGeneral
	}
}

// SmartTruncateToolResult truncates tool results based on content type.
func SmartTruncateToolResult(toolName string, content string, maxBytes int) string {
	if len(content) <= maxBytes {
		return content
	}
	switch categorizeToolOutput(toolName) {
	case OutputTypeSearchResults:
		return truncateSearchResults(content, maxBytes)
	case OutputTypeFileContent:
		return truncateFileContent(content, maxBytes)
	case OutputTypeError:
		return content // preserve errors fully
	case OutputTypeLog:
		return truncateLog(content, maxBytes)
	default:
		return truncateDefault(content, maxBytes)
	}
}

// truncateSearchResults keeps the first N lines up to maxBytes (linear time).
func truncateSearchResults(content string, maxBytes int) string {
	lines := strings.Split(content, "\n")
	var sb strings.Builder
	currentLen := 0
	for _, line := range lines {
		lineLen := len(line) + 1 // +1 for newline
		if currentLen+lineLen > maxBytes {
			break
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
		currentLen += lineLen
	}
	result := sb.String()
	if len(result) < len(content) {
		result += fmt.Sprintf("...[truncated %d bytes]", len(content)-len(result))
	}
	return result
}

// truncateFileContent keeps head and tail with an ellipsis in the middle.
func truncateFileContent(content string, maxBytes int) string {
	if maxBytes < 200 {
		cutAt := utf8SafeCutAt(content, maxBytes)
		return content[:cutAt] + fmt.Sprintf("\n...[truncated %d bytes]", len(content)-cutAt)
	}
	half := maxBytes / 2
	headCut := utf8SafeCutAt(content, half)
	head := content[:headCut]
	// For tail: find a UTF-8-safe start point near len(content)-half
	tailStart := len(content) - half
	if tailStart < 0 {
		tailStart = 0
	}
	// Advance to the next rune boundary if needed
	for tailStart < len(content) && !utf8.RuneStart(content[tailStart]) {
		tailStart++
	}
	tail := content[tailStart:]
	return head + fmt.Sprintf("\n...[%d bytes omitted]...\n", len(content)-maxBytes) + tail
}

// truncateLog keeps the last N lines up to maxBytes.
func truncateLog(content string, maxBytes int) string {
	if len(content) <= maxBytes {
		return content
	}
	tailStart := len(content) - maxBytes
	// Advance to a UTF-8 rune boundary
	for tailStart < len(content) && !utf8.RuneStart(content[tailStart]) {
		tailStart++
	}
	tail := content[tailStart:]
	// Find first newline to avoid cutting mid-line
	if nl := strings.Index(tail, "\n"); nl >= 0 {
		tail = tail[nl+1:]
	}
	return fmt.Sprintf("...[truncated %d bytes]\n", len(content)-len(tail)) + tail
}

// truncateDefault keeps the first maxBytes (UTF-8-safe).
func truncateDefault(content string, maxBytes int) string {
	cutAt := utf8SafeCutAt(content, maxBytes)
	return content[:cutAt] + fmt.Sprintf("\n...[truncated %d bytes]", len(content)-cutAt)
}

// utf8SafeCutAt returns the largest index ≤ maxBytes that falls on a UTF-8 rune boundary.
func utf8SafeCutAt(s string, maxBytes int) int {
	if maxBytes >= len(s) {
		return len(s)
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return cut
}

// dynamicTruncateThreshold returns the truncation byte threshold based on context window size.
func (cs *CompressionStrategy) dynamicTruncateThreshold() int {
	switch {
	case cs.maxTokens <= 8192:
		return 2048
	case cs.maxTokens <= 32768:
		return 4096
	case cs.maxTokens <= 131072:
		return 8192
	default:
		return 16384
	}
}

// TruncateLargeToolResultsSmart is like TruncateLargeToolResults but uses smart
// per-type truncation and a dynamic threshold derived from the context window.
// It matches tool results to their tool names via the preceding assistant message's ToolCalls.
func (cs *CompressionStrategy) TruncateLargeToolResultsSmart(messages []llm.Message) ([]llm.Message, int) {
	threshold := cs.dynamicTruncateThreshold()

	// Build a map of tool-call-ID → tool name from assistant messages.
	toolNames := make(map[string]string)
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			toolNames[tc.ID] = tc.Name
		}
	}

	result := make([]llm.Message, len(messages))
	copy(result, messages)
	savedBytes := 0

	for i, msg := range result {
		if msg.Role != "tool" {
			continue
		}
		if len(msg.Content) <= threshold {
			continue
		}
		original := len(msg.Content)
		toolName := toolNames[msg.ToolCallID]
		truncated := SmartTruncateToolResult(toolName, msg.Content, threshold)
		result[i].Content = truncated
		savedBytes += original - len(result[i].Content)
	}
	return result, savedBytes
}

// RemoveOldMessages drops the oldest non-system messages (up to removeCount pairs) from
// the conversation. It never removes the system message or the most recent minKeep messages.
// Returns the trimmed message list and the number of messages removed.
func (cs *CompressionStrategy) RemoveOldMessages(messages []llm.Message, removeCount int) ([]llm.Message, int) {
	if removeCount <= 0 || len(messages) == 0 {
		return messages, 0
	}

	startIdx := 0
	if len(messages) > 0 && messages[0].Role == "system" {
		startIdx = 1
	}

	nonSystem := messages[startIdx:]
	keepAtLeast := cs.minMessagesToKeep
	maxRemovable := len(nonSystem) - keepAtLeast
	if maxRemovable <= 0 {
		return messages, 0
	}
	if removeCount > maxRemovable {
		removeCount = maxRemovable
	}

	// Find a safe boundary (don't split inside a tool call / tool result pair)
	safeEnd := cs.findSmartSplitIndex(nonSystem, removeCount)

	kept := append([]llm.Message{}, messages[:startIdx]...)
	kept = append(kept, nonSystem[safeEnd:]...)
	return kept, safeEnd
}

// CollapseRedundantToolResults replaces repeated identical tool results (same tool + same
// content appearing more than once) with a single representative plus a folded placeholder.
// This is a local, zero-cost operation.
func (cs *CompressionStrategy) CollapseRedundantToolResults(messages []llm.Message) ([]llm.Message, int) {
	type key struct{ toolCallID, content string }
	seen := make(map[key]int) // key → first occurrence index in result
	result := make([]llm.Message, 0, len(messages))
	collapsed := 0

	for _, msg := range messages {
		if msg.Role != "tool" {
			result = append(result, msg)
			continue
		}
		k := key{toolCallID: msg.ToolCallID, content: msg.Content}
		if _, exists := seen[k]; exists {
			// Replace with a minimal placeholder instead of the full content
			placeholder := msg
			placeholder.Content = "[duplicate tool result omitted]"
			result = append(result, placeholder)
			collapsed++
			continue
		}
		seen[k] = len(result)
		result = append(result, msg)
	}
	return result, collapsed
}

// localCompress applies cheap local operations to reduce token count without calling the LLM.
// It returns the (potentially modified) message list and the number of tokens saved.
func (cs *CompressionStrategy) localCompress(messages []llm.Message, targetSaving int) ([]llm.Message, int) {
	current := cs.EstimateTokenCount(messages)
	saved := 0

	// Layer 0: clear old tool results (zero LLM cost)
	msgs, clearedCount := cs.ClearOldToolResultsWithCount(messages, 3)
	if clearedCount > 0 {
		after := cs.EstimateTokenCount(msgs)
		delta := current - after
		saved += delta
		current = after
		messages = msgs
		logger.Infof("localCompress: cleared %d old tool results, saved ~%d tokens", clearedCount, delta)
	}

	if saved >= targetSaving {
		return messages, saved
	}

	// Step 1: collapse redundant tool results (zero-cost)
	msgs, collapsedCount := cs.CollapseRedundantToolResults(messages)
	if collapsedCount > 0 {
		after := cs.EstimateTokenCount(msgs)
		delta := current - after
		saved += delta
		current = after
		messages = msgs
		logger.Infof("localCompress: collapsed %d redundant tool results, saved ~%d tokens", collapsedCount, delta)
	}

	if saved >= targetSaving {
		return messages, saved
	}

	// Step 2: smart-truncate oversized tool results using the dynamic threshold
	msgs, _ = cs.TruncateLargeToolResultsSmart(messages)
	after := cs.EstimateTokenCount(msgs)
	delta := current - after
	if delta > 0 {
		saved += delta
		current = after
		messages = msgs
		logger.Infof("localCompress: smart-truncated large tool results, saved ~%d tokens", delta)
	}

	if saved >= targetSaving {
		return messages, saved
	}

	// Step 3: progressively remove oldest messages
	threshold := int(float64(cs.maxTokens) * cs.thresholdRatio)
	for current > threshold {
		msgs, removed := cs.RemoveOldMessages(messages, 2)
		if removed == 0 {
			break
		}
		after = cs.EstimateTokenCount(msgs)
		delta = current - after
		if delta <= 0 {
			break
		}
		saved += delta
		current = after
		messages = msgs
		logger.Infof("localCompress: removed %d old messages, saved ~%d tokens total so far", removed, saved)
	}

	return messages, saved
}

// ClearOldToolResultsWithCount replaces old tool result content with a placeholder,
// always keeping the most recent protectRecent tool messages intact.
// Returns the updated messages and the number of messages whose content was cleared.
func (cs *CompressionStrategy) ClearOldToolResultsWithCount(messages []llm.Message, protectRecent int) ([]llm.Message, int) {
	// Collect indices of tool messages
	var toolIndices []int
	for i, msg := range messages {
		if msg.Role == "tool" {
			toolIndices = append(toolIndices, i)
		}
	}

	if len(toolIndices) <= protectRecent {
		return messages, 0
	}

	// The oldest ones (all except the last protectRecent) will be cleared
	clearUpTo := len(toolIndices) - protectRecent
	result := make([]llm.Message, len(messages))
	copy(result, messages)

	cleared := 0
	for i := 0; i < clearUpTo; i++ {
		idx := toolIndices[i]
		if result[idx].Content != "[Old tool result cleared]" {
			result[idx].Content = "[Old tool result cleared]"
			cleared++
		}
	}
	return result, cleared
}

// GenerateSummary creates a structured XML summary like Gemini CLI
func (cs *CompressionStrategy) GenerateSummary(ctx context.Context, client llm.StreamClient, messages []llm.Message) (string, error) {
	if len(messages) == 0 {
		return "No previous context", nil
	}

	spb := NewSystemPromptBuilder("", nil, nil, config.Get())
	summaryPrompt := spb.BuildCompressionPrompt()

	// Format messages for summarization
	conversationText := cs.formatMessagesForSummary(messages)

	summaryMessages := []llm.Message{
		{Role: "system", Content: summaryPrompt},
		{Role: "user", Content: conversationText},
	}

	// Generate summary using LLM
	var summary strings.Builder
	// Use non-thinking mode to avoid reasoning tokens during compression summarization
	err := client.StreamCompletionWithoutReasoning(ctx, summaryMessages, func(event event.StreamEvent) {
		summary.WriteString(event.Content)
	})

	if err != nil {
		logger.Errorf("Failed to generate summary: %v", err)
		return cs.createFallbackSummary(messages), nil
	}

	return summary.String(), nil
}

// formatMessagesForSummary formats messages for the summarization prompt
func (cs *CompressionStrategy) formatMessagesForSummary(messages []llm.Message) string {
	var formatted strings.Builder

	for i, msg := range messages {
		role := msg.Role
		if role == "" {
			role = "unknown"
		}

		content := msg.Content
		roleLabel := role
		if role == "tool" {
			content = cs.condenseToolContentForSummary(content)
			if msg.ToolCallID != "" {
				roleLabel = fmt.Sprintf("tool[%s]", msg.ToolCallID)
			}
		}

		fmt.Fprintf(&formatted, "[%d] %s: %s\n", i+1, roleLabel, content)

		// Include tool calls
		if len(msg.ToolCalls) > 0 {
			formatted.WriteString("  Tools called:\n")
			for _, tc := range msg.ToolCalls {
				fmt.Fprintf(&formatted, "    - %s: %v\n", tc.Name, tc.Arguments)
			}
		}

		formatted.WriteString("\n")
	}

	return formatted.String()
}

func (cs *CompressionStrategy) condenseToolContentForSummary(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	firstLine := trimmed
	if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
		firstLine = trimmed[:idx]
	}

	if strings.HasPrefix(firstLine, "Viewed file ") {
		rest := strings.TrimPrefix(firstLine, "Viewed file ")
		pathPart := rest
		if idx := strings.Index(rest, " (lines "); idx >= 0 {
			pathPart = rest[:idx]
		}
		return fmt.Sprintf("File: %s", strings.TrimSpace(pathPart))
	}

	if strings.HasPrefix(firstLine, "Web fetch from: ") {
		return fmt.Sprintf("URL: %s", strings.TrimSpace(strings.TrimPrefix(firstLine, "Web fetch from: ")))
	}

	return firstLine
}

// createFallbackSummary creates a simple fallback when LLM summarization fails
func (cs *CompressionStrategy) createFallbackSummary(messages []llm.Message) string {
	var summary strings.Builder

	summary.WriteString("<state_snapshot>")

	// Count message types
	userMessages, assistantMessages, toolCalls := 0, 0, 0
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			userMessages++
		case "assistant":
			assistantMessages++
			toolCalls += len(msg.ToolCalls)
		}
	}

	fmt.Fprintf(&summary, "<summary>Conversation history: %d user messages, %d assistant responses, %d tool calls</summary>",
		userMessages, assistantMessages, toolCalls)

	summary.WriteString("</state_snapshot>")

	return summary.String()
}

// EstimateTokenCount provides accurate token estimation for messages
func (cs *CompressionStrategy) EstimateTokenCount(messages []llm.Message) int {
	if cs.tokenCounter == nil {
		// Fallback to simple estimation
		totalTokens := 0
		for _, msg := range messages {
			totalTokens += len(msg.Content) / 4
			if len(msg.ToolCalls) > 0 {
				for _, toolCall := range msg.ToolCalls {
					totalTokens += len(toolCall.Name) / 4
					if toolCall.Arguments != nil {
						if argsBytes, err := json.Marshal(toolCall.Arguments); err == nil {
							totalTokens += len(argsBytes) / 4
						}
					}
					totalTokens += 10 // Extra tokens for structure
				}
			}
			if msg.ToolCallID != "" {
				totalTokens += 5
			}
			totalTokens += 10
		}
		return totalTokens
	}

	// Use accurate token counter
	return cs.tokenCounter.CountMessagesTokens(messages)
}

// EstimateTokenCountWithSystemPrompt estimates tokens including system prompt
func (cs *CompressionStrategy) EstimateTokenCountWithSystemPrompt(messages []llm.Message, systemPrompt string) int {
	messageTokens := cs.EstimateTokenCount(messages)
	for _, msg := range messages {
		if msg.Role == "system" {
			return messageTokens
		}
	}

	if cs.tokenCounter != nil {
		systemTokens := cs.tokenCounter.CountTokens(systemPrompt)
		logger.Debugf("Message tokens: %d, System prompt tokens: %d", messageTokens, systemTokens)
		return messageTokens + systemTokens
	}

	// Fallback estimation
	systemTokens := len(systemPrompt) / 4
	logger.Debugf("Message tokens: %d, System prompt tokens: %d (fallback)", messageTokens, systemTokens)
	return messageTokens + systemTokens
}

// CompressMessages compresses conversation messages using the strategy.
// It first applies cheap local operations (collapse duplicates, truncate large results,
// remove old messages). Only when local operations are insufficient does it invoke the
// LLM API for summarization.
func (cs *CompressionStrategy) CompressMessages(ctx context.Context, client llm.StreamClient, messages []llm.Message, force bool) ([]llm.Message, *CompressionInfo, error) {
	if len(messages) == 0 {
		return messages, nil, nil
	}

	// Estimate current token count
	currentTokens := cs.EstimateTokenCount(messages)

	// Check if compression is needed unless forced
	if !force {
		if !cs.ShouldCompress(messages, currentTokens) {
			logger.Info("Compression not needed based on token count")
			return messages, nil, nil
		}
	} else {
		logger.Info("Forcing compression regardless of token threshold")
	}

	threshold := int(float64(cs.maxTokens) * cs.thresholdRatio)
	targetSaving := currentTokens - threshold
	if targetSaving < 0 {
		targetSaving = 0
	}

	// ── Phase 1: local lightweight operations (no LLM API call) ──────────────
	localMessages, localSaved := cs.localCompress(messages, targetSaving)
	afterLocalTokens := cs.EstimateTokenCount(localMessages)

	logger.Infof("localCompress result: %d → %d tokens (saved %d)", currentTokens, afterLocalTokens, localSaved)

	// If local operations brought us below threshold (or saved enough), skip LLM summarization.
	localSufficient := afterLocalTokens <= threshold
	if force && !localSufficient {
		// Force mode: still try LLM summarization even if local was enough
		localSufficient = false
	}

	if localSufficient {
		// Guard: skip if savings are negligible
		minSavings := 256
		if twoPercent := int(float64(currentTokens) * 0.02); twoPercent > minSavings {
			minSavings = twoPercent
		}
		if localSaved < minSavings {
			logger.Infof("Compression skipped: local savings insufficient (%d < %d required)", localSaved, minSavings)
			return messages, nil, nil
		}

		compressionInfo := &CompressionInfo{
			OriginalTokens:   currentTokens,
			CompressedTokens: afterLocalTokens,
			TokensSaved:      localSaved,
			CompressionRatio: float64(afterLocalTokens) / float64(currentTokens),
			MessagesBefore:   len(messages),
			MessagesAfter:    len(localMessages),
			TriggeredBy: func() string {
				if force {
					return "forced_local"
				}
				return "threshold_local"
			}(),
			Summary: "(local compression: no LLM summary)",
		}
		logger.Infof("Compression done (local only): %d → %d tokens (%.2f%% reduction)",
			currentTokens, afterLocalTokens, (1.0-compressionInfo.CompressionRatio)*100)
		return localMessages, compressionInfo, nil
	}

	// ── Phase 2: LLM-based summarization (only if local was insufficient) ────
	// Even here, start from the locally-compressed messages to reduce what we summarize.
	workMessages := localMessages
	workTokens := afterLocalTokens
	_ = workTokens // used for logging

	// Segment history for LLM summarization
	toCompress, toPreserve := cs.SegmentHistory(workMessages)
	if len(toCompress) == 0 {
		logger.Info("No messages to summarize after local compression + segmentation")
		// Return local result if it saved something meaningful
		if localSaved > 0 {
			compressionInfo := &CompressionInfo{
				OriginalTokens:   currentTokens,
				CompressedTokens: afterLocalTokens,
				TokensSaved:      localSaved,
				CompressionRatio: float64(afterLocalTokens) / float64(currentTokens),
				MessagesBefore:   len(messages),
				MessagesAfter:    len(localMessages),
				TriggeredBy:      "threshold_local",
				Summary:          "(local compression only)",
			}
			return localMessages, compressionInfo, nil
		}
		return messages, nil, nil
	}

	// Generate LLM summary
	summary, err := cs.GenerateSummary(ctx, client, toCompress)
	if err != nil {
		logger.Warnf("LLM summary failed, falling back to simple truncation: %v", err)
		return cs.fallbackTruncate(messages)
	}

	// Create compressed messages following Gemini CLI flow:
	// system → user(summary) → assistant(ack) → preserved
	compressedMessages := []llm.Message{}

	// 1) Preserve original system instruction (unchanged)
	var originalSystem llm.Message
	var hasOriginalSystem bool
	if len(workMessages) > 0 && workMessages[0].Role == "system" {
		originalSystem = workMessages[0]
		hasOriginalSystem = true
	}

	// Determine if toPreserve already includes system message
	hasSystemInPreserved := len(toPreserve) > 0 && toPreserve[0].Role == "system"

	// Add system message once at the beginning when available
	if hasOriginalSystem && !hasSystemInPreserved {
		compressedMessages = append(compressedMessages, originalSystem)
	} else if hasSystemInPreserved {
		compressedMessages = append(compressedMessages, toPreserve[0])
		toPreserve = toPreserve[1:]
	}

	// 2) Add compressed summary as a user message (not modifying system prompt)
	summaryMsg := llm.Message{
		Role:    "user",
		Content: fmt.Sprintf("<!-- COMPRESSED CONTEXT -->\n%s\n<!-- END COMPRESSED CONTEXT -->", summary),
	}
	compressedMessages = append(compressedMessages, summaryMsg)

	// 3) Add canned assistant acknowledgement immediately after summary
	ackMsg := llm.Message{
		Role:    "assistant",
		Content: "Got it. Thanks for the additional context!",
	}
	compressedMessages = append(compressedMessages, ackMsg)

	// 4) Append preserved recent history
	if len(toPreserve) > 0 {
		compressedMessages = append(compressedMessages, toPreserve...)
	}

	// Calculate compression statistics
	compressedTokens := cs.EstimateTokenCount(compressedMessages)
	compressionInfo := &CompressionInfo{
		OriginalTokens:   currentTokens,
		CompressedTokens: compressedTokens,
		TokensSaved:      currentTokens - compressedTokens,
		CompressionRatio: float64(compressedTokens) / float64(currentTokens),
		MessagesBefore:   len(messages),
		MessagesAfter:    len(compressedMessages),
		TriggeredBy: func() string {
			if force {
				return "forced"
			}
			return "threshold"
		}(),
		Summary: summary,
	}

	// Guard: skip compression when savings are negligible
	minSavings := 256
	twoPercent := int(float64(currentTokens) * 0.02)
	if twoPercent > minSavings {
		minSavings = twoPercent
	}
	if compressionInfo.TokensSaved <= 0 || compressionInfo.TokensSaved < minSavings {
		logger.Infof("Compression skipped: insufficient savings (%d → %d, saved %d, min required %d)",
			currentTokens, compressedTokens, compressionInfo.TokensSaved, minSavings)
		return messages, nil, nil
	}

	logger.Infof("Compression completed (LLM summary): %d → %d tokens (%.2f%% reduction)",
		currentTokens, compressedTokens, (1.0-compressionInfo.CompressionRatio)*100)

	return compressedMessages, compressionInfo, nil
}

// fallbackTruncate implements a simple truncation strategy when LLM summarization fails.
// It preserves system messages and the most recent conversation turns.
func (cs *CompressionStrategy) fallbackTruncate(messages []llm.Message) ([]llm.Message, *CompressionInfo, error) {
	if len(messages) == 0 {
		return messages, nil, nil
	}

	// Determine how many recent turns to preserve
	preserveCount := cs.minMessagesToKeep
	if preserveCount == 0 {
		preserveCount = 4 // Default to 4 recent turns
	}

	var result []llm.Message

	// 1. Preserve all system messages
	for _, msg := range messages {
		if msg.Role == "system" {
			result = append(result, msg)
		}
	}

	// 2. Preserve the most recent N turns (user + assistant pairs)
	// Each turn typically consists of 2 messages (user + assistant)
	recentStart := len(messages) - (preserveCount * 2)
	if recentStart < len(result) {
		recentStart = len(result)
	}
	if recentStart < 0 {
		recentStart = 0
	}

	// Find the first non-system message to append recent turns
	systemCount := len(result)
	for i := recentStart; i < len(messages); i++ {
		if messages[i].Role != "system" || i >= systemCount {
			result = append(result, messages[i])
		}
	}

	// Calculate compression info
	originalTokens := cs.EstimateTokenCount(messages)
	finalTokens := cs.EstimateTokenCount(result)

	info := &CompressionInfo{
		OriginalTokens:   originalTokens,
		CompressedTokens: finalTokens,
		TokensSaved:      originalTokens - finalTokens,
		CompressionRatio: float64(finalTokens) / float64(originalTokens),
		MessagesBefore:   len(messages),
		MessagesAfter:    len(result),
		TriggeredBy:      "llm_failure_fallback",
		Summary:          "(fallback truncation: preserved system messages and recent turns)",
	}

	logger.Infof("Fallback truncation: %d → %d tokens (%.2f%% reduction), %d → %d messages",
		originalTokens, finalTokens, (1.0-info.CompressionRatio)*100,
		len(messages), len(result))

	return result, info, nil
}
