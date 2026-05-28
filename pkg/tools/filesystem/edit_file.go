package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

// replacementResult holds the result of a string replacement operation
type replacementResult struct {
	newContent   string
	replacements int
}

// EditTool implements edit_file functionality inspired by trae-agent
type EditTool struct {
	workingDir  string
	config      map[string]interface{}
	readState   *ReadFileState // shared with filesystem tools; nil disables the check
	pathChecker *sandbox.PathChecker
}

// NewEditTool creates a new EditTool instance.
// checker may be nil (no path-level sandbox checks).
func NewEditTool(workingDir string, config map[string]interface{}, checker *sandbox.PathChecker) *EditTool {
	if config == nil {
		config = make(map[string]interface{})
	}
	pc := checker
	if pc == nil {
		pc = sandbox.NewPathChecker(nil)
	}
	return &EditTool{
		workingDir:  workingDir,
		config:      config,
		pathChecker: pc,
	}
}

// NewEditToolWithState creates an EditTool that enforces the "read before edit" policy
// using the provided ReadFileState.
// checker may be nil (no path-level sandbox checks).
func NewEditToolWithState(workingDir string, config map[string]interface{}, checker *sandbox.PathChecker, state *ReadFileState) *EditTool {
	t := NewEditTool(workingDir, config, checker)
	t.readState = state
	return t
}

func (t *EditTool) Name() string { //nolint:revive
	return "edit_file"
}

func (t *EditTool) Description() string { //nolint:revive
	return "Enhanced file editing tool with str_replace and insert operations. IMPORTANT: Always read the file first using read_file tool to understand its current content before making edits. This ensures accurate replacements and prevents errors."
}

func (t *EditTool) Category() interfaces.ToolCategory { //nolint:revive
	return interfaces.CategoryFileSystem
}

// RequiresConfirmationForParams checks if confirmation is required for specific file edit parameters
func (t *EditTool) RequiresConfirmationForParams(params map[string]interface{}) bool {
	filePath, ok := params["path"].(string)
	if !ok {
		return true // Missing file path is suspicious
	}

	// Use the same logic as WriteFileTool for critical file detection
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

func (t *EditTool) RequiresConfirmation() bool { //nolint:revive
	return false // Confirmation will be determined dynamically by file path
}

// ConcurrencySafe returns false: editing files mutates the filesystem.
func (t *EditTool) ConcurrencySafe() bool { return false }

func (t *EditTool) Schema() *interfaces.ToolSchema { //nolint:revive
	commandProp := interfaces.NewStringProperty("Operation to perform: str_replace or insert")
	commandProp.Examples = []string{"str_replace", "insert"}
	commandProp.Usage = "Use str_replace to replace text, insert to insert new content at a specific line."

	pathProp := interfaces.NewStringProperty("File path to operate on")
	pathProp.Examples = []string{"/Users/user/project/main.go", "./src/app.js"}
	pathProp.Usage = "Must be within working directory. Relative paths are resolved and validated. IMPORTANT: Read the file first with read_file tool to understand its current content."

	oldStrProp := interfaces.NewStringProperty("Text to replace (for str_replace)")
	oldStrProp.Examples = []string{"fmt.Println(\"Hello\")", "const DEBUG = true"}
	oldStrProp.Usage = "Required for str_replace. Must exactly match existing text (no regex). Use read_file tool first to get the exact text to replace."

	newStrProp := interfaces.NewStringProperty("New text (for str_replace and insert)")
	newStrProp.Examples = []string{"fmt.Println(\"Hello, World!\")", "const DEBUG = false", "// TODO: add tests"}
	newStrProp.Usage = "Provide the replacement text (str_replace) or the content to insert (insert). Can include newlines."

	insertLineProp := interfaces.NewNumberProperty("Line number to insert at (for insert, 1-indexed)")
	insertLineProp.Examples = []string{"1", "42", "999"}
	insertLineProp.Usage = "Required for insert. 1 inserts at top; len(file)+1 appends to end. Use read_file tool first to determine the correct line number."

	expectedProp := interfaces.NewNumberProperty("Expected number of replacements (optional validation for str_replace)")
	expectedProp.Examples = []string{"1", "2"}
	expectedProp.Usage = "If set, tool will error unless occurrences equal this number. Helps avoid accidental mass changes."

	createIfProp := interfaces.NewBooleanProperty("Create file if it doesn't exist (for str_replace)")
	createIfProp.Examples = []string{"true", "false"}
	createIfProp.Usage = "When true and file missing, a new file will be created. In that case old_str must be empty."

	return interfaces.CreateSchema(
		"Enhanced file editing - ALWAYS read the file first with read_file tool before editing to understand current content and ensure accurate modifications",
		map[string]*interfaces.PropertySchema{
			"command":               commandProp,
			"path":                  pathProp,
			"old_str":               oldStrProp,
			"new_str":               newStrProp,
			"insert_line":           insertLineProp,
			"expected_replacements": expectedProp,
			"create_if_not_exists":  createIfProp,
		},
		[]string{"command", "path"},
	)
}

func (t *EditTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) { //nolint:revive
	if params == nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "tool parameters are missing",
		}, nil
	}

	// Extract required parameters
	command, ok := params["command"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "command parameter is required and must be a string",
		}, nil
	}

	path, ok := params["path"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "path parameter is required and must be a string",
		}, nil
	}

	// Validate and clean path
	absPath, err := t.validatePath(path)
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

	// Enforce "read before edit" policy.
	// When create_if_not_exists is true we allow creating a brand-new file without a prior read.
	createIfNotExists, _ := params["create_if_not_exists"].(bool)
	if t.readState != nil && !createIfNotExists {
		if _, statErr := os.Stat(absPath); statErr == nil {
			// File exists – require a prior read_file call.
			if !t.readState.HasRead(absPath) {
				return &interfaces.ToolResult{
					Success: false,
					Error: fmt.Sprintf(
						"Safety check failed: %s has not been read in this session. "+
							"Use the read_file tool to read the file before editing it.",
						absPath,
					),
				}, nil
			}
		}
	}

	// Execute command
	switch command {
	case "str_replace":
		return t.executeStrReplace(absPath, params)
	case "insert":
		return t.executeInsert(absPath, params)
	default:
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Unknown command: %s. Supported commands: str_replace, insert", command),
		}, nil
	}
}

