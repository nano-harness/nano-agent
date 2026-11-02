package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
	"github.com/nano-harness/nano-agent/pkg/ui/tview"
)

// TViewAdapter wraps pkg/ui/tview as a ui.Adapter.
type TViewAdapter struct {
	integration *tview.Integration
}

// Run starts the tview event loop and blocks until exit.
func (a *TViewAdapter) Run(ctx context.Context, src eventsource.EventSource) error {
	a.integration.BindOutbound(src.Send)
	if provider, ok := src.(interface{ GoalHandler() func(string) string }); ok {
		a.integration.SetGoalHandler(provider.GoalHandler())
	}
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := src.Start(childCtx); err != nil {
		return err
	}
	go a.pumpInbound(childCtx, src)
	err := a.integration.Run()
	_ = src.Close()
	return err
}

func (a *TViewAdapter) pumpInbound(ctx context.Context, src eventsource.EventSource) {
	for {
		select {
		case <-ctx.Done():
			return
		case in, ok := <-src.Inbound():
			if !ok {
				return
			}
			if in.State != nil {
				a.integration.SetConnectionStatus(in.State.String(), src.Describe())
			}
			if in.Notice != "" {
				a.integration.AddNotice(in.Notice)
			} else if in.ResumedFrom > 0 {
				a.integration.AddNotice(fmt.Sprintf("已重连，自 seq=%d 续传事件", in.ResumedFrom))
			}
			if in.Event != nil {
				a.sendEvent(*in.Event)
			}
		}
	}
}

func (a *TViewAdapter) sendEvent(e event.StreamEvent) {
	switch e.Type {
	case event.EventTypeCronTaskStarted, event.EventTypeCronTaskFinished, event.EventTypeCronTaskProgress:
		return
	case event.EventTypeStreamContent:
		a.integration.AddMessage("assistant", e.Content)
	case event.EventTypeContent:
		if e.Source != "llm_client" {
			a.integration.AddMessage("assistant", e.Content)
		}
	case event.EventTypeError:
		a.integration.AddMessage("error", e.Error)
	case event.EventTypeThinking:
		a.integration.AddThinking(e.Content, e.Reasoning, e.Metadata)
	case event.EventTypeDone:
		a.integration.FinishStreaming()
	case event.EventTypeToolUse:
		if e.ToolUse != nil {
			a.integration.AddToolUse(e.ToolUse)
		}
	case event.EventTypeWaitingForUser:
		if req, ok := approvalFromEvent(e); ok {
			a.integration.ShowConfirmation(fmt.Sprintf("确认执行工具：%s (ID: %s)？", req.toolName, req.callID), req.toolInfo, func(approved bool) {
				_ = a.integration.SendOutbound(eventsource.Outbound{
					Kind: "approval",
					Approval: &eventsource.ApprovalDecision{
						CallID: req.callID,
						Allow:  approved,
					},
				})
			})
		}
	case event.EventTypeMailboxSent:
		a.integration.UpdateSwarmRoster(e.Content)
	case event.EventTypeWarning:
		if strings.HasPrefix(e.Content, "✅ /goal achieved:") ||
			strings.HasPrefix(e.Content, "/goal max turns reached:") {
			a.integration.SetGoalActive(false)
		}
		a.integration.AddMessage("system", e.Content)
	}
}

func (a *TViewAdapter) SetCronTracker(t *CronStatusTracker) {
	a.integration.SetCronTracker(t)
}

// Stop stops the tview integration.
func (a *TViewAdapter) Stop() {
	a.integration.Stop()
}
