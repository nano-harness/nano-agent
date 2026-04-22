// Package memory implements a three-layer local memory system for nano-agent.
//
// Layer 1 – Project:   .nano/MEMORY.md + .nano/memory/rules/*.md
// Layer 2 – Session:   ~/.nano/memory/sessions.db  (SQLite FTS5, per session_id)
// Layer 3 – Knowledge: ~/.nano/memory/knowledge.db (SQLite FTS5)
//
// All SQLite databases are opened in WAL mode for safe concurrent reads.
package memory //nolint:revive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// Manager is the top-level memory system.
// Use ForSession to obtain a session-scoped view.
type Manager struct {
	project   *ProjectMemory
	sessions  *SessionStore
	knowledge *KnowledgeStore
	autoSave  bool
	enabled   bool
}

// MemoryStats contains basic memory system statistics.
type MemoryStats struct { //nolint:revive
	Timestamp             time.Time `json:"timestamp"`
	Mem0ServiceAvailable  bool      `json:"mem0_service_available"`
	Mem0ServiceConfigured bool      `json:"mem0_service_configured"`
}

// LLMClientLike interface for backward compatibility.
type LLMClientLike interface{}

// NewManager creates a local-only memory Manager.
//
// workingDir is the project root (used for project memory).
// dataDir is the user-level data directory (default: ~/.nano/memory).
// If dataDir is empty, it defaults to ~/.nano/memory.
func NewManager(workingDir, dataDir string, enabled bool) *Manager {
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			dataDir = filepath.Join(home, ".nano", "memory")
		} else {
			dataDir = filepath.Join(os.TempDir(), "nano-memory")
		}
	}

	m := &Manager{
		project:  NewProjectMemory(workingDir),
		enabled:  enabled,
		autoSave: enabled,
	}

	sessDB := filepath.Join(dataDir, "sessions.db")
	ss, err := newSessionStore(sessDB)
	if err != nil {
		logger.Warnf("memory: failed to open session store %s: %v (memory disabled)", sessDB, err)
	} else {
		m.sessions = ss
	}

	knowledgeDB := filepath.Join(dataDir, "knowledge.db")
	ks, err := newKnowledgeStore(knowledgeDB)
	if err != nil {
		logger.Warnf("memory: failed to open knowledge store %s: %v", knowledgeDB, err)
	} else {
		m.knowledge = ks
	}

	return m
}

// NewManagerFromAPIKey is a backward-compatible constructor that ignores the Mem0 API key
// and creates a local-only Manager instead.
func NewManagerFromAPIKey(_, _, _, _, _ string, enabled bool) *Manager {
	wd, _ := os.Getwd()
	return NewManager(wd, "", enabled)
}

// Close releases database resources.
func (m *Manager) Close() {
	if m.sessions != nil {
		_ = m.sessions.Close()
	}
	if m.knowledge != nil {
		_ = m.knowledge.Close()
	}
}

// ForSession returns a session-scoped view of the memory system.
func (m *Manager) ForSession(sessionID string) *SessionView {
	return &SessionView{manager: m, sessionID: sessionID}
}

// SaveMemory saves conversation turns to the session store.
func (m *Manager) SaveMemory(ctx context.Context, messages []llm.Message, _, _ string, _ map[string]interface{}) error { //nolint:revive
	if !m.enabled || m.sessions == nil {
		return fmt.Errorf("memory is disabled")
	}
	sessionID := sessionIDFromContext(ctx)
	for _, msg := range messages {
		if err := m.sessions.Add(sessionID, msg.Role, msg.Content); err != nil {
			return err
		}
	}
	return nil
}

// SearchMemory searches session and knowledge stores.
func (m *Manager) SearchMemory(_ context.Context, query, _, _ string, limit int) (string, error) { //nolint:revive
	if m.sessions == nil && m.knowledge == nil {
		return "", fmt.Errorf("memory not available")
	}

	var parts []string

	if m.sessions != nil {
		entries, err := m.sessions.Search(query, "", limit)
		if err == nil && len(entries) > 0 {
			var sb strings.Builder
			sb.WriteString("### Session Memory\n\n")
			for _, e := range entries {
				fmt.Fprintf(&sb, "[%s] %s: %s\n", e.CreatedAt.Format("2006-01-02 15:04"), e.Role, e.Content)
			}
			parts = append(parts, sb.String())
		}
	}

	if m.knowledge != nil {
		k := m.knowledge.FormatForPrompt(query, limit)
		if k != "" {
			parts = append(parts, k)
		}
	}

	return strings.Join(parts, "\n\n"), nil
}

// SaveMemoryWithContext is an alias for SaveMemory.
func (m *Manager) SaveMemoryWithContext(ctx context.Context, messages []llm.Message, userID, agentID string, metadata map[string]interface{}) error {
	return m.SaveMemory(ctx, messages, userID, agentID, metadata)
}

// SearchMemoryWithContext is an alias for SearchMemory.
func (m *Manager) SearchMemoryWithContext(ctx context.Context, query, userID, agentID string, limit int) (string, error) {
	return m.SearchMemory(ctx, query, userID, agentID, limit)
}

// SaveConversationMemory asynchronously saves messages to session memory.
func (m *Manager) SaveConversationMemory(ctx context.Context, messages []llm.Message, userID, agentID string, metadata map[string]interface{}) {
	if !m.enabled || m.sessions == nil {
		return
	}
	go func() {
		if err := m.SaveMemory(ctx, messages, userID, agentID, metadata); err != nil {
			logger.Debugf("memory: save conversation: %v", err)
		}
	}()
}

