package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// Session represents an isolated conversation session for a user/request.
// Each session maintains its own conversation history, enabling concurrent
// requests from different users without interference.
type Session struct {
	ID                  string        `json:"id"`
	ConversationHistory []llm.Message `json:"history"`
	CreatedAt           time.Time     `json:"created_at"`
	LastActiveAt        time.Time     `json:"last_active_at"`
	TotalTokens         int           `json:"totalTokens"`
	Duration            float64       `json:"duration"`
	Metadata            map[string]interface{}
	mutex               sync.RWMutex
}

// NewSession creates a new session with a unique ID.
func NewSession() *Session {
	return &Session{
		ID:                  generateSessionID(),
		ConversationHistory: []llm.Message{},
		CreatedAt:           time.Now(),
		LastActiveAt:        time.Now(),
		Metadata:            make(map[string]interface{}),
	}
}

// NewSessionWithID creates a new session with a specified ID.
func NewSessionWithID(id string) *Session {
	return &Session{
		ID:                  id,
		ConversationHistory: []llm.Message{},
		CreatedAt:           time.Now(),
		LastActiveAt:        time.Now(),
		Metadata:            make(map[string]interface{}),
	}
}

// generateSessionID generates a unique session ID.
func generateSessionID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if random generation fails
		return fmt.Sprintf("session_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("session_%s", hex.EncodeToString(bytes))
}

// GetConversationHistory returns a copy of the conversation history.
func (s *Session) GetConversationHistory() []llm.Message {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// Return a copy to prevent external modifications
	history := make([]llm.Message, len(s.ConversationHistory))
	copy(history, s.ConversationHistory)
	return history
}

func (s *Session) GetMetadataCopy() map[string]interface{} { //nolint:revive
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if s.Metadata == nil {
		return map[string]interface{}{}
	}

	copied := make(map[string]interface{}, len(s.Metadata))
	for k, v := range s.Metadata {
		copied[k] = v
	}
	return copied
}

// SetConversationHistory sets the conversation history.
func (s *Session) SetConversationHistory(history []llm.Message) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.ConversationHistory = history
	s.LastActiveAt = time.Now()
}

// AppendMessage appends a message to the conversation history.
func (s *Session) AppendMessage(msg llm.Message) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.ConversationHistory = append(s.ConversationHistory, msg)
	s.LastActiveAt = time.Now()
}

func (s *Session) UpdateStats(totalTokens int, duration float64) { //nolint:revive
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.TotalTokens = totalTokens
	s.Duration += duration
	s.LastActiveAt = time.Now()
}

// cleanupMessageSequenceUnsafe removes incomplete tool call sequences.
// Caller must hold the mutex.
func (s *Session) cleanupMessageSequenceUnsafe() { //nolint:unused
	if len(s.ConversationHistory) == 0 {
		return
	}

	var cleanedHistory []llm.Message

	for i, msg := range s.ConversationHistory {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Check if this assistant message with tool calls has following tool messages
			hasFollowingToolMessage := false

			for j := i + 1; j < len(s.ConversationHistory); j++ {
				nextMsg := s.ConversationHistory[j]
				if nextMsg.Role == "tool" {
					hasFollowingToolMessage = true
					break
				}
				if nextMsg.Role == "assistant" {
					break
				}
			}

			if hasFollowingToolMessage {
				// Valid sequence, keep the message
				cleanedHistory = append(cleanedHistory, msg)
			} else {
				// Incomplete tool call sequence, remove the tool calls
				cleanedMsg := msg
				cleanedMsg.ToolCalls = nil
				cleanedHistory = append(cleanedHistory, cleanedMsg)
				logger.Warnf("Session %s: Removed incomplete tool calls from message at index %d", s.ID, i)
			}
		} else {
			// Regular message, keep it
			cleanedHistory = append(cleanedHistory, msg)
		}
	}

	s.ConversationHistory = cleanedHistory
}

// Touch updates the last active time.
func (s *Session) Touch() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.LastActiveAt = time.Now()
}

// IsExpired checks if the session has expired based on the given TTL.
func (s *Session) IsExpired(ttl time.Duration) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return time.Since(s.LastActiveAt) > ttl
}

// SetMetadata sets a metadata value for the session.
func (s *Session) SetMetadata(key string, value interface{}) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.Metadata[key] = value
}

// GetMetadata gets a metadata value from the session.
func (s *Session) GetMetadata(key string) (interface{}, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	val, ok := s.Metadata[key]
	return val, ok
}

// SessionManager manages multiple sessions with automatic cleanup.
type SessionManager struct {
	sessions   map[string]*Session
	mutex      sync.RWMutex
	sessionTTL time.Duration
	cleanupCh  chan struct{}
	wg         sync.WaitGroup
	storage    SessionStorage
}

// SessionManagerOption is a functional option for SessionManager.
type SessionManagerOption func(*SessionManager)

// WithSessionTTL sets the session TTL.
func WithSessionTTL(ttl time.Duration) SessionManagerOption {
	return func(sm *SessionManager) {
		sm.sessionTTL = ttl
	}
}

// WithSessionStorage sets the session storage backend.
func WithSessionStorage(storage SessionStorage) SessionManagerOption {
	return func(sm *SessionManager) {
		sm.storage = storage
	}
}

func (sm *SessionManager) SetStorage(storage SessionStorage) { //nolint:revive
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.storage = storage
}

