package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

// WriteFileTool implements file writing functionality with diff preview
type WriteFileTool struct {
	workingDir  string
	config      map[string]interface{}
	readState   *ReadFileState // shared with read/edit tools; nil disables cache invalidation
	pathChecker *sandbox.PathChecker
}

// NewWriteFileTool creates a new WriteFileTool instance.
// checker may be nil (no path-level sandbox checks).
func NewWriteFileTool(workingDir string, config map[string]interface{}, checker *sandbox.PathChecker) *WriteFileTool {
	if config == nil {
		config = make(map[string]interface{})
	}
	pc := checker
	if pc == nil {
		pc = sandbox.NewPathChecker(nil)
	}
	return &WriteFileTool{
		workingDir:  workingDir,
		config:      config,
		pathChecker: pc,
	}
}

// NewWriteFileToolWithState creates a WriteFileTool that invalidates the shared read cache
// after successful writes so later edits must re-read the modified file.
// checker may be nil (no path-level sandbox checks).
func NewWriteFileToolWithState(workingDir string, config map[string]interface{}, checker *sandbox.PathChecker, state *ReadFileState) *WriteFileTool {
	t := NewWriteFileTool(workingDir, config, checker)
	t.readState = state
	return t
}

// Name returns the tool name
func (t *WriteFileTool) Name() string {
	return "write_file"
}

// Description returns the tool description
func (t *WriteFileTool) Description() string {
	return "Create, overwrite, or append to files with diff preview functionality and pure Go diff implementation for cross-platform reliability"
}

// Category returns the tool category
func (t *WriteFileTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryFileSystem
}

// RequiresConfirmationForParams checks if confirmation is required for specific file write parameters
func (t *WriteFileTool) RequiresConfirmationForParams(params map[string]interface{}) bool {
	filePath, ok := params["file_path"].(string)
	if !ok {
		return true // Missing file path is suspicious
	}

	// Normalize path for checking
	cleanPath := filepath.Clean(filePath)
	baseName := filepath.Base(cleanPath)
	lowerPath := strings.ToLower(cleanPath)
	lowerBase := strings.ToLower(baseName)

	// System and critical files that require confirmation
	criticalFiles := []string{
		".env", ".env.local", ".env.production", ".env.development",
		"dockerfile", "docker-compose.yml", "docker-compose.yaml",
		"makefile", "cmakelists.txt",
		"package.json", "package-lock.json", "yarn.lock",
		"go.mod", "go.sum",
		"requirements.txt", "setup.py", "pyproject.toml",
		"cargo.toml", "cargo.lock",
		"pom.xml", "build.gradle", "build.gradle.kts",
		".gitignore", ".gitattributes",
		"readme.md", "readme.txt", "readme.rst",
		"license", "license.txt", "license.md",
		"changelog.md", "changelog.txt",
		"config.yml", "config.yaml", "config.json", "config.toml",
		".nano/nano.yaml", ".nano.yml", ".nano.json",
	}

	// Check if it's a critical file
	for _, critical := range criticalFiles {
		if lowerBase == critical {
			return true
		}
	}

	// Check for dangerous patterns
	dangerousPatterns := []string{
		"/etc/", "/usr/", "/bin/", "/sbin/", "/var/", "/sys/", "/proc/",
		"/system/", "/windows/", "/program files/",
		".ssh/", ".config/", ".local/",
	}

	// Check for dangerous paths
	for _, pattern := range dangerousPatterns {
		if strings.Contains(lowerPath, pattern) {
			return true
		}
	}

	// Check for executable files
	executableExtensions := []string{".exe", ".bat", ".cmd", ".sh", ".bash", ".zsh", ".fish", ".ps1"}
	ext := strings.ToLower(filepath.Ext(filePath))
	for _, execExt := range executableExtensions {
		if ext == execExt {
			return true
		}
	}

	return false // Regular project files don't need confirmation
}

// RequiresConfirmation returns whether the tool requires confirmation
func (t *WriteFileTool) RequiresConfirmation() bool {
	return false // Confirmation will be determined dynamically by file path
}

