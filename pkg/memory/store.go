package memory //nolint:revive

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

// openDB opens (or creates) the SQLite database at path with WAL mode and FTS5.
func openDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("memory: create db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?_journal=WAL&_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("memory: open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite WAL: single writer
	return db, nil
}

// SessionEntry is a row in the session memory table.
type SessionEntry struct {
	ID        int64
	SessionID string
	Role      string // "user" | "assistant" | "system"
	Content   string
	CreatedAt time.Time
}

// KnowledgeEntry is a row in the semantic knowledge table.
type KnowledgeEntry struct {
	ID        int64
	Key       string
	Value     string
	Tags      string // comma-separated tags
	CreatedAt time.Time
	UpdatedAt time.Time
}

// initSessionDB creates the sessions table and FTS5 virtual table.
func initSessionDB(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT    NOT NULL,
    role       TEXT    NOT NULL,
    content    TEXT    NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
CREATE INDEX IF NOT EXISTS idx_sessions_session ON sessions(session_id);

CREATE VIRTUAL TABLE IF NOT EXISTS sessions_fts USING fts5(
    content,
    content='sessions',
    content_rowid='id',
    tokenize='unicode61'
);

CREATE TRIGGER IF NOT EXISTS sessions_ai AFTER INSERT ON sessions BEGIN
    INSERT INTO sessions_fts(rowid, content) VALUES (new.id, new.content);
END;
CREATE TRIGGER IF NOT EXISTS sessions_ad AFTER DELETE ON sessions BEGIN
    INSERT INTO sessions_fts(sessions_fts, rowid, content) VALUES('delete', old.id, old.content);
END;
CREATE TRIGGER IF NOT EXISTS sessions_au AFTER UPDATE ON sessions BEGIN
    INSERT INTO sessions_fts(sessions_fts, rowid, content) VALUES('delete', old.id, old.content);
    INSERT INTO sessions_fts(rowid, content) VALUES (new.id, new.content);
END;
`)
	return err
}

// initKnowledgeDB creates the knowledge table and FTS5 virtual table.
func initKnowledgeDB(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS knowledge (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    key        TEXT    NOT NULL UNIQUE,
    value      TEXT    NOT NULL,
    tags       TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts USING fts5(
    key,
    value,
    tags,
    content='knowledge',
    content_rowid='id',
    tokenize='unicode61'
);

CREATE TRIGGER IF NOT EXISTS knowledge_ai AFTER INSERT ON knowledge BEGIN
    INSERT INTO knowledge_fts(rowid, key, value, tags) VALUES (new.id, new.key, new.value, new.tags);
END;
CREATE TRIGGER IF NOT EXISTS knowledge_ad AFTER DELETE ON knowledge BEGIN
    INSERT INTO knowledge_fts(knowledge_fts, rowid, key, value, tags) VALUES('delete', old.id, old.key, old.value, old.tags);
END;
CREATE TRIGGER IF NOT EXISTS knowledge_au AFTER UPDATE ON knowledge BEGIN
    INSERT INTO knowledge_fts(knowledge_fts, rowid, key, value, tags) VALUES('delete', old.id, old.key, old.value, old.tags);
    INSERT INTO knowledge_fts(rowid, key, value, tags) VALUES (new.id, new.key, new.value, new.tags);
END;
`)
	return err
}
