package workspace

import (
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// RegisterWorkspaceTools registers all workspace-related tools with the given registry
func RegisterWorkspaceTools(registry interfaces.ToolRegistry, workingDir string, config map[string]interface{}, _ interface{}) {
	// Create and register OSS manager tool
	ossManager := NewOSSManagerTool(workingDir, config)
	if err := registry.Register(ossManager); err != nil {
		logger.Warnf("Failed to register OSS manager tool: %v", err)
	}

	logger.Infof("Registered %d workspace tools", len(GetWorkspaceToolNames()))
}

// GetWorkspaceToolNames returns the names of all workspace tools
func GetWorkspaceToolNames() []string {
	return []string{
		"oss_manager",
	}
}