// ConcurrencySafe returns false: writing files mutates the filesystem.
func (t *WriteFileTool) ConcurrencySafe() bool { return false }

// Schema returns the tool schema
func (t *WriteFileTool) Schema() *interfaces.ToolSchema {
	filePathProp := interfaces.NewStringProperty("Absolute path to the file to write")
	filePathProp.Examples = []string{"/Users/user/project/README.md", "./docs/guide.md", "/tmp/example.txt"}
	filePathProp.Usage = "Must be within the working directory. Relative paths are resolved against workspace and validated."

	contentProp := interfaces.NewStringProperty("Content to write to the file")
	contentProp.Examples = []string{"# Project Title\nA brief description...", "console.log('Hello');\n", "name: app\nversion: 1.0.0"}
	contentProp.Usage = "Provide full file content. Existing file will be overwritten unless creating a new file."

	createDirsProp := interfaces.NewBooleanProperty("Create parent directories if they don't exist")
	createDirsProp.Examples = []string{"true", "false"}
	createDirsProp.Usage = "Enable when writing to nested paths that may not exist."

	backupProp := interfaces.NewBooleanProperty("Create a backup of existing file")
	backupProp.Examples = []string{"true", "false"}
	backupProp.Usage = "When true and file exists, a timestamped .backup file will be created before overwrite."

	modeProp := interfaces.NewStringProperty("Write mode: overwrite or append")
	modeProp.Enum = []string{"overwrite", "append"}
	modeProp.Default = "overwrite"
	modeProp.Examples = []string{"overwrite", "append"}
	modeProp.Usage = "Specify 'overwrite' to replace entire file content (default), or 'append' to add content to the end of existing file. Append mode preserves existing content and adds new content at the end."

	return interfaces.CreateSchema(
		"Create, overwrite, or append to files with diff preview",
		map[string]*interfaces.PropertySchema{
			"file_path":          filePathProp,
			"content":            contentProp,
			"create_directories": createDirsProp,
			"backup":             backupProp,
			"mode":               modeProp,
		},
		[]string{"file_path", "content"},
	)
}

// Execute runs the tool with provided parameters
func (t *WriteFileTool) Execute(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	if params == nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "tool parameters are missing",
		}, nil
	}

	// Extract parameters
	filePath, ok := params["file_path"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "file_path parameter is required and must be a string",
		}, nil
	}

	content, ok := params["content"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "content parameter is required and must be a string",
		}, nil
	}

	// Get optional parameters
	createDirectories := false
	if createDirParam, ok := params["create_directories"]; ok {
		createDirectories, _ = createDirParam.(bool)
	}

	backup := false
	if backupParam, ok := params["backup"]; ok {
		backup, _ = backupParam.(bool)
	}

	// Validate and clean path
	absPath, err := t.validatePath(filePath)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Invalid file path: %v", err),
		}, nil
	}

	// Sandbox path check
	if err := t.pathChecker.Check(absPath, sandbox.OpWrite); err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Access denied: %v", err),
		}, nil
	}

	// Check if file exists for diff generation
	var existingContent string
	var isNewFile bool

	// Get write mode
	mode := "overwrite"
	if modeParam, ok := params["mode"]; ok {
		if modeStr, isStr := modeParam.(string); isStr {
			mode = modeStr
		}
	}

	// Compute new content based on mode
	var computedNew string
	if mode == "append" {
		computedNew = existingContent + content
	} else {
		computedNew = content
	}

	// Validate content before writing
	contentBytes := []byte(computedNew)
	detection := DetectBinaryContent(contentBytes)

	// Check if content appears to be binary
	if detection.IsBinary {
		// Allow writing binary content only if the file extension suggests it's a binary file
		if !IsBinaryFileExtension(absPath) {
			return &interfaces.ToolResult{
				Success: false,
				Error:   fmt.Sprintf("Content appears to contain binary data (encoding: %s, confidence: %.2f). Use appropriate binary file tools for file: %s", detection.Encoding, detection.Confidence, absPath),
			}, nil
		}
		// If it's a known binary extension, warn but allow
		// This could be useful for cases where users intentionally write binary data
	}

	// Generate diff
	diff := t.generateDiff(existingContent, computedNew, absPath, isNewFile)

	// Create parent directories if needed
	if createDirectories {
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return &interfaces.ToolResult{
				Success: false,
				Error:   fmt.Sprintf("Failed to create parent directories: %v", err),
			}, nil
		}
	}

	// Create backup if requested and file exists
	if backup && !isNewFile {
		backupPath := absPath + ".backup." + time.Now().Format("20060102150405")
		if err := t.createBackup(absPath, backupPath); err != nil {
			return &interfaces.ToolResult{
				Success: false,
				Error:   fmt.Sprintf("Failed to create backup: %v", err),
			}, nil
		}
	}

	// Write the file with sandbox protection
	if mode == "append" {
		appendData := []byte(content)
		appendFunc := func(path string, data []byte) error {
			flags := os.O_APPEND | os.O_CREATE | os.O_WRONLY
			f, e := os.OpenFile(path, flags, 0644)
			if e != nil {
				return e
			}
			defer f.Close() //nolint:errcheck
			_, e = f.Write(data)
			return e
		}
		err = appendFunc(absPath, appendData)
	} else {
		err = os.WriteFile(absPath, []byte(content), 0644)
	}
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to write file: %v", err),
		}, nil
	}
	if t.readState != nil {
		t.readState.Forget(absPath)
	}

	// Prepare metadata
	metadata := map[string]interface{}{
		"file_path":      absPath,
		"is_new_file":    isNewFile,
		"content_size":   len(content),
		"backup_created": backup && !isNewFile,
		"mode":           mode,
	}

	// Format content for display
	userContent := t.formatForUser(diff, metadata)
	llmContent := t.formatForLLM(diff, metadata)

	return &interfaces.ToolResult{
		Success:     true,
		Data:        diff,
		Metadata:    metadata,
		LLMContent:  llmContent,
		UserContent: userContent,
	}, nil
}

