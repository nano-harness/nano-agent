// Package swarm: hook dispatcher for subagent / teammate lifecycle events.
package swarm

import (
	"context"

	"github.com/nano-harness/nano-agent/pkg/middleware"
)

// SwarmHookDispatcher centralises swarm-level hook firing so callers do not
// need to hand-craft payloads. A nil receiver is a no-op.
type SwarmHookDispatcher struct {
	hookEngine *middleware.HookEngine
}

// NewSwarmHookDispatcher returns a dispatcher that fires hooks via the given
// middleware HookEngine. Returns nil when engine is nil.
func NewSwarmHookDispatcher(engine *middleware.HookEngine) *SwarmHookDispatcher {
	if engine == nil {
		return nil
	}
	return &SwarmHookDispatcher{hookEngine: engine}
}

// DispatchSubagentStart fires HookSubagentStart with the given identity.
func (d *SwarmHookDispatcher) DispatchSubagentStart(ctx context.Context, identity *TeammateIdentity) error {
	if d == nil || d.hookEngine == nil || identity == nil {
		return nil
	}
	_, err := d.hookEngine.Execute(ctx, middleware.HookSubagentStart, "subagent", identityParams(identity, ""))
	return err
}

// DispatchSubagentStop fires HookSubagentStop with the final status string
// (typically "success" or "failed").
func (d *SwarmHookDispatcher) DispatchSubagentStop(ctx context.Context, identity *TeammateIdentity, status string) error {
	if d == nil || d.hookEngine == nil || identity == nil {
		return nil
	}
	_, err := d.hookEngine.Execute(ctx, middleware.HookSubagentStop, "subagent", identityParams(identity, status))
	return err
}

// DispatchTeammateIdle fires HookTeammateIdle when a teammate is waiting for
// input or has nothing to do.
func (d *SwarmHookDispatcher) DispatchTeammateIdle(ctx context.Context, identity *TeammateIdentity) error {
	if d == nil || d.hookEngine == nil || identity == nil {
		return nil
	}
	_, err := d.hookEngine.Execute(ctx, middleware.HookTeammateIdle, "teammate", identityParams(identity, ""))
	return err
}

func identityParams(identity *TeammateIdentity, status string) map[string]interface{} {
	params := map[string]interface{}{
		"agent_id":          identity.AgentID,
		"agent_name":        identity.AgentName,
		"team_name":         identity.TeamName,
		"permission_mode":   identity.PermissionMode,
		"model":             identity.Model,
		"parent_session_id": identity.ParentSessionID,
	}
	if status != "" {
		params["status"] = status
	}
	return params
}
