package slash

import (
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/agentprofile"
)

// rewriteAgentFromProfile turns a slash command targeting an agent profile
// (e.g. "/reviewer check pkg/agent") into a natural-language instruction
// that asks the LLM to call spawn_teammate with the profile's defaults and
// the user's prompt.
//
// The output format is intentionally identical to the legacy preprocessor
// rewrite of "@reviewer ..." so downstream agent behaviour is preserved.
func rewriteAgentFromProfile(profile agentprofile.AgentProfile, prompt string) string {
	initialPrompt := defaultAgentPrompt(profile, prompt)
	return fmt.Sprintf(
		"The user explicitly invoked agent profile %q. Call spawn_teammate with name=%q, kind=%q, permission_mode=%q, color=%q, model=%q, fallbacks=%s, context_providers=%q, and initial_prompt=%q. After spawning, summarize the teammate session ID.",
		profile.Name,
		profile.Name,
		defaultString(profile.Kind, "in_process"),
		profile.PermissionMode,
		profile.Color,
		profile.Model,
		formatStringList(profile.Fallbacks),
		strings.Join(profile.ContextProviders, ","),
		initialPrompt,
	)
}

// defaultAgentPrompt picks the prompt to send when invoking an agent
// profile. Resolution order is:
//
//  1. the user-supplied prompt (if non-empty),
//  2. the profile's InitialPrompt,
//  3. the profile's Description,
//  4. a generic "Run agent profile <name>" fallback.
func defaultAgentPrompt(profile agentprofile.AgentProfile, prompt string) string {
	if p := strings.TrimSpace(prompt); p != "" {
		return p
	}
	if p := strings.TrimSpace(profile.InitialPrompt); p != "" {
		return p
	}
	if p := strings.TrimSpace(profile.Description); p != "" {
		return p
	}
	return "Run agent profile " + profile.Name
}

// formatStringList formats a list of strings as a bracketed comma-separated
// quoted list, matching the legacy preprocessor output: ["a", "b"].
func formatStringList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("%q", value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// defaultString returns value when it has any non-whitespace content,
// otherwise it returns fallback.
func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