func (t *WriteFileTool) validatePath(path string) (string, error) {
	return validatePathCommon(t.workingDir, path, t.pathChecker.AllowedPaths())
}

func (t *WriteFileTool) generateDiff(oldContent, newContent, filePath string, isNewFile bool) string {
	return generateDiffCommon(oldContent, newContent, filePath, isNewFile, t.config, false)
}

func (t *WriteFileTool) createBackup(srcPath, backupPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}

	return os.WriteFile(backupPath, data, 0644)
}

func (t *WriteFileTool) formatForUser(diff string, metadata map[string]interface{}) string {
	var result strings.Builder

	filePath := metadata["file_path"].(string)
	isNewFile := metadata["is_new_file"].(bool)
	contentSize := metadata["content_size"].(int)
	backupCreated := metadata["backup_created"].(bool)
	mode := metadata["mode"].(string)

	if mode == "append" {
		if isNewFile {
			fmt.Fprintf(&result, "📄 Created new file with append: %s\n", filePath)
		} else {
			fmt.Fprintf(&result, "📎 Appended to file: %s\n", filePath)
		}
	} else {
		if isNewFile {
			fmt.Fprintf(&result, "📄 Created new file: %s\n", filePath)
		} else {
			fmt.Fprintf(&result, "📝 Modified file: %s\n", filePath)
		}
	}

	fmt.Fprintf(&result, "📏 Content size: %d bytes\n", contentSize)

	if backupCreated {
		result.WriteString("💾 Backup created\n")
	}

	result.WriteString("─────────────────────────────────────\n")
	result.WriteString("📋 Changes:\n")
	result.WriteString(diff)

	return result.String()
}

func (t *WriteFileTool) formatForLLM(diff string, metadata map[string]interface{}) string {
	filePath := metadata["file_path"].(string)
	isNewFile := metadata["is_new_file"].(bool)
	contentSize := metadata["content_size"].(int)
	mode := metadata["mode"].(string)

	action := "Modified"
	if mode == "append" {
		action = "Appended to"
	} else if isNewFile {
		action = "Created"
	}

	return fmt.Sprintf("%s file: %s\nSize: %d bytes\n\nDiff:\n%s",
		action, filePath, contentSize, diff)
}
