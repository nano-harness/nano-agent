package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	"github.com/nano-harness/nano-agent/pkg/ui/tview"
)

// Config holds common configuration shared by all UI backends.
type Config struct {
	APIBaseURL string
	WorkingDir string
	// ShowBanner controls whether to play the animated banner on TUI startup. Default true.
	ShowBanner bool
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
	return &BubbleTeaAdapter{
		model:      bubbletea.New(nil, nil, f.cfg.APIBaseURL, f.cfg.WorkingDir),
		sendCh:     make(chan tea.Msg, 1024),
		showBanner: f.cfg.ShowBanner,
	}
}

// ─── TView adapter ───────────────────────────────────────────────────────────

func (f *Factory) newTViewAdapter() *TViewAdapter {
	integration := tview.NewIntegration()
	return &TViewAdapter{integration: integration}
}
