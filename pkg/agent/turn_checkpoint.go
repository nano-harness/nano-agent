package agent

import "github.com/nano-harness/nano-agent/pkg/llm"

type TurnCheckpoint struct {
	ContextPackage     TurnContextPackage `json:"context_package"`
	Status             CompletionStatus   `json:"status"`
	CompletionCriteria CompletionCriteria `json:"completion_criteria"`
	Response           string             `json:"response,omitempty"`
	Messages           []llm.Message      `json:"messages,omitempty"`
}

func CaptureTurnCheckpoint(t *Turn) TurnCheckpoint {
	if t == nil {
		return TurnCheckpoint{}
	}
	cp := TurnCheckpoint{
		ContextPackage: NewContextPackager().PackageTurn(t),
		Status:         t.Status,
		Response:       t.Response.String(),
		Messages:       append([]llm.Message(nil), t.Messages...),
	}
	if t.CompletionCriteria != nil {
		cp.CompletionCriteria = *t.CompletionCriteria
	}
	return cp
}

func RestoreTurnCheckpoint(t *Turn, cp TurnCheckpoint) {
	if t == nil {
		return
	}
	t.Messages = append([]llm.Message(nil), cp.Messages...)
	t.Status = cp.Status
	t.Response.Reset()
	t.Response.WriteString(cp.Response)
	if t.CompletionCriteria == nil {
		t.CompletionCriteria = &CompletionCriteria{}
	}
	*t.CompletionCriteria = cp.CompletionCriteria
}
