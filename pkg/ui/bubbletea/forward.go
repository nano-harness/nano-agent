package bubbletea

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/nano-harness/nano-agent/pkg/event"
)

// ForwardStreamEvent translates a StreamEvent into the appropriate Bubble Tea
// message and delivers it via the provided send function. This is the canonical
// translation layer shared by runBubbleTeaMode (pkg/cli/root.go) and
// BubbleTeaAdapter (pkg/ui/bubbletea_adapter.go). Any new event type should be
// added here so both code paths receive it automatically.
//
// The send parameter is called with the constructed tea.Msg. Callers may pass
// p.Send directly (for synchronous delivery) or wrap it in a serialising channel
// (as BubbleTeaAdapter does) to preserve event ordering.
//
// The following event types are intentionally NOT handled here because they
// require caller-specific context:
//   - EventTypeWaitingForUser: root.go uses a blocking approval handler wired
//     into the agent scheduler; BubbleTeaAdapter uses outbound channels. Each
//     caller must handle this type itself.
//   - EventTypeCronTask*: consumed by CronStatusTracker, not the TUI model.
func ForwardStreamEvent(send func(tea.Msg), e event.StreamEvent) {
	switch e.Type {
	case event.EventTypeCronTaskStarted, event.EventTypeCronTaskFinished, event.EventTypeCronTaskProgress:
		// Handled by CronStatusTracker; not forwarded to the TUI model.
		return
	case event.EventTypeStreamContent:
		send(Message{Role: "assistant_stream", Content: e.Content})
	case event.EventTypeContent:
		if e.Source != "llm_client" {
			send(Message{Role: "assistant_stream", Content: e.Content})
		}
	case event.EventTypeError:
		send(Message{Role: "error", Content: e.Error})
	case event.EventTypeThinking:
		send(ThinkingMsg{
			Title:          e.Content,
			Reasoning:      e.Reasoning,
			ReasoningDelta: e.ReasoningDelta,
			Metadata:       e.Metadata,
		})
	case event.EventTypeToolUse:
		if e.ToolUse == nil {
			return
		}
		send(ToolUseMsg{
			ID:       e.ToolUse.ID,
			ToolName: e.ToolUse.ToolName,
			Status:   e.ToolUse.Status,
			Params:   e.ToolUse.Parameters,
			Result:   e.ToolUse.Result,
		})
	case event.EventTypeTokenStats:
		if e.TokenStats != nil {
			send(TokenStatsUpdate{
				InputTokens:       e.TokenStats.InputTokens,
				OutputTokens:      e.TokenStats.OutputTokens,
				TotalTokens:       e.TokenStats.TotalTokens,
				Peak:              e.TokenStats.PeakTokensPerSecond,
				ContextWindowMax:  e.TokenStats.ContextWindowMax,
				ContextUsedTokens: e.TokenStats.ContextUsedTokens,
			})
		}
	case event.EventTypeDone:
		send(StatusUpdate("完成"))
	case event.EventTypeTaskCompletion:
		send(TaskCompletionMsg{Reason: forwardStringFromMetadata(e.Metadata, "reason")})
	case event.EventTypeMailboxSent:
		send(MailboxMsg{
			From:    forwardStringFromMetadata(e.Metadata, "from"),
			To:      forwardStringFromMetadata(e.Metadata, "to"),
			Kind:    forwardStringFromMetadata(e.Metadata, "kind"),
			Preview: e.Content,
		})
	case event.EventTypeCompression:
		orig := e.Metadata["original_tokens"]
		cmp := e.Metadata["compressed_tokens"]
		ratio := e.Metadata["compression_ratio"]
		before := e.Metadata["messages_before"]
		after := e.Metadata["messages_after"]
		trigger := e.Metadata["triggered_by"]
		summary := e.Metadata["summary_full"]
		content := fmt.Sprintf("🗜️ 上下文压缩: %v → %v tokens (减少 %.2f%%)\n消息数: %v → %v，触发: %v\n摘要: %v",
			orig, cmp, (1.0-forwardFloat64(ratio))*100, before, after, trigger, forwardTruncatePreview(summary))
		send(Message{Role: "system", Content: content})
	}
}

// forwardStringFromMetadata safely extracts a string from a metadata map.
func forwardStringFromMetadata(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

// forwardFloat64 converts interface{} to float64 safely.
func forwardFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	}
	return 0
}

// forwardTruncatePreview safely truncates preview text to a reasonable length.
func forwardTruncatePreview(v interface{}) string {
	if v == nil {
		return ""
	}
	s := fmt.Sprintf("%v", v)
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}
