package ui

import (
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	"github.com/nano-harness/nano-agent/pkg/ui/tview"
	tea "github.com/charmbracelet/bubbletea"
)

// Config holds common configuration shared by all UI backends.
type Config struct {
	APIBaseURL string
	WorkingDir string
}

// Factory creates UI adapters.
type Factory struct {
	cfg Config
}

// NewFactory creates a Factory with the given configuration.
func NewFactory(cfg Config) *Factory {
	return &Factory{cfg: cfg}
}

// Create returns an Adapter for the specified mode.
// If mode is ModeAuto it selects ModeBubbleTea.
func (f *Factory) Create(mode Mode) (Adapter, error) {
	switch mode {
	case ModeAuto, ModeBubbleTea:
		return f.newBubbleTeaAdapter(), nil
	case ModeTView:
		return f.newTViewAdapter(), nil
	default:
		return nil, fmt.Errorf("ui: unknown mode %q", mode)
	}
}

// ─── Bubble Tea adapter ──────────────────────────────────────────────────────

func (f *Factory) newBubbleTeaAdapter() *BubbleTeaAdapter {
	submitCh := make(chan string, 1)
	cancelCh := make(chan struct{}, 1)
	return &BubbleTeaAdapter{
		model:    bubbletea.New(submitCh, cancelCh, f.cfg.APIBaseURL, f.cfg.WorkingDir),
		submitCh: submitCh,
		cancelCh: cancelCh,
		sendCh:   make(chan tea.Msg, 256),
	}
}

// ─── TView adapter ───────────────────────────────────────────────────────────

func (f *Factory) newTViewAdapter() *TViewAdapter {
	integration := tview.NewIntegration()
	return &TViewAdapter{integration: integration}
}
