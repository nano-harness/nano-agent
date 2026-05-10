package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// SessionMetadata contains structured metadata about a session
type SessionMetadata struct {
	TeamName   string `json:"teamName,omitempty"`
	AgentName  string `json:"agentName,omitempty"` // teammate name; lead is "team-lead"
	IsTeammate bool   `json:"isTeammate,omitempty"`
}

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
	State               SessionState `json:"state"`
	StateChangedAt      time.Time    `json:"state_changed_at"`
	LastPersistedSeq    int64        `json:"last_persisted_seq"`
	LastCompactionSeq   int64        `json:"last_compaction_seq"`
	transcript          *TranscriptWriter
	ralphContext        *RalphContext
	mutex               sync.RWMutex
}

// NewSession creates a new session with a unique ID.
func NewSession() *Session {
	s := &Session{
		ID:                  generateSessionID(),
		ConversationHistory: []llm.Message{},
		CreatedAt:           time.Now(),
		LastActiveAt:        time.Now(),
		Metadata:            make(map[string]interface{}),
		State:               SessionStateIdle,
		StateChangedAt:      time.Now(),
	}
	s.initRuntimeState()
	return s
}

// NewSessionWithID creates a new session with a specified ID.
func NewSessionWithID(id string) *Session {
	s := &Session{
		ID:                  id,
		ConversationHistory: []llm.Message{},
		CreatedAt:           time.Now(),
		LastActiveAt:        time.Now(),
		Metadata:            make(map[string]interface{}),
		State:               SessionStateIdle,
		StateChangedAt:      time.Now(),
	}
	s.initRuntimeState()
	return s
}

func (s *Session) initRuntimeState() {
	if s == nil {
		return
	}
	s.ralphContext = NewRalphContext(nil)
	if tw, err := NewTranscriptWriter(s.ID); err == nil {
		s.transcript = tw
	} else {
		logger.Warnf("Failed to initialize transcript for session %s: %v", s.ID, err)
	}
}

func (s *Session) Transcript() *TranscriptWriter {
	if s == nil {
		return nil
	}
	return s.transcript
}

func (s *Session) TranscriptPath() string {
	if s == nil || s.transcript == nil {
		return ""
	}
	return s.transcript.Path()
}