// NewSessionManager creates a new session manager.
func NewSessionManager(opts ...SessionManagerOption) *SessionManager {
	sm := &SessionManager{
		sessions:   make(map[string]*Session),
		sessionTTL: 30 * time.Minute, // Default TTL
		cleanupCh:  make(chan struct{}),
	}

	for _, opt := range opts {
		opt(sm)
	}

	// Start cleanup goroutine
	sm.wg.Add(1)
	go sm.cleanupLoop()

	return sm
}

// GetOrCreateSession gets an existing session or creates a new one.
func (sm *SessionManager) GetOrCreateSession(sessionID string) *Session {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if sessionID != "" {
		if session, exists := sm.sessions[sessionID]; exists {
			session.Touch()
			return session
		}

		// Try to load from storage if enabled
		if sm.storage != nil {
			if session, err := sm.storage.LoadSession(sessionID); err == nil && session != nil {
				session.Touch()
				sm.sessions[session.ID] = session
				logger.Infof("Loaded session from storage: %s", session.ID)
				return session
			}
		}
	}

	// Create new session
	var session *Session
	if sessionID != "" {
		session = NewSessionWithID(sessionID)
	} else {
		session = NewSession()
	}
	sm.sessions[session.ID] = session
	logger.Infof("Created new session: %s", session.ID)
	return session
}

// GetSession gets an existing session by ID.
func (sm *SessionManager) GetSession(sessionID string) (*Session, bool) {
	sm.mutex.RLock()
	// Check memory first
	session, exists := sm.sessions[sessionID]
	sm.mutex.RUnlock()

	if exists {
		session.Touch()
		return session, true
	}

	// Double check with lock for storage load
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	// Re-check memory
	if session, exists := sm.sessions[sessionID]; exists {
		session.Touch()
		return session, true
	}

	// Try storage
	if sm.storage != nil {
		if session, err := sm.storage.LoadSession(sessionID); err == nil && session != nil {
			session.Touch()
			sm.sessions[session.ID] = session
			logger.Infof("Loaded session from storage: %s", session.ID)
			return session, true
		}
	}

	return nil, false
}

// SaveSession persists a specific session to storage
func (sm *SessionManager) SaveSession(sessionID string) error {
	if sm.storage == nil {
		return nil
	}

	sm.mutex.RLock()
	session, exists := sm.sessions[sessionID]
	sm.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("session %s not found in memory", sessionID)
	}

	return sm.storage.SaveSession(session)
}

// ListStoredSessions returns a list of sessions available in storage
func (sm *SessionManager) ListStoredSessions() ([]string, error) {
	if sm.storage == nil {
		return []string{}, nil
	}
	return sm.storage.ListSessions()
}

func (sm *SessionManager) ListStoredSessionInfos() ([]SessionInfo, error) { //nolint:revive
	if sm.storage == nil {
		return []SessionInfo{}, nil
	}
	return sm.storage.ListSessionInfos()
}

// DeleteSession deletes a session.
func (sm *SessionManager) DeleteSession(sessionID string) (bool, error) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	deleted := false
	if _, exists := sm.sessions[sessionID]; exists {
		delete(sm.sessions, sessionID)
		logger.Infof("Deleted session from memory: %s", sessionID)
		deleted = true
	}

	if sm.storage != nil {
		if err := sm.storage.DeleteSession(sessionID); err != nil {
			return deleted, err
		}
		// If storage.DeleteSession succeeds (even if not found), we consider it handled.
		// We can't easily know if it was actually in storage without checking first,
		// but checking is expensive.
		// Since we don't know if it was in storage, we can't set deleted=true based solely on success unless we trust the caller knows it exists.
		// However, for idempotency, if it's gone, it's gone.
		// Let's assume if we invoked DeleteSession and it didn't error, we are good.
		// But to match previous behavior:
		// "deleted" meant "found and deleted".
		// If we want to detect "not found", we might need to change logic.
		// But for now, let's just propagate the error.
	}

	return deleted, nil
}

// ListSessions returns a list of all active session IDs in memory.
func (sm *SessionManager) ListSessions() []*Session {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	sessions := make([]*Session, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// GetSessionCount returns the number of active sessions.
func (sm *SessionManager) GetSessionCount() int {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return len(sm.sessions)
}

// cleanupLoop periodically cleans up expired sessions.
func (sm *SessionManager) cleanupLoop() {
	defer sm.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sm.cleanupExpiredSessions()
		case <-sm.cleanupCh:
			return
		}
	}
}

// cleanupExpiredSessions removes expired sessions.
func (sm *SessionManager) cleanupExpiredSessions() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	expired := make([]string, 0)
	for id, session := range sm.sessions {
		if session.IsExpired(sm.sessionTTL) {
			expired = append(expired, id)
		}
	}

	for _, id := range expired {
		// Save to storage before removing from memory if storage is enabled
		// This effectively "archives" the session
		if sm.storage != nil {
			if err := sm.storage.SaveSession(sm.sessions[id]); err != nil {
				logger.Errorf("Failed to save expired session %s to storage: %v", id, err)
			}
		}

		delete(sm.sessions, id)
		logger.Infof("Cleaned up expired session from memory: %s", id)
	}

	if len(expired) > 0 {
		logger.Infof("Cleaned up %d expired sessions, %d remaining", len(expired), len(sm.sessions))
	}
}

// Shutdown stops the session manager and cleans up resources.
func (sm *SessionManager) Shutdown() {
	close(sm.cleanupCh)
	sm.wg.Wait()

	// Save all sessions on shutdown
	if sm.storage != nil {
		sm.mutex.RLock()
		defer sm.mutex.RUnlock()
		for _, session := range sm.sessions {
			_ = sm.storage.SaveSession(session)
		}
	}

	logger.Info("Session manager shutdown completed")
}
