package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
	nanoruntime "github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	"github.com/spf13/cobra"
)

// NewTeammateCommand creates the teammate command for subprocess execution
func NewTeammateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "teammate",
		Short:  "Run as a teammate agent (internal use)",
		Hidden: true, // Hidden from help - only called by spawn_teammate tool
		Long: `Run as a teammate agent in subprocess mode.

This command is typically invoked automatically by the spawn_teammate tool
and should not be called directly by users. It requires specific flags
to identify the teammate's role and initial task.`,
		RunE: runTeammate,
	}

	cmd.Flags().String("team", "", "Team name")
	cmd.Flags().String("name", "", "Teammate name")
	cmd.Flags().String("session", "", "Parent session ID")
	cmd.Flags().String("initial-prompt-file", "", "Path to file containing initial prompt")

	// Mark required flags
	_ = cmd.MarkFlagRequired("team")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("session")
	_ = cmd.MarkFlagRequired("initial-prompt-file")

	return cmd
}

func runTeammate(cmd *cobra.Command, args []string) error {
	// Get required parameters
	team, _ := cmd.Flags().GetString("team")
	name, _ := cmd.Flags().GetString("name")
	sessionID, _ := cmd.Flags().GetString("session")
	promptFile, _ := cmd.Flags().GetString("initial-prompt-file")

	// Validate parameters
	if team == "" || name == "" || sessionID == "" || promptFile == "" {
		return fmt.Errorf("missing required flags: team, name, session, and initial-prompt-file are all required")
	}

	// Read initial prompt from file
	promptBytes, err := os.ReadFile(promptFile)
	if err != nil {
		return fmt.Errorf("failed to read initial prompt file: %w", err)
	}
	initialPrompt := string(promptBytes)

	// Load configuration
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not initialized")
	}

	// Create teammate identity
	identity := &swarm.TeammateIdentity{
		TeamName:        team,
		AgentName:       name,
		AgentID:         fmt.Sprintf("%s@%s", name, team),
		ParentSessionID: sessionID,
	}

	logger.Infof("Starting teammate '%s' in team '%s'", name, team)

	// Setup signal handling
	ctx := signalContext()

	// Create approval handler (auto-approve for teammates)
	approvalHandler := func(info *agent.ToolCallInfo) bool {
		return true
	}

	// Build teammate engine
	eng, err := engine.NewTeammateEngine(cfg, approvalHandler, identity)
	if err != nil {
		return fmt.Errorf("failed to create teammate engine: %w", err)
	}
	defer eng.Shutdown()

	// Run the teammate with initial prompt
	err = runTeammateLoop(ctx, eng, identity, initialPrompt)
	if err != nil {
		return fmt.Errorf("teammate execution failed: %w", err)
	}

	logger.Infof("Teammate '%s' completed successfully", name)
	return nil
}

// runTeammateLoop runs the teammate's main loop
func runTeammateLoop(ctx context.Context, eng *engine.Engine, identity *swarm.TeammateIdentity, initialPrompt string) error {
	// Generate session ID for this teammate
	teammateSessionID := nanoruntime.BuildTeammateSessionID(identity.TeamName, identity.AgentName)
	ctx = swarm.WithTeammate(ctx, identity)
	session := eng.Agent.GetSessionManager().GetOrCreateSession(teammateSessionID)
	session.SetMetadata("swarm", agent.SessionMetadata{
		TeamName:   identity.TeamName,
		AgentName:  identity.AgentName,
		IsTeammate: true,
	})

	// Process initial prompt
	logger.Infof("Teammate '%s' processing initial prompt", identity.AgentName)

	err := eng.Agent.ProcessStreamWithMultimodalAndSession(
		ctx,
		teammateSessionID,
		initialPrompt,
		nil, // no images
		func(e event.StreamEvent) {
			// Event handler - could log or stream events
			// For now, events flow through the agent's normal processing
		},
	)

	if err != nil {
		return fmt.Errorf("failed to process initial prompt: %w", err)
	}

	// After completing initial task, the stop hooks will automatically
	// send idle_notification to team-lead via mailbox

	logger.Infof("Teammate '%s' finished initial task", identity.AgentName)
	return nil
}
