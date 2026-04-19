package ui

import (
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// BubbleTeaAdapter wraps pkg/ui/bubbletea as a ui.Adapter.
type BubbleTeaAdapter struct {
	model    *bubbletea.Model
	program  *tea.Program
	submitCh chan string
	cancelCh chan struct{}

	// sendCh serializes all program.Send calls so that event ordering is
	// preserved and goroutine count stays bounded.
	sendCh chan tea.Msg
}

// Run starts the Bubble Tea program and blocks until exit.
func (a *BubbleTeaAdapter) Run() error {
	a.program = tea.NewProgram(a.model)

	// Start the serializing dispatcher before blocking on Run.
	go func() {
		for msg := range a.sendCh {
			a.program.Send(msg)
		}
	}()

	_, err := a.program.Run()
	return err
}

// send enqueues a message for delivery to the Bubble Tea program.
// All sends go through this single channel to guarantee ordering.
func (a *BubbleTeaAdapter) send(msg tea.Msg) {
	if a.program == nil {
		return
	}
	// Non-blocking send: drop the message if the channel is full to avoid
	// blocking the caller on a stalled/stopped program.
	select {
	case a.sendCh <- msg:
	default:
	}
}

// SendEvent forwards a StreamEvent to the Bubble Tea model via the serialized send queue.
func (a *BubbleTeaAdapter) SendEvent(e event.StreamEvent) {
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
	case event.EventTypeTokenStats:
		if e.TokenStats != nil {
			a.send(bubbletea.TokenStatsUpdate{
				InputTokens:  e.TokenStats.InputTokens,
				OutputTokens: e.TokenStats.OutputTokens,
				TotalTokens:  e.TokenStats.TotalTokens,
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
	}
}

// SubmitChannel returns the channel that receives user-submitted text.
func (a *BubbleTeaAdapter) SubmitChannel() <-chan string { return a.submitCh }

// CancelChannel returns the channel that fires on user cancellation.
func (a *BubbleTeaAdapter) CancelChannel() <-chan struct{} { return a.cancelCh }

// Stop quits the Bubble Tea program.
func (a *BubbleTeaAdapter) Stop() {
	if a.program != nil {
		a.program.Quit()
	}
}

// SetPermissionManager wires a permission.Manager into the Bubble Tea model so
// that slash commands (/yolo, /permission, etc.) work at runtime.
func (a *BubbleTeaAdapter) SetPermissionManager(mgr *permission.Manager) {
	if a.model != nil {
		a.model.SetPermissionManager(mgr)
	}
}

// SetEngine wires an Engine into the Bubble Tea model so that slash commands
// (/think) can control thinking mode and other engine-level settings.
func (a *BubbleTeaAdapter) SetEngine(eng *engine.Engine) {
	if a.model != nil {
		a.model.SetEngine(eng)
	}
}

// SetAvailableToolNames passes the list of tool names to the model for Tab completion.
func (a *BubbleTeaAdapter) SetAvailableToolNames(names []string) {
	if a.model != nil {
		a.model.SetAvailableToolNames(names)
	}
}

// ShowConfirmation sends a confirmation request to the Bubble Tea event loop.
// It uses program.Send() directly (blocking, goroutine-safe) to guarantee
// delivery — confirmation messages must never be silently dropped, as losing
// one would leave the tool scheduler permanently stuck in awaiting_approval.
func (a *BubbleTeaAdapter) ShowConfirmation(message string, toolInfo map[string]interface{}, callback func(bool)) {
	if a.program == nil {
		return
	}
	a.program.Send(bubbletea.ShowConfirmationMsg{
		Message:  message,
		ToolInfo: toolInfo,
		Callback: callback,
	})
}

// SetAllowlistHandler registers the callback for the "始终允许" (always allow) option.
func (a *BubbleTeaAdapter) SetAllowlistHandler(h func(toolName string, params map[string]interface{})) {
	if a.model != nil {
		a.model.SetAllowlistHandler(h)
	}
}
