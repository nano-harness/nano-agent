package filesystem

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

// ReadFileTool implements file viewing functionality with line range support
type ReadFileTool struct {
	workingDir  string
	config      map[string]interface{}
	readState   *ReadFileState // shared with EditTool to track which files have been read
	pathChecker *sandbox.PathChecker
}

// NewReadFileTool creates a new ReadFileTool instance.
// checker may be nil (no path-level sandbox checks).
func NewReadFileTool(workingDir string, config map[string]interface{}, checker *sandbox.PathChecker) *ReadFileTool {
	if config == nil {
		config = make(map[string]interface{})
	}
	pc := checker
	if pc == nil {
		pc = sandbox.NewPathChecker(nil)
	}
	return &ReadFileTool{
		workingDir:  workingDir,
		config:      config,
		pathChecker: pc,
	}
}

// NewReadFileToolWithState creates a ReadFileTool that records reads into the given ReadFileState.
// checker may be nil (no path-level sandbox checks).
func NewReadFileToolWithState(workingDir string, config map[string]interface{}, checker *sandbox.PathChecker, state *ReadFileState) *ReadFileTool {
	t := NewReadFileTool(workingDir, config, checker)
	t.readState = state
	return t
}

// Name returns the tool name
func (t *ReadFileTool) Name() string {
	return "read_file"
}

// Description returns the tool description
func (t *ReadFileTool) Description() string {
	return "Intelligent file reading tool with multiple display modes. Uses normal view for small files, automatically switches to code skeleton parsing mode for large code files (over 100K characters or 2000 lines). Supports intelligent structure analysis for 15+ programming languages (Go, Python, JavaScript, TypeScript, Java, C++, C#, Rust, PHP, Ruby, Swift, Kotlin, Scala, Dart, Shell, etc.), providing overview of code elements like functions, classes, and interfaces."
}

// Category returns the tool category
func (t *ReadFileTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryFileSystem
}

// RequiresConfirmation returns whether the tool requires confirmation
func (t *ReadFileTool) RequiresConfirmation() bool {
	return false // Viewing files is safe
}

// ConcurrencySafe returns true: reading files does not mutate shared state.
func (t *ReadFileTool) ConcurrencySafe() bool { return true }

// Schema returns the tool schema
func (t *ReadFileTool) Schema() *interfaces.ToolSchema {
	filePathProp := interfaces.NewStringProperty("Path to the file to view")
	filePathProp.Examples = []string{"/Users/user/project/main.go", "./src/utils.py", "/etc/config.yml"}
	filePathProp.Usage = "Use absolute paths or relative paths within workspace. For code files (.go, .js/.jsx, .ts/.tsx, .py, .java, .c/.cpp/.cc/.cxx, .h/.hpp, .rs, .php, .rb, .cs, .kt, .swift), very large files (>100K chars or >2000 lines) automatically use skeleton parsing when viewing entire file. Path validation ensures security."

	startLineProp := interfaces.NewNumberProperty("Starting line number (1-indexed, optional)")
	startLineProp.Examples = []string{"1", "25", "100"}
	startLineProp.Usage = "Use to read from a specific line. Defaults to beginning of file if not specified. When specified, prefers normal view over skeleton parsing for targeted access."

	endLineProp := interfaces.NewNumberProperty("Ending line number (1-indexed, optional)")
	endLineProp.Examples = []string{"50", "200", "500"}
	endLineProp.Usage = "Use with start_line for ranges. Defaults to end of file if not specified. Line ranges override automatic skeleton mode unless requesting >80% of a very large file (>100K chars or >2000 lines)."

	maxLinesProp := interfaces.NewNumberProperty("Maximum number of lines to display (default: 100)")
	maxLinesProp.Examples = []string{"50", "100", "500"}
	maxLinesProp.Usage = "Limits output size for normal view mode. Default is 100 lines. Useful for large files to prevent overwhelming display. Ignored when skeleton mode is automatically activated for very large files (>100K chars or >2000 lines)."

	showLineNumbersProp := interfaces.NewBooleanProperty("Show line numbers (default: true)")
	showLineNumbersProp.Examples = []string{"true", "false"}
	showLineNumbersProp.Usage = "Display line numbers for easier navigation and reference. In skeleton mode, shows original line numbers from source file for accurate positioning."

	return interfaces.CreateSchema(
		"View file contents with intelligent display modes. For very large code files (>100K characters or >2000 lines), automatically switches to code skeleton parsing mode when viewing the entire file or >80% of content. Supports 20+ programming languages including Go, JavaScript/TypeScript, Python, Java, C/C++, Rust, PHP, Ruby, C#, Kotlin, Swift. Line ranges override skeleton mode unless file is extremely large.",
		map[string]*interfaces.PropertySchema{
			"file_path":         filePathProp,
			"start_line":        startLineProp,
			"end_line":          endLineProp,
			"max_lines":         maxLinesProp,
			"show_line_numbers": showLineNumbersProp,
		},
		[]string{"file_path"},
	)
}

