package swarm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeHandle represents an active git worktree for agent isolation.
type WorktreeHandle struct {
	Path       string // Absolute path to the worktree
	BranchName string // Branch name used
	BaseDir    string // Original repository path
}

// CreateAgentWorktree creates a git worktree for isolated agent execution.
// Returns nil, nil if the project is not a git repository (graceful degradation).
func CreateAgentWorktree(baseDir, agentID string) (*WorktreeHandle, error) {
	// Check if this is a git repo
	if !isGitRepo(baseDir) {
		return nil, nil // Silent degradation for non-git projects
	}

	// Generate a unique branch/worktree name
	safeName := sanitizeWorktreeName(agentID)
	branchName := fmt.Sprintf("agent/%s", safeName)
	worktreePath := filepath.Join(os.TempDir(), "nano-agent-worktrees", safeName)

	// Clean up any existing worktree at this path
	_ = removeWorktree(baseDir, worktreePath)

	// Create the worktree directory
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktree parent dir: %w", err)
	}

	// Create worktree with a new branch from HEAD
	cmd := exec.Command("git", "worktree", "add", "-b", branchName, worktreePath, "HEAD")
	cmd.Dir = baseDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If branch already exists, try without -b
		cmd = exec.Command("git", "worktree", "add", worktreePath, "HEAD")
		cmd.Dir = baseDir
		output, err = cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("git worktree add failed: %s: %w", strings.TrimSpace(string(output)), err)
		}
	}

	return &WorktreeHandle{
		Path:       worktreePath,
		BranchName: branchName,
		BaseDir:    baseDir,
	}, nil
}

// CleanupAgentWorktree removes a worktree. If there are uncommitted changes,
// the worktree is preserved and an error is returned.
func CleanupAgentWorktree(handle *WorktreeHandle) error {
	if handle == nil {
		return nil
	}

	// Check for uncommitted changes
	if hasUncommittedChanges(handle.Path) {
		return fmt.Errorf("worktree %s has uncommitted changes, preserving", handle.Path)
	}

	// Remove the worktree
	if err := removeWorktree(handle.BaseDir, handle.Path); err != nil {
		return err
	}

	// Try to delete the branch (best effort)
	if handle.BranchName != "" {
		cmd := exec.Command("git", "branch", "-d", handle.BranchName)
		cmd.Dir = handle.BaseDir
		_ = cmd.Run()
	}

	return nil
}

func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run() == nil
}

func hasUncommittedChanges(dir string) bool {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return true // Assume changes on error
	}
	return len(strings.TrimSpace(string(output))) > 0
}

func removeWorktree(baseDir, worktreePath string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", worktreePath)
	cmd.Dir = baseDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: just remove the directory
		if removeErr := os.RemoveAll(worktreePath); removeErr != nil {
			return fmt.Errorf("git worktree remove failed (%s) and cleanup failed: %w", strings.TrimSpace(string(output)), removeErr)
		}
		// Prune orphaned worktree references
		pruneCmd := exec.Command("git", "worktree", "prune")
		pruneCmd.Dir = baseDir
		_ = pruneCmd.Run()
	}
	return nil
}

func sanitizeWorktreeName(name string) string {
	// Replace invalid characters for branch names
	replacer := strings.NewReplacer(
		"@", "-",
		"/", "-",
		" ", "-",
		":", "-",
		"..", "-",
	)
	return replacer.Replace(name)
}
