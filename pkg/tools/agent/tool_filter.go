package agent

import (
	"strings"

	"github.com/nano-harness/nano-agent/pkg/agentprofile"
)

// AllAgentDisallowedTools are tools that no subagent may use, regardless of profile.
// This prevents recursive Agent spawning and mode-escape tools.
var AllAgentDisallowedTools = []string{
	"Agent",          // Prevent recursive agent spawning
	"ExitPlanMode",   // Mode escape not available to subagents
	"team_create",    // Legacy, removed
	"spawn_teammate", // Legacy, removed
	"team_list",      // Legacy, removed
}

// AsyncAgentAllowedTools are tools available only to async (background) agents.
// Sync agents do NOT get these.
var AsyncAgentAllowedTools = []string{
	"send_message",
}

// ResolveAgentTools computes the final tool list for a subagent based on:
// 1. Profile's Tools whitelist (or ['*'] for all)
// 2. Profile's DisallowedTools
// 3. Global AllAgentDisallowedTools
// 4. Async-only tools (added only if isAsync)
//
// parentTools is the parent agent's available tool list (used when profile specifies ['*']).
func ResolveAgentTools(profile agentprofile.AgentProfile, parentTools []string, isAsync bool) []string {
	// Determine base tool set
	var baseTools []string
	if len(profile.Tools) == 0 || containsStar(profile.Tools) {
		// Use all parent tools as base
		baseTools = append([]string(nil), parentTools...)
	} else {
		baseTools = append([]string(nil), profile.Tools...)
	}

	// Add MCP tools transparently (mcp__* pass through)
	for _, t := range parentTools {
		if strings.HasPrefix(t, "mcp__") && !contains(baseTools, t) {
			baseTools = append(baseTools, t)
		}
	}

	// Add async-only tools if applicable
	if isAsync {
		for _, t := range AsyncAgentAllowedTools {
			if !contains(baseTools, t) {
				baseTools = append(baseTools, t)
			}
		}
	}

	// Build denylist: global + profile-specific
	denySet := make(map[string]bool)
	for _, t := range AllAgentDisallowedTools {
		denySet[t] = true
	}
	for _, t := range profile.DisallowedTools {
		denySet[t] = true
	}

	// Remove async-only tools from sync agents
	if !isAsync {
		for _, t := range AsyncAgentAllowedTools {
			denySet[t] = true
		}
	}

	// Filter
	var result []string
	for _, t := range baseTools {
		if !denySet[t] {
			result = append(result, t)
		}
	}

	return result
}

func containsStar(tools []string) bool {
	for _, t := range tools {
		if t == "*" {
			return true
		}
	}
	return false
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
