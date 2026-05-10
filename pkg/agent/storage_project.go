package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// ProjectSessionStorage implements SessionStorage for TUI mode with per-project JSONL storage
type ProjectSessionStorage struct {
	projectDir  string // Encoded project directory path under ~/.nano/projects/
	sessionsDir string // Full path to sessions directory
	indexPath   string // Full path to sessions-index.json
	workingDir  string // Absolute working directory of the project
	mu          sync.RWMutex
}

// NewProjectSessionStorage creates a new project-scoped session storage
func NewProjectSessionStorage(workingDir string) (*ProjectSessionStorage, error) {
	if workingDir == "" {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	// Get absolute path
	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve working directory: %w", err)
	}

	// Encode project path with hash to avoid collisions
	encodedPath := encodeProjectPathWithHash(absWorkingDir)

	// Build storage paths
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	projectDir := filepath.Join(home, ".nano", "projects", encodedPath)
	sessionsDir := filepath.Join(projectDir, "sessions")
	indexPath := filepath.Join(projectDir, "sessions-index.json")

	storage := &ProjectSessionStorage{
		projectDir:  projectDir,
		sessionsDir: sessionsDir,
		indexPath:   indexPath,
		workingDir:  absWorkingDir,
	}

	// Create directories if they don't exist
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sessions directory: %w", err)
	}

	return storage, nil
}

// validateSessionID validates that a session ID is safe and doesn't contain path traversal
func validateSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("session ID is empty")
	}

	// Prevent path traversal
	if strings.Contains(id, "..") || strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return fmt.Errorf("session ID contains invalid characters")
	}

	// Basic alphanumeric + hyphen check
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, id)
	if !matched {
		return fmt.Errorf("session ID must be alphanumeric with hyphens/underscores only")
	}

	return nil
}

// SaveSession saves a session to JSONL format
func (s *ProjectSessionStorage) SaveSession(session *Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}

	// Validate session ID
	if err := validateSessionID(session.ID); err != nil {
		return fmt.Errorf("invalid session ID: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Convert session history to events
	events := MessagesToSessionEvents(session.GetConversationHistory())
	lastDiskSeq, persistedMessages, err := s.getJournalStatsLocked(session.ID)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to get journal stats: %w", err)
	}
	if len(events) < persistedMessages {
		logger.Warnf("Session %s has fewer in-memory messages (%d) than persisted message events (%d); rewriting journal", session.ID, len(events), persistedMessages)
		lastDiskSeq = 0
		persistedMessages = 0
	}
	for i := range events {
		if lastDiskSeq == 0 {
			events[i].Seq = int64(i + 1)
			continue
		}
		events[i].Seq = lastDiskSeq + int64(i-persistedMessages+1)
	}

	sessionPath := filepath.Join(s.sessionsDir, session.ID+".jsonl")
	var f *os.File
	if lastDiskSeq == 0 {
		tmpPath := sessionPath + ".tmp"
		f, err = os.Create(tmpPath)
		if err != nil {
			return fmt.Errorf("failed to create temp file: %w", err)
		}
		defer func() { _ = os.Remove(tmpPath) }()
		defer func() { _ = f.Close() }()
	} else {
		f, err = os.OpenFile(sessionPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open session file for append: %w", err)
		}
		defer func() { _ = f.Close() }()
	}

	for _, event := range events {
		if lastDiskSeq > 0 && event.Seq <= lastDiskSeq {
			continue
		}
		line, err := event.ToJSONL()
		if err != nil {
			return fmt.Errorf("failed to marshal event: %w", err)
		}
		if _, err := f.Write(line); err != nil {
			return fmt.Errorf("failed to write event: %w", err)
		}
	}

	if lastDiskSeq == 0 {
		if err := f.Close(); err != nil {
			return fmt.Errorf("failed to close temp file: %w", err)
		}
		if err := os.Rename(sessionPath+".tmp", sessionPath); err != nil {
			return fmt.Errorf("failed to rename temp file: %w", err)
		}
	}

	// Update index
	summary := extractSummary(session)

	entry := SessionIndexEntry{
		ID:                session.ID,
		Summary:           summary,
		MessageCount:      len(session.GetConversationHistory()),
		CreatedAt:         session.CreatedAt.Unix(),
		ModifiedAt:        time.Now().Unix(),
		WorkingDir:        s.workingDir,
		LastSeq:           maxSeqFromEvents(events, lastDiskSeq),
		State:             string(session.GetState()),
		LastCompactionSeq: session.LastCompactionSeq,
	}
	session.LastPersistedSeq = maxSeqFromEvents(events, lastDiskSeq)

	return s.updateIndex(entry)
}

