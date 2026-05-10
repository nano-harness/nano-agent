package interfaces

// TurnContext holds lightweight per-turn metadata used by tools.
// Kept in pkg/interfaces to avoid cycles between agent/tools/system.
type TurnContext struct {
	SessionID string
}

// TurnContextKey is the context key used to attach TurnContext.
type TurnContextKey struct{}
