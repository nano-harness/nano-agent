package openspec

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

// validChangeNameRegexp restricts change names to safe characters,
// preventing path traversal (e.g., ".." or "/" in the name).
var validChangeNameRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)

// maxChangeNameLength is the maximum allowed length for a change name.
const maxChangeNameLength = 128

// validateChangeName checks that a change name is safe for use in filesystem paths.
func validateChangeName(name string) error {
	if name == "" {
		return fmt.Errorf("change name cannot be empty")
	}
	if len(name) > maxChangeNameLength {
		return fmt.Errorf("change name too long (max %d characters)", maxChangeNameLength)
	}
	if !validChangeNameRegexp.MatchString(name) {
		return fmt.Errorf("invalid change name %q: must contain only lowercase letters, digits, hyphens, dots, or underscores, and must start/end with a letter or digit", name)
	}
	return nil
}

// ArtifactManager manages the file structure, dependency tracking, and
// state of OpenSpec changes and their artifacts on disk.
type ArtifactManager struct {
	rootDir         string // Absolute path to openspec root (e.g., /project/openspec)
	workingDir      string // Project working directory
	maxArtifactSize int64  // Max artifact file size in bytes (0 = unlimited)
}

// NewArtifactManager creates a new ArtifactManager.
// rootDir is the OpenSpec root directory name (e.g. "openspec").
// workingDir is the project root directory.
func NewArtifactManager(rootDir, workingDir string) *ArtifactManager {
	absRoot := rootDir
	if !filepath.IsAbs(rootDir) {
		absRoot = filepath.Join(workingDir, rootDir)
	}
	return &ArtifactManager{
		rootDir:    absRoot,
		workingDir: workingDir,
	}
}

// SetMaxArtifactSize configures the maximum artifact file size in bytes.
// A value of 0 means unlimited.
func (am *ArtifactManager) SetMaxArtifactSize(size int64) {
	am.maxArtifactSize = size
}

// RootDir returns the absolute path to the openspec root directory.
func (am *ArtifactManager) RootDir() string {
	return am.rootDir
}

// changesDir returns the path to the changes directory.
func (am *ArtifactManager) changesDir() string {
	return filepath.Join(am.rootDir, "changes")
}

// specsDir returns the path to the main specs directory.
func (am *ArtifactManager) specsDir() string {
	return filepath.Join(am.rootDir, "specs")
}

