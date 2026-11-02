package memory //nolint:revive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProjectMemory manages the project-level memory layer.
// Memory is stored in .nano/NANO.md; rules live in .nano/rules/*.md.
type ProjectMemory struct {
	workingDir string
}

// NewProjectMemory creates a ProjectMemory rooted at workingDir.
func NewProjectMemory(workingDir string) *ProjectMemory {
	return &ProjectMemory{workingDir: workingDir}
}

// memoryFile returns the path to .nano/NANO.md.
// This is the single source-of-truth for project session memory;
// project-level instructions live in NANO.md (root) and are managed
// separately by InstructionLoader.
func (p *ProjectMemory) memoryFile() string {
	return filepath.Join(p.workingDir, ".nano", "NANO.md")
}

// rulesDir returns the path to .nano/rules/.
func (p *ProjectMemory) rulesDir() string {
	return filepath.Join(p.workingDir, ".nano", "rules")
}

// Read returns the content of .nano/MEMORY.md (empty string if missing).
func (p *ProjectMemory) Read() (string, error) {
	data, err := os.ReadFile(p.memoryFile())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("project memory: read: %w", err)
	}
	return string(data), nil
}

// Append adds a new entry to .nano/MEMORY.md with a timestamp header.
func (p *ProjectMemory) Append(content string) error {
	f := p.memoryFile()
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return fmt.Errorf("project memory: mkdir: %w", err)
	}
	entry := fmt.Sprintf("\n## %s\n\n%s\n", time.Now().Format("2006-01-02 15:04:05"), strings.TrimSpace(content))
	fh, err := os.OpenFile(f, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("project memory: open: %w", err)
	}
	defer fh.Close() //nolint:errcheck
	_, err = fh.WriteString(entry)
	return err
}

// WriteRule creates or overwrites .nano/memory/rules/<name>.md.
func (p *ProjectMemory) WriteRule(name, content string) error {
	if err := os.MkdirAll(p.rulesDir(), 0o755); err != nil {
		return fmt.Errorf("project memory: mkdir rules: %w", err)
	}
	path := filepath.Join(p.rulesDir(), name+".md")
	return os.WriteFile(path, []byte(content), 0o644)
}

// ReadRules reads all .md files from .nano/memory/rules/ and returns their combined content.
func (p *ProjectMemory) ReadRules() (string, error) {
	entries, err := os.ReadDir(p.rulesDir())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("project memory: read rules dir: %w", err)
	}
	var sb strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(p.rulesDir(), entry.Name()))
		if err != nil {
			continue
		}
		sb.WriteString("### ")
		sb.WriteString(strings.TrimSuffix(entry.Name(), ".md"))
		sb.WriteString("\n\n")
		sb.Write(data)
		sb.WriteString("\n\n")
	}
	return sb.String(), nil
}

// Summary returns a combined summary of project memory for system prompt injection.
func (p *ProjectMemory) Summary() string {
	var parts []string
	if content, err := p.Read(); err == nil && content != "" {
		parts = append(parts, "## Project Memory\n\n"+content)
	}
	if rules, err := p.ReadRules(); err == nil && rules != "" {
		parts = append(parts, "## Memory Rules\n\n"+rules)
	}
	return strings.Join(parts, "\n\n")
}
