package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

// LSTool implements directory listing functionality with filtering
type LSTool struct {
	workingDir  string
	config      map[string]interface{}
	pathChecker *sandbox.PathChecker
}

// NewLSTool creates a new LSTool instance.
// checker may be nil (no path-level sandbox checks).
func NewLSTool(workingDir string, config map[string]interface{}, checker *sandbox.PathChecker) *LSTool {
	if config == nil {
		config = make(map[string]interface{})
	}
	pc := checker
	if pc == nil {
		pc = sandbox.NewPathChecker(nil)
	}
	return &LSTool{
		workingDir:  workingDir,
		config:      config,
		pathChecker: pc,
	}
}

func (t *LSTool) Name() string { //nolint:revive
	return "list_directory"
}

func (t *LSTool) Description() string { //nolint:revive
	return "List directory contents with filtering and metadata"
}

func (t *LSTool) Category() interfaces.ToolCategory { //nolint:revive
	return interfaces.CategoryFileSystem
}

func (t *LSTool) RequiresConfirmation() bool { //nolint:revive
	return false
}

// ConcurrencySafe returns true: directory listing is read-only.
func (t *LSTool) ConcurrencySafe() bool { return true }

func (t *LSTool) Schema() *interfaces.ToolSchema { //nolint:revive
	pathProp := interfaces.NewStringProperty("Directory path to list (defaults to current working directory)")
	pathProp.Examples = []string{".", "./src", "/Users/user/project"}
	pathProp.Usage = "Must be within working directory. Relative paths resolved against workspace."

	recursiveProp := interfaces.NewBooleanProperty("List contents recursively")
	recursiveProp.Examples = []string{"false", "true"}
	recursiveProp.Usage = "Enable to traverse subdirectories. Combine with max_depth to limit recursion."

	showHiddenProp := interfaces.NewBooleanProperty("Include hidden files (starting with .)")
	showHiddenProp.Examples = []string{"false", "true"}
	showHiddenProp.Usage = "Hidden files are skipped unless enabled."

	ignoreProp := interfaces.NewArrayProperty("Glob patterns to ignore", "string")
	ignoreProp.Examples = []string{"node_modules", "*.log", ".git"}
	ignoreProp.Usage = "Filename-only glob patterns matched per entry (e.g., *.log, .git, node_modules)."

	maxDepthProp := interfaces.NewNumberProperty("Maximum recursion depth (default: 3)")
	maxDepthProp.Examples = []string{"1", "2", "5"}
	maxDepthProp.Usage = "Only applies when recursive=true. Depth counts from starting directory (0)."

	includeMetaProp := interfaces.NewBooleanProperty("Include file metadata (size, mod time, etc.)")
	includeMetaProp.Examples = []string{"true", "false"}
	includeMetaProp.Usage = "Toggle to reduce output verbosity if not needed."

	return interfaces.CreateSchema(
		"List directory contents with filtering and metadata",
		map[string]*interfaces.PropertySchema{
			"path":             pathProp,
			"recursive":        recursiveProp,
			"show_hidden":      showHiddenProp,
			"ignore_patterns":  ignoreProp,
			"max_depth":        maxDepthProp,
			"include_metadata": includeMetaProp,
		},
		[]string{},
	)
}

type FileInfo struct { //nolint:revive
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	Mode     string    `json:"mode"`
	IsHidden bool      `json:"is_hidden"`
	Depth    int       `json:"depth"`
}