// LoadSession loads a session from JSONL format
func (s *ProjectSessionStorage) LoadSession(id string) (*Session, error) {
	// Validate session ID
	if err := validateSessionID(id); err != nil {
		return nil, fmt.Errorf("invalid session ID: %w", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	sessionPath := filepath.Join(s.sessionsDir, id+".jsonl")

	f, err := os.Open(sessionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session not found")
		}
		return nil, fmt.Errorf("failed to open session file: %w", err)
	}
	defer func() { _ = f.Close() }()

	events, maxSeq, lastCompactionSeq, err := readSessionEvents(f, id)
	if err != nil {
		return nil, err
	}

	// Convert events to messages
	messages := SessionEventsToMessages(events)

	// Get metadata from index
	entry, _ := s.getIndexEntry(id)

	session := NewSessionWithID(id)
	session.ConversationHistory = messages
	if entry != nil {
		session.CreatedAt = time.Unix(entry.CreatedAt, 0)
		session.LastActiveAt = time.Unix(entry.ModifiedAt, 0)
		if entry.Summary != "" {
			session.SetMetadata("title", entry.Summary)
		}
		session.LastCompactionSeq = entry.LastCompactionSeq
		if entry.State != "" {
			session.State = SessionState(entry.State)
		}
	}
	if session.State == "" {
		session.State = SessionStateIdle
	}
	if session.StateChangedAt.IsZero() {
		session.StateChangedAt = session.LastActiveAt
	}
	session.LastPersistedSeq = maxSeq
	if lastCompactionSeq > session.LastCompactionSeq {
		session.LastCompactionSeq = lastCompactionSeq
	}
	session.SanitizeMessageSequence()

	return session, nil
}

// ListSessions returns a list of session IDs
func (s *ProjectSessionStorage) ListSessions() ([]string, error) {
	infos, err := s.ListSessionInfos()
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(infos))
	for _, info := range infos {
		ids = append(ids, info.ID)
	}
	return ids, nil
}

// ListSessionInfos returns session metadata from the index
func (s *ProjectSessionStorage) ListSessionInfos() ([]SessionInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	index, err := s.loadIndex()
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionInfo{}, nil // Return empty list if index doesn't exist yet
		}
		return nil, fmt.Errorf("failed to load index: %w", err)
	}

	// Convert index entries to SessionInfo
	infos := make([]SessionInfo, 0, len(index))
	for _, entry := range index {
		info := SessionInfo{
			ID:           entry.ID,
			Title:        entry.Summary,
			MessageCount: entry.MessageCount,
			WorkingDir:   entry.WorkingDir,
			State:        entry.State,
			LastSeq:      entry.LastSeq,
			CreatedAt:    time.Unix(entry.CreatedAt, 0).Format(time.RFC3339),
			UpdatedAt:    time.Unix(entry.ModifiedAt, 0).Format(time.RFC3339),
		}
		infos = append(infos, info)
	}

	// Sort by modification time (newest first)
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].UpdatedAt > infos[j].UpdatedAt
	})

	return infos, nil
}

