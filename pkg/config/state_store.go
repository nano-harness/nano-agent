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

	// Watcher rules created at runtime (via manage_watcher tool or /watcher command).
	WatcherRules []PersistedWatchRule `json:"watcher_rules,omitempty"`

	// Last known MCP server health status
	MCPServerStatus map[string]string `json:"mcp_server_status,omitempty"`

	// Tool calls awaiting user approval at last process termination.
	PendingApprovals []PendingApproval `json:"pending_approvals,omitempty"`
}

// PendingApproval records a tool call that was awaiting user approval when the
// process last terminated.
type PendingApproval struct {
	CallID   string `json:"call_id"`
	ToolName string `json:"tool_name"`
}
type PersistedTask struct {
	ID        string `json:"id"`
	CronExpr  string `json:"cron_expression"`
	Command   string `json:"command"`
	Source    string `json:"source"`     // "tui", "daemon", "conversation"
	CreatedAt string `json:"created_at"` // RFC3339
	MaxRuns   int    `json:"max_runs,omitempty"`
}

// PersistedWatchRule holds a watcher rule created at runtime (via tool call or
// slash command) that must survive process restarts.
type PersistedWatchRule struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	Event        string `json:"event"`
	Filter       string `json:"filter,omitempty"`
	Command      string `json:"command"`
	Interval     string `json:"interval,omitempty"` // duration string
	Timeout      string `json:"timeout,omitempty"`  // duration string
	ShellCommand string `json:"shell_command,omitempty"`
	CreatedAt    string `json:"created_at"`
	// LastPoll is the RFC3339 timestamp of the last successful poll, used to
	// prevent re-triggering events after a restart.
	LastPoll string `json:"last_poll,omitempty"`
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

// SetPendingApproval records that a tool call is awaiting user approval.
func (s *StateStore) SetPendingApproval(callID, toolName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pa := range s.state.PendingApprovals {
		if pa.CallID == callID {
			return // already recorded
		}
	}
	s.state.PendingApprovals = append(s.state.PendingApprovals, PendingApproval{CallID: callID, ToolName: toolName})
	s.dirty = true
}

// GetPendingApprovals returns a copy of the pending approval list.
func (s *StateStore) GetPendingApprovals() []PendingApproval {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.state.PendingApprovals) == 0 {
		return nil
	}
	out := make([]PendingApproval, len(s.state.PendingApprovals))
	copy(out, s.state.PendingApprovals)
	return out
}

// ClearPendingApproval removes a specific pending approval by call ID.
func (s *StateStore) ClearPendingApproval(callID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	originalLen := len(s.state.PendingApprovals)
	filtered := s.state.PendingApprovals[:0]
	for _, pa := range s.state.PendingApprovals {
		if pa.CallID != callID {
			filtered = append(filtered, pa)
		}
	}
	if len(filtered) == originalLen {
		return
	}
	s.state.PendingApprovals = filtered
	s.dirty = true
}

// ClearAllPendingApprovals removes all pending approvals (called on startup to
// discard stale entries from the previous run).
func (s *StateStore) ClearAllPendingApprovals() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.state.PendingApprovals) == 0 {
		return
	}
	s.state.PendingApprovals = nil
	s.dirty = true
}

// AddWatcherRule appends or updates a watcher rule in the persisted list.
// If a rule with the same ID already exists it is replaced in-place.
func (s *StateStore) AddWatcherRule(rule PersistedWatchRule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rule.CreatedAt == "" {
		rule.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	for i, r := range s.state.WatcherRules {
		if r.ID == rule.ID {
			s.state.WatcherRules[i] = rule
			s.dirty = true
			return
		}
	}
	s.state.WatcherRules = append(s.state.WatcherRules, rule)
	s.dirty = true
}

// UpdateWatcherRuleCheckpoint persists the last-poll timestamp for the given
// rule ID so that events are not re-triggered after a restart.
func (s *StateStore) UpdateWatcherRuleCheckpoint(id string, lastPoll time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := lastPoll.UTC().Format(time.RFC3339)
	for i, r := range s.state.WatcherRules {
		if r.ID == id {
			s.state.WatcherRules[i].LastPoll = ts
			s.dirty = true
			return
		}
	}
}

// RemoveWatcherRule removes a watcher rule by ID.
func (s *StateStore) RemoveWatcherRule(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rules := s.state.WatcherRules[:0]
	for _, r := range s.state.WatcherRules {
		if r.ID != id {
			rules = append(rules, r)
		}
	}
	s.state.WatcherRules = rules
	s.dirty = true
}

// GetWatcherRules returns a copy of the persisted watcher rule list.
func (s *StateStore) GetWatcherRules() []PersistedWatchRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.state.WatcherRules) == 0 {
		return nil
	}
	out := make([]PersistedWatchRule, len(s.state.WatcherRules))
	copy(out, s.state.WatcherRules)
	return out
}

// copyFile copies src to dst with restricted permissions (0600), overwriting dst if it exists.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
