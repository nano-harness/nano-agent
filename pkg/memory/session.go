package memory //nolint:revive

import (
	"database/sql"
	"fmt"
	"time"
)

// SessionStore manages per-session conversation memory backed by SQLite FTS5.
type SessionStore struct {
	db *sql.DB
}

// newSessionStore opens or creates the session memory database at path.
func newSessionStore(path string) (*SessionStore, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	if err := initSessionDB(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("session store: init schema: %w", err)
	}
	return &SessionStore{db: db}, nil
}

// Close closes the underlying database.
func (s *SessionStore) Close() error { return s.db.Close() }

// Add inserts a new conversation turn into the given session.
func (s *SessionStore) Add(sessionID, role, content string) error {
	_, err := s.db.Exec(
		`INSERT INTO sessions (session_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
		sessionID, role, content, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("session store: add: %w", err)
	}
	return nil
}

// Search performs a full-text search across all sessions or within a specific session.
// Pass empty sessionID to search all sessions.
func (s *SessionStore) Search(query, sessionID string, limit int) ([]SessionEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows *sql.Rows
	var err error
	if sessionID != "" {
		rows, err = s.db.Query(`
SELECT s.id, s.session_id, s.role, s.content, s.created_at
FROM sessions s
JOIN sessions_fts f ON s.id = f.rowid
WHERE sessions_fts MATCH ? AND s.session_id = ?
ORDER BY rank
LIMIT ?`, query, sessionID, limit)
	} else {
		rows, err = s.db.Query(`
SELECT s.id, s.session_id, s.role, s.content, s.created_at
FROM sessions s
JOIN sessions_fts f ON s.id = f.rowid
WHERE sessions_fts MATCH ?
ORDER BY rank
LIMIT ?`, query, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("session store: search: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	return scanSessionEntries(rows)
}

// Recent returns the most recent N turns of a session.
func (s *SessionStore) Recent(sessionID string, limit int) ([]SessionEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
SELECT id, session_id, role, content, created_at
FROM sessions
WHERE session_id = ?
ORDER BY id DESC
LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("session store: recent: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	entries, err := scanSessionEntries(rows)
	if err != nil {
		return nil, err
	}
	// Reverse to chronological order.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

// DeleteSession removes all entries for a session.
func (s *SessionStore) DeleteSession(sessionID string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE session_id = ?`, sessionID)
	return err
}

// ListSessions returns all distinct session IDs.
func (s *SessionStore) ListSessions() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT session_id FROM sessions ORDER BY session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanSessionEntries(rows *sql.Rows) ([]SessionEntry, error) {
	var entries []SessionEntry
	for rows.Next() {
		var e SessionEntry
		var ts int64
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Role, &e.Content, &ts); err != nil {
			return nil, err
		}
		e.CreatedAt = time.Unix(ts, 0)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
