package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

func cleanupTranscriptFile(sessionID string) {
	if sessionID == "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	baseDir := filepath.Join(home, ".nano-agent", "sessions")
	sessionDir := filepath.Clean(filepath.Join(baseDir, sanitizeSessionIDForPath(sessionID)))
	if sessionDir == baseDir || !strings.HasPrefix(sessionDir, baseDir+string(os.PathSeparator)) {
		return
	}
	_ = os.RemoveAll(sessionDir)
}

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

// CleanupCandidate describes a session that would be removed by
// CleanupOldSessions. It is the unit of result returned by
// PreviewCleanupCandidates and CleanupAllProjectsPreview so callers can
// implement --dry-run output.
type CleanupCandidate struct {
	ProjectDir string `json:"project_dir,omitempty"`
	SessionID  string `json:"session_id"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	Reason     string `json:"reason"`
}

// PreviewCleanupCandidates computes the set of sessions that
// CleanupOldSessions would delete for the given storage, without performing
// any deletion. The reasons returned mirror the cleanup heuristics:
//
//   - "idle_ttl"      – session UpdatedAt is older than MaxSessionAge.
//   - "max_per_project" – session falls outside the most-recent
//     MaxSessionsPerProject window.
//
// When a session matches both heuristics the first one observed is reported.
func PreviewCleanupCandidates(storage *ProjectSessionStorage) ([]CleanupCandidate, error) {
	if storage == nil {
		return nil, fmt.Errorf("storage is nil")
	}
	infos, err := storage.ListSessionInfos()
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	if len(infos) == 0 {
		return nil, nil
	}

	now := time.Now()
	candidates := make([]CleanupCandidate, 0)
	seen := make(map[string]bool)

	for _, info := range infos {
		modifiedAt, err := time.Parse(time.RFC3339, info.UpdatedAt)
		if err != nil {
			continue
		}
		if now.Sub(modifiedAt) > MaxSessionAge {
			candidates = append(candidates, CleanupCandidate{
				SessionID: info.ID,
				UpdatedAt: info.UpdatedAt,
				Reason:    "idle_ttl",
			})
			seen[info.ID] = true
		}
	}

	if len(infos) > MaxSessionsPerProject {
		sorted := append([]SessionInfo(nil), infos...)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].UpdatedAt > sorted[j].UpdatedAt
		})
		for i := MaxSessionsPerProject; i < len(sorted); i++ {
			if seen[sorted[i].ID] {
				continue
			}
			candidates = append(candidates, CleanupCandidate{
				SessionID: sorted[i].ID,
				UpdatedAt: sorted[i].UpdatedAt,
				Reason:    "max_per_project",
			})
			seen[sorted[i].ID] = true
		}
	}

	return candidates, nil
}

// CleanupAllProjectsPreview is the dry-run analogue of CleanupAllProjects:
// it returns the union of cleanup candidates across every project under
// ~/.nano/projects without modifying any files.
func CleanupAllProjectsPreview() ([]CleanupCandidate, error) {
	return previewAllProjects("")
}

// CleanupAllProjectsByReason deletes cleanup candidates whose reason matches
// the supplied reason. Valid reasons currently mirror PreviewCleanupCandidates:
// "idle_ttl" and "max_per_project".
func CleanupAllProjectsByReason(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return CleanupAllProjects()
	}
	return cleanupAllProjectsByReason(reason)
}

func previewAllProjects(reason string) ([]CleanupCandidate, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	projectsDir := filepath.Join(home, ".nano", "projects")
	if _, err := os.Stat(projectsDir); os.IsNotExist(err) {
		return nil, nil
	}
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read projects directory: %w", err)
	}

	var all []CleanupCandidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectDir := filepath.Join(projectsDir, entry.Name())
		sessionsDir := filepath.Join(projectDir, "sessions")
		if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
			continue
		}
		storage := &ProjectSessionStorage{
			projectDir:  projectDir,
			sessionsDir: sessionsDir,
			indexPath:   filepath.Join(projectDir, "sessions-index.json"),
		}
		cands, err := PreviewCleanupCandidates(storage)
		if err != nil {
			logger.Warnf("preview cleanup for project %s: %v", entry.Name(), err)
			continue
		}
		for i := range cands {
			if reason != "" && cands[i].Reason != reason {
				continue
			}
			cands[i].ProjectDir = projectDir
			all = append(all, cands[i])
		}
	}
	return all, nil
}

func cleanupAllProjectsByReason(reason string) error {
	cands, err := previewAllProjects(reason)
	if err != nil {
		return err
	}
	for _, cand := range cands {
		if cand.ProjectDir == "" {
			continue
		}
		storage := &ProjectSessionStorage{
			projectDir:  cand.ProjectDir,
			sessionsDir: filepath.Join(cand.ProjectDir, "sessions"),
			indexPath:   filepath.Join(cand.ProjectDir, "sessions-index.json"),
		}
		if err := storage.DeleteSession(cand.SessionID); err != nil {
			logger.Warnf("Failed to delete session %s from project %s: %v", cand.SessionID, cand.ProjectDir, err)
		} else {
			logger.Infof("Cleaned up session: %s (reason=%s)", cand.SessionID, cand.Reason)
		}
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
