// Package swarm provides multi-agent runtime and spawn capabilities
package swarm

import (
	"context"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/config"
)

// Runner is the interface for running a teammate agent
type Runner interface {
	// Run starts a teammate agent with the given identity and initial prompt
	// Blocks until the agent completes or context is cancelled
	Run(ctx context.Context, identity *TeammateIdentity, initialPrompt string) error
}

// DefaultRunner implements Runner using the standard agent.Agent
type DefaultRunner struct {
	cfg     *config.Config
	runFunc func(context.Context, *TeammateIdentity, string, *config.Config) error
}

var defaultRunFunc func(context.Context, *TeammateIdentity, string, *config.Config) error

// SetDefaultRunFunc configures how DefaultRunner constructs and runs teammates.
func SetDefaultRunFunc(fn func(context.Context, *TeammateIdentity, string, *config.Config) error) {
	defaultRunFunc = fn
}

// NewDefaultRunner creates a new DefaultRunner with the given configuration
func NewDefaultRunner(cfg *config.Config) *DefaultRunner {
	return &DefaultRunner{cfg: cfg, runFunc: defaultRunFunc}
}

// Run implements Runner by creating and running an agent.Agent in teammate mode
func (r *DefaultRunner) Run(ctx context.Context, identity *TeammateIdentity, initialPrompt string) error {
	if identity == nil {
		return fmt.Errorf("identity cannot be nil")
	}

	// Inject teammate identity into context
	ctx = WithTeammate(ctx, identity)

	if r.runFunc == nil {
		return fmt.Errorf("teammate runner not initialized; this is a system configuration error")
	}
	return r.runFunc(ctx, identity, initialPrompt, r.cfg)
}
