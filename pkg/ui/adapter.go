// Package ui provides a unified interface for terminal user interfaces.
// It abstracts over tview-based (pkg/tui) and Bubble Tea-based (pkg/teaui) backends.
//
// Usage:
//
//	adapter := ui.NewFactory(cfg).Create(ui.ModeBubbleTea)
//	adapter.Run()
package ui

import (
	"github.com/nano-harness/nano-agent/pkg/event"
)

// Mode selects which UI backend to use.
type Mode string

const (
	// ModeTView uses the tview (tcell) backend.
	ModeTView Mode = "tview"
	// ModeBubbleTea uses the Bubble Tea backend.
	ModeBubbleTea Mode = "bubbletea"
	// ModeAuto selects the best available backend automatically.
	ModeAuto Mode = "auto"
)

// Adapter is the common interface that every UI backend must implement.
type Adapter interface {
	// Run starts the UI event loop. It blocks until the UI exits.
	Run() error

	// SendEvent forwards a stream event to the UI for display.
	SendEvent(e event.StreamEvent)

	// SubmitChannel returns a channel that receives user-submitted text.
	SubmitChannel() <-chan string

	// CancelChannel returns a channel that fires when the user requests cancellation.
	CancelChannel() <-chan struct{}

	// Stop requests the UI to stop.
	Stop()
}
