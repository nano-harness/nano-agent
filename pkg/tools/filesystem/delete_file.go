package filesystem

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

// DeleteFileTool implements safe deletion of files or directories within the workspace
// It supports dry-run preview and a safe trash mode that moves items to a workspace-local .trash directory
// instead of permanently deleting them.
type DeleteFileTool struct {
	workingDir  string
	config      map[string]interface{}
	readState   *ReadFileState // shared with read/edit tools; nil disables cache invalidation
	pathChecker *sandbox.PathChecker
}

// NewDeleteTool creates a new DeleteFileTool instance.
// checker may be nil (no path-level sandbox checks).
func NewDeleteTool(workingDir string, config map[string]interface{}, checker *sandbox.PathChecker) *DeleteFileTool {
	if config == nil {
		config = make(map[string]interface{})
	}
	pc := checker
	if pc == nil {
		pc = sandbox.NewPathChecker(nil)
	}
	return &DeleteFileTool{
		workingDir:  workingDir,
		config:      config,
		pathChecker: pc,
	}
}

// NewDeleteToolWithState creates a DeleteFileTool that invalidates the shared read cache
// after successful deletions so recreated paths cannot be edited from stale memory.
// checker may be nil (no path-level sandbox checks).
func NewDeleteToolWithState(workingDir string, config map[string]interface{}, checker *sandbox.PathChecker, state *ReadFileState) *DeleteFileTool {
	t := NewDeleteTool(workingDir, config, checker)
	t.readState = state
	return t
}

func (t *DeleteFileTool) Name() string { //nolint:revive
	return "delete_file"
}

func (t *DeleteFileTool) Description() string { //nolint:revive
	return "Safely delete files or directories within the workspace, with dry-run preview and optional trash"
}

func (t *DeleteFileTool) Category() interfaces.ToolCategory { //nolint:revive
	return interfaces.CategoryFileSystem
}

// RequiresConfirmationForParams checks if confirmation is required for specific deletion parameters
func (t *DeleteFileTool) RequiresConfirmationForParams(params map[string]interface{}) bool {
	path, ok := params["path"].(string)
	if !ok || strings.TrimSpace(path) == "" {
		return true // Missing path is suspicious
	}

	// Normalize
	cleanPath := filepath.Clean(path)
	baseName := filepath.Base(cleanPath)
	lowerPath := strings.ToLower(cleanPath)
	lowerBase := strings.ToLower(baseName)

	// Critical files similar to write/edit tools
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
		".nano.yaml", ".nano.yml", ".nano.json",
	}

	for _, critical := range criticalFiles {
		if lowerBase == critical {
			return true
		}
	}

	// Dangerous directories or patterns
	dangerousPatterns := []string{
		"/etc/", "/usr/", "/bin/", "/sbin/", "/var/", "/sys/", "/proc/",
		"/system/", "/windows/", "/program files/",
		".ssh/", ".config/", ".local/",
		".git/",   // deleting VCS folder is critical
		".trash/", // operating on trash itself is risky
	}
	for _, p := range dangerousPatterns {
		if strings.Contains(lowerPath, p) {
			return true
		}
	}

	// Executable file extensions
	executableExtensions := []string{".exe", ".bat", ".cmd", ".sh", ".bash", ".zsh", ".fish", ".ps1"}
	ext := strings.ToLower(filepath.Ext(cleanPath))
	for _, execExt := range executableExtensions {
		if ext == execExt {
			return true
		}
	}

	// Deleting directories or forcing permanent delete typically needs confirmation
	recursive := false
	if v, ok := params["recursive"]; ok {
		if b, ok := v.(bool); ok {
			recursive = b
		}
	}
	trash := true // default behavior is to trash; permanent delete is riskier
	if v, ok := params["trash"]; ok {
		if b, ok := v.(bool); ok {
			trash = b
		}
	}
	if recursive || !trash {
		return true
	}

	return false
}

func (t *DeleteFileTool) RequiresConfirmation() bool { //nolint:revive
	return false // Use contextual confirmation
}

// ConcurrencySafe returns false: deleting files mutates the filesystem.
func (t *DeleteFileTool) ConcurrencySafe() bool { return false }

