package ui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	"github.com/nano-harness/nano-agent/pkg/ui/bubbletea/banner"
	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
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
		// Channel full; deliver via goroutine so critical messages (e.g.
		// TaskCompletionMsg, ShowConfirmationMsg) are never silently dropped.
		// Ordering may be affected under extreme event bursts, but correctness
		// is preferred over strict ordering in the overflow case.
		go func() { a.sendCh <- msg }()
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
	case event.EventTypeCronTaskStarted, event.EventTypeCronTaskFinished, event.EventTypeCronTaskProgress:
		return
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
				InputTokens:       e.TokenStats.InputTokens,
				OutputTokens:      e.TokenStats.OutputTokens,
				TotalTokens:       e.TokenStats.TotalTokens,
				Peak:              e.TokenStats.PeakTokensPerSecond,
				ContextWindowMax:  e.TokenStats.ContextWindowMax,
				ContextUsedTokens: e.TokenStats.ContextUsedTokens,
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
			callID := req.callID
			a.send(bubbletea.ShowConfirmationMsg{
				Message:  fmt.Sprintf("确认执行工具：%s (ID: %s)？", req.toolName, callID),
				ToolInfo: req.toolInfo,
				// Callback handles "同意" (yes) and "拒绝" (no).
				Callback: func(approved bool) {
					_ = a.model.SendOutbound(eventsource.Outbound{
						Kind: "approval",
						Approval: &eventsource.ApprovalDecision{
							CallID: callID,
							Allow:  approved,
						},
					})
				},
				// AlwaysCallback handles "始终允许": sends Allow:true with Always:true so
				// the daemon can persist the rule. The model's allowlistHandler (if wired)
				// is still invoked afterwards to update the local session allowlist.
				AlwaysCallback: func() {
					_ = a.model.SendOutbound(eventsource.Outbound{
						Kind: "approval",
						Approval: &eventsource.ApprovalDecision{
							CallID: callID,
							Allow:  true,
							Always: true,
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

func (a *BubbleTeaAdapter) AttachCronTracker(t *CronStatusTracker) {
	if t == nil {
		return
	}
	t.SetOnChange(func() {
		a.send(bubbletea.CronStatusMsg{Indicator: t.FormatIndicator()})
	})
}

// Stop quits the Bubble Tea program.
func (a *BubbleTeaAdapter) Stop() {
	if a.program != nil {
		a.program.Quit()
	}
}

// ── Capability setters ───────────────────────────────────────────────────────
// These methods forward configuration to the underlying bubbletea.Model so
// that callers using BubbleTeaAdapter via the Adapter interface can still wire
// runtime capabilities such as the permission manager and engine.

// SetPermissionManager enables /yolo, /permission, /allow, /disallow, /permissions
// slash commands and Shift+Tab permission-mode cycling.
func (a *BubbleTeaAdapter) SetPermissionManager(mgr *permission.Manager) {
	a.model.SetPermissionManager(mgr)
}

// SetPersistentAllowlist enables /disallow to remove rules from persistent storage.
func (a *BubbleTeaAdapter) SetPersistentAllowlist(store *permission.PersistentAllowlistStore, workdir string) {
	a.model.SetPersistentAllowlist(store, workdir)
}

// SetEngine enables /think and other engine-level slash commands.
func (a *BubbleTeaAdapter) SetEngine(eng *engine.Engine) {
	a.model.SetEngine(eng)
}

// SetAllowlistHandler registers the callback invoked when the user selects
// "始终允许" to add a rule to the local session allowlist.
func (a *BubbleTeaAdapter) SetAllowlistHandler(h func(toolName string, params map[string]interface{})) {
	a.model.SetAllowlistHandler(h)
}

// SetAvailableToolNames provides tool names used for Tab completion of /allow <tool>.
func (a *BubbleTeaAdapter) SetAvailableToolNames(names []string) {
	a.model.SetAvailableToolNames(names)
}

// SetNewSessionHandler registers the callback invoked by Ctrl+L / /clear.
func (a *BubbleTeaAdapter) SetNewSessionHandler(h func() string) {
	a.model.SetNewSessionHandler(h)
}

// SetTeamName scopes /teammates and /agents slash commands to the given team.
func (a *BubbleTeaAdapter) SetTeamName(name string) {
	a.model.SetTeamName(name)
}

func (a *BubbleTeaAdapter) SetModelLister(fn func() string) {
	a.model.SetModelLister(fn)
}

func (a *BubbleTeaAdapter) SetSkillLister(fn func() string) {
	a.model.SetSkillLister(fn)
}

func (a *BubbleTeaAdapter) SetModelStatusGetter(fn func() string) {
	a.model.SetModelStatusGetter(fn)
}

func (a *BubbleTeaAdapter) SetModelSwitcher(fn func(string) string) {
	a.model.SetModelSwitcher(fn)
}

func (a *BubbleTeaAdapter) SetModelFallbackHandler(fn func(string) string) {
	a.model.SetModelFallbackHandler(fn)
}

func (a *BubbleTeaAdapter) SetModelDoctor(fn func(string) string) {
	a.model.SetModelDoctor(fn)
}

func (a *BubbleTeaAdapter) SetContextStatusGetter(fn func() string) {
	a.model.SetContextStatusGetter(fn)
}

func (a *BubbleTeaAdapter) SetDoctorReporter(fn func() string) {
	a.model.SetDoctorReporter(fn)
}

func (a *BubbleTeaAdapter) SetEventsQuerier(fn func(string) string) {
	a.model.SetEventsQuerier(fn)
}

func (a *BubbleTeaAdapter) SetAuditQuerier(fn func(string) string) {
	a.model.SetAuditQuerier(fn)
}

// SetRoutinesLister wires the callback for /routines list.
func (a *BubbleTeaAdapter) SetRoutinesLister(fn func() string) {
	a.model.SetRoutinesLister(fn)
}

// SetRunningStatusLister wires the callback for /routines status.
func (a *BubbleTeaAdapter) SetRunningStatusLister(fn func() string) {
	a.model.SetRunningStatusLister(fn)
}

func (a *BubbleTeaAdapter) SetRoutinesAdder(fn func(string) string) {
	a.model.SetRoutinesAdder(fn)
}

func (a *BubbleTeaAdapter) SetRoutinesRemover(fn func(string) string) {
	a.model.SetRoutinesRemover(fn)
}

func (a *BubbleTeaAdapter) SetRoutinesPauser(fn func(string) string) {
	a.model.SetRoutinesPauser(fn)
}

func (a *BubbleTeaAdapter) SetRoutinesResumer(fn func(string) string) {
	a.model.SetRoutinesResumer(fn)
}

// ─────────────────────────────────────────────────────────────────────────────

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
