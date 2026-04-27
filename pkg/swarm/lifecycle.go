package swarm

import (
	"context"

	"github.com/nano-harness/nano-agent/pkg/team"
)

type Lifecycle struct {
	teamName string
	agentID  string
	cancel   context.CancelFunc
}

func NewLifecycle(teamName, agentID string, cancel context.CancelFunc) Lifecycle {
	return Lifecycle{teamName: teamName, agentID: agentID, cancel: cancel}
}

func (l Lifecycle) Cancel() {
	if l.cancel != nil {
		l.cancel()
	}
}

func (l Lifecycle) Deactivate() {
	if l.teamName != "" && l.agentID != "" {
		_ = team.SetMemberActive(l.teamName, l.agentID, false)
	}
}

func (l Lifecycle) Finish() {
	l.Deactivate()
	l.Cancel()
}
