// Package agent provides the core agent logic and execution orchestration
package agent

// AgentType represents the type of built-in agent.
type AgentType string

const (
	AgentTypeExplore AgentType = "explore"
	AgentTypePlan    AgentType = "plan"
	AgentTypeExecute AgentType = "execute"
	AgentTypeVerify  AgentType = "verify"
)

// AgentTypeConfig defines the configuration for a built-in agent type.
type AgentTypeConfig struct {
	Type         AgentType
	SystemPrompt func(workDir string) string
	AllowedTools []string
	DeniedTools  []string
}

// builtinAgentTypes returns the predefined configuration for each agent type.
func builtinAgentTypes() map[AgentType]*AgentTypeConfig {
	return map[AgentType]*AgentTypeConfig{
		AgentTypeExplore: {
			Type: AgentTypeExplore,
			AllowedTools: []string{
				"read_file", "list_directory", "glob", "search_file_content",
				"web_search", "web_fetch",
			},
			DeniedTools: []string{
				"write_file", "edit_file", "delete_file", "run_shell_command",
			},
			SystemPrompt: func(_ string) string {
				return "You are a read-only code exploration agent. You may only read files and search the codebase. Do not modify any files."
			},
		},
		AgentTypePlan: {
			Type: AgentTypePlan,
			AllowedTools: []string{
				"read_file", "list_directory", "glob", "search_file_content",
			},
			DeniedTools: []string{
				"write_file", "edit_file", "delete_file", "run_shell_command",
			},
			SystemPrompt: func(_ string) string {
				return "You are a planning agent. Produce a structured, actionable plan based on the task. Do not modify any files."
			},
		},
		AgentTypeExecute: {
			Type:         AgentTypeExecute,
			AllowedTools: []string{"*"},
			DeniedTools:  []string{},
			SystemPrompt: func(_ string) string {
				return ""
			},
		},
		AgentTypeVerify: {
			Type: AgentTypeVerify,
			AllowedTools: []string{
				"read_file", "list_directory", "glob", "search_file_content", "run_shell_command",
			},
			DeniedTools: []string{
				"write_file", "edit_file", "delete_file",
			},
			SystemPrompt: func(_ string) string {
				return "You are a verification agent. Verify the correctness of the implementation. You may run read-only shell commands but must not modify any project files."
			},
		},
	}
}

// GetAgentTypeConfig returns the configuration for the given agent type.
func GetAgentTypeConfig(t AgentType) *AgentTypeConfig {
	configs := builtinAgentTypes()
	if cfg, ok := configs[t]; ok {
		return cfg
	}
	return configs[AgentTypeExecute]
}
