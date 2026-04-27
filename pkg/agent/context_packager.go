package agent

import "github.com/nano-harness/nano-agent/pkg/llm"

type TurnContextPackage struct {
	TurnID         string        `json:"turn_id"`
	SessionID      string        `json:"session_id,omitempty"`
	UserInput      string        `json:"user_input,omitempty"`
	WorkingDir     string        `json:"working_dir,omitempty"`
	SystemPrompt   string        `json:"system_prompt,omitempty"`
	Messages       []llm.Message `json:"messages,omitempty"`
	Iteration      int           `json:"iteration"`
	ToolResultSize int           `json:"tool_result_size"`
}

type ContextPackager struct{}

func NewContextPackager() ContextPackager {
	return ContextPackager{}
}

func (ContextPackager) PackageTurn(t *Turn) TurnContextPackage {
	if t == nil {
		return TurnContextPackage{}
	}
	messages := append([]llm.Message(nil), t.Messages...)
	iteration := 0
	if t.CompletionCriteria != nil {
		iteration = t.CompletionCriteria.CurrentIteration
	}
	return TurnContextPackage{
		TurnID:         t.ID,
		SessionID:      t.SessionID,
		UserInput:      t.UserInput,
		WorkingDir:     t.WorkingDir,
		SystemPrompt:   t.systemPrompt,
		Messages:       messages,
		Iteration:      iteration,
		ToolResultSize: len(t.ToolResults),
	}
}
