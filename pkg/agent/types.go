package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// Core types needed for agentic flow

// IntentType represents different types of user intents
type IntentType int

const (
	// IntentUnknown represents an unknown user intent
	IntentUnknown IntentType = iota
	// IntentAnalyze represents an intent to analyze code
	IntentAnalyze
	// IntentGenerate represents an intent to generate code
	IntentGenerate
	// IntentModify represents an intent to modify code
	IntentModify
	// IntentFix represents an intent to fix code
	IntentFix
	// IntentImprove represents an intent to improve code
	IntentImprove
	// IntentDebug represents an intent to debug code
	IntentDebug
	// IntentRefactor represents an intent to refactor code
	IntentRefactor
	// IntentTest represents an intent to test code
	IntentTest
	// IntentDocument represents an intent to document code
	IntentDocument
	// IntentOptimize represents an intent to optimize code
	IntentOptimize
	// IntentWebSearch represents an intent to perform a web search
	IntentWebSearch
)

// String returns string representation of IntentType
func (it IntentType) String() string {
	switch it {
	case IntentAnalyze:
		return "analyze"
	case IntentGenerate:
		return "generate"
	case IntentModify:
		return "modify"
	case IntentFix:
		return "fix"
	case IntentImprove:
		return "improve"
	case IntentDebug:
		return "debug"
	case IntentRefactor:
		return "refactor"
	case IntentTest:
		return "test"
	case IntentDocument:
		return "document"
	case IntentOptimize:
		return "optimize"
	case IntentWebSearch:
		return "web_search"
	default:
		return "unknown"
	}
}

// Intent represents user's intent (simplified)
type Intent struct {
	Type        IntentType
	Description string
	Goal        string
}

// WorkContext represents the working context
type WorkContext struct {
	WorkingDirectory string
	Files            []string
	Directories      []string
	ProjectType      string
	Technologies     []string
	RecentFiles      []string
	GitStatus        *GitStatus
}

// GitStatus represents git repository status
type GitStatus struct {
	Branch     string
	HasChanges bool
	Staged     []string
	Modified   []string
	Untracked  []string
}

// ContextAnalyzer analyzes working context
type ContextAnalyzer struct {
	agent *Agent
}

// NewContextAnalyzer creates a new context analyzer
func NewContextAnalyzer(agent *Agent) *ContextAnalyzer {
	return &ContextAnalyzer{agent: agent}
}

// AnalyzeContext analyzes the working context (simplified implementation)
func (ca *ContextAnalyzer) AnalyzeContext(_ string, _ *Intent) (*WorkContext, error) {
	context := &WorkContext{
		Files:        make([]string, 0),
		Directories:  make([]string, 0),
		Technologies: make([]string, 0),
		RecentFiles:  make([]string, 0),
	}

	// Get current working directory
	wd, err := getCurrentWorkingDir()
	if err != nil {
		return nil, err
	}
	context.WorkingDirectory = wd

	// Quick scan for project type
	context.ProjectType = detectProjectType(wd)
	context.Technologies = []string{context.ProjectType}

	// Simple file count (avoid expensive directory traversal)
	maxFiles := 100 // default
	files, err := getCodeFiles(wd, maxFiles)
	if err == nil {
		context.Files = files
	}

	return context, nil
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

// getCodeFiles returns a list of code files in the directory
func getCodeFiles(dir string, maxFiles int) ([]string, error) {
	var files []string
	count := 0

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if count >= maxFiles {
			return filepath.SkipDir
		}

		if info.IsDir() {
			// Skip certain directories
			basename := filepath.Base(path)
			if basename == ".git" || basename == "node_modules" || basename == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		if isCodeFile(path) {
			files = append(files, path)
			count++
		}

		return nil
	})

	return files, err
}

// isCodeFile checks if a file is a code file based on extension
func isCodeFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	codeExtensions := map[string]bool{
		".go":    true,
		".js":    true,
		".ts":    true,
		".py":    true,
		".java":  true,
		".c":     true,
		".cpp":   true,
		".h":     true,
		".hpp":   true,
		".rs":    true,
		".rb":    true,
		".php":   true,
		".cs":    true,
		".kt":    true,
		".swift": true,
		".dart":  true,
		".sql":   true,
		".sh":    true,
		".yaml":  true,
		".yml":   true,
		".json":  true,
		".xml":   true,
		".html":  true,
		".css":   true,
		".scss":  true,
		".md":    true,
	}

	return codeExtensions[ext]
}

// parseIntentType parses intent type from string
func parseIntentType(typeStr string) IntentType { //nolint:unused
	switch typeStr {
	case "analyze":
		return IntentAnalyze
	case "generate":
		return IntentGenerate
	case "modify":
		return IntentModify
	case "fix":
		return IntentFix
	case "improve":
		return IntentImprove
	case "debug":
		return IntentDebug
	case "refactor":
		return IntentRefactor
	case "test":
		return IntentTest
	case "document":
		return IntentDocument
	case "optimize":
		return IntentOptimize
	default:
		return IntentUnknown
	}
}
