package permission

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// WorkdirRules represents the allowlist rules for a specific working directory.
type WorkdirRules struct {
	Rules     []string  `json:"rules"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PersistentAllowlistData holds all workdir-scoped allowlist rules.
type PersistentAllowlistData struct {
	Version  int                     `json:"version"`
	Workdirs map[string]WorkdirRules `json:"workdirs"`
}

const persistentAllowlistVersion = 1

// PersistentAllowlistStore manages persistent allowlist rules across sessions.
// Rules are stored per working directory in ~/.nano/allowlist.json.
// Writes are atomic: data is written to a temp file then renamed, and a .bak
// copy is kept from the previous successful save.
type PersistentAllowlistStore struct {
	path string
	mu   sync.RWMutex
	data *PersistentAllowlistData
}

// NewPersistentAllowlistStore creates a PersistentAllowlistStore backed by the given file path.
// Call Load() before reading state.
func NewPersistentAllowlistStore(path string) *PersistentAllowlistStore {
	return &PersistentAllowlistStore{
		path: path,
		data: &PersistentAllowlistData{
			Version:  persistentAllowlistVersion,
			Workdirs: make(map[string]WorkdirRules),
		},
	}
}

// DefaultPersistentAllowlistPath returns the default path for the allowlist file.
func DefaultPersistentAllowlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".nano", "allowlist.json"), nil
}

// Load reads allowlist rules from the backing file. If the file does not exist the
// store starts with empty data. If the file is corrupt, Load falls back to
// empty data and returns nil so callers can continue (logs a warning).
func (s *PersistentAllowlistStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Fresh start
			s.data = &PersistentAllowlistData{
				Version:  persistentAllowlistVersion,
				Workdirs: make(map[string]WorkdirRules),
			}
			return nil
		}
		return fmt.Errorf("read allowlist file %q: %w", s.path, err)
	}

	var d PersistentAllowlistData
	if err := json.Unmarshal(data, &d); err != nil {
		// Corrupt data – start fresh but don't return an error so startup
		// is not blocked.
		s.data = &PersistentAllowlistData{
			Version:  persistentAllowlistVersion,
			Workdirs: make(map[string]WorkdirRules),
		}
		return nil
	}

	// Ensure the Workdirs map is not nil
	if d.Workdirs == nil {
		d.Workdirs = make(map[string]WorkdirRules)
	}

	s.data = &d
	return nil
}

// save atomically persists the current data to disk.
// Caller must hold s.mu.Lock().
func (s *PersistentAllowlistStore) save() error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create allowlist directory: %w", err)
	}

	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal allowlist: %w", err)
	}

	// Back up existing allowlist file if present
	bakPath := s.path + ".bak"
	if _, statErr := os.Stat(s.path); statErr == nil {
		if copyErr := copyFileInternal(s.path, bakPath); copyErr != nil {
			// Non-fatal – continue with the save
			_ = copyErr
		}
	}

	// Atomic write: create a unique temp file in the same directory,
	// write data, then rename into place.
	dir := filepath.Dir(s.path)
	tmpFile, err := os.CreateTemp(dir, "allowlist-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp allowlist file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp allowlist file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp allowlist file: %w", err)
	}

	// Set file permissions to 0600 (owner read/write only)
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp allowlist file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath) // clean up
		return fmt.Errorf("rename allowlist file: %w", err)
	}

	return nil
}

// normalizeWorkdir returns the absolute, symlink-resolved path for a working directory.
// Falls back to absolute path if symlink resolution fails.
func (s *PersistentAllowlistStore) normalizeWorkdir(workdir string) string {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		return workdir
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// If symlink resolution fails, use the absolute path
		return abs
	}
	return resolved
}

// RulesForWorkdir returns a copy of the rules for the specified working directory.
func (s *PersistentAllowlistStore) RulesForWorkdir(workdir string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	normalized := s.normalizeWorkdir(workdir)
	wr, ok := s.data.Workdirs[normalized]
	if !ok || len(wr.Rules) == 0 {
		return nil
	}
	out := make([]string, len(wr.Rules))
	copy(out, wr.Rules)
	return out
}

// AddRuleForWorkdir adds a rule for the specified working directory.
// Returns true if the rule was added (was not already present), false if it was a duplicate.
func (s *PersistentAllowlistStore) AddRuleForWorkdir(workdir string, rule string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := s.normalizeWorkdir(workdir)
	wr, ok := s.data.Workdirs[normalized]
	if !ok {
		wr = WorkdirRules{
			Rules:     []string{},
			UpdatedAt: time.Now().UTC(),
		}
	}

	// Check for duplicates
	for _, r := range wr.Rules {
		if r == rule {
			return false, nil // already present
		}
	}

	wr.Rules = append(wr.Rules, rule)
	wr.UpdatedAt = time.Now().UTC()
	s.data.Workdirs[normalized] = wr

	return true, s.save()
}

// RemoveRuleForWorkdir removes a rule for the specified working directory.
// Returns true if the rule was found and removed, false if it was not present.
func (s *PersistentAllowlistStore) RemoveRuleForWorkdir(workdir string, rule string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := s.normalizeWorkdir(workdir)
	wr, ok := s.data.Workdirs[normalized]
	if !ok {
		return false, nil // workdir not present
	}

	// Find and remove the rule
	found := false
	newRules := make([]string, 0, len(wr.Rules))
	for _, r := range wr.Rules {
		if r == rule {
			found = true
			continue
		}
		newRules = append(newRules, r)
	}

	if !found {
		return false, nil
	}

	wr.Rules = newRules
	wr.UpdatedAt = time.Now().UTC()
	if len(wr.Rules) == 0 {
		// Remove the workdir entry if no rules remain
		delete(s.data.Workdirs, normalized)
	} else {
		s.data.Workdirs[normalized] = wr
	}

	return true, s.save()
}

// copyFileInternal copies src to dst with restricted permissions (0600), overwriting dst if it exists.
func copyFileInternal(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