// EnsureDirectories creates the openspec directory structure if it doesn't exist.
func (am *ArtifactManager) EnsureDirectories() error {
	dirs := []string{
		am.rootDir,
		am.changesDir(),
		am.specsDir(),
		filepath.Join(am.changesDir(), "archive"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

// HasOpenSpecDir checks whether the openspec directory exists in the project.
func (am *ArtifactManager) HasOpenSpecDir() bool {
	info, err := os.Stat(am.rootDir)
	return err == nil && info.IsDir()
}

// ListChanges returns all active (non-archived) change names.
func (am *ArtifactManager) ListChanges() ([]string, error) {
	dir := am.changesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read changes directory: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "archive" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// GetChange loads a change by name, inspecting the filesystem for artifact status.
func (am *ArtifactManager) GetChange(name string) (*Change, error) {
	if err := validateChangeName(name); err != nil {
		return nil, err
	}

	changePath := filepath.Join(am.changesDir(), name)
	info, err := os.Stat(changePath)
	if err != nil {
		return nil, fmt.Errorf("change %q not found: %w", name, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("change %q is not a directory", name)
	}

	// Read metadata if exists
	meta := ChangeMetadata{}
	metaPath := filepath.Join(changePath, ".openspec.yaml")
	if data, err := os.ReadFile(metaPath); err == nil {
		_ = yaml.Unmarshal(data, &meta)
	}

	schemaName := meta.Schema
	if schemaName == "" {
		schemaName = "spec-driven"
	}
	schema := GetSchema(schemaName)
	if schema == nil {
		return nil, fmt.Errorf("unknown schema %q", schemaName)
	}

	change := &Change{
		Name:      name,
		Schema:    schemaName,
		Path:      changePath,
		Artifacts: make(map[string]*Artifact),
		Metadata:  meta,
		CreatedAt: info.ModTime(),
	}

	// Populate artifacts from schema
	for _, sa := range schema.Artifacts {
		art := &Artifact{
			ID:           sa.ID,
			FilePath:     sa.Generates,
			FilePattern:  sa.Generates,
			Dependencies: sa.Requires,
		}

		// Check if artifact file exists
		if strings.Contains(sa.Generates, "*") {
			// Glob pattern — check if any matching files exist
			matches, _ := filepath.Glob(filepath.Join(changePath, sa.Generates))
			if len(matches) > 0 {
				art.Status = ArtifactStatusCreated
			}
		} else {
			fullPath := filepath.Join(changePath, sa.Generates)
			if _, err := os.Stat(fullPath); err == nil {
				art.Status = ArtifactStatusCreated
			}
		}

		// Determine pending vs ready if not created
		if art.Status != ArtifactStatusCreated {
			allDeps := true
			for _, dep := range sa.Requires {
				depArt := change.Artifacts[dep]
				if depArt == nil || depArt.Status != ArtifactStatusCreated {
					allDeps = false
					break
				}
			}
			if allDeps {
				art.Status = ArtifactStatusReady
			} else {
				art.Status = ArtifactStatusPending
			}
		}

		change.Artifacts[sa.ID] = art
	}

	// Second pass: update status for artifacts whose deps were loaded in wrong order
	for _, sa := range schema.Artifacts {
		art := change.Artifacts[sa.ID]
		if art.Status == ArtifactStatusCreated {
			continue
		}
		allDeps := true
		for _, dep := range sa.Requires {
			depArt := change.Artifacts[dep]
			if depArt == nil || depArt.Status != ArtifactStatusCreated {
				allDeps = false
				break
			}
		}
		if allDeps {
			art.Status = ArtifactStatusReady
		} else {
			art.Status = ArtifactStatusPending
		}
	}

	return change, nil
}

// CreateChange creates a new change directory with .openspec.yaml metadata.
func (am *ArtifactManager) CreateChange(name, schemaName string) (*Change, error) {
	if err := validateChangeName(name); err != nil {
		return nil, err
	}

	changePath := filepath.Join(am.changesDir(), name)
	if _, err := os.Stat(changePath); err == nil {
		return nil, fmt.Errorf("change %q already exists", name)
	}

	if err := am.EnsureDirectories(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(changePath, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create change directory: %w", err)
	}

	// Create specs subdirectory for the change
	if err := os.MkdirAll(filepath.Join(changePath, "specs"), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create specs directory: %w", err)
	}

	// Write .openspec.yaml metadata
	meta := ChangeMetadata{
		Schema:    schemaName,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	metaData, err := yaml.Marshal(&meta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(changePath, ".openspec.yaml"), metaData, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	return am.GetChange(name)
}

// WriteArtifact writes content to an artifact file within a change.
func (am *ArtifactManager) WriteArtifact(changeName, artifactID, content string) error {
	if err := validateChangeName(changeName); err != nil {
		return err
	}

	// Enforce max artifact size if configured
	if am.maxArtifactSize > 0 && int64(len(content)) > am.maxArtifactSize {
		return fmt.Errorf("artifact content exceeds maximum size (%d bytes > %d bytes)", len(content), am.maxArtifactSize)
	}

	changePath := filepath.Join(am.changesDir(), changeName)
	if _, err := os.Stat(changePath); err != nil {
		return fmt.Errorf("change %q not found: %w", changeName, err)
	}

	// Resolve schema from change metadata instead of hard-coding
	schema, err := am.resolveSchemaForChange(changeName)
	if err != nil {
		return err
	}

	var filePath string
	for _, sa := range schema.Artifacts {
		if sa.ID == artifactID {
			filePath = sa.Generates
			break
		}
	}
	if filePath == "" {
		return fmt.Errorf("unknown artifact ID %q", artifactID)
	}

	// For glob patterns, use a default file path
	if strings.Contains(filePath, "*") {
		// Default to specs/spec.md for the specs artifact
		filePath = "specs/spec.md"
	}

	fullPath := filepath.Join(changePath, filePath)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return os.WriteFile(fullPath, []byte(content), 0o644)
}

// ReadArtifact reads the content of an artifact file within a change.
func (am *ArtifactManager) ReadArtifact(changeName, artifactID string) (string, error) {
	if err := validateChangeName(changeName); err != nil {
		return "", err
	}

	changePath := filepath.Join(am.changesDir(), changeName)

	// Resolve schema from change metadata instead of hard-coding
	schema, err := am.resolveSchemaForChange(changeName)
	if err != nil {
		return "", err
	}

	var filePath string
	for _, sa := range schema.Artifacts {
		if sa.ID == artifactID {
			filePath = sa.Generates
			break
		}
	}
	if filePath == "" {
		return "", fmt.Errorf("unknown artifact ID %q", artifactID)
	}

	// Handle glob patterns
	if strings.Contains(filePath, "*") {
		matches, err := filepath.Glob(filepath.Join(changePath, filePath))
		if err != nil {
			return "", fmt.Errorf("failed to glob for artifact: %w", err)
		}
		if len(matches) == 0 {
			return "", fmt.Errorf("no files found for artifact %q", artifactID)
		}
		var combined strings.Builder
		for _, m := range matches {
			// Ensure matched file is within the change directory (prevent symlink escape)
			absMatch, _ := filepath.Abs(m)
			absChange, _ := filepath.Abs(changePath)
			if !strings.HasPrefix(absMatch, absChange+string(filepath.Separator)) {
				continue
			}
			data, err := os.ReadFile(m)
			if err != nil {
				return "", fmt.Errorf("failed to read %s: %w", m, err)
			}
			rel, _ := filepath.Rel(changePath, m)
			fmt.Fprintf(&combined, "--- %s ---\n%s\n\n", rel, string(data))
		}
		return combined.String(), nil
	}

	data, err := os.ReadFile(filepath.Join(changePath, filePath))
	if err != nil {
		return "", fmt.Errorf("failed to read artifact %q: %w", artifactID, err)
	}
	return string(data), nil
}

// GetChangeStatus returns a summary of the change's artifact and task completion state.
func (am *ArtifactManager) GetChangeStatus(name string) (*ChangeStatus, error) {
	change, err := am.GetChange(name)
	if err != nil {
		return nil, err
	}

	statuses := make(map[string]ArtifactStatus)
	for id, art := range change.Artifacts {
		statuses[id] = art.Status
	}

	schema := GetSchema(change.Schema)
	ready := GetReadyArtifacts(schema, statuses)

	status := &ChangeStatus{
		Name:             name,
		ArtifactStatuses: statuses,
		ReadyArtifacts:   ready,
	}

	// Parse tasks if tasks.md exists
	tasksContent, err := am.ReadArtifact(name, "tasks")
	if err == nil {
		tasks := ParseTasks(tasksContent)
		status.TasksTotal = len(tasks)
		for _, t := range tasks {
			if t.Status == TaskStatusComplete {
				status.TasksCompleted++
			}
		}
	}

	return status, nil
}

// ArchiveChange moves a change to the archive directory.
func (am *ArtifactManager) ArchiveChange(name string) error {
	if err := validateChangeName(name); err != nil {
		return err
	}

	changePath := filepath.Join(am.changesDir(), name)
	if _, err := os.Stat(changePath); err != nil {
		return fmt.Errorf("change %q not found: %w", name, err)
	}

	archiveDir := filepath.Join(am.changesDir(), "archive")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return fmt.Errorf("failed to create archive directory: %w", err)
	}

	archiveName := time.Now().Format("2006-01-02-150405") + "-" + name
	archivePath := filepath.Join(archiveDir, archiveName)

	// Handle collision by appending suffix
	if _, err := os.Stat(archivePath); err == nil {
		for i := 2; i <= 100; i++ {
			candidate := fmt.Sprintf("%s-%d", archivePath, i)
			if _, err := os.Stat(candidate); os.IsNotExist(err) {
				archivePath = candidate
				break
			}
		}
	}

	return os.Rename(changePath, archivePath)
}

// resolveSchemaForChange reads the change's .openspec.yaml metadata and returns
// the corresponding schema definition. Falls back to "spec-driven" if no
// schema is recorded in metadata.
func (am *ArtifactManager) resolveSchemaForChange(changeName string) (*SchemaDefinition, error) {
	changePath := filepath.Join(am.changesDir(), changeName)
	meta := ChangeMetadata{}
	metaPath := filepath.Join(changePath, ".openspec.yaml")
	if data, err := os.ReadFile(metaPath); err == nil {
		_ = yaml.Unmarshal(data, &meta)
	}
	schemaName := meta.Schema
	if schemaName == "" {
		schemaName = "spec-driven"
	}
	schema := GetSchema(schemaName)
	if schema == nil {
		return nil, fmt.Errorf("unknown schema %q for change %q", schemaName, changeName)
	}
	return schema, nil
}

// ReadProjectConfig reads the openspec/config.yaml project-level configuration.
func (am *ArtifactManager) ReadProjectConfig() (*ProjectConfig, error) {
	configPath := filepath.Join(am.rootDir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProjectConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read project config: %w", err)
	}
	var pc ProjectConfig
	if err := yaml.Unmarshal(data, &pc); err != nil {
		return nil, fmt.Errorf("failed to parse project config: %w", err)
	}
	return &pc, nil
}

// ReadMainSpecs reads all spec files from the openspec/specs/ directory.
func (am *ArtifactManager) ReadMainSpecs() (map[string]string, error) {
	specsDir := am.specsDir()
	result := make(map[string]string)

	err := filepath.Walk(specsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil // skip unreadable
		}
		relPath, _ := filepath.Rel(specsDir, path)
		result[relPath] = string(data)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to walk specs directory: %w", err)
	}
	return result, nil
}
