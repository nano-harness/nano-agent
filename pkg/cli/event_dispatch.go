package cli

import (
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/ui/tview"
)

// dispatchStreamEvent handles a stream event in TUI mode, routing it to the
// appropriate tview integration method. This consolidates the event switch
// that was previously duplicated for input-handler and initial-command paths.
func dispatchStreamEvent(integration *tview.Integration, se event.StreamEvent) {
	switch se.Type {
	case event.EventTypeStreamContent:
		integration.AddMessage("assistant", se.Content)
	case event.EventTypeContent:
		if se.Source != "llm_client" {
			integration.AddMessage("assistant", se.Content)
		}
	case event.EventTypeError:
		integration.AddMessage("system", fmt.Sprintf("Error: %s", se.Error))
	case event.EventTypeDone:
		integration.GetModel().GetStateManager().SetIdle()
	case event.EventTypeToolUse:
		if se.ToolUse != nil {
			integration.GetModel().GetStateManager().SetToolExecution(se.ToolUse.ToolName, "")
		}
		integration.GetModel().AddToolUse(se.ToolUse)
	case event.EventTypeTokenStats:
		integration.GetModel().GetStateManager().UpdateTokenStats(se.TokenStats)
	case event.EventTypeThinking:
		activity := se.Content
		if activity == "" {
			activity = "思考中"
		}
		integration.AddThinking(se.Content, se.Reasoning, se.Metadata)
		integration.GetModel().GetStateManager().SetThinking(activity)
	case event.EventTypeCompression:
		orig := se.Metadata["original_tokens"]
		cmp := se.Metadata["compressed_tokens"]
		ratio := se.Metadata["compression_ratio"]
		before := se.Metadata["messages_before"]
		after := se.Metadata["messages_after"]
		trigger := se.Metadata["triggered_by"]
		summary := se.Metadata["summary_full"]
		msg := fmt.Sprintf("🗜️ 上下文压缩: %v → %v tokens (减少 %.2f%%)\n消息数: %v → %v，触发: %v\n摘要: %v",
			orig, cmp, (1.0-float64FromAny(ratio))*100, before, after, trigger, truncatePreview(summary))
		integration.AddMessage("system", msg)
	case event.EventTypeTaskStart:
		activity := se.Content
		if activity == "" {
			activity = "开始任务"
		}
		integration.GetModel().GetStateManager().SetThinking(activity)
		integration.AddMessage("system", fmt.Sprintf("🟡 任务开始: %s", activity))
	case event.EventTypeTaskProgress:
		pct := int(se.Progress * 100)
		activity := fmt.Sprintf("任务进度: %d%%", pct)
		if se.Content != "" {
			activity = fmt.Sprintf("%s（%d%%）", se.Content, pct)
		}
		integration.GetModel().GetStateManager().SetProcessing(activity)
	case event.EventTypeTaskCompletion:
		activity := se.Content
		if activity == "" {
			activity = "任务完成"
		}
		integration.GetModel().GetStateManager().SetCompleted(activity)
		integration.GetModel().PlaySound("cough")
		integration.AddMessage("system", fmt.Sprintf("✅ 任务完成: %s", activity))
	case event.EventTypeSatisfactionEval:
		integration.GetModel().PlaySound("cough")
	case event.EventTypeTerminationSignal:
		integration.GetModel().PlaySound("cough")
	}
}
