package workspace

import (
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// RegisterWorkspaceTools registers all workspace-related tools with the given registry
func RegisterWorkspaceTools(registry interfaces.ToolRegistry, workingDir string, config map[string]interface{}, _ interface{}) {
	// Create and register workspace manager tool
	workspaceManager := &ManagerTool{
		workingDir: workingDir,
		config:     config,
	}
	if err := registry.Register(workspaceManager); err != nil {
		logger.Warnf("Failed to register workspace manager tool: %v", err)
	}

	// Create and register Git manager tool (enhanced version as default)
	gitManager := NewGitManagerTool(workingDir, config, nil)
	if err := registry.Register(gitManager); err != nil {
		logger.Warnf("Failed to register git manager tool: %v", err)
	}

	// Create and register OSS manager tool
	ossManager := NewOSSManagerTool(workingDir, config)
	if err := registry.Register(ossManager); err != nil {
		logger.Warnf("Failed to register OSS manager tool: %v", err)
	}

	// Create and register engineering tools
	engineeringTools := &EngineeringToolsTool{
		workingDir: workingDir,
		config:     config,
	}
	if err := registry.Register(engineeringTools); err != nil {
		logger.Warnf("Failed to register engineering tools: %v", err)
	}

	logger.Infof("Registered %d workspace tools", len(GetWorkspaceToolNames()))
}

// GetWorkspaceToolNames returns the names of all workspace tools
func GetWorkspaceToolNames() []string {
	return []string{
		"workspace_manager",
		"git_manager",
		"oss_manager",
		"engineering_tools",
	}
}
