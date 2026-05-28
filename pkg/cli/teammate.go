package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/agentprofile"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
	nanoruntime "github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	"github.com/spf13/cobra"
)

// NewTeammateCommand creates the teammate command for subprocess execution.
//
// **Internal use only.** This subcommand is invoked by the `spawn_teammate`
// tool when running teammates in subprocess mode (`docker_lifecycle=session`
// or `subprocess` swarm backend). End users should never invoke
// `nano teammate` directly; use `spawn_teammate` from a lead agent instead.
//
// The command is hidden from `nano --help` output and accepts a stable
// argument set that is treated as a private contract between the cli layer
// and pkg/swarm.
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
	cmd.Flags().Int("max-runtime-sec", 0, "Maximum teammate runtime in seconds (0 = unlimited)")
	cmd.Flags().String("model", "", "Optional teammate-specific model override")
	cmd.Flags().String("context-providers", "", "Comma-separated teammate context provider allowlist (memory,skills,openspec)")

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
	maxRuntimeSec, _ := cmd.Flags().GetInt("max-runtime-sec")
	model, _ := cmd.Flags().GetString("model")
	contextProvidersFlag, _ := cmd.Flags().GetString("context-providers")
	contextProviders := splitComma(contextProvidersFlag)
	fallbacks := teammateProfileFallbacks(name, cfgWorkingDir(config.Get()))

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
	if model != "" {
		childCfg := cfg.DeepCopy()
		childCfg.Model = model
		cfg = childCfg
	}

	// Create teammate identity
	identity := &swarm.TeammateIdentity{
		TeamName:         team,
		AgentName:        name,
		AgentID:          fmt.Sprintf("%s@%s", name, team),
		ParentSessionID:  sessionID,
		Model:            model,
		Fallbacks:        fallbacks,
		ContextProviders: contextProviders,
	}
	cfg = configForTeammate(cfg, identity)

	// Resolve permission mode using unified resolver
	res, warns := ResolvePermission(cfg, PermissionResolveOpts{
		EnvHintEnabled: true,
	})
	LogPermissionResolution("teammate", res, warns)

	logger.Infof("Starting teammate '%s' in team '%s'", name, team)
	logger.Infof("Teammate entry auto-approves all tools by default; to restrict, set PermissionMode=plan/default + ConfirmPolicy=block")

	// Setup signal handling
	ctx := signalContext()
	if maxRuntimeSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(maxRuntimeSec)*time.Second)
		defer cancel()
	}

	// Build teammate engine
	eng, err := engine.NewTeammateEngine(cfg, identity)
	if err != nil {
		return fmt.Errorf("failed to create teammate engine: %w", err)
	}
	// Auto-approve all tools for teammate autonomy.
	// To tighten control, configure permission_mode and confirm_policy in config.
	eng.Agent.SetApprovalHandlerV2(func(*agent.ToolCallInfo) agent.ApprovalDecision {
		return agent.ApprovalApproveOnce
	})
	defer eng.Shutdown()

	// Run the teammate with initial prompt
	err = runTeammateLoop(ctx, eng, identity, initialPrompt)
	if err != nil {
		return fmt.Errorf("teammate execution failed: %w", err)
	}

	logger.Infof("Teammate '%s' completed successfully", name)
	return nil
}

func splitComma(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func cfgWorkingDir(cfg *config.Config) string {
	if cfg != nil && cfg.WorkingDir != "" {
		return cfg.WorkingDir
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

func teammateProfileFallbacks(name, cwd string) []string {
	if name == "" || cwd == "" {
		return nil
	}
	profile, ok := agentprofile.NewManager(cwd).Find(name)
	if !ok || len(profile.Fallbacks) == 0 {
		return nil
	}
	return append([]string(nil), profile.Fallbacks...)
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
