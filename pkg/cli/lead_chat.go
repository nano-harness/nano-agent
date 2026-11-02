package cli

import (
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/daemon"
	nanoruntime "github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/nano-harness/nano-agent/pkg/ui"
	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
	"github.com/spf13/cobra"
)

// NewLeadChatCommand creates a daemon-backed team-lead REPL client.
func NewLeadChatCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lead-chat",
		Short: "Start a daemon-backed team-lead REPL",
		Long: `Start an interactive team-lead REPL backed by the nano daemon.

Each input line is sent over the team-lead WebSocket stream as a lead_input frame.
The client resumes from the last received sequence after transient disconnects.`,
		RunE: runLeadChat,
	}
	cmd.Flags().String("team", "default", "Team name for this lead session")
	cmd.Flags().String("session-id", "", "Team-lead session id to create or resume")
	cmd.Flags().Int64("since-seq", 0, "Resume event streaming after this sequence")
	cmd.Flags().Int("timeout", daemon.DefaultTaskTimeoutSeconds, "Per-turn timeout in seconds")
	return cmd
}

func runLeadChat(cmd *cobra.Command, _ []string) error {
	ctx := signalContext()
	teamName, _ := cmd.Flags().GetString("team")
	sessionID, _ := cmd.Flags().GetString("session-id")
	sinceSeq, _ := cmd.Flags().GetInt64("since-seq")
	uiMode := getUIMode(cmd)

	teamName = strings.TrimSpace(teamName)
	if teamName == "" {
		teamName = "default"
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = nanoruntime.BuildLeadSessionID(teamName, "lead-chat")
	}

	client := createDaemonClient()
	session, err := client.CreateTeamLeadSessionWithOptions(sessionID, teamName, true)
	if err != nil {
		return fmt.Errorf("failed to create team-lead session: %w", err)
	}
	sessionID = session.SessionID

	cfg := ui.Config{ShowBanner: true}
	adapter, err := ui.NewFactory(cfg).Create(uiMode)
	if err != nil {
		return err
	}
	src := eventsource.NewDaemonWS(client, sessionID, teamName, sinceSeq)
	return adapter.Run(ctx, src)
}
