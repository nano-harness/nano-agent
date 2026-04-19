package patch //nolint:revive

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Generator handles patch generation for SWE-bench evaluation
type Generator struct {
	projectPath string
	baseCommit  string
}

// NewGenerator creates a new patch generator
func NewGenerator(projectPath, baseCommit string) *Generator {
	return &Generator{
		projectPath: projectPath,
		baseCommit:  baseCommit,
	}
}

// GenerateGitDiff generates a git diff patch for the current changes
func (g *Generator) GenerateGitDiff() (string, error) {
	// Change to project directory
	originalDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %v", err)
	}

	defer func() {
		os.Chdir(originalDir) //nolint:errcheck
	}()

	if err := os.Chdir(g.projectPath); err != nil {
		return "", fmt.Errorf("failed to change to project directory: %v", err)
	}

	// Check if we're in a git repository
	if !g.isGitRepository() {
		return "", fmt.Errorf("not a git repository: %s", g.projectPath)
	}

	// Stage all changes
	if err := exec.Command("git", "add", "-A").Run(); err != nil {
		return "", fmt.Errorf("failed to stage changes: %v", err)
	}

	// Generate diff from staged changes
	var cmd *exec.Cmd
	if g.baseCommit != "" {
		cmd = exec.Command("git", "-c", "core.quotepath=false", "diff", "--no-color", "--cached", g.baseCommit)
	} else {
		cmd = exec.Command("git", "-c", "core.quotepath=false", "diff", "--no-color", "--cached")
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to generate git diff: %v", err)
	}

	return string(output), nil
}

// GenerateUnifiedDiff generates a unified diff for specific files
func (g *Generator) GenerateUnifiedDiff(files []string) (string, error) {
	if len(files) == 0 {
		return g.GenerateGitDiff()
	}

	// Change to project directory
	originalDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %v", err)
	}

	defer func() {
		os.Chdir(originalDir) //nolint:errcheck
	}()

	if err := os.Chdir(g.projectPath); err != nil {
		return "", fmt.Errorf("failed to change to project directory: %v", err)
	}

	if !g.isGitRepository() {
		return "", fmt.Errorf("not a git repository: %s", g.projectPath)
	}

	// Stage all changes
	if err := exec.Command("git", "add", "-A").Run(); err != nil {
		return "", fmt.Errorf("failed to stage changes: %v", err)
	}

	var allDiffs strings.Builder

	for _, file := range files {
		var cmd *exec.Cmd
		if g.baseCommit != "" {
			cmd = exec.Command("git", "-c", "core.quotepath=false", "diff", "--no-color", "--cached", g.baseCommit, "--", file)
		} else {
			cmd = exec.Command("git", "-c", "core.quotepath=false", "diff", "--no-color", "--cached", "--", file)
		}

		output, err := cmd.Output()
		if err != nil {
			continue // Skip files that can't be diffed
		}

		fileDiff := string(output)
		if strings.TrimSpace(fileDiff) != "" {
			allDiffs.WriteString(fileDiff)
		}
	}

	return allDiffs.String(), nil
}

// SavePatch saves the patch to a file
func (g *Generator) SavePatch(patch, outputPath string) error {
	// Create output directory if it doesn't exist
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	// Write patch to file
	if err := os.WriteFile(outputPath, []byte(patch), 0644); err != nil {
		return fmt.Errorf("failed to write patch file: %v", err)
	}

	return nil
}

// GetChangedFiles returns a list of files that have been modified
func (g *Generator) GetChangedFiles() ([]string, error) {
	// Change to project directory
	originalDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %v", err)
	}

	defer func() {
		os.Chdir(originalDir) //nolint:errcheck
	}()

	if err := os.Chdir(g.projectPath); err != nil {
		return nil, fmt.Errorf("failed to change to project directory: %v", err)
	}

	if !g.isGitRepository() {
		return nil, fmt.Errorf("not a git repository: %s", g.projectPath)
	}

	// Use porcelain status to include untracked files (??), modified (M), added (A), renamed (R), etc.
	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get git status: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Format: XY <path> or XY <old> -> <new>
		// We want the path on the right (new path for renames)
		// Remove status columns
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 3 {
			continue
		}
		entry := strings.TrimSpace(trimmed[3:])
		if strings.Contains(entry, " -> ") {
			parts := strings.Split(entry, " -> ")
			entry = parts[len(parts)-1]
		}
		files = append(files, entry)
	}

	return files, nil
}

// isGitRepository checks if the current directory is a git repository
func (g *Generator) isGitRepository() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// GetGitStatus returns the current git status
func (g *Generator) GetGitStatus() (string, error) {
	// Change to project directory
	originalDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %v", err)
	}

	defer func() {
		os.Chdir(originalDir) //nolint:errcheck
	}()

	if err := os.Chdir(g.projectPath); err != nil {
		return "", fmt.Errorf("failed to change to project directory: %v", err)
	}

	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git status: %v", err)
	}

	return string(output), nil
}

// StageAllChanges stages all changes for commit
func (g *Generator) StageAllChanges() error {
	// Change to project directory
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %v", err)
	}

	defer func() {
		os.Chdir(originalDir) //nolint:errcheck
	}()

	if err := os.Chdir(g.projectPath); err != nil {
		return fmt.Errorf("failed to change to project directory: %v", err)
	}

	cmd := exec.Command("git", "add", "-A")
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to stage changes: %v", err)
	}

	return nil
}