func (t *DeleteFileTool) Schema() *interfaces.ToolSchema { //nolint:revive
	pathProp := interfaces.NewStringProperty("File or directory path to delete")
	pathProp.Examples = []string{"/Users/user/project/tmp.txt", "./build", "./dist/app.js"}
	pathProp.Usage = "Must be within working directory. Relative paths are resolved and validated."

	recursiveProp := interfaces.NewBooleanProperty("Recursively delete directory contents (required for directories)")
	recursiveProp.Examples = []string{"false", "true"}
	recursiveProp.Usage = "If target is a directory, set to true to delete it. Otherwise the operation will fail."

	trashProp := interfaces.NewBooleanProperty("Move to workspace .trash instead of permanent deletion")
	trashProp.Examples = []string{"true", "false"}
	trashProp.Usage = "When true, the path is moved to <workspace>/.trash for potential recovery. Safer default."

	dryRunProp := interfaces.NewBooleanProperty("Preview what would be deleted without making changes")
	dryRunProp.Examples = []string{"false", "true"}
	dryRunProp.Usage = "Enable to see a summary of files/directories that would be affected."

	maxPreviewProp := interfaces.NewNumberProperty("Maximum number of items to include in preview (for dry-run or directory deletion)")
	maxPreviewProp.Examples = []string{"50", "100"}
	maxPreviewProp.Usage = "Limits the number of item paths shown in the report to avoid excessive output."

	return interfaces.CreateSchema(
		"Safely delete files or directories within the workspace, with dry-run preview and optional trash",
		map[string]*interfaces.PropertySchema{
			"path":              pathProp,
			"recursive":         recursiveProp,
			"trash":             trashProp,
			"dry_run":           dryRunProp,
			"max_preview_items": maxPreviewProp,
		},
		[]string{"path"},
	)
}

func (t *DeleteFileTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) { //nolint:revive
	if params == nil {
		return &interfaces.ToolResult{Success: false, Error: "tool parameters are missing"}, nil
	}

	// Extract parameters
	pathParam, ok := params["path"].(string)
	if !ok {
		return &interfaces.ToolResult{Success: false, Error: "path parameter is required and must be a string"}, nil
	}

	recursive := false
	if v, ok := params["recursive"]; ok {
		if b, ok := v.(bool); ok {
			recursive = b
		}
	}

	trash := true
	if v, ok := params["trash"]; ok {
		if b, ok := v.(bool); ok {
			trash = b
		}
	}

	dryRun := false
	if v, ok := params["dry_run"]; ok {
		if b, ok := v.(bool); ok {
			dryRun = b
		}
	}

	maxPreview := 100
	if v, ok := params["max_preview_items"]; ok {
		if f, ok := v.(float64); ok {
			maxPreview = int(f)
		}
	} else if cfgVal, ok := t.config["delete_preview_max_items"].(int); ok && cfgVal > 0 {
		maxPreview = cfgVal
	}

	// Validate path
	absPath, err := t.validatePath(pathParam)
	if err != nil {
		return &interfaces.ToolResult{Success: false, Error: fmt.Sprintf("Invalid path: %v", err)}, nil
	}

	// Sandbox path check
	if err := t.pathChecker.Check(absPath, sandbox.OpDelete); err != nil {
		return &interfaces.ToolResult{Success: false, Error: fmt.Sprintf("Access denied: %v", err)}, nil
	}

	// Check existence and type
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &interfaces.ToolResult{Success: false, Error: fmt.Sprintf("Path does not exist: %s", absPath)}, nil
		}
		return &interfaces.ToolResult{Success: false, Error: fmt.Sprintf("Failed to access path: %v", err)}, nil
	}

	isDir := info.IsDir()
	if isDir && !recursive {
		return &interfaces.ToolResult{Success: false, Error: "Target is a directory. Set recursive=true to delete directories."}, nil
	}

	// Prepare preview list
	items, totalItems, previewTruncated, previewErr := t.previewItems(absPath, isDir, recursive, maxPreview)
	if previewErr != nil {
		return &interfaces.ToolResult{Success: false, Error: fmt.Sprintf("Failed to enumerate items: %v", previewErr)}, nil
	}

	metadata := map[string]interface{}{
		"path":              absPath,
		"is_dir":            isDir,
		"recursive":         recursive,
		"trash":             trash,
		"dry_run":           dryRun,
		"total_items":       totalItems,
		"preview_truncated": previewTruncated,
	}

	if dryRun {
		user := t.formatForUser(items, metadata, "preview")
		llm := t.formatForLLM(items, metadata, "preview")
		return &interfaces.ToolResult{Success: true, Data: items, Metadata: metadata, UserContent: user, LLMContent: llm}, nil
	}

	var action string
	var trashDest string

	if trash {
		trashDir := filepath.Join(t.workingDir, ".trash")
		if err := os.MkdirAll(trashDir, 0755); err != nil {
			return &interfaces.ToolResult{Success: false, Error: fmt.Sprintf("Failed to create trash directory: %v", err)}, nil
		}
		base := filepath.Base(absPath)
		ts := time.Now().Format("20060102_150405")
		trashDest = filepath.Join(trashDir, fmt.Sprintf("%s_%s", base, ts))
		err = os.Rename(absPath, trashDest)
		if err != nil {
			return &interfaces.ToolResult{Success: false, Error: fmt.Sprintf("Failed to move to trash: %v", err)}, nil
		}
		action = "trashed"
		metadata["trash_path"] = trashDest
	} else {
		var err error
		if isDir {
			err = os.RemoveAll(absPath)
		} else {
			err = os.Remove(absPath)
		}
		if err != nil {
			return &interfaces.ToolResult{Success: false, Error: fmt.Sprintf("Failed to delete: %v", err)}, nil
		}
		action = "deleted"
	}

	metadata["action"] = action
	if t.readState != nil {
		t.readState.Forget(absPath)
	}

	user := t.formatForUser(items, metadata, action)
	llm := t.formatForLLM(items, metadata, action)

	return &interfaces.ToolResult{
		Success:     true,
		Data:        map[string]interface{}{"action": action, "path": absPath, "is_dir": isDir, "items": items, "trash_path": trashDest},
		Metadata:    metadata,
		UserContent: user,
		LLMContent:  llm,
	}, nil
}

