// Package team provides team metadata management and file layout for multi-agent swarm systems.
//
// This package implements the Go equivalent of Claude Code's teamHelpers.ts,
// managing team configuration, member lifecycles, and persistent storage.
package team

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	nanoruntime "github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/gofrs/flock"
)

// Kind constants define teammate execution modes
const (
	KindInProcess  = "in_process" // Teammate runs in the same process (goroutine)
	KindSubprocess = "subprocess" // Teammate runs in a separate subprocess (tmux/iTerm2)
)

// Team represents a multi-agent team configuration
type Team struct {
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	CreatedAt     time.Time    `json:"createdAt"`
	LeadAgentID   string       `json:"leadAgentId"`   // Fully qualified ID like "team-lead@alpha"
	LeadSessionID string       `json:"leadSessionId"` // Session ID of the lead agent
	Members       []TeamMember `json:"members"`
}

// TeamMember represents a teammate in the swarm
type TeamMember struct {
	AgentID    string `json:"agentId"`              // Fully qualified ID like "researcher@alpha"
	Name       string `json:"name"`                 // Short name like "researcher"
	Color      string `json:"color,omitempty"`      // Optional UI color
	Mode       string `json:"mode,omitempty"`       // Default permission mode
	IsActive   bool   `json:"isActive"`             // Whether the teammate is currently active
	TmuxPaneID string `json:"tmuxPaneId,omitempty"` // Tmux pane ID for subprocess teammates
	PID        int    `json:"pid,omitempty"`        // Process ID for subprocess teammates
	SessionID  string `json:"sessionId"`            // Session ID for this teammate
	Kind       string `json:"kind"`                 // KindInProcess or KindSubprocess
}

// TeamsRoot returns the root directory for all teams (~/.nano/teams)
func TeamsRoot() string {
	return nanoruntime.TeamsDir()
}

// TeamDir returns the directory for a specific team
func TeamDir(name string) string {
	return filepath.Join(TeamsRoot(), name)
}

// ConfigPath returns the path to a team's config file
func ConfigPath(name string) string {
	return filepath.Join(TeamDir(name), "config.json")
}

// InboxPath returns the path to a teammate's inbox file
func InboxPath(team, agent string) string {
	return filepath.Join(TeamDir(team), "inboxes", agent+".json")
}

// LockPath returns the path to a team's lock file
func lockPath(name string) string {
	return filepath.Join(TeamDir(name), ".lock")
}

// ReadTeam reads a team configuration from disk
func ReadTeam(name string) (*Team, error) {
	if name == "" {
		return nil, fmt.Errorf("team name cannot be empty")
	}

	path := ConfigPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("team %q does not exist", name)
		}
		return nil, fmt.Errorf("failed to read team config: %w", err)
	}

	var team Team
	if err := json.Unmarshal(data, &team); err != nil {
		return nil, fmt.Errorf("failed to parse team config: %w", err)
	}

	return &team, nil
}

// WriteTeam writes a team configuration to disk with file locking
func WriteTeam(t *Team) error {
	if t == nil {
		return fmt.Errorf("team is nil")
	}
	if t.Name == "" {
		return fmt.Errorf("team name cannot be empty")
	}

	// Ensure team directory exists BEFORE acquiring lock
	teamDir := TeamDir(t.Name)
	if err := os.MkdirAll(teamDir, 0755); err != nil {
		return fmt.Errorf("failed to create team directory: %w", err)
	}

	// Acquire lock (blocking)
	lock := flock.New(lockPath(t.Name))
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	return writeTeamUnlocked(t)
}

// writeTeamUnlocked writes a team configuration without acquiring a lock.
// Caller must hold the lock.
func writeTeamUnlocked(t *Team) error {
	if t == nil {
		return fmt.Errorf("team is nil")
	}
	if t.Name == "" {
		return fmt.Errorf("team name cannot be empty")
	}

	// Ensure team directory exists
	teamDir := TeamDir(t.Name)
	if err := os.MkdirAll(teamDir, 0755); err != nil {
		return fmt.Errorf("failed to create team directory: %w", err)
	}

	// Ensure inboxes directory exists
	inboxesDir := filepath.Join(teamDir, "inboxes")
	if err := os.MkdirAll(inboxesDir, 0755); err != nil {
		return fmt.Errorf("failed to create inboxes directory: %w", err)
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal team config: %w", err)
	}

	// Write atomically using a temp file
	path := ConfigPath(t.Name)
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp config file: %w", err)
	}

	return nil
}

// CreateTeam creates a new team with the given parameters
func CreateTeam(name, desc, leadAgentID, leadSessionID string) (*Team, error) {
	if name == "" {
		return nil, fmt.Errorf("team name cannot be empty")
	}
	if leadAgentID == "" {
		return nil, fmt.Errorf("lead agent ID cannot be empty")
	}
	if leadSessionID == "" {
		return nil, fmt.Errorf("lead session ID cannot be empty")
	}

	// Check if team already exists
	if _, err := ReadTeam(name); err == nil {
		return nil, fmt.Errorf("team %q already exists", name)
	}

	team := &Team{
		Name:          name,
		Description:   desc,
		CreatedAt:     time.Now(),
		LeadAgentID:   leadAgentID,
		LeadSessionID: leadSessionID,
		Members:       []TeamMember{},
	}

	if err := WriteTeam(team); err != nil {
		return nil, err
	}

	return team, nil
}

