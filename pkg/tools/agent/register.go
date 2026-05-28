package agent

import (
	"github.com/nano-harness/nano-agent/pkg/agentprofile"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// RegisterAgentTools registers the unified Agent tool with the given registry.
func RegisterAgentTools(registry interfaces.ToolRegistry, cfg *config.Config) {
	resolver := agentprofile.NewResolver(cfg.WorkingDir)
	agentTool := NewAgentTool(cfg, resolver)
	if err := registry.Register(agentTool); err != nil {
		logger.Warnf("Failed to register Agent tool: %v", err)
	} else {
		logger.Infof("Registered unified Agent tool")
	}
}

// GetAgentToolNames returns the names of all agent tools registered by this package.
func GetAgentToolNames() []string {
	return []string{
		"Agent",
		"TaskOutput",
		"TaskStop",
		"send_message",
	}
}