// GetMemoryStats returns memory statistics.
func (m *Manager) GetMemoryStats() MemoryStats {
	return MemoryStats{
		Timestamp:             time.Now(),
		Mem0ServiceAvailable:  m.enabled && m.sessions != nil,
		Mem0ServiceConfigured: m.enabled && m.sessions != nil,
	}
}

// GetMemoryTools returns the memory tool.
func (m *Manager) GetMemoryTools() []interfaces.Tool {
	t := NewLocalMemoryTool(m)
	return []interfaces.Tool{t}
}

// ProjectSummary returns the project memory summary for system prompt injection.
func (m *Manager) ProjectSummary() string {
	if m.project == nil {
		return ""
	}
	return m.project.Summary()
}

// SetKnowledge upserts a knowledge entry.
func (m *Manager) SetKnowledge(key, value, tags string) error {
	if m.knowledge == nil {
		return fmt.Errorf("knowledge store not available")
	}
	return m.knowledge.Set(key, value, tags)
}

// SaveWithRollback attempts to save session and knowledge data atomically.
// Since Session and Knowledge use independent SQLite databases, true cross-database
// transactions aren't possible. This method provides best-effort rollback:
// 1. Save session data first
// 2. If session save fails, return error immediately
// 3. Save knowledge entries
// 4. If knowledge save fails, attempt to rollback session changes (best-effort)
//
// Returns the original error if rollback fails, wrapped with rollback error details.
func (m *Manager) SaveWithRollback(ctx context.Context, messages []llm.Message, knowledgeEntries map[string]KnowledgeEntry) error {
	if !m.enabled {
		return fmt.Errorf("memory is disabled")
	}

	sessionID := sessionIDFromContext(ctx)

	// Phase 1: Save session data and track what was inserted
	var savedSessionIDs []int64
	if m.sessions != nil && len(messages) > 0 {
		for _, msg := range messages {
			// Get current max ID before insert to enable rollback
			var maxID int64
			err := m.sessions.db.QueryRow("SELECT COALESCE(MAX(id), 0) FROM sessions").Scan(&maxID)
			if err != nil {
				return fmt.Errorf("get max session ID: %w", err)
			}

			if err := m.sessions.Add(sessionID, msg.Role, msg.Content); err != nil {
				return fmt.Errorf("save session: %w", err)
			}

			// Track the ID that was just inserted (maxID + 1)
			savedSessionIDs = append(savedSessionIDs, maxID+1)
		}
	}

	// Phase 2: Save knowledge entries, rollback sessions on failure
	if m.knowledge != nil && len(knowledgeEntries) > 0 {
		for key, entry := range knowledgeEntries {
			if err := m.knowledge.Set(key, entry.Value, entry.Tags); err != nil {
				// Knowledge save failed, attempt to rollback session saves
				logger.Warnf("memory: knowledge save failed, attempting session rollback: %v", err)

				var rollbackErr error
				if m.sessions != nil && len(savedSessionIDs) > 0 {
					// Delete the session entries we just inserted
					for _, id := range savedSessionIDs {
						if _, delErr := m.sessions.db.Exec("DELETE FROM sessions WHERE id = ?", id); delErr != nil {
							rollbackErr = fmt.Errorf("rollback session ID %d: %w", id, delErr)
							break
						}
					}
				}

				if rollbackErr != nil {
					return fmt.Errorf("knowledge save failed: %w (rollback also failed: %v)", err, rollbackErr)
				}
				return fmt.Errorf("knowledge save failed (session changes rolled back): %w", err)
			}
		}
	}

	return nil
}

// ─── SessionView ────────────────────────────────────────────────────────────

// SessionView provides memory operations scoped to a single session.
// Multiple SessionViews sharing the same Manager are safe for concurrent use.
type SessionView struct {
	manager   *Manager
	sessionID string
}

// SessionID returns the session identifier.
func (v *SessionView) SessionID() string { return v.sessionID }

// Add adds a conversation turn to this session.
func (v *SessionView) Add(role, content string) error {
	if v.manager.sessions == nil {
		return nil // silently skip when store not available
	}
	return v.manager.sessions.Add(v.sessionID, role, content)
}

// Search performs full-text search within this session.
func (v *SessionView) Search(query string, limit int) ([]SessionEntry, error) {
	if v.manager.sessions == nil {
		return nil, nil
	}
	return v.manager.sessions.Search(query, v.sessionID, limit)
}

// Recent returns the most recent N conversation turns.
func (v *SessionView) Recent(limit int) ([]SessionEntry, error) {
	if v.manager.sessions == nil {
		return nil, nil
	}
	return v.manager.sessions.Recent(v.sessionID, limit)
}

// Clear removes all memory for this session.
func (v *SessionView) Clear() error {
	if v.manager.sessions == nil {
		return nil
	}
	return v.manager.sessions.DeleteSession(v.sessionID)
}

// ─── Helpers ────────────────────────────────────────────────────────────────

type sessionIDKey struct{}

// ContextWithSessionID returns a copy of ctx carrying the given session ID.
func ContextWithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

// sessionIDFromContext extracts the session ID from ctx, or "default".
func sessionIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(sessionIDKey{}).(string); ok && id != "" {
		return id
	}
	return "default"
}
