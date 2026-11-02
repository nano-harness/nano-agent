package agent

import (
	"github.com/nano-harness/nano-agent/pkg/agentprofile"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/mailbox"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	"github.com/nano-harness/nano-agent/pkg/tools/sendmessage"
	"github.com/nano-harness/nano-agent/pkg/tools/task"
)

// RegisterSwarmTools registers the unified Agent tool and supporting tools.
// For async subagent contexts, additionally registers SendMessage.
func RegisterSwarmTools(registry interfaces.ToolRegistry, cfg *config.Config, mailboxBackend mailbox.Backend, identity *swarm.TeammateIdentity) {
	isTeammate := (identity != nil)

	// The unified Agent tool is registered for non-teammate contexts (lead/parent agents)
	if !isTeammate {
		resolver := agentprofile.NewResolver(cfg.WorkingDir)
		agentTool := NewAgentTool(cfg, resolver)
		if err := registry.Register(agentTool); err != nil {
			logger.Warnf("Failed to register Agent tool: %v", err)
		} else {
			logger.Infof("Registered unified Agent tool")
		}

		// TaskOutput and TaskStop for reading async agent results
		outputTool := task.NewOutputTool()
		if err := registry.Register(outputTool); err != nil {
			logger.Warnf("Failed to register TaskOutput tool: %v", err)
		}
		stopTool := task.NewStopTool()
		if err := registry.Register(stopTool); err != nil {
			logger.Warnf("Failed to register TaskStop tool: %v", err)
		}
	}

	// SendMessage is available to async subagents (for mailbox communication)
	if isTeammate && mailboxBackend != nil {
		sendMsgTool := sendmessage.New(mailboxBackend)
		if err := registry.Register(sendMsgTool); err != nil {
			logger.Warnf("Failed to register send_message tool: %v", err)
		} else {
			logger.Infof("Registered send_message tool for teammate %s", identity.AgentID)
		}
	}
}
