package ui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	"github.com/nano-harness/nano-agent/pkg/ui/bubbletea/banner"
	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// BubbleTeaAdapter wraps pkg/ui/bubbletea as a ui.Adapter.
type BubbleTeaAdapter struct {
	model   *bubbletea.Model
	program *tea.Program

	// sendCh serializes all program.Send calls so that event ordering is
	// preserved and goroutine count stays bounded.
	sendCh     chan tea.Msg
	showBanner bool
}

// Run starts the Bubble Tea program and blocks until exit.
func (a *BubbleTeaAdapter) Run(ctx context.Context, src eventsource.EventSource) error {
	if a.showBanner {
		_ = banner.Play(os.Stdout, banner.Options{
			Theme:    banner.DefaultTheme,
			Colorize: banner.IsInteractiveTTY(),
		})
	}

	a.model.BindOutbound(src.Send)
	a.program = tea.NewProgram(a.model)

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := src.Start(childCtx); err != nil {
		return err
	}

	go func() {
		for msg := range a.sendCh {
			a.program.Send(msg)
		}
	}()
	go a.pumpInbound(childCtx, src)

	_, err := a.program.Run()
	_ = src.Close()
	return err
}

func (a *BubbleTeaAdapter) send(msg tea.Msg) {
	if a.program == nil {
		return
	}
	select {
	case a.sendCh <- msg:
	default:
	}
}

func (a *BubbleTeaAdapter) pumpInbound(ctx context.Context, src eventsource.EventSource) {
	for {
		select {
		case <-ctx.Done():
			return
		case in, ok := <-src.Inbound():
			if !ok {
				return
			}
			if in.State != nil {
				a.send(bubbletea.ConnectionStatusMsg{State: in.State.String(), Detail: src.Describe()})
			}
			if in.Notice != "" {
				a.send(bubbletea.NoticeMsg(in.Notice))
			} else if in.ResumedFrom > 0 {
				a.send(bubbletea.NoticeMsg(fmt.Sprintf("已重连，自 seq=%d 续传事件", in.ResumedFrom)))
			}
			if in.Event != nil {
				a.sendEvent(*in.Event)
			}
		}
	}
}

func (a *BubbleTeaAdapter) sendEvent(e event.StreamEvent) {
	if a.program == nil {
		return
	}
	switch e.Type {
	case event.EventTypeStreamContent:
		a.send(bubbletea.Message{Role: "assistant_stream", Content: e.Content})
	case event.EventTypeContent:
		if e.Source != "llm_client" {
			a.send(bubbletea.Message{Role: "assistant_stream", Content: e.Content})
		}
	case event.EventTypeError:
		a.send(bubbletea.Message{Role: "error", Content: e.Error})
	case event.EventTypeThinking:
		a.send(bubbletea.ThinkingMsg{
			Title:          e.Content,
			Reasoning:      e.Reasoning,
			ReasoningDelta: e.ReasoningDelta,
			Metadata:       e.Metadata,
		})
	case event.EventTypeDone:
		a.send(bubbletea.StatusUpdate("完成"))
	case event.EventTypeTaskCompletion:
		a.send(bubbletea.TaskCompletionMsg{Reason: stringFromMetadata(e.Metadata, "reason")})
	case event.EventTypeTokenStats:
		if e.TokenStats != nil {
			a.send(bubbletea.TokenStatsUpdate{
				InputTokens:  e.TokenStats.InputTokens,
				OutputTokens: e.TokenStats.OutputTokens,
				TotalTokens:  e.TokenStats.TotalTokens,
				Peak:         e.TokenStats.PeakTokensPerSecond,
			})
		}
	case event.EventTypeToolUse:
		if e.ToolUse != nil {
			toolID := e.ToolUse.ID
			if strings.TrimSpace(toolID) == "" {
				toolID = fmt.Sprintf("tooluse-%s", uuid.New().String())
			}
			a.send(bubbletea.ToolUseMsg{
				ID:       toolID,
				ToolName: e.ToolUse.ToolName,
				Status:   e.ToolUse.Status,
				Params:   e.ToolUse.Parameters,
				Result:   e.ToolUse.Result,
			})
		}
	case event.EventTypeWaitingForUser:
		if req, ok := approvalFromEvent(e); ok {
			a.send(bubbletea.ShowConfirmationMsg{
				Message:  fmt.Sprintf("确认执行工具：%s (ID: %s)？", req.toolName, req.callID),
				ToolInfo: req.toolInfo,
				Callback: func(approved bool) {
					_ = a.model.SendOutbound(eventsource.Outbound{
						Kind: "approval",
						Approval: &eventsource.ApprovalDecision{
							CallID: req.callID,
							Allow:  approved,
						},
					})
				},
			})
		}
	case event.EventTypeMailboxSent:
		a.send(bubbletea.MailboxMsg{
			From:    stringFromMetadata(e.Metadata, "from"),
			To:      stringFromMetadata(e.Metadata, "to"),
			Kind:    stringFromMetadata(e.Metadata, "kind"),
			Preview: e.Content,
		})
	}
}

// Stop quits the Bubble Tea program.
func (a *BubbleTeaAdapter) Stop() {
	if a.program != nil {
		a.program.Quit()
	}
}

type approvalRequest struct {
	callID   string
	toolName string
	toolInfo map[string]interface{}
}

func approvalFromEvent(e event.StreamEvent) (approvalRequest, bool) {
	if e.Metadata == nil || strings.TrimSpace(stringFromMetadata(e.Metadata, "kind")) != "tool_approval_request" {
		return approvalRequest{}, false
	}
	callID := strings.TrimSpace(stringFromMetadata(e.Metadata, "call_id"))
	if callID == "" {
		return approvalRequest{}, false
	}
	toolName := strings.TrimSpace(stringFromMetadata(e.Metadata, "tool_name"))
	params, _ := e.Metadata["parameters"].(map[string]interface{})
	return approvalRequest{
		callID:   callID,
		toolName: toolName,
		toolInfo: map[string]interface{}{
			"ID":         callID,
			"Name":       toolName,
			"Parameters": params,
		},
	}, true
}

func stringFromMetadata(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
