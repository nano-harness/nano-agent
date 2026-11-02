package swarm

import (
	"context"
	"fmt"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
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
	TeamName         string // Name of the team
	Name             string // Short name of the teammate
	Color            string // Optional UI color
	InitialPrompt    string // Initial prompt to send to the teammate
	PermissionMode   string // Permission mode (e.g., "auto", "ask")
	AllowedTools     []string
	Model            string
	Fallbacks        []string
	ContextProviders []string
	MaxRuntimeSec    int
	Sandbox          team.SandboxPolicy
	Runner           Runner               // Optional custom runner (defaults to DefaultRunner)
	HookDispatcher   *SwarmHookDispatcher // Optional dispatcher for SubagentStart/Stop/Idle hooks
}

// SpawnInProcess spawns a teammate in the same process as a goroutine
func SpawnInProcess(ctx context.Context, opts SpawnOptions) (*SpawnHandle, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	identity := opts.newIdentity()

	var teammateCtx context.Context
	var cancel context.CancelFunc
	if opts.MaxRuntimeSec > 0 {
		teammateCtx, cancel = context.WithTimeout(context.Background(), time.Duration(opts.MaxRuntimeSec)*time.Second)
	} else {
		// Create independent context for the teammate (not cancelled when parent turn ends)
		teammateCtx, cancel = context.WithCancel(context.Background())
	}
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
		// Default to the same runner wiring used by the spawn_teammate tool.
		// The runner itself is configured via swarm.SetDefaultRunFunc (wired in pkg/cli).
		runner = NewDefaultRunner(config.Get())
	}

	// Launch teammate goroutine
	go func() {
		var err error
		dispatcher := opts.HookDispatcher
		// M1F: SubagentStart fires before the teammate begins running.
		_ = dispatcher.DispatchSubagentStart(teammateCtx, identity.teammate)
		defer func() {
			lifecycle.Finish()
			// M1F: SubagentStop fires regardless of success/failure with status.
			status := "success"
			if err != nil {
				status = "failed"
			}
			_ = dispatcher.DispatchSubagentStop(teammateCtx, identity.teammate, status)
			if err != nil {
				done <- err
			}
			close(done)
		}()

		err = runner.Run(teammateCtx, identity.teammate, opts.InitialPrompt)
		if err != nil {
			return
		}

	}()

	return &SpawnHandle{
		AgentID:   identity.agentID,
		SessionID: identity.sessionID,
		Done:      done,
	}, nil
}
