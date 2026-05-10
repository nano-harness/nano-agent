package preprocessor

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/nano-harness/nano-agent/pkg/agentprofile"
)

// RewriteAgentMention rewrites @agent-name requests into spawn_teammate guidance
// when the named profile exists under .nano/agents.
func RewriteAgentMention(input, workingDir string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "@") {
		return input, false
	}
	name, rest := splitAgentMention(trimmed)
	if name == "" {
		return input, false
	}
	profile, ok := agentprofile.NewManager(workingDir).Find(name)
	if !ok {
		return input, false
	}
	initialPrompt := strings.TrimSpace(rest)
	if initialPrompt == "" {
		initialPrompt = strings.TrimSpace(profile.InitialPrompt)
	}
	if initialPrompt == "" {
		initialPrompt = profile.Description
	}
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
	), true
}

// AgentMentionStep rewrites explicit @agent-name invocations.
func AgentMentionStep() Step {
	return StepFunc{
		StepName: "agent_mention",
		Fn: func(_ context.Context, req *Request) error {
			if rewritten, ok := RewriteAgentMention(req.UserInput, req.WorkingDir); ok {
				req.UserInput = rewritten
				req.SetMetadata("agent.profile", "true")
			}
			return nil
		},
	}
}

func splitAgentMention(input string) (string, string) {
	if !strings.HasPrefix(input, "@") {
		return "", ""
	}
	rest := strings.TrimPrefix(input, "@")
	idx := 0
	for idx < len(rest) {
		r := rune(rest[idx])
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			break
		}
		idx++
	}
	if idx == 0 {
		return "", ""
	}
	return rest[:idx], strings.TrimSpace(rest[idx:])
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

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