// Execute runs the tool with provided parameters
func (t *ReadFileTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	if params == nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "tool parameters are missing",
			UserContent: "❌ Failed to read file: tool parameters are missing",
			LLMContent:  "read_file failed: tool parameters are missing",
		}, nil
	}

	// Extract file path
	filePath, ok := params["file_path"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "file_path parameter is required and must be a string",
			UserContent: "❌ Failed to read file: file_path parameter is required and must be a string",
			LLMContent:  "read_file failed: file_path parameter is required and must be a string",
		}, nil
	}

	// Validate and clean path
	absPath, err := t.validatePath(filePath)
	if err != nil {
		msg := fmt.Sprintf("Invalid file path: %v", err)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       msg,
			UserContent: "❌ Failed to read file: " + msg,
			LLMContent:  "read_file failed: " + msg,
		}, nil
	}

	// Sandbox path check
	if err := t.pathChecker.Check(absPath, sandbox.OpRead); err != nil {
		msg := fmt.Sprintf("Access denied: %v", err)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       msg,
			UserContent: "❌ Failed to read file: " + msg,
			LLMContent:  "read_file failed: " + msg,
		}, nil
	}

	// Check if file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		msg := fmt.Sprintf("File does not exist: %s", absPath)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       msg,
			UserContent: "❌ Failed to read file: " + msg,
			LLMContent:  "read_file failed: " + msg,
		}, nil
	}

	// Read file content
	var data []byte
	data, err = os.ReadFile(absPath)
	if err != nil {
		msg := fmt.Sprintf("Failed to read file: %v", err)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       msg,
			UserContent: "❌ Failed to read file: " + msg,
			LLMContent:  "read_file failed: " + msg,
		}, nil
	}

	// Detect if file is binary before converting to string
	detection := DetectBinaryContent(data)
	if detection.IsBinary {
		// Check if it's a known binary file extension
		if IsBinaryFileExtension(absPath) {
			msg := fmt.Sprintf("Cannot read binary file: %s (detected as %s)", absPath, detection.Encoding)
			return &interfaces.ToolResult{
				Success:     false,
				Error:       msg,
				UserContent: "❌ Cannot read binary file: " + filepath.Base(absPath) + " (binary files are not supported for text viewing)",
				LLMContent:  "read_file failed: " + msg,
			}, nil
		}
		// For files that appear binary but don't have binary extensions, provide a warning
		msg := fmt.Sprintf("Warning: File appears to contain binary data: %s (encoding: %s, confidence: %.2f)", absPath, detection.Encoding, detection.Confidence)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       msg,
			UserContent: "⚠️ Cannot read file: " + filepath.Base(absPath) + " appears to contain binary data and may not display correctly as text",
			LLMContent:  "read_file failed: " + msg,
		}, nil
	}

	// Safe to convert to string for text files
	content := string(data)

	// Record that this file has been successfully read so that EditTool allows editing it.
	if t.readState != nil {
		t.readState.Mark(absPath)
	}

	// Smart strategy for large file handling
	if t.shouldUseCodeSkeleton(content, absPath, params) {
		// Use code skeleton parsing for large code files
		return t.parseCodeSkeletonForLargeFile(ctx, absPath, content, params)
	}

	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	// Extract optional parameters
	startLine := 1
	if startParam, ok := params["start_line"]; ok {
		if startFloat, ok := startParam.(float64); ok {
			startLine = int(startFloat)
		}
	}

	endLine := totalLines
	if endParam, ok := params["end_line"]; ok {
		if endFloat, ok := endParam.(float64); ok {
			endLine = int(endFloat)
		}
	}

	maxLines := 100 // Default value
	if maxLinesConfig, ok := t.config["read_file_max_lines"].(int); ok && maxLinesConfig > 0 {
		maxLines = maxLinesConfig
	}

	if maxParam, ok := params["max_lines"]; ok {
		if maxFloat, ok := maxParam.(float64); ok && maxFloat > 0 {
			maxLines = int(maxFloat)
		}
	}

	showLineNumbers := true
	if showParam, ok := params["show_line_numbers"]; ok {
		if showBool, ok := showParam.(bool); ok {
			showLineNumbers = showBool
		}
	}

	// Validate line ranges
	if startLine < 1 {
		startLine = 1
	}
	if endLine > totalLines {
		endLine = totalLines
	}
	if startLine > endLine {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "start_line must be less than or equal to end_line",
			UserContent: "❌ Failed to read file: start_line must be less than or equal to end_line",
			LLMContent:  "read_file failed: start_line must be less than or equal to end_line",
		}, nil
	}

	// Apply max_lines limit
	if endLine-startLine+1 > maxLines {
		endLine = startLine + maxLines - 1
	}

	// Extract the requested lines
	displayLines := lines[startLine-1 : endLine]
	displayContent := strings.Join(displayLines, "\n")

	// Format output
	var result strings.Builder
	fmt.Fprintf(&result, "File: %s\n", absPath)
	fmt.Fprintf(&result, "Lines: %d-%d (total: %d)\n", startLine, endLine, totalLines)
	fmt.Fprintf(&result, "Size: %d bytes\n", len(content))
	result.WriteString(strings.Repeat("-", 60) + "\n")

	if showLineNumbers {
		// Calculate the width needed for line numbers
		lineNumWidth := len(strconv.Itoa(endLine))
		for i, line := range displayLines {
			lineNum := startLine + i
			fmt.Fprintf(&result, "%*d: %s\n", lineNumWidth, lineNum, line)
		}
	} else {
		result.WriteString(displayContent)
		if !strings.HasSuffix(displayContent, "\n") {
			result.WriteString("\n")
		}
	}

	// Prepare metadata
	metadata := map[string]interface{}{
		"file_path":         absPath,
		"total_lines":       totalLines,
		"start_line":        startLine,
		"end_line":          endLine,
		"displayed_lines":   endLine - startLine + 1,
		"file_size":         len(content),
		"show_line_numbers": showLineNumbers,
	}

	// Check if file was truncated
	truncated := false
	if endLine < totalLines {
		truncated = true
		metadata["truncated"] = true
		metadata["remaining_lines"] = totalLines - endLine
	}

	userContent := result.String()
	if truncated {
		userContent += fmt.Sprintf("\n📝 Note: File has %d more lines. Use start_line and end_line parameters to view more.", totalLines-endLine)
	}

	// Wrap LLM content with isolation tags to protect against prompt injection
	llmContentRaw := fmt.Sprintf("Viewed file %s (lines %d-%d of %d):\n%s", absPath, startLine, endLine, totalLines, displayContent)
	llmContent := wrapFileContentForLLM(llmContentRaw, absPath)

	return &interfaces.ToolResult{
		Success:     true,
		Data:        displayContent,
		Metadata:    metadata,
		UserContent: userContent,
		LLMContent:  llmContent,
	}, nil
}

