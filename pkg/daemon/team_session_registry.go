package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// ErrTeamLeadSessionNotFound indicates a requested team-lead session does not exist.
var ErrTeamLeadSessionNotFound = errors.New("team-lead session not found")

// TeamLeadRegistry manages all team-lead sessions
type TeamLeadRegistry struct {
	sessions map[string]*TeamLeadSession // sessionID -> session
	mu       sync.RWMutex

	// Cleanup configuration
	idleTimeout    time.Duration
	stopChan       chan struct{}
	cleanupDone    chan struct{}
	stopOnce       sync.Once
	sessionManager *agent.SessionManager
}

// NewTeamLeadRegistry creates a new registry for team-lead sessions
func NewTeamLeadRegistry(idleTimeout time.Duration) *TeamLeadRegistry {
	if idleTimeout == 0 {
		idleTimeout = 30 * time.Minute // Default 30 minutes
	}

	registry := &TeamLeadRegistry{
		sessions:    make(map[string]*TeamLeadSession),
		idleTimeout: idleTimeout,
		stopChan:    make(chan struct{}),
		cleanupDone: make(chan struct{}),
	}

	// Start cleanup goroutine
	go registry.cleanupLoop()

	return registry
}

// SetSessionManager links daemon team sessions to the agent session lifecycle.
func (r *TeamLeadRegistry) SetSessionManager(sm *agent.SessionManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionManager = sm
}

// OnSessionLifecycle implements agent.LifecycleHook.
func (r *TeamLeadRegistry) OnSessionLifecycle(ctx context.Context, sessionID string, ev agent.SessionLifecycleEvent, meta map[string]interface{}) error {
	if ev != agent.SessionLifecycleBeforeCleanup && ev != agent.SessionLifecycleBeforeShutdown {
		return nil
	}
	if _, exists := r.Get(sessionID); !exists {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- r.Remove(sessionID) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if errors.Is(err, ErrTeamLeadSessionNotFound) {
			return nil
		}
		return err
	}
}

// GetOrCreate gets an existing session or creates a new one
func (r *TeamLeadRegistry) GetOrCreate(sessionID, teamName string, cfg *config.Config) (*TeamLeadSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if session already exists
	if session, exists := r.sessions[sessionID]; exists {
		session.UpdateActivity()
		return session, nil
	}

	// Create new session
	session, err := NewTeamLeadSession(sessionID, teamName, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create team-lead session: %w", err)
	}

	r.sessions[sessionID] = session
	logger.Infof("Registered new team-lead session: %s (team: %s)", sessionID, teamName)
	return session, nil
}

// Get retrieves a session by ID
func (r *TeamLeadRegistry) Get(sessionID string) (*TeamLeadSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	session, exists := r.sessions[sessionID]
	if exists {
		session.UpdateActivity()
	}
	return session, exists
}

// Remove removes a session from the registry and shuts it down
func (r *TeamLeadRegistry) Remove(sessionID string) error {
	r.mu.Lock()
	session, exists := r.sessions[sessionID]
	if !exists {
		r.mu.Unlock()
		return fmt.Errorf("%w: session %s", ErrTeamLeadSessionNotFound, sessionID)
	}
	delete(r.sessions, sessionID)
	r.mu.Unlock()

	// Shutdown the session
	if err := session.Shutdown(); err != nil {
		logger.Warnf("Error shutting down session %s: %v", sessionID, err)
		return err
	}

	logger.Infof("Removed team-lead session: %s", sessionID)
	return nil
}

// List returns all active sessions
func (r *TeamLeadRegistry) List() []*TeamLeadSession {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sessions := make([]*TeamLeadSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// Count returns the number of active sessions
func (r *TeamLeadRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}

// Shutdown gracefully shuts down all sessions and stops the registry
func (r *TeamLeadRegistry) Shutdown() error {
	return r.ShutdownWithTimeout(0)
}

// ShutdownWithTimeout gracefully shuts down all sessions, bounding per-session drain by timeout.
func (r *TeamLeadRegistry) ShutdownWithTimeout(timeout time.Duration) error {
	logger.Info("Shutting down team-lead registry")

	// Stop cleanup goroutine
	r.stopOnce.Do(func() { close(r.stopChan) })
	<-r.cleanupDone

	// Shutdown all sessions
	r.mu.Lock()
	sessions := make([]*TeamLeadSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	r.sessions = make(map[string]*TeamLeadSession)
	r.mu.Unlock()

	// Shutdown each session
	deadline := time.Time{}
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for _, session := range sessions {
		var err error
		if deadline.IsZero() {
			err = session.Shutdown()
		} else {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				session.CancelActiveTasks()
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), remaining)
			err = session.ShutdownCtx(ctx)
			cancel()
		}
		if err != nil {
			logger.Warnf("Error shutting down session %s: %v", session.ID, err)
		}
	}

	logger.Infof("Team-lead registry shutdown complete (%d sessions closed)", len(sessions))
	return nil
}

// cleanupLoop periodically removes idle sessions
func (r *TeamLeadRegistry) cleanupLoop() {
	defer close(r.cleanupDone)

	ticker := time.NewTicker(5 * time.Minute) // Check every 5 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.cleanupIdleSessions()
		case <-r.stopChan:
			return
		}
	}
}

// cleanupIdleSessions removes sessions that have been idle for too long
func (r *TeamLeadRegistry) cleanupIdleSessions() {
	r.mu.Lock()
	now := time.Now()
	toRemove := make([]string, 0)
	sm := r.sessionManager

	for sessionID, session := range r.sessions {
		session.mu.RLock()
		lastActive := session.LastActiveAt
		session.mu.RUnlock()

		expired := now.Sub(lastActive) > r.idleTimeout
		if sm != nil {
			if agentSession, ok := sm.GetSession(sessionID); ok && agentSession != nil {
				state := agentSession.GetState()
				switch state {
				case agent.SessionStateAwaitingInput:
					expired, _ = agentSession.IsExpiredByState(now, r.idleTimeout, agent.DefaultAwaitingInputTTL)
				case agent.SessionStateSuspended:
					expired = false
				}
			}
		}
		if expired {
			toRemove = append(toRemove, sessionID)
			logger.Infof("Session %s idle for %v, scheduling for removal", sessionID, now.Sub(lastActive))
		}
	}
	r.mu.Unlock()

	// Remove idle sessions
	for _, sessionID := range toRemove {
		if err := r.Remove(sessionID); err != nil {
			logger.Warnf("Failed to remove idle session %s: %v", sessionID, err)
		}
	}

	if len(toRemove) > 0 {
		logger.Infof("Cleaned up %d idle team-lead sessions", len(toRemove))
	}
}
