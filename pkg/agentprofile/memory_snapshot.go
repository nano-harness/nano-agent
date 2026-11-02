package agentprofile

import (
	"os"
	"path/filepath"
	"strings"
)

// SnapshotState describes the state of a memory snapshot for an agent.
type SnapshotState string

const (
	SnapshotNone         SnapshotState = "none"
	SnapshotInitialize   SnapshotState = "initialize"
	SnapshotPromptUpdate SnapshotState = "prompt-update"
)

// SnapshotDirs returns the snapshot directory path.
func SnapshotDirs(projectRoot string) string {
	return filepath.Join(projectRoot, ".claude", "agent-memory-snapshots")
}

// GetSnapshotState determines the current state of the snapshot for an agent.
func GetSnapshotState(projectRoot, agentName string) SnapshotState {
	snapshotDir := SnapshotDirs(projectRoot)
	snapshotFile := filepath.Join(snapshotDir, agentName+".md")

	if _, err := os.Stat(snapshotFile); os.IsNotExist(err) {
		return SnapshotNone
	}

	// Check if project memory differs from snapshot
	projectMemory := loadMemoryFile(filepath.Join(projectRoot, ".nano", "agent-memory"), agentName)
	snapshotContent, err := os.ReadFile(snapshotFile)
	if err != nil {
		return SnapshotInitialize
	}

	if strings.TrimSpace(projectMemory) != strings.TrimSpace(string(snapshotContent)) {
		return SnapshotPromptUpdate
	}

	return SnapshotInitialize
}

// InitializeSnapshot creates or updates the snapshot file from current project memory.
func InitializeSnapshot(projectRoot, agentName string) error {
	snapshotDir := SnapshotDirs(projectRoot)
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return err
	}

	projectMemory := loadMemoryFile(filepath.Join(projectRoot, ".nano", "agent-memory"), agentName)
	snapshotFile := filepath.Join(snapshotDir, agentName+".md")
	return os.WriteFile(snapshotFile, []byte(projectMemory+"\n"), 0644)
}

// ReplaceFromSnapshot replaces project memory with the snapshot content.
func ReplaceFromSnapshot(projectRoot, agentName string) error {
	snapshotDir := SnapshotDirs(projectRoot)
	snapshotFile := filepath.Join(snapshotDir, agentName+".md")

	data, err := os.ReadFile(snapshotFile)
	if err != nil {
		return err
	}

	projectMemoryDir := filepath.Join(projectRoot, ".nano", "agent-memory")
	if err := os.MkdirAll(projectMemoryDir, 0755); err != nil {
		return err
	}

	projectFile := filepath.Join(projectMemoryDir, agentName+".md")
	return os.WriteFile(projectFile, data, 0644)
}
