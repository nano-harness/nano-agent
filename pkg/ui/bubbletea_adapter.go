package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

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
	model   tea.Model
	program *tea.Program

	// sendCh serializes all program.Send calls so that event ordering is
	// preserved and goroutine count stays bounded.
	sendCh     chan tea.Msg
	showBanner bool

	// shutdownMu protects shutting state to prevent writing to closed channel
	shutdownMu sync.RWMutex
	shutting   bool
}

// Run starts the Bubble Tea program and blocks until exit.
func (a *BubbleTeaAdapter) Run(ctx context.Context, src eventsource.EventSource) error {
	if a.showBanner {
		_, isFullscreen := a.model.(*bubbletea.FullscreenModel)
		// Fullscreen (alt-screen) mode clears the terminal on start, so
		// playing the animation to stdout would be invisible. Skip Play()
		// and only inject the static banner art into the model.
		if !isFullscreen {
			_ = banner.Play(os.Stdout, banner.Options{
				Theme:    banner.DefaultTheme,
				Colorize: banner.IsInteractiveTTY(),
				IconMode: banner.IconTea,
			})
		}
		// Persist the static last frame inside the inline View so the TUI
		// keeps showing the product mark after the animation finishes.
		if m, ok := a.model.(*bubbletea.Model); ok {
			if art, err := banner.LastFrameRendered(banner.DefaultTheme, banner.IsInteractiveTTY(), banner.IconTea); err == nil {
				m.SetBannerArt(art)
			}
		}
		if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
			if art, err := banner.LastFrameRendered(banner.DefaultTheme, banner.IsInteractiveTTY(), banner.IconMilkTea); err == nil {
				m.SetBannerArt(art)
			}
		}
	}

	// Bind outbound channel based on model type
	if binder, ok := a.model.(interface {
		BindOutbound(func(eventsource.Outbound) error)
	}); ok {
		binder.BindOutbound(src.Send)
	}

	// Create program (fullscreen models set AltScreen in their View() return)
	a.program = tea.NewProgram(a.model)

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := src.Start(childCtx); err != nil {
		return err
	}

	// Use WaitGroup to ensure both goroutines complete before returning
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for msg := range a.sendCh {
			a.program.Send(msg)
		}
	}()
	go func() {
		defer wg.Done()
		a.pumpInbound(childCtx, src)
	}()

	_, err := a.program.Run()

	// Signal that we're shutting down to prevent writes to closed channel
	a.shutdownMu.Lock()
	a.shutting = true
	a.shutdownMu.Unlock()

	// Close event source first to unblock pumpInbound
	_ = src.Close()

	// Cancel context to signal goroutines to exit
	cancel()

	// Close sendCh to stop the message pump goroutine
	close(a.sendCh)

	// Wait for all goroutines to complete with a timeout to prevent hanging
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed successfully
	case <-time.After(2 * time.Second):
		// Timeout waiting for goroutines - this shouldn't happen but prevents indefinite hang
	}

	return err
}

func (a *BubbleTeaAdapter) send(msg tea.Msg) {
	if a.program == nil {
		return
	}

	// Check if we're shutting down to avoid writing to closed channel
	a.shutdownMu.RLock()
	isShutting := a.shutting
	a.shutdownMu.RUnlock()

	if isShutting {
		return
	}

	select {
	case a.sendCh <- msg:
	default:
		// Channel full; deliver via goroutine so critical messages (e.g.
		// TaskCompletionMsg, ShowConfirmationMsg) are never silently dropped.
		// Ordering may be affected under extreme event bursts, but correctness
		// is preferred over strict ordering in the overflow case.
		go func() {
			// Re-check shutting state in the goroutine
			a.shutdownMu.RLock()
			defer a.shutdownMu.RUnlock()
			if !a.shutting {
				a.sendCh <- msg
			}
		}()
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
	// Delegate all standard event translation to the shared forwarder so both
	// runBubbleTeaMode (root.go) and BubbleTeaAdapter stay in sync. The forwarder
	// sends via a.send() (which serialises through sendCh) to preserve ordering.
	// EventTypeWaitingForUser requires adapter-specific outbound handling, so it
	// is processed below after the common switch.
	// EventTypeToolUse also requires a UUID fallback for empty IDs, handled below.
	switch e.Type {
	case event.EventTypeWaitingForUser:
		if req, ok := approvalFromEvent(e); ok {
			callID := req.callID
			a.send(bubbletea.ShowConfirmationMsg{
				Message:  fmt.Sprintf("确认执行工具：%s (ID: %s)？", req.toolName, callID),
				ToolInfo: req.toolInfo,
				// Callback handles "同意" (yes) and "拒绝" (no).
				Callback: func(approved bool) {
					if sender, ok := a.model.(interface {
						SendOutbound(eventsource.Outbound) error
					}); ok {
						_ = sender.SendOutbound(eventsource.Outbound{
							Kind: "approval",
							Approval: &eventsource.ApprovalDecision{
								CallID: callID,
								Allow:  approved,
							},
						})
					}
				},
				// AlwaysCallback handles "始终允许": sends Allow:true with Always:true so
				// the daemon can persist the rule. The model's allowlistHandler (if wired)
				// is still invoked afterwards to update the local session allowlist.
				AlwaysCallback: func() {
					if sender, ok := a.model.(interface {
						SendOutbound(eventsource.Outbound) error
					}); ok {
						_ = sender.SendOutbound(eventsource.Outbound{
							Kind: "approval",
							Approval: &eventsource.ApprovalDecision{
								CallID: callID,
								Allow:  true,
								Always: true,
							},
						})
					}
				},
			})
		}
		return
	case event.EventTypeToolUse:
		// Generate a UUID when the upstream tool ID is empty so the dedup
		// map in the TUI model always has a stable key.
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
		return
	}
	bubbletea.ForwardStreamEvent(a.send, e)
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
// These methods forward configuration to the underlying bubbletea model.
// Inline (Model) mode receives every setter. Fullscreen (FullscreenModel)
// mode receives the subset it implements (permission manager, new-session
// handler, model lister); other setters are intentionally no-ops there
// until the corresponding feature is ported.

// SetPermissionManager enables /yolo, /permission, /allow, /disallow, /permissions
// slash commands and Shift+Tab permission-mode cycling.
func (a *BubbleTeaAdapter) SetPermissionManager(mgr *permission.Manager) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetPermissionManager(mgr)
		return
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetPermissionManager(mgr)
	}
}

