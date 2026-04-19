package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

const (
	// MaxSessionAge is the maximum age of a session before cleanup (30 days)
	MaxSessionAge = 30 * 24 * time.Hour
	// MaxSessionsPerProject is the maximum number of sessions to keep per project
	MaxSessionsPerProject = 50
)

// CleanupOldSessions removes old sessions based on age and count limits
func CleanupOldSessions(storage *ProjectSessionStorage) error {
	if storage == nil {
		return fmt.Errorf("storage is nil")
	}

	// Get all sessions
	infos, err := storage.ListSessionInfos()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(infos) == 0 {
		return nil
	}

	now := time.Now()
	var toDelete []string

	// Age-based cleanup: delete sessions older than MaxSessionAge
	for _, info := range infos {
		modifiedAt, err := time.Parse(time.RFC3339, info.UpdatedAt)
		if err != nil {
			logger.Warnf("Failed to parse session modification time: %v", err)
			continue
		}

		if now.Sub(modifiedAt) > MaxSessionAge {
			toDelete = append(toDelete, info.ID)
		}
	}

	// Count-based cleanup: keep only the most recent MaxSessionsPerProject sessions
	if len(infos) > MaxSessionsPerProject {
		// Sort by modification time (newest first)
		sort.Slice(infos, func(i, j int) bool {
			return infos[i].UpdatedAt > infos[j].UpdatedAt
		})

		// Mark sessions beyond the limit for deletion
		for i := MaxSessionsPerProject; i < len(infos); i++ {
			// Avoid duplicates
			alreadyMarked := false
			for _, id := range toDelete {
				if id == infos[i].ID {
					alreadyMarked = true
					break
				}
			}
			if !alreadyMarked {
				toDelete = append(toDelete, infos[i].ID)
			}
		}
	}

	// Delete marked sessions
	for _, id := range toDelete {
		if err := storage.DeleteSession(id); err != nil {
			logger.Warnf("Failed to delete session %s: %v", id, err)
		} else {
			logger.Infof("Cleaned up session: %s", id)
		}
	}

	if len(toDelete) > 0 {
		logger.Infof("Cleaned up %d sessions from project", len(toDelete))
	}

	return nil
}

// CleanupAllProjects cleans up sessions across all projects
func CleanupAllProjects() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	projectsDir := filepath.Join(home, ".nano", "projects")

	// Check if projects directory exists
	if _, err := os.Stat(projectsDir); os.IsNotExist(err) {
		return nil // No projects to clean up
	}

	// List all project directories
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return fmt.Errorf("failed to read projects directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectDir := filepath.Join(projectsDir, entry.Name())
		sessionsDir := filepath.Join(projectDir, "sessions")

		// Check if sessions directory exists
		if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
			continue
		}

		// Create a temporary storage instance for this project
		storage := &ProjectSessionStorage{
			projectDir:  projectDir,
			sessionsDir: sessionsDir,
			indexPath:   filepath.Join(projectDir, "sessions-index.json"),
		}

		// Clean up sessions for this project
		if err := CleanupOldSessions(storage); err != nil {
			logger.Warnf("Failed to cleanup sessions in project %s: %v", entry.Name(), err)
		}
	}

	return nil
}
