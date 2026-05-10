// Package swarm provides teammate identity and context management for multi-agent swarm systems.
//
// This package implements the Go equivalent of Claude Code's teammateContext.ts,
// using context.Context instead of AsyncLocalStorage for propagating teammate identity.
package swarm

import "context"

// TeammateIdentity contains the identity and metadata for a teammate agent in a swarm.
// This is passed through context.Context to identify the agent and manage its lifecycle.
type TeammateIdentity struct {
	// AgentID is the fully qualified agent identifier (e.g., "researcher@my-team")
	AgentID string

	// AgentName is the short name of the agent (e.g., "researcher")
	AgentName string

	// TeamName is the name of the team this agent belongs to
	TeamName string

	// Color is an optional color code for UI display (e.g., "#FF5733")
	Color string

	// PermissionMode is the teammate's independent permission mode.
	PermissionMode string

	// AllowedTools optionally constrains this teammate's tool surface.
	AllowedTools []string

	// Model optionally overrides the teammate's model independently of the parent.
	Model string

	// Fallbacks optionally overrides the teammate's model fallback chain.
	Fallbacks []string

	// ContextProviders optionally constrains teammate context sources.
	ContextProviders []string

	// PlanModeRequired indicates whether this agent must enter plan mode before implementation
	PlanModeRequired bool

	// ParentSessionID is the session ID of the parent/lead agent
	ParentSessionID string

	// AbortCtx and AbortCancel allow the parent to cancel the teammate's execution
	AbortCtx    context.Context
	AbortCancel context.CancelFunc
}

// ctxKey is a private type for storing TeammateIdentity in context
type ctxKey struct{}
type teamLeadCtxKey struct{}

// TeamLeadContext contains lead session metadata propagated to tools.
type TeamLeadContext struct {
	TeamName  string
	SessionID string
}

// WithTeammate returns a new context with the given TeammateIdentity attached.
// This is used when spawning a teammate agent to propagate its identity through the call chain.
func WithTeammate(parent context.Context, id *TeammateIdentity) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithValue(parent, ctxKey{}, id)
}

// WithTeamLead returns a new context with lead team/session metadata attached.
func WithTeamLead(parent context.Context, teamName, sessionID string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithValue(parent, teamLeadCtxKey{}, TeamLeadContext{
		TeamName:  teamName,
		SessionID: sessionID,
	})
}

// TeamLeadFromContext retrieves lead team/session metadata from context.
func TeamLeadFromContext(ctx context.Context) (TeamLeadContext, bool) {
	if ctx == nil {
		return TeamLeadContext{}, false
	}
	info, ok := ctx.Value(teamLeadCtxKey{}).(TeamLeadContext)
	return info, ok
}

// FromContext retrieves the TeammateIdentity from the context.
// Returns (nil, false) if no teammate identity is present (indicating a lead agent or standalone agent).
func FromContext(ctx context.Context) (*TeammateIdentity, bool) {
	if ctx == nil {
		return nil, false
	}
	id, ok := ctx.Value(ctxKey{}).(*TeammateIdentity)
	return id, ok
}

// IsTeammate returns true if the context contains a teammate identity.
// This is useful for quickly checking if the current agent is a teammate or a lead.
func IsTeammate(ctx context.Context) bool {
	_, ok := FromContext(ctx)
	return ok
}

// IsTeamLead returns true if the context does NOT contain a teammate identity.
// This is the inverse of IsTeammate and indicates the agent is a team lead or standalone agent.
func IsTeamLead(ctx context.Context) bool {
	return !IsTeammate(ctx)
}

// GetAgentName returns the agent name from context.
// For teammates, it returns TeammateIdentity.AgentName.
// For team leads or standalone agents, it returns "team-lead".
func GetAgentName(ctx context.Context) string {
	if id, ok := FromContext(ctx); ok {
		return id.AgentName
	}
	return "team-lead"
}
