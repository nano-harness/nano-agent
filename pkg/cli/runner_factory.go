package cli

import (
	"context"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/event"
	nanoruntime "github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/nano-harness/nano-agent/pkg/swarm"
)

func init() {
	swarm.SetDefaultRunFunc(runDefaultTeammate)
}

func runDefaultTeammate(ctx context.Context, identity *swarm.TeammateIdentity, initialPrompt string, cfg *config.Config) error {
	// Match the hidden teammate CLI path: the lead-authorized spawn controls when
	// this runner starts, while teammate mode withholds lead-only swarm tools.
	approvalHandler := func(info *agent.ToolCallInfo) bool {
		return true
	}
	eng, err := engine.NewTeammateEngine(cfg, approvalHandler, identity)
	if err != nil {
		return err
	}
	defer eng.Shutdown()

	ctx = swarm.WithTeammate(ctx, identity)
	sessionID := nanoruntime.BuildTeammateSessionID(identity.TeamName, identity.AgentName)
	session := eng.Agent.GetSessionManager().GetOrCreateSession(sessionID)
	session.SetMetadata("swarm", agent.SessionMetadata{
		TeamName:   identity.TeamName,
		AgentName:  identity.AgentName,
		IsTeammate: true,
	})
	return eng.Agent.ProcessStreamWithMultimodalAndSession(ctx, sessionID, initialPrompt, nil, func(event.StreamEvent) {})
}
