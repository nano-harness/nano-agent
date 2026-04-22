package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/logger"
	"gopkg.in/yaml.v2"
)

const (
	// MaxExpertFileSize is the maximum size for a single expert markdown file (1MB)
	MaxExpertFileSize = 1024 * 1024

	// Expert directory paths
	userExpertDir    = "~/.config/nano/agents"
	projectExpertDir = ".nano/agents"
)

// ExpertFrontmatter represents the YAML frontmatter in an expert markdown file
type ExpertFrontmatter struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	WhenToUse    string   `yaml:"when_to_use"`
	Model        string   `yaml:"model"`
	Temperature  float64  `yaml:"temperature"`
	MaxTurns     int      `yaml:"max_turns"`
	MaxTime      int      `yaml:"max_time_minutes"`
	AllowedTools []string `yaml:"allowed_tools"`
	OutputName   string   `yaml:"output_name"`
}

// LoadMarkdownExperts loads expert definitions from markdown files
// Scans both user-level (~/.config/nano/agents/) and project-level (.nano/agents/) directories
// Priority: project > user (project experts override user experts with same name)
// Builtin experts cannot be overridden
func LoadMarkdownExperts(registry *ExpertRegistry, workDir string) error {
	// Expand user directory
	home, err := os.UserHomeDir()
	if err != nil {
		logger.Warnf("Failed to get home directory: %v", err)
		home = ""
	}

	var userDir, projectDir string
	if home != "" {
		userDir = filepath.Join(home, ".config", "nano", "agents")
	}
	if workDir != "" {
		projectDir = filepath.Join(workDir, ".nano", "agents")
	}

	// Track loaded experts for priority handling
	loadedExperts := make(map[string]string) // name -> source

	// Load user-level experts first
	if userDir != "" {
		if err := loadExpertsFromDir(userDir, "user", registry, loadedExperts); err != nil {
			logger.Warnf("Failed to load user experts from %s: %v", userDir, err)
		}
	}

	// Load project-level experts (override user experts)
	if projectDir != "" {
		if err := loadExpertsFromDir(projectDir, "project", registry, loadedExperts); err != nil {
			logger.Warnf("Failed to load project experts from %s: %v", projectDir, err)
		}
	}

	return nil
}

// loadExpertsFromDir loads all *.md files from a directory as experts
func loadExpertsFromDir(dir, source string, registry *ExpertRegistry, loadedExperts map[string]string) error {
	// Check if directory exists
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil // Directory doesn't exist, not an error
	}
	if err != nil {
		return fmt.Errorf("stat directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read directory: %w", err)
	}

	// Resolve the directory path once to handle symlinks like /var -> /private/var on macOS
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		realDir = dir // Fallback to original dir if EvalSymlinks fails
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())

		// Security: Check for symlink escape attempts
		realPath, err := filepath.EvalSymlinks(filePath)
		if err != nil {
			logger.Warnf("Skipping expert file %s: symlink evaluation failed: %v", filePath, err)
			continue
		}

		// Verify the resolved path is still within the expected directory
		if !strings.HasPrefix(realPath, realDir) {
			logger.Warnf("Skipping expert file %s: symlink escape detected", filePath)
			continue
		}

		// Check file size
		fileInfo, err := os.Stat(realPath)
		if err != nil {
			logger.Warnf("Skipping expert file %s: %v", filePath, err)
			continue
		}
		if fileInfo.Size() > MaxExpertFileSize {
			logger.Warnf("Skipping expert file %s: exceeds maximum size (%d bytes)", filePath, MaxExpertFileSize)
			continue
		}

		expert, err := parseExpertMarkdown(realPath, source)
		if err != nil {
			logger.Warnf("Failed to parse expert file %s: %v", filePath, err)
			continue
		}

		// Check if expert with this name already exists
		if existingSource, exists := loadedExperts[expert.Name]; exists {
			if existingSource == "builtin" {
				logger.Warnf("Skipping expert %q from %s: cannot override builtin expert", expert.Name, filePath)
				continue
			}
			// Override previous expert (project overrides user)
			logger.Infof("Expert %q from %s overrides previous definition from %s", expert.Name, source, existingSource)
		}

		if err := registry.Register(expert); err != nil {
			logger.Warnf("Failed to register expert %q from %s: %v", expert.Name, filePath, err)
			continue
		}

		loadedExperts[expert.Name] = source
		logger.Infof("Loaded expert %q from %s", expert.Name, filePath)
	}

	return nil
}

// parseExpertMarkdown parses an expert markdown file
func parseExpertMarkdown(filePath, source string) (*Expert, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	frontmatter, systemPrompt, err := splitExpertFrontmatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("split frontmatter: %w", err)
	}

	var meta ExpertFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	// Validate name
	if !isValidExpertName(meta.Name) {
		return nil, fmt.Errorf("invalid expert name %q: must match ^[a-z][a-z0-9-]*$", meta.Name)
	}

	// Set defaults
	if meta.OutputName == "" {
		meta.OutputName = "result"
	}
	if meta.MaxTurns == 0 {
		meta.MaxTurns = 20
	}
	if meta.MaxTime == 0 {
		meta.MaxTime = 10
	}
	if len(meta.AllowedTools) == 0 {
		meta.AllowedTools = []string{"*"}
	}

	// Build Expert
	expert := &Expert{
		Name:           meta.Name,
		DisplayName:    meta.Name, // Can be customized in frontmatter if needed
		Description:    meta.Description,
		Source:         source,
		SystemPrompt:   strings.TrimSpace(systemPrompt),
		QueryTemplate:  "${request}", // Default template
		Model:          meta.Model,
		Temperature:    meta.Temperature,
		MaxTurns:       meta.MaxTurns,
		MaxTimeMinutes: meta.MaxTime,
		AllowedTools:   meta.AllowedTools,
		OutputName:     meta.OutputName,
		InputSchema: &ExpertInputSchema{
			Type: "object",
			Properties: map[string]*ExpertPropertySchema{
				"request": {
					Type:        "string",
					Description: "The task request for this expert",
				},
			},
			Required: []string{"request"},
		},
	}

	return expert, nil
}

// splitExpertFrontmatter splits a markdown document into YAML frontmatter and body
func splitExpertFrontmatter(content string) (frontmatter, body string, err error) {
	lines := strings.SplitAfter(content, "\n")

	var (
		inFrontmatter bool
		fmStart       int
		fmEnd         int
	)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inFrontmatter {
			if trimmed == "---" {
				inFrontmatter = true
				fmStart = i + 1
				continue
			}
			continue
		}
		if trimmed == "---" {
			fmEnd = i
			break
		}
	}

	if !inFrontmatter || fmEnd == 0 {
		return "", "", fmt.Errorf("no valid YAML frontmatter found (missing --- delimiters)")
	}

	var fmBuilder strings.Builder
	for i := fmStart; i < fmEnd; i++ {
		fmBuilder.WriteString(lines[i])
	}

	var bodyBuilder strings.Builder
	for i := fmEnd + 1; i < len(lines); i++ {
		bodyBuilder.WriteString(lines[i])
	}

	return fmBuilder.String(), bodyBuilder.String(), nil
}
