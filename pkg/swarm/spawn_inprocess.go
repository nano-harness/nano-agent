package swarm

import (
	"context"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/team"
)

// SpawnHandle represents a spawned teammate agent
type SpawnHandle struct {
	AgentID   string       // Fully qualified agent ID (e.g., "researcher@my-team")
	SessionID string       // Session ID for this teammate
	Done      <-chan error // Channel that closes when teammate exits
}

// SpawnOptions configures teammate spawn behavior
type SpawnOptions struct {
	TeamName       string // Name of the team
	Name           string // Short name of the teammate
	Color          string // Optional UI color
	InitialPrompt  string // Initial prompt to send to the teammate
	PermissionMode string // Permission mode (e.g., "auto", "ask")
	Runner         Runner // Optional custom runner (defaults to DefaultRunner)
}

// SpawnInProcess spawns a teammate in the same process as a goroutine
func SpawnInProcess(ctx context.Context, opts SpawnOptions) (*SpawnHandle, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	identity := opts.newIdentity()

	// Create independent context for the teammate (not cancelled when parent turn ends)
	teammateCtx, cancel := context.WithCancel(context.Background())
	identity.teammate.AbortCtx = teammateCtx
	identity.teammate.AbortCancel = cancel
	lifecycle := NewLifecycle(opts.TeamName, identity.agentID, cancel)

	// Add member to team
	member := opts.newTeamMember(identity.agentID, identity.sessionID, team.KindInProcess)
	if err := team.AddMember(opts.TeamName, member); err != nil {
		lifecycle.Cancel()
		return nil, fmt.Errorf("failed to add team member: %w", err)
	}

	// Create done channel
	done := make(chan error, 1)

	// Get or create runner
	runner := opts.Runner
	if runner == nil {
		// TODO: Get config from context or global config
		// For now, return error
		lifecycle.Cancel()
		return nil, fmt.Errorf("runner not provided and default runner creation not yet implemented")
	}

	// Launch teammate goroutine
	go func() {
		defer close(done)
		defer lifecycle.Finish()

		err := runner.Run(teammateCtx, identity.teammate, opts.InitialPrompt)
		if err != nil {
			done <- err
		}

	}()

	return &SpawnHandle{
		AgentID:   identity.agentID,
		SessionID: identity.sessionID,
		Done:      done,
	}, nil
}
