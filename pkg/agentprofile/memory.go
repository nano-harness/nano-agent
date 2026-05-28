package agentprofile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MemoryLevel represents the scope of agent memory.
type MemoryLevel string

const (
	MemoryLevelUser    MemoryLevel = "user"
	MemoryLevelProject MemoryLevel = "project"
	MemoryLevelLocal   MemoryLevel = "local"
)

// MemoryDirs returns the three-level directory paths for agent memory.
type MemoryDirs struct {
	UserDir    string // ~/.nano/agent-memory/
	ProjectDir string // <projectRoot>/.nano/agent-memory/
	LocalDir   string // <projectRoot>/.nano/agent-memory-local/ (gitignored)
}

// ComputeMemoryDirs computes the memory directories for a given project root.
func ComputeMemoryDirs(projectRoot string) MemoryDirs {
	homeDir, _ := os.UserHomeDir()
	return MemoryDirs{
		UserDir:    filepath.Join(homeDir, ".nano", "agent-memory"),
		ProjectDir: filepath.Join(projectRoot, ".nano", "agent-memory"),
		LocalDir:   filepath.Join(projectRoot, ".nano", "agent-memory-local"),
	}
}

// LoadAgentMemoryPrompt reads memory files from all three levels and returns
// a formatted string suitable for system prompt injection.
func LoadAgentMemoryPrompt(dirs MemoryDirs, agentName string) string {
	var sections []string

	// Load from each level (user < project < local priority)
	levels := []struct {
		label string
		dir   string
	}{
		{"user", dirs.UserDir},
		{"project", dirs.ProjectDir},
		{"local", dirs.LocalDir},
	}

	for _, level := range levels {
		content := loadMemoryFile(level.dir, agentName)
		if content != "" {
			sections = append(sections, fmt.Sprintf("## %s memory\n%s", level.label, content))
		}
	}

	if len(sections) == 0 {
		return ""
	}
	return "# Agent Memory\n\n" + strings.Join(sections, "\n\n")
}

// WriteAgentMemory appends a memory entry to the specified level.
func WriteAgentMemory(dirs MemoryDirs, level MemoryLevel, agentName, content string) error {
	dir := memoryDirForLevel(dirs, level)
	if dir == "" {
		return fmt.Errorf("unknown memory level: %s", level)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, agentName+".md")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content + "\n")
	return err
}

// ReadAgentMemory reads the memory file for a specific agent at a given level.
func ReadAgentMemory(dirs MemoryDirs, level MemoryLevel, agentName string) (string, error) {
	dir := memoryDirForLevel(dirs, level)
	if dir == "" {
		return "", fmt.Errorf("unknown memory level: %s", level)
	}
	return loadMemoryFile(dir, agentName), nil
}

func memoryDirForLevel(dirs MemoryDirs, level MemoryLevel) string {
	switch level {
	case MemoryLevelUser:
		return dirs.UserDir
	case MemoryLevelProject:
		return dirs.ProjectDir
	case MemoryLevelLocal:
		return dirs.LocalDir
	default:
		return ""
	}
}

func loadMemoryFile(dir, agentName string) string {
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, agentName+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
