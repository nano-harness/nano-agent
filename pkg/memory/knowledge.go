package memory //nolint:revive

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// KnowledgeStore manages semantic knowledge backed by SQLite FTS5.
type KnowledgeStore struct {
	db *sql.DB
}

// newKnowledgeStore opens or creates the knowledge database at path.
func newKnowledgeStore(path string) (*KnowledgeStore, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	if err := initKnowledgeDB(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("knowledge store: init schema: %w", err)
	}
	return &KnowledgeStore{db: db}, nil
}

// Close closes the underlying database.
func (k *KnowledgeStore) Close() error { return k.db.Close() }

// Set upserts a key-value knowledge entry with optional comma-separated tags.
func (k *KnowledgeStore) Set(key, value, tags string) error {
	_, err := k.db.Exec(`
INSERT INTO knowledge (key, value, tags, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    value      = excluded.value,
    tags       = excluded.tags,
    updated_at = excluded.updated_at`,
		key, value, tags, time.Now().Unix(), time.Now().Unix(),
	)
	return err
}

// Get retrieves a knowledge entry by key.
func (k *KnowledgeStore) Get(key string) (*KnowledgeEntry, error) {
	row := k.db.QueryRow(`SELECT id, key, value, tags, created_at, updated_at FROM knowledge WHERE key = ?`, key)
	return scanKnowledge(row)
}

// Search performs a full-text search over keys, values, and tags.
func (k *KnowledgeStore) Search(query string, limit int) ([]KnowledgeEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := k.db.Query(`
SELECT kn.id, kn.key, kn.value, kn.tags, kn.created_at, kn.updated_at
FROM knowledge kn
JOIN knowledge_fts f ON kn.id = f.rowid
WHERE knowledge_fts MATCH ?
ORDER BY rank
LIMIT ?`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("knowledge store: search: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	return scanKnowledgeRows(rows)
}

// Delete removes a knowledge entry by key.
func (k *KnowledgeStore) Delete(key string) error {
	_, err := k.db.Exec(`DELETE FROM knowledge WHERE key = ?`, key)
	return err
}

// List returns all knowledge entries, ordered by updated_at descending.
func (k *KnowledgeStore) List(limit int) ([]KnowledgeEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := k.db.Query(`SELECT id, key, value, tags, created_at, updated_at FROM knowledge ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	return scanKnowledgeRows(rows)
}

// FormatForPrompt returns a compact Markdown representation for system prompt injection.
func (k *KnowledgeStore) FormatForPrompt(query string, limit int) string {
	entries, err := k.Search(query, limit)
	if err != nil || len(entries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Relevant Knowledge\n\n")
	for _, e := range entries {
		sb.WriteString("**")
		sb.WriteString(e.Key)
		sb.WriteString("**")
		if e.Tags != "" {
			sb.WriteString(" [")
			sb.WriteString(e.Tags)
			sb.WriteString("]")
		}
		sb.WriteString("\n")
		sb.WriteString(e.Value)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func scanKnowledge(row *sql.Row) (*KnowledgeEntry, error) {
	var e KnowledgeEntry
	var createdAt, updatedAt int64
	if err := row.Scan(&e.ID, &e.Key, &e.Value, &e.Tags, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil //nolint:nilnil
		}
		return nil, err
	}
	e.CreatedAt = time.Unix(createdAt, 0)
	e.UpdatedAt = time.Unix(updatedAt, 0)
	return &e, nil
}

func scanKnowledgeRows(rows *sql.Rows) ([]KnowledgeEntry, error) {
	var entries []KnowledgeEntry
	for rows.Next() {
		var e KnowledgeEntry
		var createdAt, updatedAt int64
		if err := rows.Scan(&e.ID, &e.Key, &e.Value, &e.Tags, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		e.CreatedAt = time.Unix(createdAt, 0)
		e.UpdatedAt = time.Unix(updatedAt, 0)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
