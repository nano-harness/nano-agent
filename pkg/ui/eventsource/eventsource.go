package eventsource

import (
	"context"

	"github.com/nano-harness/nano-agent/pkg/event"
)

type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateReconnecting
	StateClosed
)

func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateReconnecting:
		return "reconnecting"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

type Outbound struct {
	Kind     string
	Text     string
	Approval *ApprovalDecision
	Control  string
}

type ApprovalDecision struct {
	CallID string
	Allow  bool
	Always bool
}

type Inbound struct {
	Event       *event.StreamEvent
	State       *ConnectionState
	ResumedFrom int64
	Notice      string
}

type EventSource interface {
	Start(ctx context.Context) error
	Inbound() <-chan Inbound
	Send(Outbound) error
	State() ConnectionState
	Close() error
	Describe() string
}