func (t *EditTool) executeStrReplace(path string, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// Get parameters
	oldStr, ok := params["old_str"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "old_str parameter is required for str_replace command",
		}, nil
	}

	newStr, ok := params["new_str"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "new_str parameter is required for str_replace command",
		}, nil
	}

	// Validate strings are different
	if oldStr == newStr {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "old_str and new_str must be different",
		}, nil
	}

	// Get optional parameters
	expectedReplacements := -1
	if expParam, ok := params["expected_replacements"]; ok {
		if expFloat, ok := expParam.(float64); ok {
			expectedReplacements = int(expFloat)
		}
	}

	createIfNotExists := false
	if createParam, ok := params["create_if_not_exists"]; ok {
		createIfNotExists, _ = createParam.(bool)
	}

	// Handle file existence
	var originalContent string
	var isNewFile bool

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if !createIfNotExists {
			return &interfaces.ToolResult{
				Success: false,
				Error:   fmt.Sprintf("File does not exist: %s", path),
			}, nil
		}

		// For new files, oldStr must be empty
		if oldStr != "" {
			return &interfaces.ToolResult{
				Success: false,
				Error:   "old_str must be empty when creating new files",
			}, nil
		}

		isNewFile = true
		originalContent = ""
	} else {
		// Read existing content
		var data []byte
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			return &interfaces.ToolResult{
				Success: false,
				Error:   fmt.Sprintf("Failed to read file: %v", err),
			}, nil
		}

		// Detect if file is binary before converting to string
		detection := DetectBinaryContent(data)
		if detection.IsBinary {
			// Check if it's a known binary file extension
			if IsBinaryFileExtension(path) {
				return &interfaces.ToolResult{
					Success: false,
					Error:   fmt.Sprintf("Cannot edit binary file: %s (detected as %s)", path, detection.Encoding),
				}, nil
			}
			// For files that appear binary but don't have binary extensions, provide a warning
			return &interfaces.ToolResult{
				Success: false,
				Error:   fmt.Sprintf("Cannot edit file: %s appears to contain binary data (encoding: %s, confidence: %.2f)", path, detection.Encoding, detection.Confidence),
			}, nil
		}

		originalContent = string(data)
	}

	// Perform replacement
	result, err := t.performReplacement(originalContent, oldStr, newStr, expectedReplacements, isNewFile)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Write the modified content back
	err = os.WriteFile(path, []byte(result.newContent), 0644)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to write file: %v", err),
		}, nil
	}
	if t.readState != nil {
		t.readState.Forget(path)
	}

	// Generate diff for display
	diff := t.generateDiff(originalContent, result.newContent, path, isNewFile)

	// Prepare metadata
	metadata := map[string]interface{}{
		"file_path":    path,
		"is_new_file":  isNewFile,
		"replacements": result.replacements,
		"old_str":      oldStr,
		"new_str":      newStr,
		"content_size": len(result.newContent),
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

func (t *EditTool) executeInsert(path string, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("File does not exist: %s", path),
		}, nil
	}

	// Get parameters
	newStr, ok := params["new_str"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "new_str parameter is required for insert command",
		}, nil
	}

	insertLineParam, ok := params["insert_line"]
	if !ok {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "insert_line parameter is required for insert command",
		}, nil
	}

	var insertLine int
	switch v := insertLineParam.(type) {
	case float64:
		insertLine = int(v)
	case int:
		insertLine = v
	default:
		return &interfaces.ToolResult{
			Success: false,
			Error:   "insert_line must be a number",
		}, nil
	}

	// Read file
	var data []byte
	var err error
	data, err = os.ReadFile(path)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to read file: %v", err),
		}, nil
	}

	// Detect if file is binary before converting to string
	detection := DetectBinaryContent(data)
	if detection.IsBinary {
		// Check if it's a known binary file extension
		if IsBinaryFileExtension(path) {
			return &interfaces.ToolResult{
				Success: false,
				Error:   fmt.Sprintf("Cannot edit binary file: %s (detected as %s)", path, detection.Encoding),
			}, nil
		}
		// For files that appear binary but don't have binary extensions, provide a warning
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Cannot edit file: %s appears to contain binary data (encoding: %s, confidence: %.2f)", path, detection.Encoding, detection.Confidence),
		}, nil
	}

	originalContent := string(data)
	lines := strings.Split(originalContent, "\n")

	// Validate insert line
	if insertLine < 1 || insertLine > len(lines)+1 {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("insert_line must be between 1 and %d", len(lines)+1),
		}, nil
	}

	// Insert new content
	newLines := strings.Split(newStr, "\n")
	var result []string

	if insertLine == len(lines)+1 {
		// Append at end
		result = append(lines, newLines...)
	} else {
		// Insert at specific position
		result = append(result, lines[:insertLine-1]...)
		result = append(result, newLines...)
		result = append(result, lines[insertLine-1:]...)
	}

	newContent := strings.Join(result, "\n")

	// Write back to file
	err = os.WriteFile(path, []byte(newContent), 0644)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to write file: %v", err),
		}, nil
	}
	if t.readState != nil {
		t.readState.Forget(path)
	}

	// Generate diff
	diff := t.generateDiff(originalContent, newContent, path, false)

	metadata := map[string]interface{}{
		"file_path":   path,
		"insert_line": insertLine,
		"lines_added": len(newLines),
		"new_str":     newStr,
		"file_size":   len(newContent),
	}

	userContent := fmt.Sprintf("📝 Inserted content in file: %s\n📍 At line: %d\n📏 Lines added: %d\n\nChanges:\n%s",
		path, insertLine, len(newLines), diff)
	llmContent := fmt.Sprintf("Inserted %d lines at line %d in %s\n\nDiff:\n%s",
		len(newLines), insertLine, path, diff)

	return &interfaces.ToolResult{
		Success:     true,
		Data:        diff,
		Metadata:    metadata,
		UserContent: userContent,
		LLMContent:  llmContent,
	}, nil
}

