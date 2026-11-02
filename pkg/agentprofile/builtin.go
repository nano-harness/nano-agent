package agentprofile

// builtinRegistry holds all built-in agent profiles indexed by name.
var builtinRegistry map[string]AgentProfile

func init() {
	builtinRegistry = map[string]AgentProfile{
		"general-purpose": {
			Name:        "general-purpose",
			Description: "Full-capability agent for complex multi-step tasks requiring the complete toolset and high-quality reasoning.",
			Tools:       []string{"*"},
			InitialPrompt: `You are a general-purpose subagent with full capabilities.
Execute the task given to you thoroughly and return a complete result.
You have access to all tools including file operations, shell commands, and web access.`,
			Kind:   "in_process",
			Source: "builtin",
		},
		"explore": {
			Name:        "explore",
			Description: "Fast agent for codebase exploration and research. Read-only operations, no file modifications.",
			Tools:       []string{"read_file", "run_shell_command", "list_files", "search_files"},
			InitialPrompt: `You are an exploration subagent optimized for codebase research and investigation.
Your job is to find information, understand code structure, and report findings.
You have read-only access. Do NOT modify any files.
Be thorough in your search and return comprehensive findings.`,
			Kind:   "in_process",
			Source: "builtin",
		},
		"plan": {
			Name:        "plan",
			Description: "Agent for design proposals and step-by-step planning. Reads code to understand context, produces structured plans.",
			Tools:       []string{"read_file", "run_shell_command", "list_files", "search_files"},
			InitialPrompt: `You are a planning subagent focused on design and architecture.
Analyze the codebase and produce a detailed, structured plan.
Include specific file paths, function names, and implementation steps.
Do NOT modify any files — only read and analyze.`,
			Kind:   "in_process",
			Source: "builtin",
		},
		"verify": {
			Name:        "verify",
			Description: "Agent for testing and validation tasks. Can run tests, linters, and verify correctness.",
			Tools:       []string{"read_file", "run_shell_command", "list_files", "search_files"},
			InitialPrompt: `You are a verification subagent focused on testing and validation.
Run tests, check builds, verify correctness, and report results.
Be thorough — check edge cases and report any issues found.
Do NOT modify source files — only verify and report.`,
			Kind:   "in_process",
			Source: "builtin",
		},
	}
}

// GetBuiltin returns a built-in profile by name, if it exists.
func GetBuiltin(name string) (AgentProfile, bool) {
	p, ok := builtinRegistry[name]
	return p, ok
}

// ListBuiltins returns all built-in profiles.
func ListBuiltins() []AgentProfile {
	out := make([]AgentProfile, 0, len(builtinRegistry))
	for _, p := range builtinRegistry {
		out = append(out, p)
	}
	return out
}

// BuiltinNames returns the names of all built-in profiles.
func BuiltinNames() []string {
	names := make([]string, 0, len(builtinRegistry))
	for name := range builtinRegistry {
		names = append(names, name)
	}
	return names
}