// isCodeFile checks if the file is a code file that can benefit from skeleton parsing
func (t *ReadFileTool) isCodeFile(filePath string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))
	codeExtensions := map[string]bool{
		"go":    true,
		"js":    true,
		"jsx":   true,
		"ts":    true,
		"tsx":   true,
		"py":    true,
		"java":  true,
		"c":     true,
		"cpp":   true,
		"cc":    true,
		"cxx":   true,
		"h":     true,
		"hpp":   true,
		"rs":    true,
		"php":   true,
		"rb":    true,
		"cs":    true,
		"kt":    true,
		"swift": true,
	}
	return codeExtensions[ext]
}

// parseCodeSkeletonForLargeFile uses code skeleton parsing for large files
func (t *ReadFileTool) parseCodeSkeletonForLargeFile(ctx context.Context, filePath, content string, originalParams map[string]interface{}) (*interfaces.ToolResult, error) {
	// Create a code skeleton tool instance
	skeletonTool := NewCodeSkeletonTool(t.workingDir, t.config, nil)

	// Prepare parameters for skeleton parsing
	skeletonParams := map[string]interface{}{
		"file_path": filePath,
	}

	// Execute skeleton parsing
	skeletonResult, err := skeletonTool.Execute(ctx, skeletonParams)
	if err != nil {
		// If skeleton parsing fails, fall back to truncated regular reading
		return t.fallbackToTruncatedRead(filePath, content, originalParams)
	}

	if !skeletonResult.Success {
		// If skeleton parsing fails, fall back to truncated regular reading
		return t.fallbackToTruncatedRead(filePath, content, originalParams)
	}

	// Enhance the skeleton result with file info
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	fileSize := len(content)

	// Create enhanced user content
	var result strings.Builder
	fmt.Fprintf(&result, "📄 Large File Auto-Parsed: %s\n", filePath)
	fmt.Fprintf(&result, "📊 File Info: %d lines, %d characters\n", totalLines, fileSize)

	// Determine reason for skeleton usage
	reason := "file size and complexity"
	if fileSize > 100000 {
		reason = "very large file size"
	} else if totalLines > 2000 {
		reason = "high line count"
	}

	fmt.Fprintf(&result, "🔍 Auto-switched to Code Skeleton view (%s)\n", reason)
	result.WriteString(strings.Repeat("=", 70) + "\n\n")
	result.WriteString(skeletonResult.UserContent)
	result.WriteString("\n\n💡 Navigation Tips:")
	result.WriteString("\n   • Use start_line/end_line parameters to view specific sections")
	result.WriteString("\n   • Example: read_file with start_line=100, end_line=150")
	result.WriteString("\n   • The skeleton above shows line numbers for easy navigation")

	// Create enhanced LLM content with isolation wrapping
	llmContentRaw := fmt.Sprintf("Large file %s (%d chars, %d lines) auto-parsed with code skeleton:\n\n%s",
		filePath, fileSize, totalLines, skeletonResult.LLMContent)
	llmContent := wrapFileContentForLLM(llmContentRaw, filePath)

	// Prepare enhanced metadata
	metadata := map[string]interface{}{
		"file_path":           filePath,
		"total_lines":         totalLines,
		"file_size":           fileSize,
		"auto_skeleton_used":  true,
		"skeleton_reason":     "file_too_large",
		"character_threshold": 16000,
		"is_code_file":        true,
	}

	// Merge skeleton metadata
	if skeletonResult.Metadata != nil {
		for k, v := range skeletonResult.Metadata {
			if k != "file_path" { // Don't override our file_path
				metadata["skeleton_"+k] = v
			}
		}
	}

	return &interfaces.ToolResult{
		Success:     true,
		Data:        skeletonResult.Data,
		Metadata:    metadata,
		UserContent: result.String(),
		LLMContent:  llmContent,
	}, nil
}