func (t *EditTool) validatePath(path string) (string, error) {
	return validatePathCommon(t.workingDir, path)
}

func (t *EditTool) performReplacement(content, oldString, newString string, expectedReplacements int, isNewFile bool) (*replacementResult, error) {
	if isNewFile {
		// For new files, just return the new string
		return &replacementResult{
			newContent:   newString,
			replacements: 1,
		}, nil
	}

	// Check if old string exists
	if !strings.Contains(content, oldString) {
		return nil, fmt.Errorf("old_string not found in file")
	}

	// Count occurrences
	occurrences := strings.Count(content, oldString)

	// Validate expected replacements if specified
	if expectedReplacements > 0 && occurrences != expectedReplacements {
		return nil, fmt.Errorf("expected %d replacements but found %d occurrences", expectedReplacements, occurrences)
	}

	// Check for ambiguous replacements (more than 1 occurrence).
	// Require the caller to supply a more specific old_str so each replacement is unambiguous.
	if occurrences > 1 && expectedReplacements != occurrences {
		return nil, fmt.Errorf(
			"str_replace failed: old_str matches %d locations in the file; "+
				"provide more surrounding context in old_str so that it uniquely identifies "+
				"the section you want to change, then retry",
			occurrences,
		)
	}

	// Perform replacement
	newContent := strings.ReplaceAll(content, oldString, newString)

	return &replacementResult{
		newContent:   newContent,
		replacements: occurrences,
	}, nil
}

func (t *EditTool) generateDiff(oldContent, newContent, filePath string, isNewFile bool) string {
	return generateDiffCommon(oldContent, newContent, filePath, isNewFile, t.config, true)
}

func (t *EditTool) formatForUser(diff string, metadata map[string]interface{}) string {
	var result strings.Builder

	filePath := metadata["file_path"].(string)
	isNewFile := metadata["is_new_file"].(bool)
	replacements := metadata["replacements"].(int)
	contentSize := metadata["content_size"].(int)

	if isNewFile {
		fmt.Fprintf(&result, "📄 Created new file: %s\n", filePath)
	} else {
		fmt.Fprintf(&result, "✏️  Edited file: %s\n", filePath)
		fmt.Fprintf(&result, "🔄 Replacements: %d\n", replacements)
	}

	fmt.Fprintf(&result, "📏 Content size: %d bytes\n", contentSize)
	result.WriteString("─────────────────────────────────────\n")
	result.WriteString("📋 Changes:\n")
	result.WriteString(diff)

	return result.String()
}

func (t *EditTool) formatForLLM(diff string, metadata map[string]interface{}) string {
	filePath := metadata["file_path"].(string)
	isNewFile := metadata["is_new_file"].(bool)
	replacements := metadata["replacements"].(int)
	contentSize := metadata["content_size"].(int)

	action := "Edited"
	if isNewFile {
		action = "Created"
	}

	return fmt.Sprintf("%s file: %s\nReplacements: %d\nSize: %d bytes\n\nDiff:\n%s",
		action, filePath, replacements, contentSize, diff)
}
