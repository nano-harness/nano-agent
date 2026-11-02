package swarm

import (
	"context"
	"sync"

	"github.com/nano-harness/nano-agent/pkg/team"
)

type Lifecycle struct {
	teamName string
	agentID  string
	cancel   context.CancelFunc
}

func NewLifecycle(teamName, agentID string, cancel context.CancelFunc) Lifecycle {
	registerLifecycle(agentID, cancel)
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
	unregisterLifecycle(l.agentID)
}

// Global lifecycle registry for agent cancellation
var (
	lifecycleMu       sync.RWMutex
	lifecycleRegistry = make(map[string]context.CancelFunc)
)

func registerLifecycle(agentID string, cancel context.CancelFunc) {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	lifecycleRegistry[agentID] = cancel
}

func unregisterLifecycle(agentID string) {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	delete(lifecycleRegistry, agentID)
}

// CancelByAgentID cancels a running agent by its ID. Returns true if found and cancelled.
func CancelByAgentID(agentID string) bool {
	lifecycleMu.RLock()
	cancel, ok := lifecycleRegistry[agentID]
	lifecycleMu.RUnlock()
	if ok && cancel != nil {
		cancel()
		unregisterLifecycle(agentID)
		return true
	}
	return false
}
