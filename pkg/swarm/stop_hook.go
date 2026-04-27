package swarm

import (
	"context"
	"encoding/json"

	"github.com/nano-harness/nano-agent/pkg/mailbox"
)

// IdleNotification is the body of an idle_notification message
type IdleNotification struct {
	AgentName  string `json:"agent_name"`
	SessionID  string `json:"session_id"`
	LastTurnID string `json:"last_turn_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// AgentWithHooks is the minimal interface needed for registering stop hooks
// This avoids import cycles with the agent package
type AgentWithHooks interface {
	RegisterStopHook(hook func(ctx context.Context, reason string))
	CurrentTurnID() string
}

// RegisterIdleHook registers a stop hook that sends idle_notification to the team lead
func RegisterIdleHook(ag AgentWithHooks, identity *TeammateIdentity, mb mailbox.Mailbox) {
	if ag == nil || identity == nil || mb == nil {
		return
	}

	ag.RegisterStopHook(func(ctx context.Context, reason string) {
		// Prepare idle notification body
		body := IdleNotification{
			AgentName:  identity.AgentName,
			SessionID:  identity.ParentSessionID,
			LastTurnID: ag.CurrentTurnID(),
			Reason:     reason,
		}

		// Convert to map for mailbox message
		bodyMap := make(map[string]interface{})
		data, err := json.Marshal(body)
		if err != nil {
			return
		}
		if err := json.Unmarshal(data, &bodyMap); err != nil {
			return
		}

		// Create message to team lead
		msg := mailbox.Message{
			From:  identity.AgentName,
			To:    "team-lead",
			Topic: "idle_notification",
			Body:  bodyMap,
		}

		// Send to team lead's mailbox
		// Note: This sends to the teammate's own mailbox, but addressed to team-lead
		// The mailbox backend should route this to team-lead's inbox
		_ = mb.Send(ctx, msg)
	})
}
