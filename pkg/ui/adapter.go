// Package ui provides a unified interface for terminal user interfaces.
// It abstracts over tview-based (pkg/tui) and Bubble Tea-based (pkg/teaui) backends.
//
// Usage:
//
//	adapter := ui.NewFactory(cfg).Create(ui.ModeBubbleTea)
//	adapter.Run(ctx, src)
package ui

import (
	"context"

	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
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
	// Run starts the UI event loop driven by src. It blocks until the UI exits.
	Run(ctx context.Context, src eventsource.EventSource) error

	// Stop requests the UI to stop.
	Stop()
}
