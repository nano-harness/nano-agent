package config //nolint:revive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PersistentState holds all runtime state that is persisted across restarts.
type PersistentState struct {
	// Version for forward compatibility
	Version int `json:"version"`

	// Skill activation state
	ActiveSkills []string `json:"active_skills,omitempty"`

	// Scheduled tasks (TUI + Daemon shared)
	ScheduledTasks []PersistedTask `json:"scheduled_tasks,omitempty"`

	// Last known MCP server health status
	MCPServerStatus map[string]string `json:"mcp_server_status,omitempty"`
}
type PersistedTask struct {
	ID        string `json:"id"`
	CronExpr  string `json:"cron_expression"`
	Command   string `json:"command"`
	Source    string `json:"source"`     // "tui", "daemon", "conversation"
	CreatedAt string `json:"created_at"` // RFC3339
	MaxRuns   int    `json:"max_runs,omitempty"`
}

// StateStore manages persistent runtime state separate from configuration.
// State file: ~/.nano/state.json
// Writes are atomic: data is written to a temp file then renamed, and a .bak
// copy is kept from the previous successful save.
type StateStore struct {
	path  string
	mu    sync.RWMutex
	state *PersistentState
	dirty bool
}

const stateVersion = 1

// NewStateStore creates a StateStore backed by the given file path.
// Call Load() before reading state.
func NewStateStore(path string) *StateStore {
	return &StateStore{
		path:  path,
		state: &PersistentState{Version: stateVersion},
	}
}

// DefaultStateStorePath returns the default path for the state file.
func DefaultStateStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".nano", "state.json"), nil
}

// Load reads state from the backing file.  If the file does not exist the
// store starts with empty state.  If the file is corrupt, Load falls back to
// empty state and returns nil so callers can continue.
func (s *StateStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Fresh start
			s.state = &PersistentState{Version: stateVersion}
			return nil
		}
		return fmt.Errorf("read state file %q: %w", s.path, err)
	}

	var st PersistentState
	if err := json.Unmarshal(data, &st); err != nil {
		// Corrupt state – start fresh but don't return an error so startup
		// is not blocked.
		s.state = &PersistentState{Version: stateVersion}
		return nil
	}

	s.state = &st
	return nil
}

// Save atomically persists the current state to disk.
// It first writes a backup of the previous file (path + ".bak"), then writes
// the new state to a temp file, and finally renames it into place.
func (s *StateStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// Back up existing state file if present
	bakPath := s.path + ".bak"
	if _, statErr := os.Stat(s.path); statErr == nil {
		if copyErr := copyFile(s.path, bakPath); copyErr != nil {
			// Non-fatal – continue with the save
			_ = copyErr
		}
	}

	// Atomic write: create a unique temp file in the same directory (os.CreateTemp
	// uses 0600 permissions), write data, then rename into place.
	dir := filepath.Dir(s.path)
	tmpFile, err := os.CreateTemp(dir, "state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp state file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath) // clean up
		return fmt.Errorf("rename state file: %w", err)
	}

	s.dirty = false
	return nil
}

// GetActiveSkills returns the persisted list of active skill names.
func (s *StateStore) GetActiveSkills() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state == nil || len(s.state.ActiveSkills) == 0 {
		return nil
	}
	out := make([]string, len(s.state.ActiveSkills))
	copy(out, s.state.ActiveSkills)
	return out
}

// SetActiveSkills replaces the persisted active skill list.
func (s *StateStore) SetActiveSkills(names []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]string, len(names))
	copy(cp, names)
	s.state.ActiveSkills = cp
	s.dirty = true
}

// AddTask adds a task to the persisted task list.
func (s *StateStore) AddTask(task PersistedTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task.CreatedAt == "" {
		task.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	s.state.ScheduledTasks = append(s.state.ScheduledTasks, task)
	s.dirty = true
}

// RemoveTask removes a task by ID.
func (s *StateStore) RemoveTask(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks := s.state.ScheduledTasks[:0]
	for _, t := range s.state.ScheduledTasks {
		if t.ID != id {
			tasks = append(tasks, t)
		}
	}
	s.state.ScheduledTasks = tasks
	s.dirty = true
}

// GetTasks returns a copy of the persisted task list.
func (s *StateStore) GetTasks() []PersistedTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.state.ScheduledTasks) == 0 {
		return nil
	}
	out := make([]PersistedTask, len(s.state.ScheduledTasks))
	copy(out, s.state.ScheduledTasks)
	return out
}

// SetMCPServerStatus updates a server's health status.
func (s *StateStore) SetMCPServerStatus(name, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.MCPServerStatus == nil {
		s.state.MCPServerStatus = make(map[string]string)
	}
	s.state.MCPServerStatus[name] = status
	s.dirty = true
}

// GetMCPServerStatus returns the last known health status for a server.
func (s *StateStore) GetMCPServerStatus(name string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state.MCPServerStatus == nil {
		return ""
	}
	return s.state.MCPServerStatus[name]
}

// IsDirty returns true if there are unsaved changes.
func (s *StateStore) IsDirty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dirty
}

// copyFile copies src to dst with restricted permissions (0600), overwriting dst if it exists.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
