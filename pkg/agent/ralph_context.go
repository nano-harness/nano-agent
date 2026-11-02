package agent

import (
	"sync"

	"github.com/nano-harness/nano-agent/pkg/config"
)

const (
	defaultRalphMaxIterations = 10
	defaultRalphHardMax       = 50
)

// RalphContext tracks Stop-hook continuation state for a session.
type RalphContext struct {
	mu           sync.Mutex
	iteration    int
	maxIteration int
	hardMax      int
	active       bool
	enabled      bool
}

func NewRalphContext(cfg *config.Config) *RalphContext {
	r := &RalphContext{}
	r.Configure(cfg)
	return r
}

func (r *RalphContext) Configure(cfg *config.Config) {
	if r == nil {
		return
	}
	enabled := true
	maxIterations := defaultRalphMaxIterations
	hardMax := defaultRalphHardMax
	if cfg != nil && cfg.Hooks != nil {
		enabled = cfg.Hooks.Ralph.Enabled
		if cfg.Hooks.Ralph.MaxIterations > 0 {
			maxIterations = cfg.Hooks.Ralph.MaxIterations
		}
		if cfg.Hooks.Ralph.HardMaxIterations > 0 {
			hardMax = cfg.Hooks.Ralph.HardMaxIterations
		}
	}
	if hardMax <= 0 || hardMax > defaultRalphHardMax {
		hardMax = defaultRalphHardMax
	}
	if maxIterations <= 0 {
		maxIterations = defaultRalphMaxIterations
	}
	if maxIterations > hardMax {
		maxIterations = hardMax
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = enabled
	r.maxIteration = maxIterations
	r.hardMax = hardMax
}

func (r *RalphContext) IsEnabled() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enabled
}

func (r *RalphContext) IsActive() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

func (r *RalphContext) SetActive(active bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = active
}

func (r *RalphContext) Iteration() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.iteration
}

func (r *RalphContext) Max() int {
	if r == nil {
		return defaultRalphMaxIterations
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maxIteration
}

func (r *RalphContext) NextIteration() (int, bool) {
	if r == nil {
		return 0, true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.iteration++
	return r.iteration, r.iteration > r.maxIteration || r.iteration > r.hardMax
}

func (r *RalphContext) Reset() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.iteration = 0
	r.active = false
}
