package agent

import (
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// Default values for context editing, applied when the config leaves them unset.
const (
	defaultEditTriggerRatio    = 0.5 // edit earlier than compaction (threshold is typically ≥0.7)
	defaultEditStaleTurns      = 4
	defaultEditKeepRecentTurns = 3
)

// clearedToolResultPrefix marks a tool result already replaced by the editor,
// so repeated editing passes are idempotent and the context prefix stays stable.
const clearedToolResultPrefix = "[tool result cleared:"

// ContextEditor implements context editing: a lightweight, deterministic
// companion to compaction. Instead of replacing a whole history segment with
// an LLM-generated summary (compaction), editing only clears *stale* tool
// results — tool outputs from turns older than a configurable threshold are
// replaced in place with a short placeholder — while recent turns are kept
// fully intact.
//
// Design constraints (from production evidence, see
// docs/features/CONTEXT_ENGINEERING.md):
//
//   - Edits happen only at stable boundaries: a "turn" runs from one user
//     message up to (but not including) the next user message, and tool
//     results are cleared only for whole turns. Message count, order, and
//     roles never change, so downstream prompt caches keyed on message
//     structure are not disturbed by partial edits.
//   - Editing is idempotent: the placeholder is deterministic
//     ("[tool result cleared: <tool_name>, <n> bytes]") and already-cleared
//     results are skipped, so a previously edited prefix remains byte-identical
//     and stays warm in the prompt cache.
//   - Editing triggers before compaction: it is cheaper (zero LLM calls) and
//     fires at a lower utilization ratio; compaction remains the fallback when
//     editing alone cannot bring the context back under budget.
type ContextEditor struct {
	triggerRatio    float64           // edit when estimated tokens exceed triggerRatio * maxTokens
	staleAfterTurns int               // tool results in turns at least this old are stale
	keepRecentTurns int               // the most recent N turns are never edited
	maxTokens       int               // context budget, aligned with the compression strategy
	tokenCounter    *llm.TokenCounter // accurate token counting
}

// NewContextEditor creates a context editor from the global config, reusing
// the compression strategy's token budget so editing and compaction agree on
// the context limit.
func NewContextEditor(cfg *config.Config, strategy *CompressionStrategy) *ContextEditor {
	maxTokens := 0
	var tokenCounter *llm.TokenCounter
	if strategy != nil {
		maxTokens = strategy.maxTokens
		tokenCounter = strategy.tokenCounter
	}

	triggerRatio := defaultEditTriggerRatio
	staleAfterTurns := defaultEditStaleTurns
	keepRecentTurns := defaultEditKeepRecentTurns
	if cfg != nil {
		if r := cfg.ContextConfig.EditTriggerRatio; r > 0 && r < 1 {
			triggerRatio = r
		}
		if n := cfg.ContextConfig.EditStaleTurns; n > 0 {
			staleAfterTurns = n
		}
		if n := cfg.ContextConfig.EditKeepRecentTurns; n > 0 {
			keepRecentTurns = n
		}
	}

	return &ContextEditor{
		triggerRatio:    triggerRatio,
		staleAfterTurns: staleAfterTurns,
		keepRecentTurns: keepRecentTurns,
		maxTokens:       maxTokens,
		tokenCounter:    tokenCounter,
	}
}

// NewContextEditorWithConfig creates a context editor with explicit settings.
// It is primarily used by tests and by callers that manage budgets manually.
func NewContextEditorWithConfig(maxTokens int, triggerRatio float64, staleAfterTurns, keepRecentTurns int) *ContextEditor {
	tokenCounter, _ := llm.NewTokenCounter("moonshot-v1")
	if triggerRatio <= 0 || triggerRatio >= 1 {
		triggerRatio = defaultEditTriggerRatio
	}
	if staleAfterTurns <= 0 {
		staleAfterTurns = defaultEditStaleTurns
	}
	if keepRecentTurns <= 0 {
		keepRecentTurns = defaultEditKeepRecentTurns
	}
	return &ContextEditor{
		triggerRatio:    triggerRatio,
		staleAfterTurns: staleAfterTurns,
		keepRecentTurns: keepRecentTurns,
		maxTokens:       maxTokens,
		tokenCounter:    tokenCounter,
	}
}

// ShouldEdit reports whether context editing should run for the given
// estimated token count. The trigger ratio is deliberately lower than the
// compaction threshold so editing always gets a chance to shrink the context
// before the more expensive compaction is considered.
func (e *ContextEditor) ShouldEdit(messages []llm.Message, currentTokens int) bool {
	if len(messages) == 0 || e.maxTokens <= 0 {
		return false
	}
	return currentTokens > int(float64(e.maxTokens)*e.triggerRatio)
}

// turnStartIndices returns the indices of messages that begin a turn. A turn
// starts at every user message; everything up to the next user message
// (assistant replies, tool calls, tool results) belongs to that turn.
func turnStartIndices(messages []llm.Message) []int {
	var starts []int
	for i, msg := range messages {
		if msg.Role == "user" {
			starts = append(starts, i)
		}
	}
	return starts
}

// EditContext replaces stale tool result contents with deterministic
// placeholders. It returns the edited messages (a copy; the input is not
// mutated) and the number of tool results cleared in this pass.
func (e *ContextEditor) EditContext(messages []llm.Message) ([]llm.Message, int) {
	if len(messages) == 0 {
		return messages, 0
	}

	turns := turnStartIndices(messages)
	if len(turns) == 0 {
		// No turn boundaries → nothing safe to edit.
		return messages, 0
	}

	// A turn is editable only when it is both stale (older than staleAfterTurns)
	// and outside the protected recent window (keepRecentTurns). The effective
	// cutoff is the more conservative of the two bounds.
	minAge := e.staleAfterTurns
	if e.keepRecentTurns > minAge {
		minAge = e.keepRecentTurns
	}
	lastEditableTurn := len(turns) - 1 - minAge
	if lastEditableTurn < 0 {
		return messages, 0
	}

	// Map tool-call IDs to tool names so placeholders can name the tool.
	toolNames := make(map[string]string)
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			toolNames[tc.ID] = tc.Name
		}
	}

	// The editable region ends at the start of the first non-editable turn,
	// i.e. a whole-turn boundary; a leading system message is never touched.
	editEnd := len(messages)
	if lastEditableTurn+1 < len(turns) {
		editEnd = turns[lastEditableTurn+1]
	}

	result := make([]llm.Message, len(messages))
	copy(result, messages)

	cleared := 0
	for i := 0; i < editEnd; i++ {
		msg := result[i]
		if msg.Role != "tool" {
			continue
		}
		if strings.HasPrefix(msg.Content, clearedToolResultPrefix) {
			continue // already cleared by an earlier pass; keeps the prefix byte-stable
		}
		toolName := toolNames[msg.ToolCallID]
		if toolName == "" {
			toolName = "unknown"
		}
		placeholder := fmt.Sprintf("%s %s, %d bytes]", clearedToolResultPrefix, toolName, len(msg.Content))
		if len(placeholder) >= len(msg.Content) {
			continue // never grow the context
		}
		result[i].Content = placeholder
		cleared++
	}

	if cleared > 0 {
		logger.Infof("Context editing: cleared %d stale tool results (turns older than %d, keeping %d recent turns)",
			cleared, minAge, e.keepRecentTurns)
	}
	return result, cleared
}

// EstimateTokenCount estimates the token count of the given messages, reusing
// the shared token counter when available.
func (e *ContextEditor) EstimateTokenCount(messages []llm.Message) int {
	if e.tokenCounter != nil {
		return e.tokenCounter.CountMessagesTokens(messages)
	}
	total := 0
	for _, msg := range messages {
		total += len(msg.Content)/4 + 10
	}
	return total
}
