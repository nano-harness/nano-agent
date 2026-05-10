package agent

import (
	"context"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/middleware"
)

const lifecycleHookTimeout = 5 * time.Second

// SessionLifecycleEvent identifies lifecycle events emitted by SessionManager.
type SessionLifecycleEvent string

const (
	SessionLifecycleCreated        SessionLifecycleEvent = "created"
	SessionLifecycleStateChanged   SessionLifecycleEvent = "state_changed"
	SessionLifecycleCheckpoint     SessionLifecycleEvent = "checkpoint"
	SessionLifecycleBeforeCleanup  SessionLifecycleEvent = "before_cleanup"
	SessionLifecycleCleaned        SessionLifecycleEvent = "cleaned"
	SessionLifecycleBeforeShutdown SessionLifecycleEvent = "before_shutdown"
)

// LifecycleHook observes session lifecycle events.
type LifecycleHook interface {
	OnSessionLifecycle(ctx context.Context, sessionID string, ev SessionLifecycleEvent, meta map[string]interface{}) error
}

// hookEngineLifecycleAdapter bridges SessionManager lifecycle events to hookservice.
type hookEngineLifecycleAdapter struct {
	hookEngine *middleware.HookEngine
}

// OnSessionLifecycle implements LifecycleHook interface.
func (h *hookEngineLifecycleAdapter) OnSessionLifecycle(ctx context.Context, sessionID string, ev SessionLifecycleEvent, meta map[string]interface{}) error {
	if h.hookEngine == nil {
		return nil
	}

	// Map session lifecycle events to hookservice events
	var hookEvent middleware.HookEvent
	switch ev {
	case SessionLifecycleCreated:
		hookEvent = middleware.HookSessionStart
	case SessionLifecycleBeforeShutdown, SessionLifecycleBeforeCleanup:
		hookEvent = middleware.HookSessionEnd
	default:
		// Only dispatch SessionStart and SessionEnd for now
		return nil
	}

	params := map[string]interface{}{
		"session_id": sessionID,
		"event":      string(ev),
	}
	if meta != nil {
		for k, v := range meta {
			params[k] = v
		}
	}

	_, err := h.hookEngine.Execute(ctx, hookEvent, "session_lifecycle", params)
	if err != nil {
		logger.Warnf("SessionLifecycle hook execution error for event %s: %v", ev, err)
	}
	return err
}

// AddLifecycleHook registers a hook for subsequent session lifecycle events.
func (sm *SessionManager) AddLifecycleHook(h LifecycleHook) {
	if sm == nil || h == nil {
		return
	}
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.lifecycleHooks = append(sm.lifecycleHooks, h)
}

// SetHookEngine registers a hookservice engine to receive session lifecycle events.
func (sm *SessionManager) SetHookEngine(engine *middleware.HookEngine) {
	if sm == nil || engine == nil {
		return
	}
	adapter := &hookEngineLifecycleAdapter{hookEngine: engine}
	sm.AddLifecycleHook(adapter)
}

func (sm *SessionManager) emitLifecycle(ctx context.Context, sessionID string, ev SessionLifecycleEvent, meta map[string]interface{}) {
	if sm == nil {
		return
	}
	sm.mutex.RLock()
	hooks := append([]LifecycleHook(nil), sm.lifecycleHooks...)
	sm.mutex.RUnlock()
	for _, hook := range hooks {
		hookCtx, cancel := context.WithTimeout(ctx, lifecycleHookTimeout)
		if err := hook.OnSessionLifecycle(hookCtx, sessionID, ev, meta); err != nil {
			logger.Warnf("Session lifecycle hook failed: session=%s event=%s error=%v", sessionID, ev, err)
		}
		cancel()
	}
}

// BroadcastShutdown notifies hooks that session manager shutdown/drain has started.
func (sm *SessionManager) BroadcastShutdown(ctx context.Context) {
	if sm == nil {
		return
	}
	sm.mutex.RLock()
	ids := make([]string, 0, len(sm.sessions))
	for id := range sm.sessions {
		ids = append(ids, id)
	}
	sm.mutex.RUnlock()
	for _, id := range ids {
		sm.emitLifecycle(ctx, id, SessionLifecycleBeforeShutdown, map[string]interface{}{"reason": "shutdown_drain"})
	}
}