func (t *LSTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) { //nolint:revive
	if params == nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "tool parameters are missing",
		}, nil
	}

	// Extract parameters
	targetPath := t.workingDir
	if pathParam, ok := params["path"].(string); ok && pathParam != "" {
		targetPath = pathParam
	}

	// Resolve to absolute path before sandbox check
	absTarget, err := filepath.Abs(filepath.Clean(targetPath))
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Invalid path: %v", err),
		}, nil
	}

	// Sandbox path check
	if err := t.pathChecker.Check(absTarget, sandbox.OpList); err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Access denied: %v", err),
		}, nil
	}
	targetPath = absTarget

	recursive := false
	if recursiveParam, ok := params["recursive"]; ok {
		recursive, _ = recursiveParam.(bool)
	}

	showHidden := false
	if showHiddenParam, ok := params["show_hidden"]; ok {
		showHidden, _ = showHiddenParam.(bool)
	}

	maxDepth := 3
	if maxDepthParam, ok := params["max_depth"]; ok {
		if maxDepthFloat, ok := maxDepthParam.(float64); ok {
			maxDepth = int(maxDepthFloat)
		}
	} else if configMaxDepth, ok := t.config["list_directory_max_depth"].(int); ok {
		maxDepth = configMaxDepth
	}

	includeMetadata := true
	if includeMetadataParam, ok := params["include_metadata"]; ok {
		includeMetadata, _ = includeMetadataParam.(bool)
	}

	// Get ignore patterns
	var ignorePatterns []string
	if ignorePatternsParam, ok := params["ignore_patterns"]; ok {
		if patterns, ok := ignorePatternsParam.([]interface{}); ok {
			for _, pattern := range patterns {
				if patternStr, ok := pattern.(string); ok {
					ignorePatterns = append(ignorePatterns, patternStr)
				}
			}
		}
	}

	// Validate and clean path
	absPath, err := t.validatePath(targetPath)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Invalid path: %v", err),
		}, nil
	}

	// Check if path exists and is a directory
	info, err := os.Stat(absPath)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Path does not exist: %s", absPath),
		}, nil
	}

	if !info.IsDir() {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Path is not a directory: %s", absPath),
		}, nil
	}

	// List directory contents
	var files []FileInfo
	if recursive {
		files, err = t.listRecursive(absPath, 0, maxDepth, showHidden, ignorePatterns)
	} else {
		files, err = t.listDirectory(absPath, 0, showHidden, ignorePatterns)
	}

	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to list directory: %v", err),
		}, nil
	}

	// Sort files
	sort.Slice(files, func(i, j int) bool {
		// Directories first, then files
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})

	// Prepare metadata
	metadata := map[string]interface{}{
		"path":             absPath,
		"total_files":      len(files),
		"recursive":        recursive,
		"show_hidden":      showHidden,
		"include_metadata": includeMetadata,
		"ignore_patterns":  ignorePatterns,
	}

	// Format content for display
	userContent := t.formatForUser(files, metadata, includeMetadata)
	llmContent := t.formatForLLM(files, metadata, includeMetadata)

	return &interfaces.ToolResult{
		Success:     true,
		Data:        files,
		Metadata:    metadata,
		LLMContent:  llmContent,
		UserContent: userContent,
	}, nil
}

func (t *LSTool) validatePath(path string) (string, error) {
	return validatePathCommon(t.workingDir, path)
}

func (t *LSTool) listDirectory(dirPath string, depth int, showHidden bool, ignorePatterns []string) ([]FileInfo, error) {
	var entries []os.DirEntry
	var err error
	entries, err = os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, entry := range entries {
		// Skip hidden files if not requested
		if !showHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		// Check ignore patterns
		if t.shouldIgnore(entry.Name(), ignorePatterns) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't get info for
		}

		fullPath := filepath.Join(dirPath, entry.Name())
		fileInfo := FileInfo{
			Name:     entry.Name(),
			Path:     fullPath,
			IsDir:    entry.IsDir(),
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			Mode:     info.Mode().String(),
			IsHidden: strings.HasPrefix(entry.Name(), "."),
			Depth:    depth,
		}

		files = append(files, fileInfo)
	}

	return files, nil
}

func (t *LSTool) listRecursive(dirPath string, depth, maxDepth int, showHidden bool, ignorePatterns []string) ([]FileInfo, error) {
	if depth > maxDepth {
		return nil, nil
	}

	files, err := t.listDirectory(dirPath, depth, showHidden, ignorePatterns)
	if err != nil {
		return nil, err
	}

	// Recursively list subdirectories
	for _, file := range files {
		if file.IsDir && depth < maxDepth {
			subFiles, err := t.listRecursive(file.Path, depth+1, maxDepth, showHidden, ignorePatterns)
			if err != nil {
				continue // Skip directories we can't read
			}
			files = append(files, subFiles...)
		}
	}

	return files, nil
}

