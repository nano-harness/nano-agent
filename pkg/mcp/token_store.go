package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TokenEntry holds an OAuth 2.0 token for an MCP server.
type TokenEntry struct {
	ServerName   string    `json:"server_name"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// IsExpired returns true if the token has passed its expiry time.
// Tokens with a zero ExpiresAt are treated as non-expiring.
func (t *TokenEntry) IsExpired() bool {
	return !t.ExpiresAt.IsZero() && time.Now().After(t.ExpiresAt)
}

// TokenStore persists OAuth tokens to a JSON file on disk.
// All methods are safe for concurrent use.
type TokenStore struct {
	mu   sync.RWMutex
	path string
	data map[string]*TokenEntry // key = server name
}

// NewTokenStore opens (or creates) the token store at the given path.
// If path is empty it defaults to ~/.nano/mcp_tokens.json.
func NewTokenStore(path string) (*TokenStore, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("token store: home dir: %w", err)
		}
		path = filepath.Join(home, ".nano", "mcp_tokens.json")
	}

	ts := &TokenStore{path: path, data: make(map[string]*TokenEntry)}
	if err := ts.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("token store: load: %w", err)
	}
	return ts, nil
}

// Set upserts a token for the given server.
func (ts *TokenStore) Set(entry *TokenEntry) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	entry.UpdatedAt = time.Now()
	ts.data[entry.ServerName] = entry
	return ts.save()
}

// Get returns the token for a server. Returns nil if not found.
func (ts *TokenStore) Get(serverName string) *TokenEntry {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.data[serverName]
}

// Delete removes the token for a server.
func (ts *TokenStore) Delete(serverName string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.data, serverName)
	return ts.save()
}

// List returns all stored token entries.
func (ts *TokenStore) List() []*TokenEntry {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	out := make([]*TokenEntry, 0, len(ts.data))
	for _, v := range ts.data {
		out = append(out, v)
	}
	return out
}

// load reads tokens from disk. Caller must NOT hold the mutex.
func (ts *TokenStore) load() error {
	data, err := os.ReadFile(ts.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &ts.data)
}

// save writes tokens to disk with 0600 permissions. Caller must hold the write lock.
func (ts *TokenStore) save() error {
	if err := os.MkdirAll(filepath.Dir(ts.path), 0o700); err != nil {
		return fmt.Errorf("token store: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(ts.data, "", "  ")
	if err != nil {
		return fmt.Errorf("token store: marshal: %w", err)
	}
	return os.WriteFile(ts.path, data, 0o600)
}
