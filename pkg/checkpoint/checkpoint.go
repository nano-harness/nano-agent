// Package checkpoint implements snapshot/restore of the agent's working tree
// so that destructive operations can be rolled back. It is the M2-4
// counterpart of Claude Code's filesystem checkpointing feature.
//
// Two strategies are supported:
//
//  1. Git-stash strategy (default when the working dir is a git repo):
//     `git stash create` produces a stash object whose tree captures both
//     tracked and untracked changes. Stashes are addressable by SHA so we do
//     not need to manage our own object database.
//
//  2. File-copy strategy (fallback for non-git directories): the working tree
//     is copied verbatim into ~/.nano/checkpoints/<id>/ for later restoration.
//
// The checkpointer is opt-in via configuration and is NOT a substitute for
// version control — it is bounded by retention policy (count, size, age) and
// always writes outside the working tree.
package checkpoint

import (
	"errors"
	"time"
)

// ErrNotFound indicates a checkpoint with the given ID does not exist.
var ErrNotFound = errors.New("checkpoint not found")

// Strategy identifies how a snapshot was captured.
type Strategy string

const (
	StrategyGitStash Strategy = "git-stash"
	StrategyFileCopy Strategy = "file-copy"
)

// Checkpoint is the metadata record for a single snapshot.
type Checkpoint struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	Reason     string    `json:"reason"`
	WorkingDir string    `json:"working_dir"`
	Strategy   Strategy  `json:"strategy"`
	GitStash   string    `json:"git_stash,omitempty"`   // sha for StrategyGitStash
	BackupPath string    `json:"backup_path,omitempty"` // path for StrategyFileCopy
	SizeBytes  int64     `json:"size_bytes,omitempty"`
	Tool       string    `json:"tool,omitempty"`
}

// Manager is the public surface every CLI/TUI uses.
type Manager interface {
	Snapshot(reason string, tool string) (*Checkpoint, error)
	List() ([]*Checkpoint, error)
	Restore(id string) error
	Delete(id string) error
	Cleanup() error
}
