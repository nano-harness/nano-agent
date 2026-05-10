package agent

import "time"

// SessionState is the explicit lifecycle state for a session.
type SessionState string

const (
	SessionStateActive        SessionState = "active"
	SessionStateIdle          SessionState = "idle"
	SessionStateAwaitingInput SessionState = "awaiting_input"
	SessionStateSuspended     SessionState = "suspended"
	SessionStateTerminated    SessionState = "terminated"
)

var validTransitions = map[SessionState]map[SessionState]bool{
	SessionStateIdle: {
		SessionStateActive:     true,
		SessionStateTerminated: true,
	},
	SessionStateActive: {
		SessionStateIdle:          true,
		SessionStateAwaitingInput: true,
		SessionStateSuspended:     true,
		SessionStateTerminated:    true,
	},
	SessionStateAwaitingInput: {
		SessionStateActive:     true,
		SessionStateTerminated: true,
	},
	SessionStateSuspended: {
		SessionStateIdle:       true,
		SessionStateActive:     true,
		SessionStateTerminated: true,
	},
}

// DefaultAwaitingInputTTL is the default maximum duration for sessions awaiting user input.
const DefaultAwaitingInputTTL = 2 * time.Hour

// CanTransitionTo reports whether a lifecycle state transition is valid.
func (s SessionState) CanTransitionTo(target SessionState) bool {
	if s == "" {
		return target == SessionStateIdle || target == SessionStateActive || target == SessionStateTerminated
	}
	if s == target {
		return true
	}
	targets, ok := validTransitions[s]
	if !ok {
		return false
	}
	return targets[target]
}