// DeleteTeam removes a team and all its data
func DeleteTeam(name string) error {
	if name == "" {
		return fmt.Errorf("team name cannot be empty")
	}

	teamDir := TeamDir(name)
	if err := os.RemoveAll(teamDir); err != nil {
		return fmt.Errorf("failed to delete team directory: %w", err)
	}

	return nil
}

// AddMember adds a new member to the team
func AddMember(name string, m TeamMember) error {
	if name == "" {
		return fmt.Errorf("team name cannot be empty")
	}
	if m.AgentID == "" {
		return fmt.Errorf("member agent ID cannot be empty")
	}
	if m.Name == "" {
		return fmt.Errorf("member name cannot be empty")
	}

	// Ensure team directory exists BEFORE acquiring lock
	teamDir := TeamDir(name)
	if err := os.MkdirAll(teamDir, 0755); err != nil {
		return fmt.Errorf("failed to create team directory: %w", err)
	}

	// Acquire lock first
	lock := flock.New(lockPath(name))
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	// Read team under lock
	team, err := ReadTeam(name)
	if err != nil {
		return err
	}

	// Check if member already exists
	for _, existing := range team.Members {
		if existing.AgentID == m.AgentID {
			return fmt.Errorf("member with agent ID %q already exists", m.AgentID)
		}
	}

	team.Members = append(team.Members, m)

	// Write under the same lock (WriteTeam will try to acquire lock again,
	// but since we already hold it with gofrs/flock, it should succeed as
	// file locks are reentrant for the same process)
	return writeTeamUnlocked(team)
}

// RemoveMemberByAgentID removes a member from the team by agent ID
func RemoveMemberByAgentID(name, agentID string) error {
	if name == "" {
		return fmt.Errorf("team name cannot be empty")
	}
	if agentID == "" {
		return fmt.Errorf("agent ID cannot be empty")
	}

	// Ensure team directory exists BEFORE acquiring lock
	teamDir := TeamDir(name)
	if err := os.MkdirAll(teamDir, 0755); err != nil {
		return fmt.Errorf("failed to create team directory: %w", err)
	}

	// Acquire lock first
	lock := flock.New(lockPath(name))
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	team, err := ReadTeam(name)
	if err != nil {
		return err
	}

	// Filter out the member
	filtered := make([]TeamMember, 0, len(team.Members))
	found := false
	for _, m := range team.Members {
		if m.AgentID == agentID {
			found = true
			continue
		}
		filtered = append(filtered, m)
	}

	if !found {
		return fmt.Errorf("member with agent ID %q not found", agentID)
	}

	team.Members = filtered
	return writeTeamUnlocked(team)
}

// SetMemberActive sets the active status of a member
func SetMemberActive(name, agentID string, active bool) error {
	if name == "" {
		return fmt.Errorf("team name cannot be empty")
	}
	if agentID == "" {
		return fmt.Errorf("agent ID cannot be empty")
	}

	// Ensure team directory exists BEFORE acquiring lock
	teamDir := TeamDir(name)
	if err := os.MkdirAll(teamDir, 0755); err != nil {
		return fmt.Errorf("failed to create team directory: %w", err)
	}

	// Acquire lock first
	lock := flock.New(lockPath(name))
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	team, err := ReadTeam(name)
	if err != nil {
		return err
	}

	found := false
	for i := range team.Members {
		if team.Members[i].AgentID == agentID {
			team.Members[i].IsActive = active
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("member with agent ID %q not found", agentID)
	}

	return writeTeamUnlocked(team)
}

// SetMemberMode sets the permission mode of a member
func SetMemberMode(name, agentID, mode string) error {
	if name == "" {
		return fmt.Errorf("team name cannot be empty")
	}
	if agentID == "" {
		return fmt.Errorf("agent ID cannot be empty")
	}

	// Ensure team directory exists BEFORE acquiring lock
	teamDir := TeamDir(name)
	if err := os.MkdirAll(teamDir, 0755); err != nil {
		return fmt.Errorf("failed to create team directory: %w", err)
	}

	// Acquire lock first
	lock := flock.New(lockPath(name))
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	team, err := ReadTeam(name)
	if err != nil {
		return err
	}

	found := false
	for i := range team.Members {
		if team.Members[i].AgentID == agentID {
			team.Members[i].Mode = mode
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("member with agent ID %q not found", agentID)
	}

	return writeTeamUnlocked(team)
}

// ListAllTeams returns all teams in the teams directory
func ListAllTeams() ([]*Team, error) {
	root := TeamsRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Team{}, nil
		}
		return nil, fmt.Errorf("failed to read teams directory: %w", err)
	}

	teams := make([]*Team, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		team, err := ReadTeam(entry.Name())
		if err != nil {
			// Skip teams that can't be read
			continue
		}
		teams = append(teams, team)
	}

	return teams, nil
}
