package agent

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Core types needed for agentic flow

// WorkContext represents the working context
type WorkContext struct {
	WorkingDirectory string
	Files            []string
	Directories      []string
	ProjectType      string
	Technologies     []string
	RecentFiles      []string
	GitStatus        *GitStatus
	Warnings         []string
}

// GitStatus represents git repository status
type GitStatus struct {
	Branch        string
	HasChanges    bool
	Staged        []string
	Modified      []string
	Untracked     []string
	ModifiedCount int
}

// ContextAnalyzer analyzes working context
type ContextAnalyzer struct{}

// NewContextAnalyzer creates a new context analyzer
func NewContextAnalyzer() *ContextAnalyzer {
	return &ContextAnalyzer{}
}

// AnalyzeContext analyzes the working context without failing prompt construction
// when optional filesystem or git probes are unavailable.
func (ca *ContextAnalyzer) AnalyzeContext(workingDir string) (*WorkContext, error) {
	workContext := &WorkContext{
		Files:        make([]string, 0),
		Directories:  make([]string, 0),
		Technologies: make([]string, 0),
		RecentFiles:  make([]string, 0),
		Warnings:     make([]string, 0),
	}

	wd := workingDir
	if wd == "" {
		current, err := getCurrentWorkingDir()
		if err != nil {
			workContext.Warnings = append(workContext.Warnings, err.Error())
			return workContext, nil
		}
		wd = current
	}
	if abs, err := filepath.Abs(wd); err == nil {
		wd = abs
	} else {
		workContext.Warnings = append(workContext.Warnings, err.Error())
	}
	workContext.WorkingDirectory = wd

	workContext.ProjectType = detectProjectType(wd)
	if workContext.ProjectType != "" && workContext.ProjectType != "unknown" {
		workContext.Technologies = []string{workContext.ProjectType}
	}

	// File inventory is intentionally not populated on the default prompt path.
	// Tools provide on-demand file access, and eager workspace walks can be
	// expensive in large projects. Call AnalyzeContextWithFiles when needed.

	gitStatus, err := analyzeGitStatus(wd)
	if err != nil {
		workContext.Warnings = append(workContext.Warnings, err.Error())
	} else {
		workContext.GitStatus = gitStatus
	}

	return workContext, nil
}

// AnalyzeContextWithFiles preserves legacy behavior for callers that explicitly
// need a bounded file inventory. It uses the git-first workspace scanner.
func (ca *ContextAnalyzer) AnalyzeContextWithFiles(ctx context.Context, workingDir string, maxFiles int) (*WorkContext, error) {
	workContext, err := ca.AnalyzeContext(workingDir)
	if err != nil {
		return nil, err
	}
	files, err := scanWorkspaceFiles(ctx, workContext.WorkingDirectory, maxFiles)
	if err != nil {
		workContext.Warnings = append(workContext.Warnings, err.Error())
		return workContext, nil
	}
	workContext.Files = files
	workContext.RecentFiles = files
	return workContext, nil
}

// getCurrentWorkingDir returns the current working directory
func getCurrentWorkingDir() (string, error) {
	return os.Getwd()
}

// detectProjectType detects project type based on files in directory
func detectProjectType(dir string) string {
	// Check for common project files
	files := []string{
		"go.mod",                                         // Go
		"package.json",                                   // JavaScript/Node.js
		"requirements.txt", "setup.py", "pyproject.toml", // Python
		"pom.xml", "build.gradle", // Java
		"Cargo.toml",                 // Rust
		"Makefile", "CMakeLists.txt", // C/C++
	}

	for _, file := range files {
		if _, err := os.Stat(filepath.Join(dir, file)); err == nil {
			switch file {
			case "go.mod":
				return "go"
			case "package.json":
				return "javascript"
			case "requirements.txt", "setup.py", "pyproject.toml":
				return "python"
			case "pom.xml", "build.gradle":
				return "java"
			case "Cargo.toml":
				return "rust"
			case "Makefile", "CMakeLists.txt":
				return "c/cpp"
			}
		}
	}

	return "unknown"
}

func analyzeGitStatus(dir string) (*GitStatus, error) {
	if _, err := runGitCommand(dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, err
	}

	status := &GitStatus{}
	if branch, err := runGitCommand(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		status.Branch = strings.TrimSpace(string(branch))
	} else if branch, err := runGitCommand(dir, "symbolic-ref", "--short", "HEAD"); err == nil {
		status.Branch = strings.TrimSpace(string(branch))
	}
	output, err := runGitCommand(dir, "status", "--porcelain")
	if err != nil {
		return status, err
	}
	parseGitStatus(status, output)
	return status, nil
}

func runGitCommand(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Output()
}

func parseGitStatus(status *GitStatus, output []byte) {
	for _, line := range bytes.Split(bytes.TrimSpace(output), []byte{'\n'}) {
		if len(line) < 3 {
			continue
		}
		path := strings.TrimSpace(string(line[3:]))
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = parts[len(parts)-1]
		}
		indexStatus := line[0]
		worktreeStatus := line[1]
		if indexStatus == '?' && worktreeStatus == '?' {
			status.Untracked = append(status.Untracked, path)
			continue
		}
		if indexStatus != ' ' {
			status.Staged = append(status.Staged, path)
		}
		if worktreeStatus != ' ' {
			status.Modified = append(status.Modified, path)
		}
	}
	status.HasChanges = len(status.Staged) > 0 || len(status.Modified) > 0 || len(status.Untracked) > 0
	status.ModifiedCount = len(status.Staged) + len(status.Modified) + len(status.Untracked)
}