// DeleteSession deletes a session file and updates the index
func (s *ProjectSessionStorage) DeleteSession(id string) error {
	// Validate session ID
	if err := validateSessionID(id); err != nil {
		return fmt.Errorf("invalid session ID: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sessionPath := filepath.Join(s.sessionsDir, id+".jsonl")

	if err := os.Remove(sessionPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete session file: %w", err)
	}

	// Remove from index
	return s.removeFromIndex(id)
}

// GetLatestSessionID returns the most recently modified session ID
func (s *ProjectSessionStorage) GetLatestSessionID() (string, error) {
	infos, err := s.ListSessionInfos()
	if err != nil {
		return "", err
	}

	if len(infos) == 0 {
		return "", fmt.Errorf("no sessions found")
	}

	// First item is the most recent (sorted in ListSessionInfos)
	return infos[0].ID, nil
}

// AppendSessionEvent appends a single event to a session's JSONL file (real-time writing)
func (s *ProjectSessionStorage) AppendSessionEvent(sessionID string, event SessionEvent) error {
	// Validate session ID
	if err := validateSessionID(sessionID); err != nil {
		return fmt.Errorf("invalid session ID: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}

	sessionPath := filepath.Join(s.sessionsDir, sessionID+".jsonl")

	f, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open session file for append: %w", err)
	}
	defer func() { _ = f.Close() }()

	line, err := event.ToJSONL()
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}
	if event.Type == SessionEventTypeCheckpoint || event.Type == SessionEventTypeCompactionMarker {
		if err := f.Sync(); err != nil {
			return fmt.Errorf("failed to fsync checkpoint event: %w", err)
		}
	}

	// Update index to reflect the change
	entry, err := s.getIndexEntry(sessionID)
	if err != nil {
		// Session doesn't exist in index yet, create a new entry
		entry = &SessionIndexEntry{
			ID:         sessionID,
			Summary:    "New Chat",
			CreatedAt:  event.Timestamp,
			WorkingDir: s.workingDir,
		}
	}

	// Update modification time and message count
	entry.ModifiedAt = event.Timestamp
	if event.Role != "" {
		entry.MessageCount++
	}
	if event.Seq > entry.LastSeq {
		entry.LastSeq = event.Seq
	}
	if event.Type == SessionEventTypeCompactionMarker {
		entry.LastCompactionSeq = event.Seq
	}
	if event.Type == SessionEventTypeStateTransition && event.StateTransition != nil {
		entry.State = event.StateTransition.To
	}

	return s.updateIndex(*entry)
}

// AppendEvent is kept for compatibility with existing callers.
func (s *ProjectSessionStorage) AppendEvent(sessionID string, event SessionEvent) error {
	return s.AppendSessionEvent(sessionID, event)
}

// LoadEventsFromSeq loads events with Seq greater than fromSeq.
func (s *ProjectSessionStorage) LoadEventsFromSeq(sessionID string, fromSeq int64) ([]SessionEvent, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, fmt.Errorf("invalid session ID: %w", err)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, err := os.Open(filepath.Join(s.sessionsDir, sessionID+".jsonl"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	events, _, _, err := readSessionEvents(f, sessionID)
	if err != nil {
		return nil, err
	}
	filtered := make([]SessionEvent, 0, len(events))
	for _, ev := range events {
		if ev.Seq > fromSeq {
			filtered = append(filtered, ev)
		}
	}
	return filtered, nil
}

// WriteCheckpoint writes a durable compaction marker event.
func (s *ProjectSessionStorage) WriteCheckpoint(sessionID string, marker CompactionMarker) error {
	lastSeq, err := s.GetLastSeq(sessionID)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return s.AppendSessionEvent(sessionID, SessionEvent{
		Type:       SessionEventTypeCompactionMarker,
		Seq:        lastSeq + 1,
		Compaction: &marker,
		Timestamp:  time.Now().Unix(),
	})
}

// GetLastSeq returns the highest sequence number in a session journal.
func (s *ProjectSessionStorage) GetLastSeq(sessionID string) (int64, error) {
	if err := validateSessionID(sessionID); err != nil {
		return 0, fmt.Errorf("invalid session ID: %w", err)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getLastSeqLocked(sessionID)
}

func (s *ProjectSessionStorage) getLastSeqLocked(sessionID string) (int64, error) {
	lastSeq, _, err := s.getJournalStatsLocked(sessionID)
	return lastSeq, err
}

func (s *ProjectSessionStorage) getJournalStatsLocked(sessionID string) (int64, int, error) {
	f, err := os.Open(filepath.Join(s.sessionsDir, sessionID+".jsonl"))
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()
	events, maxSeq, _, err := readSessionEvents(f, sessionID)
	if err != nil {
		return 0, 0, err
	}
	messageCount := 0
	for _, ev := range events {
		if ev.Role != "" {
			messageCount++
		}
	}
	return maxSeq, messageCount, nil
}

func maxSeqFromEvents(events []SessionEvent, fallback int64) int64 {
	maxSeq := fallback
	for _, ev := range events {
		if ev.Seq > maxSeq {
			maxSeq = ev.Seq
		}
	}
	return maxSeq
}

func readSessionEvents(r *os.File, sessionID string) ([]SessionEvent, int64, int64, error) {
	var events []SessionEvent
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var maxSeq int64
	var lastCompactionSeq int64
	var afterCompaction []SessionEvent
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		event, err := ParseJSONL(line)
		if err != nil {
			logger.Warnf("Failed to parse JSONL line in session %s: %v", sessionID, err)
			continue
		}
		if event.Seq == 0 {
			maxSeq++
			event.Seq = maxSeq
		} else if event.Seq > maxSeq {
			maxSeq = event.Seq
		}
		if event.Type == SessionEventTypeCompactionMarker {
			lastCompactionSeq = event.Seq
			afterCompaction = afterCompaction[:0]
			if len(events) > 0 {
				prev := events[len(events)-1]
				if prev.Role != "" {
					afterCompaction = append(afterCompaction, prev)
				}
			}
			events = append(events, *event)
			continue
		}
		afterCompaction = append(afterCompaction, *event)
		events = append(events, *event)
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, 0, fmt.Errorf("failed to read session file: %w", err)
	}
	if lastCompactionSeq > 0 {
		events = afterCompaction
	}
	return events, maxSeq, lastCompactionSeq, nil
}

// loadIndex loads the sessions index
func (s *ProjectSessionStorage) loadIndex() ([]SessionIndexEntry, error) {
	data, err := os.ReadFile(s.indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionIndexEntry{}, nil
		}
		return nil, fmt.Errorf("failed to read index: %w", err)
	}

	var index []SessionIndexEntry
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("failed to parse index: %w", err)
	}

	return index, nil
}

// updateIndex updates or adds an entry in the index
func (s *ProjectSessionStorage) updateIndex(entry SessionIndexEntry) error {
	index, err := s.loadIndex()
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Find and update existing entry or append new one
	found := false
	for i, e := range index {
		if e.ID == entry.ID {
			index[i] = entry
			found = true
			break
		}
	}
	if !found {
		index = append(index, entry)
	}

	// Write index
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	tmpPath := s.indexPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	if err := os.Rename(tmpPath, s.indexPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename index: %w", err)
	}

	return nil
}

// removeFromIndex removes an entry from the index
func (s *ProjectSessionStorage) removeFromIndex(id string) error {
	index, err := s.loadIndex()
	if err != nil {
		return err
	}

	// Filter out the entry
	newIndex := make([]SessionIndexEntry, 0, len(index))
	for _, e := range index {
		if e.ID != id {
			newIndex = append(newIndex, e)
		}
	}

	// Write index
	data, err := json.MarshalIndent(newIndex, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	tmpPath := s.indexPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	if err := os.Rename(tmpPath, s.indexPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename index: %w", err)
	}

	return nil
}

// getIndexEntry retrieves a single entry from the index
func (s *ProjectSessionStorage) getIndexEntry(id string) (*SessionIndexEntry, error) {
	index, err := s.loadIndex()
	if err != nil {
		return nil, err
	}

	for _, e := range index {
		if e.ID == id {
			return &e, nil
		}
	}

	return nil, fmt.Errorf("entry not found in index")
}

// extractSummary extracts a summary from the session (first user message or metadata title)
func extractSummary(session *Session) string {
	// Try metadata first
	if title, ok := session.GetMetadata("title"); ok {
		if titleStr, ok := title.(string); ok && titleStr != "" {
			return titleStr
		}
	}

	// Fall back to first user message
	for _, msg := range session.GetConversationHistory() {
		if msg.Role == "user" && msg.Content != "" {
			content := msg.Content
			// Truncate to 100 characters
			runes := []rune(content)
			if len(runes) > 100 {
				return string(runes[:100]) + "..."
			}
			return content
		}
	}

	return "New Chat"
}
