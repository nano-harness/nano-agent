package agent

import (
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/mailbox"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	"github.com/nano-harness/nano-agent/pkg/tools/sendmessage"
	"github.com/nano-harness/nano-agent/pkg/tools/teamcreate"
	"github.com/nano-harness/nano-agent/pkg/tools/teamlist"
	"github.com/nano-harness/nano-agent/pkg/tools/teammate"
)

// RegisterSwarmTools registers swarm collaboration tools based on role (lead vs teammate)
func RegisterSwarmTools(registry interfaces.ToolRegistry, cfg *config.Config, mailboxBackend mailbox.Backend, identity *swarm.TeammateIdentity) {
	if mailboxBackend == nil {
		logger.Debug("No mailbox backend provided, skipping swarm tools registration")
		return
	}

	isTeammate := (identity != nil)

	// Communication tools - available to both lead and teammates
	sendMsgTool := sendmessage.New(mailboxBackend)
	if err := registry.Register(sendMsgTool); err != nil {
		logger.Warnf("Failed to register send_message tool: %v", err)
	} else {
		logger.Infof("Registered swarm tool: %s", sendMsgTool.Name())
	}

	teamListTool := teamlist.New(mailboxBackend)
	if err := registry.Register(teamListTool); err != nil {
		logger.Warnf("Failed to register team_list tool: %v", err)
	} else {
		logger.Infof("Registered swarm tool: %s", teamListTool.Name())
	}

	// Lead-only tools
	if !isTeammate {
		// spawn_teammate
		spawnTool := teammate.NewSpawnTool(cfg)
		if err := registry.Register(spawnTool); err != nil {
			logger.Warnf("Failed to register spawn_teammate tool: %v", err)
		} else {
			logger.Infof("Registered swarm tool: %s (lead-only)", spawnTool.Name())
		}

		// team_create
		createTool := teamcreate.New()
		if err := registry.Register(createTool); err != nil {
			logger.Warnf("Failed to register team_create tool: %v", err)
		} else {
			logger.Infof("Registered swarm tool: %s (lead-only)", createTool.Name())
		}

		logger.Info("Registered lead-only swarm tools")
	} else {
		logger.Infof("Teammate mode: spawn and create tools not registered")
	}
}