// SetPersistentAllowlist enables /disallow to remove rules from persistent storage.
func (a *BubbleTeaAdapter) SetPersistentAllowlist(store *permission.PersistentAllowlistStore, workdir string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetPersistentAllowlist(store, workdir)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetPersistentAllowlist(store, workdir)
	}
}

// SetEngine enables /think and other engine-level slash commands.
func (a *BubbleTeaAdapter) SetEngine(eng *engine.Engine) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetEngine(eng)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetEngine(eng)
	}
}

// SetAllowlistHandler registers the callback invoked when the user selects
// "始终允许" to add a rule to the local session allowlist.
func (a *BubbleTeaAdapter) SetAllowlistHandler(h func(toolName string, params map[string]interface{})) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetAllowlistHandler(h)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetAllowlistHandler(h)
	}
}

// SetAvailableToolNames provides tool names used for Tab completion of /allow <tool>.
func (a *BubbleTeaAdapter) SetAvailableToolNames(names []string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetAvailableToolNames(names)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetAvailableToolNames(names)
	}
}

// SetNewSessionHandler registers the callback invoked by Ctrl+L / /clear.
func (a *BubbleTeaAdapter) SetNewSessionHandler(h func() string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetNewSessionHandler(h)
		return
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetNewSessionHandler(h)
	}
}

// SetTeamName scopes /teammates and /agents slash commands to the given team.
func (a *BubbleTeaAdapter) SetTeamName(name string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetTeamName(name)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetTeamName(name)
	}
}

func (a *BubbleTeaAdapter) SetModelLister(fn func() string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetModelLister(fn)
		return
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetModelLister(fn)
	}
}

func (a *BubbleTeaAdapter) SetSkillLister(fn func() string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetSkillLister(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetSkillLister(fn)
	}
}

func (a *BubbleTeaAdapter) SetModelStatusGetter(fn func() string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetModelStatusGetter(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetModelStatusGetter(fn)
	}
}

func (a *BubbleTeaAdapter) SetModelSwitcher(fn func(string) string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetModelSwitcher(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetModelSwitcher(fn)
	}
}

func (a *BubbleTeaAdapter) SetModelFallbackHandler(fn func(string) string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetModelFallbackHandler(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetModelFallbackHandler(fn)
	}
}

func (a *BubbleTeaAdapter) SetModelDoctor(fn func(string) string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetModelDoctor(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetModelDoctor(fn)
	}
}

func (a *BubbleTeaAdapter) SetContextStatusGetter(fn func() string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetContextStatusGetter(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetContextStatusGetter(fn)
	}
}

func (a *BubbleTeaAdapter) SetDoctorReporter(fn func() string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetDoctorReporter(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetDoctorReporter(fn)
	}
}

func (a *BubbleTeaAdapter) SetEventsQuerier(fn func(string) string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetEventsQuerier(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetEventsQuerier(fn)
	}
}

func (a *BubbleTeaAdapter) SetAuditQuerier(fn func(string) string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetAuditQuerier(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetAuditQuerier(fn)
	}
}

// SetRoutinesLister wires the callback for /routines list.
func (a *BubbleTeaAdapter) SetRoutinesLister(fn func() string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetRoutinesLister(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetRoutinesLister(fn)
	}
}

// SetRunningStatusLister wires the callback for /routines status.
func (a *BubbleTeaAdapter) SetRunningStatusLister(fn func() string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetRunningStatusLister(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetRunningStatusLister(fn)
	}
}

func (a *BubbleTeaAdapter) SetRoutinesAdder(fn func(string) string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetRoutinesAdder(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetRoutinesAdder(fn)
	}
}

func (a *BubbleTeaAdapter) SetRoutinesRemover(fn func(string) string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetRoutinesRemover(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetRoutinesRemover(fn)
	}
}

func (a *BubbleTeaAdapter) SetRoutinesPauser(fn func(string) string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetRoutinesPauser(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetRoutinesPauser(fn)
	}
}

func (a *BubbleTeaAdapter) SetRoutinesResumer(fn func(string) string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetRoutinesResumer(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetRoutinesResumer(fn)
	}
}

func (a *BubbleTeaAdapter) SetRoutinesRunner(fn func(string) string) {
	if m, ok := a.model.(*bubbletea.Model); ok {
		m.SetRoutinesRunner(fn)
	}
	if m, ok := a.model.(*bubbletea.FullscreenModel); ok {
		m.SetRoutinesRunner(fn)
	}
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
