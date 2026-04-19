package ui

import (
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/ui/tview"
)

// TViewAdapter wraps pkg/ui/tview as a ui.Adapter.
type TViewAdapter struct {
	integration *tview.Integration
	submitCh    chan string
	cancelCh    chan struct{}
}

// Run starts the tview event loop and blocks until exit.
func (a *TViewAdapter) Run() error {
	return a.integration.Run()
}

// SendEvent forwards a StreamEvent to the tview model for rendering.
func (a *TViewAdapter) SendEvent(e event.StreamEvent) {
	switch e.Type {
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
	}
}

// SubmitChannel returns the channel that receives user-submitted text.
func (a *TViewAdapter) SubmitChannel() <-chan string { return a.submitCh }

// CancelChannel returns the channel that fires on user cancellation.
func (a *TViewAdapter) CancelChannel() <-chan struct{} { return a.cancelCh }

// Stop stops the tview integration.
func (a *TViewAdapter) Stop() {
	a.integration.Stop()
}
