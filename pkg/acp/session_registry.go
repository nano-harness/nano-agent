package acp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// SessionRegistry manages the mapping between ACP session IDs and nano session IDs
type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*ACPSession
	cancels  map[string]context.CancelFunc
}

// NewSessionRegistry creates a new session registry
func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{
		sessions: make(map[string]*ACPSession),
		cancels:  make(map[string]context.CancelFunc),
	}
}

// Create creates a new ACP session and returns it
func (r *SessionRegistry) Create(nanoSessionID, cwd string, env map[string]string, caps SessionCapabilities, clientCaps ClientCapabilities, fsMode FSMode) (*ACPSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	acpSessionID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("generate session ID: %w", err)
	}

	session := &ACPSession{
		ACPSessionID:  acpSessionID,
		NanoSessionID: nanoSessionID,
		CWD:           cwd,
		Env:           env,
		ClientCaps:    caps,
		ClientInfo:    clientCaps,
		FSMode:        fsMode,
		CreatedAt:     time.Now(),
		LastActiveAt:  time.Now(),
	}

	r.sessions[acpSessionID] = session
	logger.Infof("ACP: Created session %s -> nano session %s", acpSessionID, nanoSessionID)
	return session, nil
}

// Get retrieves an ACP session by ID
func (r *SessionRegistry) Get(acpSessionID string) (*ACPSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sess, ok := r.sessions[acpSessionID]
	if ok {
		// Update last active time (Note: this is not thread-safe for the session itself,
		// but acceptable for this use case)
		sess.LastActiveAt = time.Now()
	}
	return sess, ok
}

// Delete removes an ACP session
func (r *SessionRegistry) Delete(acpSessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, ok := r.sessions[acpSessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", acpSessionID)
	}

	// Cancel the context if exists
	if cancel, ok := r.cancels[acpSessionID]; ok {
		cancel()
		delete(r.cancels, acpSessionID)
	}

	delete(r.sessions, acpSessionID)
	logger.Infof("ACP: Deleted session %s", acpSessionID)
	return nil
}

// List returns all active sessions
func (r *SessionRegistry) List() []*ACPSession {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sessions := make([]*ACPSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// SetCancel stores a cancel function for a session
func (r *SessionRegistry) SetCancel(acpSessionID string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels[acpSessionID] = cancel
}

// GetCancel retrieves the cancel function for a session
func (r *SessionRegistry) GetCancel(acpSessionID string) context.CancelFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cancels[acpSessionID]
}

// generateSessionID generates a unique session ID
func generateSessionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "acp-sess-" + hex.EncodeToString(bytes)[:16], nil
}