// fallbackToTruncatedRead provides a fallback when skeleton parsing fails
func (t *ReadFileTool) fallbackToTruncatedRead(filePath, content string, originalParams map[string]interface{}) (*interfaces.ToolResult, error) { //nolint:revive
	const maxDisplayChars = 8000 // Show first 8000 characters as fallback

	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	// Truncate content
	displayContent := content
	truncated := false
	if len(content) > maxDisplayChars {
		displayContent = content[:maxDisplayChars]
		truncated = true
	}

	// Format output
	var result strings.Builder
	fmt.Fprintf(&result, "📄 Large File (Truncated): %s\n", filePath)
	fmt.Fprintf(&result, "📊 File Info: %d lines, %d characters\n", totalLines, len(content))
	fmt.Fprintf(&result, "⚠️  Showing first %d characters (file too large)\n", len(displayContent))
	result.WriteString(strings.Repeat("-", 60) + "\n")
	result.WriteString(displayContent)

	if truncated {
		result.WriteString("\n\n... [TRUNCATED] ...")
		fmt.Fprintf(&result, "\n💡 File has %d more characters. Use line ranges or code skeleton parsing for better navigation.", len(content)-len(displayContent))
	}

	metadata := map[string]interface{}{
		"file_path":         filePath,
		"total_lines":       totalLines,
		"file_size":         len(content),
		"displayed_chars":   len(displayContent),
		"truncated":         truncated,
		"truncation_reason": "file_too_large_fallback",
	}

	llmContent := fmt.Sprintf("Large file %s (truncated to %d chars of %d):\n%s",
		filePath, len(displayContent), len(content), displayContent)

	return &interfaces.ToolResult{
		Success:     true,
		Data:        displayContent,
		Metadata:    metadata,
		UserContent: result.String(),
		LLMContent:  llmContent,
	}, nil
}

// shouldUseCodeSkeleton determines whether to use code skeleton parsing based on intelligent criteria
func (t *ReadFileTool) shouldUseCodeSkeleton(content, filePath string, params map[string]interface{}) bool {
	if !t.isCodeFile(filePath) {
		return false
	}

	lines := strings.Split(content, "\n")
	lineCount := len(lines)
	charCount := len(content)

	// Only use for very large files
	if charCount <= 100000 && lineCount <= 2000 {
		return false
	}

	// Check if viewing most of the file
	_, hasStartLine := params["start_line"]
	_, hasEndLine := params["end_line"]
	if !hasStartLine && !hasEndLine {
		return true // Viewing entire file, which is large
	}

	// If ranges specified, calculate fraction
	startLine := 1
	if startParam, ok := params["start_line"].(float64); ok {
		startLine = int(startParam)
	}
	endLine := lineCount
	if endParam, ok := params["end_line"].(float64); ok {
		endLine = int(endParam)
	}

	requestedLines := endLine - startLine + 1
	fraction := float64(requestedLines) / float64(lineCount)

	// Consider "most" as >80%
	if fraction > 0.8 {
		return true
	}

	return false
}

func (t *ReadFileTool) validatePath(path string) (string, error) {
	return validatePathCommon(t.workingDir, path)
}

// wrapFileContentForLLM wraps file content with isolation tags
func wrapFileContentForLLM(content, filePath string) string {
	// Escape file path for safe XML attribute usage
	escapedPath := html.EscapeString(filePath)
	// Use consistent "file:" prefix format like web tools use "search:" prefix
	return fmt.Sprintf("<external_data source=\"file:%s\" type=\"file\">\n%s\n</external_data>", escapedPath, content)
}
