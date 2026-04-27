package agent

import (
	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// RegisterAgentTools registers agent-related tools with the given registry.
func RegisterAgentTools(registry interfaces.ToolRegistry, cfg *config.Config, mainAgent *agent.Agent) {
	if mainAgent == nil {
		return
	}

	// Register main_agent tool
	mainAgentTool := NewMainAgentTool(cfg, mainAgent)
	if err := registry.Register(mainAgentTool); err != nil {
		logger.Warnf("Failed to register main agent tool: %v", err)
	} else {
		logger.Infof("Registered agent tool: %s", mainAgentTool.Name())
	}

	// Register send_message tool (only for sub-agents)
	if cfg.IsSubAgent && mainAgent.IsSubAgent() {
		sendMessageTool := NewSendMessageTool(mainAgent)
		if err := registry.Register(sendMessageTool); err != nil {
			logger.Warnf("Failed to register send_message tool: %v", err)
		} else {
			logger.Infof("Registered agent tool: %s", sendMessageTool.Name())
		}
	}
}

// GetAgentToolNames returns the names of all agent tools registered by this package.
func GetAgentToolNames() []string {
	return []string{
		"main_agent",
		"spawn_teammate",
		"team_create",
		"team_list",
		"send_message",
	}
}

// RegisterMainAgentTool registers only the main agent tool.
func RegisterMainAgentTool(registry interfaces.ToolRegistry, cfg *config.Config, mainAgent *agent.Agent) error {
	if mainAgent == nil {
		return nil
	}

	mainAgentTool := NewMainAgentTool(cfg, mainAgent)
	if err := registry.Register(mainAgentTool); err != nil {
		logger.Warnf("Failed to register main agent tool: %v", err)
		return err
	}

	logger.Infof("Registered main agent tool: %s", mainAgentTool.Name())
	return nil
}
