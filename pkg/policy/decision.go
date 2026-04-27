package policy

import "fmt"

type Action string

const (
	ActionContinue   Action = "continue"
	ActionTerminate  Action = "terminate"
	ActionRequestLLM Action = "request_llm"
	ActionExecute    Action = "execute"
	ActionComplete   Action = "complete"
)

type Decision struct {
	Action   Action                 `json:"action"`
	Reason   string                 `json:"reason,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

func NewDecision(action Action, reason string) Decision {
	return Decision{Action: action, Reason: reason}
}

func (d Decision) WithMetadata(key string, value interface{}) Decision {
	if d.Metadata == nil {
		d.Metadata = make(map[string]interface{})
	}
	d.Metadata[key] = value
	return d
}

func (d Decision) String() string {
	if d.Reason == "" {
		return string(d.Action)
	}
	return fmt.Sprintf("%s: %s", d.Action, d.Reason)
}
