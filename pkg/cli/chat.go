package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/logger"
	nanoruntime "github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	"github.com/nano-harness/nano-agent/pkg/ui"
	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
	"github.com/spf13/cobra"
)

// NewChatCommand creates the chat command for team-lead REPL
func NewChatCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start a long-running team-lead REPL",
		Long: `Start an interactive team-lead session with mailbox support.

This command launches a REPL (Read-Eval-Print Loop) where the agent acts as a team-lead,
capable of spawning teammates and coordinating multi-agent tasks. Messages from teammates
are automatically injected at the start of each turn.

Example:
  nano chat --team alpha`,
		RunE: runChat,
	}

	cmd.Flags().String("team", "default", "Team name for this lead session")
	cmd.Flags().Bool("daemon", false, "Use daemon-backed EventSource")
	cmd.Flags().String("session-id", "", "Session id to create or resume")
	cmd.Flags().Int64("since-seq", 0, "Resume daemon event streaming after this sequence")
	return cmd
}

func runChat(cmd *cobra.Command, args []string) error {
	ctx := signalContext()
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not initialized")
	}

	teamName, _ := cmd.Flags().GetString("team")
	teamName = strings.TrimSpace(teamName)
	if teamName == "" {
		teamName = "default"
	}
	sessionID, _ := cmd.Flags().GetString("session-id")
	if strings.TrimSpace(sessionID) == "" {
		sessionID = nanoruntime.BuildLeadSessionID(teamName, "chat")
	}
	uiMode := getUIMode(cmd)
	useDaemon, _ := cmd.Flags().GetBool("daemon")
	sinceSeq, _ := cmd.Flags().GetInt64("since-seq")

	logger.Infof("Starting team-lead REPL for team '%s'", teamName)

	var src eventsource.EventSource
	if useDaemon {
		client := createDaemonClient()
		session, err := client.CreateTeamLeadSessionWithOptions(sessionID, teamName, true)
		if err != nil {
			return fmt.Errorf("failed to create team-lead session: %w", err)
		}
		src = eventsource.NewDaemonWS(client, session.SessionID, teamName, sinceSeq)
	} else {
		approvalHandler := func(*agent.ToolCallInfo) bool { return false }
		eng, err := engine.NewLeadEngine(cfg, approvalHandler, teamName)
		if err != nil {
			return fmt.Errorf("failed to create lead engine: %w", err)
		}
		defer func() { _ = eng.Shutdown() }()
		session := eng.Agent.GetSessionManager().GetOrCreateSession(sessionID)
		session.SetMetadata("swarm", agent.SessionMetadata{
			TeamName:   teamName,
			AgentName:  "team-lead",
			IsTeammate: false,
		})
		ctx = swarm.WithTeamLead(ctx, teamName, sessionID)
		src = eventsource.NewInProcess(eng, sessionID, eng.Agent.GetPermissionManager())
	}
	adapter, err := ui.NewFactory(ui.Config{APIBaseURL: cfg.BaseURL, ShowBanner: true}).Create(uiMode)
	if err != nil {
		return err
	}
	return adapter.Run(ctx, src)
}

// signalContext returns a context that is cancelled on SIGINT or SIGTERM
func signalContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("Received termination signal")
		cancel()
	}()

	return ctx
}