func (t *LSTool) shouldIgnore(filename string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, filename); matched {
			return true
		}
	}
	return false
}

func (t *LSTool) formatForUser(files []FileInfo, metadata map[string]interface{}, includeMetadata bool) string {
	var result strings.Builder

	path := metadata["path"].(string)
	totalFiles := metadata["total_files"].(int)

	fmt.Fprintf(&result, "📁 Directory: %s\n", path)
	fmt.Fprintf(&result, "📊 Total items: %d\n", totalFiles)
	result.WriteString("─────────────────────────────────────\n")

	if len(files) == 0 {
		result.WriteString("📭 Directory is empty\n")
		return result.String()
	}

	parentChildren := make(map[string][]FileInfo)
	for _, f := range files {
		parent := filepath.Dir(f.Path)
		parentChildren[parent] = append(parentChildren[parent], f)
	}

	var sortChildren = func(items []FileInfo) {
		sort.Slice(items, func(i, j int) bool {
			if items[i].IsDir != items[j].IsDir {
				return items[i].IsDir
			}
			return items[i].Name < items[j].Name
		})
	}

	var render func(string, string)
	render = func(current string, prefix string) {
		children := parentChildren[current]
		if len(children) == 0 {
			return
		}
		sortChildren(children)
		for idx, child := range children {
			isLast := idx == len(children)-1
			connector := "├──"
			nextPrefix := prefix + "│   "
			if isLast {
				connector = "└──"
				nextPrefix = prefix + "    "
			}

			icon := "📄"
			if child.IsDir {
				icon = "📁"
			}
			if child.IsHidden {
				icon = "🔒"
			}

			fmt.Fprintf(&result, "%s%s %s %s", prefix, connector, icon, child.Name)
			if includeMetadata {
				if child.IsDir {
					result.WriteString(" (dir)")
				} else {
					fmt.Fprintf(&result, " (%d bytes)", child.Size)
				}
			}
			result.WriteString("\n")

			if child.IsDir {
				render(child.Path, nextPrefix)
			}
		}
	}

	render(path, "")
	return result.String()
}

func (t *LSTool) formatForLLM(files []FileInfo, metadata map[string]interface{}, includeMetadata bool) string {
	var result strings.Builder

	path := metadata["path"].(string)
	totalFiles := metadata["total_files"].(int)

	fmt.Fprintf(&result, "Directory: %s\n", path)
	fmt.Fprintf(&result, "Total items: %d\n", totalFiles)
	result.WriteString("\n")

	if len(files) == 0 {
		result.WriteString("Directory is empty\n")
		return result.String()
	}

	parentChildren := make(map[string][]FileInfo)
	for _, f := range files {
		parent := filepath.Dir(f.Path)
		parentChildren[parent] = append(parentChildren[parent], f)
	}

	var sortChildren = func(items []FileInfo) {
		sort.Slice(items, func(i, j int) bool {
			if items[i].IsDir != items[j].IsDir {
				return items[i].IsDir
			}
			return items[i].Name < items[j].Name
		})
	}

	var render func(string, string)
	render = func(current string, prefix string) {
		children := parentChildren[current]
		if len(children) == 0 {
			return
		}
		sortChildren(children)
		for idx, child := range children {
			isLast := idx == len(children)-1
			connector := "├──"
			nextPrefix := prefix + "│   "
			if isLast {
				connector = "└──"
				nextPrefix = prefix + "    "
			}

			fmt.Fprintf(&result, "%s%s %s", prefix, connector, child.Name)
			if includeMetadata {
				if child.IsDir {
					result.WriteString(" (dir)")
				} else {
					fmt.Fprintf(&result, " (%d bytes, modified: %s)", child.Size, child.ModTime.Format("2006-01-02 15:04:05"))
				}
			}
			result.WriteString("\n")

			if child.IsDir {
				render(child.Path, nextPrefix)
			}
		}
	}

	render(path, "")
	return result.String()
}