func (t *DeleteFileTool) validatePath(path string) (string, error) {
	return validatePathCommon(t.workingDir, path)
}

// previewItems collects items that would be affected by deletion
func (t *DeleteFileTool) previewItems(absPath string, isDir, recursive bool, max int) ([]string, int, bool, error) { //nolint:revive
	if !isDir {
		return []string{absPath}, 1, false, nil
	}

	var items []string
	count := 0
	truncated := false

	root := absPath
	appendItem := func(p string) {
		count++
		if len(items) < max {
			rel, err := filepath.Rel(t.workingDir, p)
			if err != nil {
				rel = p
			}
			items = append(items, rel)
		} else {
			truncated = true
		}
	}

	// always include the directory itself as first item
	appendItem(root)

	if recursive {
		walkFn := func(path string, d fs.DirEntry, err error) error { //nolint:revive
			if err != nil {
				return err
			}
			if path == root {
				return nil
			}
			appendItem(path)
			return nil
		}
		if err := filepath.WalkDir(root, walkFn); err != nil {
			return nil, 0, false, err
		}
	}

	return items, count, truncated, nil
}

func (t *DeleteFileTool) formatForUser(items []string, metadata map[string]interface{}, mode string) string {
	var b strings.Builder

	path := metadata["path"].(string)
	isDir := metadata["is_dir"].(bool)
	recursive := metadata["recursive"].(bool)
	total := metadata["total_items"].(int)
	truncated := metadata["preview_truncated"].(bool)

	action := mode // "preview", "trashed", "deleted"

	switch action {
	case "preview":
		if isDir {
			fmt.Fprintf(&b, "🧪 Preview delete directory: %s (recursive=%v)\n", path, recursive)
		} else {
			fmt.Fprintf(&b, "🧪 Preview delete file: %s\n", path)
		}
	case "trashed":
		trashPath, _ := metadata["trash_path"].(string)
		if isDir {
			fmt.Fprintf(&b, "🗑️ Moved directory to trash: %s → %s\n", path, trashPath)
		} else {
			fmt.Fprintf(&b, "🗑️ Moved file to trash: %s → %s\n", path, trashPath)
		}
	default:
		if isDir {
			fmt.Fprintf(&b, "❌ Deleted directory: %s\n", path)
		} else {
			fmt.Fprintf(&b, "❌ Deleted file: %s\n", path)
		}
	}

	b.WriteString("─────────────────────────────────────\n")
	fmt.Fprintf(&b, "📊 Affected items: %d\n", total)

	for _, item := range items {
		fmt.Fprintf(&b, "• %s\n", item)
	}
	if truncated {
		b.WriteString("… (preview truncated)\n")
	}

	return b.String()
}

func (t *DeleteFileTool) formatForLLM(items []string, metadata map[string]interface{}, mode string) string {
	path := metadata["path"].(string)
	isDir := metadata["is_dir"].(bool)
	recursive := metadata["recursive"].(bool)
	total := metadata["total_items"].(int)
	truncated := metadata["preview_truncated"].(bool)

	action := mode // "preview", "trashed", "deleted"

	var b strings.Builder
	switch action {
	case "preview":
		if isDir {
			fmt.Fprintf(&b, "Preview deletion of directory: %s (recursive=%v)\n", path, recursive)
		} else {
			fmt.Fprintf(&b, "Preview deletion of file: %s\n", path)
		}
	case "trashed":
		trashPath, _ := metadata["trash_path"].(string)
		if isDir {
			fmt.Fprintf(&b, "Trashed directory: %s -> %s\n", path, trashPath)
		} else {
			fmt.Fprintf(&b, "Trashed file: %s -> %s\n", path, trashPath)
		}
	default:
		if isDir {
			fmt.Fprintf(&b, "Deleted directory: %s\n", path)
		} else {
			fmt.Fprintf(&b, "Deleted file: %s\n", path)
		}
	}

	fmt.Fprintf(&b, "Affected items: %d\n", total)
	if len(items) > 0 {
		b.WriteString("Items:\n")
		for _, item := range items {
			b.WriteString("- " + item + "\n")
		}
		if truncated {
			b.WriteString("(truncated)\n")
		}
	}
	return b.String()
}
