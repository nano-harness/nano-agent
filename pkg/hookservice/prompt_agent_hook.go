package hookservice

import (
	"context"
	"encoding/json"
	"strings"
	"text/template"
)

// renderTemplate renders a Go text/template with the canonical hook input
// fields exposed as `.Tool`, `.Params`, `.Event`, `.WorkingDir`. Errors fall
// back to the raw template so misformatted user templates do not deadlock.
func renderTemplate(tpl string, in Input) string {
	if !strings.Contains(tpl, "{{") {
		return tpl
	}
	t, err := template.New("hook").Parse(tpl)
	if err != nil {
		return tpl
	}
	var sb strings.Builder
	if err := t.Execute(&sb, struct {
		Tool       string
		Params     map[string]interface{}
		Event      Event
		WorkingDir string
		Input      Input
	}{
		Tool:       in.ToolName,
		Params:     in.Params,
		Event:      in.Event,
		WorkingDir: in.WorkingDir,
		Input:      in,
	}); err != nil {
		return tpl
	}
	return sb.String()
}

func (s *Service) executePromptHook(ctx context.Context, h *Hook, event Event, toolName string, params map[string]interface{}, inputJSON string) (*Decision, error) {
	cfg := h.PromptConfig
	if cfg == nil || strings.TrimSpace(cfg.Prompt) == "" {
		return s.decisionForFailure(h, "prompt hook missing prompt"), nil
	}
	if s.options.LLMDecider == nil {
		return s.decisionForFailure(h, "no LLM decider configured for prompt hook"), nil
	}
	hookInput := Input{
		Event:               event,
		ToolName:            toolName,
		Params:              params,
		WorkingDir:          s.options.WorkingDir,
		LegacyToolInputJSON: inputJSON,
	}
	prompt := renderTemplate(cfg.Prompt, hookInput)
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 512
	}
	resp, err := s.options.LLMDecider.Decide(ctx, cfg.Model, prompt, maxTokens)
	if err != nil {
		return s.decisionForFailure(h, "llm error: "+err.Error()), nil
	}
	trimmed := strings.TrimSpace(resp)
	// Loose JSON: {"ok": true|false, "reason": "..."} takes precedence over the
	// strict Output schema because it is the typical few-shot pattern.
	if strings.HasPrefix(trimmed, "{") {
		var loose struct {
			OK     *bool  `json:"ok"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(trimmed), &loose); err == nil && loose.OK != nil {
			if *loose.OK {
				return &Decision{Action: ActionAllow, Reason: loose.Reason, Rule: h.Name}, nil
			}
			return &Decision{Action: ActionBlock, Reason: loose.Reason, Rule: h.Name}, nil
		}
	}
	if decision, ok := s.decisionFromStructuredOutput(h, resp); ok {
		return decision, nil
	}
	return &Decision{Action: ActionAllow, Reason: "prompt hook " + h.Name + " allowed (no decision in response)"}, nil
}

func (s *Service) executeAgentHook(ctx context.Context, h *Hook, event Event, toolName string, params map[string]interface{}, inputJSON string) (*Decision, error) {
	cfg := h.AgentConfig
	if cfg == nil || strings.TrimSpace(cfg.Agent) == "" {
		return s.decisionForFailure(h, "agent hook missing agent name"), nil
	}
	if s.options.AgentDecider == nil {
		return s.decisionForFailure(h, "no agent decider configured for agent hook"), nil
	}
	hookInput := Input{
		Event:               event,
		ToolName:            toolName,
		Params:              params,
		WorkingDir:          s.options.WorkingDir,
		LegacyToolInputJSON: inputJSON,
	}
	task := cfg.Task
	if strings.TrimSpace(task) == "" {
		task = "Review tool invocation and return JSON {action: allow|confirm|block, reason: ...}"
	}
	task = renderTemplate(task, hookInput)
	resp, err := s.options.AgentDecider.Run(ctx, cfg.Agent, task)
	if err != nil {
		return s.decisionForFailure(h, "agent error: "+err.Error()), nil
	}
	if decision, ok := s.decisionFromStructuredOutput(h, resp); ok {
		return decision, nil
	}
	return &Decision{Action: ActionAllow, Reason: "agent hook " + h.Name + " allowed (no decision)"}, nil
}