func (s *Session) RalphContext() *RalphContext {
	if s == nil {
		return NewRalphContext(nil)
	}
	if s.ralphContext == nil {
		s.ralphContext = NewRalphContext(nil)
	}
	return s.ralphContext
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

// ClearMetadata atomically resets metadata to an empty map and zeroes
// the session-level statistics. Used by ResetSession to fully restore
// a session to "as-new" state while preserving its ID and CreatedAt.
func (s *Session) ClearMetadata() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.Metadata = make(map[string]interface{})
	s.TotalTokens = 0
	s.Duration = 0
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
func (s *Session) cleanupMessageSequenceUnsafe() int {
	if len(s.ConversationHistory) == 0 {
		return 0
	}

	var cleanedHistory []llm.Message
	removedCount := 0
	knownToolCalls := make(map[string]struct{})

	for i, msg := range s.ConversationHistory {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			currentToolCalls := make(map[string]struct{}, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				if call.ID != "" {
					currentToolCalls[call.ID] = struct{}{}
				}
			}
			// Check if this assistant message with tool calls has following tool messages
			hasFollowingToolMessage := false

			for j := i + 1; j < len(s.ConversationHistory); j++ {
				nextMsg := s.ConversationHistory[j]
				if nextMsg.Role == "tool" {
					_, hasFollowingToolMessage = currentToolCalls[nextMsg.ToolCallID]
				}
				if hasFollowingToolMessage {
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
				removedCount += len(msg.ToolCalls)
				logger.Warnf("Session %s: Removed incomplete tool calls from message at index %d", s.ID, i)
			}
			for callID := range currentToolCalls {
				knownToolCalls[callID] = struct{}{}
			}
		} else if msg.Role == "tool" && msg.ToolCallID != "" {
			if _, ok := knownToolCalls[msg.ToolCallID]; ok {
				cleanedHistory = append(cleanedHistory, msg)
			} else {
				removedCount++
				logger.Warnf("Session %s: Removed orphan tool result for call %s", s.ID, msg.ToolCallID)
			}
		} else {
			// Regular message, keep it
			cleanedHistory = append(cleanedHistory, msg)
		}
	}

	s.ConversationHistory = cleanedHistory
	return removedCount
}

// SanitizeMessageSequence removes orphan assistant tool calls and orphan tool results.
func (s *Session) SanitizeMessageSequence() (removedCount int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.cleanupMessageSequenceUnsafe()
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

// TransitionState transitions a session through the explicit lifecycle state machine.
func (s *Session) TransitionState(target SessionState, reason string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.State == "" {
		s.State = SessionStateIdle
	}
	if !s.State.CanTransitionTo(target) {
		return fmt.Errorf("invalid session state transition: %s -> %s", s.State, target)
	}
	s.State = target
	s.StateChangedAt = time.Now()
	s.LastActiveAt = s.StateChangedAt
	if reason != "" {
		if s.Metadata == nil {
			s.Metadata = make(map[string]interface{})
		}
		s.Metadata["state_reason"] = reason
	}
	return nil
}

// GetState returns the explicit session lifecycle state.
func (s *Session) GetState() SessionState {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if s.State == "" {
		return SessionStateIdle
	}
	return s.State
}

// NextSeq atomically advances and returns the next session event sequence.
func (s *Session) NextSeq() int64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.LastPersistedSeq++
	return s.LastPersistedSeq
}

// IsExpiredByState checks expiration according to the explicit lifecycle state.
func (s *Session) IsExpiredByState(now time.Time, idleTTL, awaitingTTL time.Duration) (bool, string) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	state := s.State
	if state == "" {
		state = SessionStateIdle
	}
	reference := s.LastActiveAt
	switch state {
	case SessionStateIdle:
		if now.Sub(reference) > idleTTL {
			return true, "idle_ttl"
		}
	case SessionStateAwaitingInput:
		if !s.StateChangedAt.IsZero() {
			reference = s.StateChangedAt
		}
		if now.Sub(reference) > awaitingTTL {
			return true, "awaiting_input_timeout"
		}
	case SessionStateTerminated:
		return true, "user_terminate"
	}
	return false, ""
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
	sessions          map[string]*Session
	mutex             sync.RWMutex
	sessionTTL        time.Duration
	awaitingInputTTL  time.Duration
	cleanupCh         chan struct{}
	wg                sync.WaitGroup
	storage           SessionStorage
	backgroundCancels map[string][]context.CancelFunc // per-session background goroutine cancellation, protected by mutex
	loadInProgress    map[string]chan struct{}
	lifecycleHooks    []LifecycleHook
	metrics           *SessionMetrics
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

// GetStorage returns the current session storage backend.
func (sm *SessionManager) GetStorage() SessionStorage {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.storage
}

// NewSessionManager creates a new session manager.
func NewSessionManager(opts ...SessionManagerOption) *SessionManager {
	sm := &SessionManager{
		sessions:          make(map[string]*Session),
		sessionTTL:        30 * time.Minute, // Default TTL
		awaitingInputTTL:  DefaultAwaitingInputTTL,
		cleanupCh:         make(chan struct{}),
		backgroundCancels: make(map[string][]context.CancelFunc),
		loadInProgress:    make(map[string]chan struct{}),
		metrics:           NewSessionMetrics(),
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
	if sessionID != "" {
		if session, exists := sm.GetSession(sessionID); exists {
			return session
		}
	}

	sm.mutex.Lock()
	if sessionID != "" {
		if session, exists := sm.sessions[sessionID]; exists {
			sm.mutex.Unlock()
			session.Touch()
			return session
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
	if sm.metrics != nil {
		sm.metrics.RecordStateChange("", session.GetState())
	}
	sm.mutex.Unlock()
	sm.emitLifecycle(context.Background(), session.ID, SessionLifecycleCreated, map[string]interface{}{"state": string(session.GetState())})
	logger.Infof("Created new session: %s", session.ID)
	return session
}

// GetSession gets an existing session by ID.
func (sm *SessionManager) GetSession(sessionID string) (*Session, bool) {
	if sessionID == "" {
		return nil, false
	}
	sm.mutex.Lock()
	if session, exists := sm.sessions[sessionID]; exists {
		sm.mutex.Unlock()
		session.Touch()
		return session, true
	}
	if waitCh, loading := sm.loadInProgress[sessionID]; loading {
		sm.mutex.Unlock()
		<-waitCh
		sm.mutex.RLock()
		session, exists := sm.sessions[sessionID]
		sm.mutex.RUnlock()
		if exists {
			session.Touch()
		}
		return session, exists
	}
	waitCh := make(chan struct{})
	sm.loadInProgress[sessionID] = waitCh
	storage := sm.storage
	sm.mutex.Unlock()

	defer func() {
		sm.mutex.Lock()
		delete(sm.loadInProgress, sessionID)
		close(waitCh)
		sm.mutex.Unlock()
	}()

	if storage == nil {
		return nil, false
	}

	session, err := storage.LoadSession(sessionID)
	if err != nil || session == nil {
		return nil, false
	}
	session.Touch()
	if session.State == "" {
		session.State = SessionStateIdle
	}
	if session.StateChangedAt.IsZero() {
		session.StateChangedAt = session.LastActiveAt
	}
	session.initRuntimeState()
	sm.mutex.Lock()
	if existing, exists := sm.sessions[sessionID]; exists {
		sm.mutex.Unlock()
		existing.Touch()
		return existing, true
	}
	sm.sessions[session.ID] = session
	if sm.metrics != nil {
		sm.metrics.RecordStateChange("", session.GetState())
	}
	sm.mutex.Unlock()
	logger.Infof("Loaded session from storage: %s", session.ID)
	return session, true
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

	if removed := session.SanitizeMessageSequence(); removed > 0 {
		logger.Infof("Sanitized %d orphan tool message item(s) before saving session %s", removed, sessionID)
	}
	err := sm.storage.SaveSession(session)
	if err == nil && sm.metrics != nil {
		lag := session.LastPersistedSeq - session.LastCompactionSeq
		if lag < 0 {
			lag = 0
		}
		sm.metrics.RecordSeqLag(lag)
	}
	return err
}

// TransitionSessionState transitions a managed session and emits lifecycle/metrics events.
func (sm *SessionManager) TransitionSessionState(sessionID string, target SessionState, reason string) error {
	session, exists := sm.GetSession(sessionID)
	if !exists || session == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}
	from := session.GetState()
	if err := session.TransitionState(target, reason); err != nil {
		return err
	}
	to := session.GetState()
	if sm.metrics != nil {
		sm.metrics.RecordStateChange(from, to)
	}
	sm.emitLifecycle(context.Background(), sessionID, SessionLifecycleStateChanged, map[string]interface{}{
		"from":   string(from),
		"to":     string(to),
		"reason": reason,
	})
	if storage, ok := sm.GetStorage().(IncrementalSessionStorage); ok {
		_ = storage.AppendSessionEvent(sessionID, SessionEvent{
			Type: SessionEventTypeStateTransition,
			Seq:  session.NextSeq(),
			StateTransition: &StateTransition{
				From:   string(from),
				To:     string(to),
				Reason: reason,
			},
			Timestamp: time.Now().Unix(),
		})
	}
	return nil
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
	cancels := sm.popBackgroundCancelsLocked(sessionID)

	deleted := false
	var deletedSession *Session
	if existing, exists := sm.sessions[sessionID]; exists {
		deletedSession = existing
		delete(sm.sessions, sessionID)
		logger.Infof("Deleted session from memory: %s", sessionID)
		deleted = true
	}
	storage := sm.storage
	sm.mutex.Unlock()

	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
	if deletedSession != nil && deletedSession.transcript != nil {
		_ = deletedSession.transcript.Close()
	}
	cleanupTranscriptFile(sessionID)

	sm.emitLifecycle(context.Background(), sessionID, SessionLifecycleBeforeCleanup, map[string]interface{}{"reason": "user_delete"})
	if storage != nil {
		if err := storage.DeleteSession(sessionID); err != nil {
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

// RegisterBackgroundCancel registers a background goroutine cancel function for a session.
// This allows proper cleanup when the session is deleted or the manager shuts down.
func (sm *SessionManager) RegisterBackgroundCancel(sessionID string, cancel context.CancelFunc) int {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.backgroundCancels[sessionID] = append(sm.backgroundCancels[sessionID], cancel)
	return len(sm.backgroundCancels[sessionID]) - 1
}

// UnregisterBackgroundCancel unregisters a background cancel function by index.
func (sm *SessionManager) UnregisterBackgroundCancel(sessionID string, idx int) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	if idx < 0 || idx >= len(sm.backgroundCancels[sessionID]) {
		return
	}
	sm.backgroundCancels[sessionID][idx] = nil
}

// cancelBackgroundsLocked cancels all registered background goroutines for a session.
// Must be called while holding the write lock (sm.mutex).
func (sm *SessionManager) cancelBackgroundsLocked(sessionID string) {
	cancels := sm.popBackgroundCancelsLocked(sessionID)
	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
}

func (sm *SessionManager) popBackgroundCancelsLocked(sessionID string) []context.CancelFunc {
	cancels := append([]context.CancelFunc(nil), sm.backgroundCancels[sessionID]...)
	delete(sm.backgroundCancels, sessionID)
	return cancels
}

// SaveSessionIfActive atomically checks if the session still exists and context is valid,
// then persists it to storage. This prevents race conditions where background goroutines
// try to save sessions that have already been deleted.
func (sm *SessionManager) SaveSessionIfActive(ctx context.Context, sessionID string) error {
	if sm.storage == nil {
		return nil
	}

	// Check context first (fast path)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	session, exists := sm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s no longer active", sessionID)
	}

	// Double-check context before saving
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if removed := session.SanitizeMessageSequence(); removed > 0 {
		logger.Infof("Sanitized %d orphan tool message item(s) before saving session %s", removed, sessionID)
	}
	return sm.storage.SaveSession(session)
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

	type expiredSession struct {
		id      string
		session *Session
		reason  string
	}
	expired := make([]expiredSession, 0)
	now := time.Now()
	for id, session := range sm.sessions {
		if ok, reason := session.IsExpiredByState(now, sm.sessionTTL, sm.awaitingInputTTL); ok {
			expired = append(expired, expiredSession{id: id, session: session, reason: reason})
		}
	}
	for _, item := range expired {
		delete(sm.sessions, item.id)
	}
	storage := sm.storage
	sm.mutex.Unlock()

	for _, item := range expired {
		sm.emitLifecycle(context.Background(), item.id, SessionLifecycleBeforeCleanup, map[string]interface{}{"reason": item.reason})
		// Save to storage before removing from memory if storage is enabled
		// This effectively "archives" the session
		if storage != nil {
			if err := storage.SaveSession(item.session); err != nil {
				logger.Errorf("Failed to save expired session %s to storage: %v", item.id, err)
			}
		}
		if sm.metrics != nil {
			sm.metrics.RecordCleanup(item.reason)
			sm.metrics.RecordSessionLifetime(now.Sub(item.session.CreatedAt))
			sm.metrics.RecordStateChange(item.session.GetState(), "")
		}
		if item.session.transcript != nil {
			_ = item.session.transcript.Close()
		}
		sm.emitLifecycle(context.Background(), item.id, SessionLifecycleCleaned, map[string]interface{}{"reason": item.reason})
		logger.Infof("Cleaned up expired session from memory: %s", item.id)
	}

	if len(expired) > 0 {
		logger.Infof("Cleaned up %d expired sessions, %d remaining", len(expired), len(sm.sessions))
	}
}

// Shutdown stops the session manager and cleans up resources.
func (sm *SessionManager) Shutdown() {
	close(sm.cleanupCh)
	sm.wg.Wait()

	sm.mutex.Lock()
	allCancels := make([]context.CancelFunc, 0)
	for sessionID := range sm.backgroundCancels {
		allCancels = append(allCancels, sm.popBackgroundCancelsLocked(sessionID)...)
	}
	sessionsCopy := make([]*Session, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		sessionsCopy = append(sessionsCopy, session)
	}
	storage := sm.storage
	sm.mutex.Unlock()

	for _, cancel := range allCancels {
		if cancel != nil {
			cancel()
		}
	}

	ctx := context.Background()
	for _, session := range sessionsCopy {
		sm.emitLifecycle(ctx, session.ID, SessionLifecycleBeforeShutdown, map[string]interface{}{"reason": "shutdown"})
	}
	if storage != nil {
		for _, session := range sessionsCopy {
			_ = storage.SaveSession(session)
		}
	}
	for _, session := range sessionsCopy {
		if session.transcript != nil {
			_ = session.transcript.Close()
		}
	}

	logger.Info("Session manager shutdown completed")
}

// MetricsSnapshot returns session observability metrics.
func (sm *SessionManager) MetricsSnapshot() SessionMetricsSnapshot {
	if sm == nil || sm.metrics == nil {
		return SessionMetricsSnapshot{CountByState: map[string]int{}, CleanupByReason: map[string]int64{}}
	}
	return sm.metrics.Snapshot()
}
