package search

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
	. "github.com/nano-harness/nano-agent/pkg/tools/filesystem" //nolint:revive,staticcheck
)

// GrepTool implements content search functionality with regex support
type GrepTool struct {
	workingDir  string
	config      map[string]interface{}
	pathChecker *sandbox.PathChecker
}

// NewGrepTool creates a new GrepTool instance
func NewGrepTool(workingDir string, config map[string]interface{}, pathChecker *sandbox.PathChecker) *GrepTool {
	if config == nil {
		config = make(map[string]interface{})
	}
	return &GrepTool{
		workingDir:  workingDir,
		config:      config,
		pathChecker: pathChecker,
	}
}

func (t *GrepTool) Name() string { //nolint:revive
	return "search_file_content"
}

func (t *GrepTool) Description() string { //nolint:revive
	return "Search file contents using regex patterns with multiple search strategies"
}

func (t *GrepTool) Category() interfaces.ToolCategory { //nolint:revive
	return interfaces.CategorySearch
}

func (t *GrepTool) RequiresConfirmation() bool { //nolint:revive
	return false
}

// ConcurrencySafe returns true: grep is a read-only search operation.
func (t *GrepTool) ConcurrencySafe() bool { return true }

func (t *GrepTool) Schema() *interfaces.ToolSchema { //nolint:revive
	patternProp := interfaces.NewStringProperty("Regex pattern to search for")
	patternProp.Examples = []string{`func\s+\w+`, `import\s+.*`, `TODO|FIXME`, `\b\w+@\w+\.\w+\b`}
	patternProp.Usage = "Regular expression pattern. Use Go regex syntax. Common patterns: \\b for word boundaries, \\s+ for whitespace, .* for any characters."

	pathProp := interfaces.NewStringProperty("Directory path to search in (defaults to current working directory)")
	pathProp.Examples = []string{"/Users/user/project", "./src", "/etc/config"}
	pathProp.Usage = "Use absolute or relative paths within workspace. Defaults to current directory if omitted."

	includeProp := interfaces.NewStringProperty("File pattern to include (e.g., '*.go', '*.js')")
	includeProp.Examples = []string{"*.go", "*.js", "*.py", "*.md"}
	includeProp.Usage = "Shell glob patterns to include specific file types. Multiple patterns not supported - use one at a time."

	excludeProp := interfaces.NewStringProperty("File pattern to exclude")
	excludeProp.Examples = []string{"*.test.go", "node_modules", "*.min.js", "vendor"}
	excludeProp.Usage = "Shell glob patterns to exclude files/directories. Helps filter out unwanted results."

	caseSensitiveProp := interfaces.NewBooleanProperty("Case sensitive search")
	caseSensitiveProp.Examples = []string{"true", "false"}
	caseSensitiveProp.Usage = "Controls case sensitivity for pattern matching. Default is false (case-insensitive)."

	maxResultsProp := interfaces.NewNumberProperty("Maximum number of results to return")
	maxResultsProp.Examples = []string{"10", "50", "100"}
	maxResultsProp.Usage = "Limits output size. Use lower values for broad searches to prevent overwhelming results."

	showContextProp := interfaces.NewBooleanProperty("Show context lines around matches")
	showContextProp.Examples = []string{"true", "false"}
	showContextProp.Usage = "Shows surrounding lines for better understanding of matches. Default is true."

	contextLinesProp := interfaces.NewNumberProperty("Number of context lines to show (default: 2)")
	contextLinesProp.Examples = []string{"1", "2", "5"}
	contextLinesProp.Usage = "Number of lines before and after each match when show_context is true. Default is 2."

	return interfaces.CreateSchema(
		"Search file contents using regex patterns",
		map[string]*interfaces.PropertySchema{
			"pattern":        patternProp,
			"path":           pathProp,
			"include":        includeProp,
			"exclude":        excludeProp,
			"case_sensitive": caseSensitiveProp,
			"max_results":    maxResultsProp,
			"show_context":   showContextProp,
			"context_lines":  contextLinesProp,
		},
		[]string{"pattern"},
	)
}

type SearchResult struct { //nolint:revive
	File       string   `json:"file"`
	LineNumber int      `json:"line_number"`
	Line       string   `json:"line"`
	Match      string   `json:"match"`
	Context    []string `json:"context"`
	StartLine  int      `json:"start_line"`
	EndLine    int      `json:"end_line"`
}

func (t *GrepTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) { //nolint:revive
	if params == nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "tool parameters are missing",
			UserContent: "❌ Failed to search: tool parameters are missing",
			LLMContent:  "search_file_content failed: tool parameters are missing",
		}, nil
	}

	// Extract parameters
	pattern, ok := params["pattern"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "pattern parameter is required and must be a string",
			UserContent: "❌ Failed to search: pattern parameter is required and must be a string",
			LLMContent:  "search_file_content failed: pattern parameter is required and must be a string",
		}, nil
	}

	// Validate regex pattern
	if _, err := regexp.Compile(pattern); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("Invalid regex pattern: %v", err),
			UserContent: "❌ Failed to search: Invalid regex pattern: " + err.Error(),
			LLMContent:  "search_file_content failed: Invalid regex pattern: " + err.Error(),
		}, nil
	}

	// Get optional parameters
	searchPath := t.workingDir
	if pathParam, ok := params["path"].(string); ok && pathParam != "" {
		searchPath = pathParam
	}

	include := ""
	if includeParam, ok := params["include"].(string); ok {
		include = includeParam
	}

	exclude := ""
	if excludeParam, ok := params["exclude"].(string); ok {
		exclude = excludeParam
	}

	caseSensitive := false
	if caseSensitiveParam, ok := params["case_sensitive"]; ok {
		caseSensitive, _ = caseSensitiveParam.(bool)
	}

	// Get max results from config
	maxResults := 0
	if maxConfig, ok := t.config["search_max_results"].(int); ok && maxConfig > 0 {
		maxResults = maxConfig
	}
	if maxResultsParam, ok := params["max_results"]; ok {
		if maxResultsFloat, ok := maxResultsParam.(float64); ok && maxResultsFloat > 0 {
			maxResults = int(maxResultsFloat)
		}
	}

	showContext := true
	if showContextParam, ok := params["show_context"]; ok {
		showContext, _ = showContextParam.(bool)
	}

	contextLines := 0
	if contextLinesParam, ok := params["context_lines"]; ok {
		if contextLinesFloat, ok := contextLinesParam.(float64); ok {
			contextLines = int(contextLinesFloat)
		}
	}

	// Validate and clean path
	absPath, err := t.validatePath(searchPath)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("Invalid path: %v", err),
			UserContent: "❌ Failed to search: Invalid path: " + err.Error(),
			LLMContent:  "search_file_content failed: Invalid path: " + err.Error(),
		}, nil
	}

	// Validate path against sandbox restrictions.
	if t.pathChecker != nil {
		if err := t.pathChecker.Check(absPath, sandbox.OpRead); err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				Error:       "path access denied: " + err.Error(),
				UserContent: "❌ Failed to search: " + err.Error(),
				LLMContent:  "search_file_content failed: " + err.Error(),
			}, nil
		}
	}

	// Perform search using multiple strategies
	results, err := t.performSearch(ctx, pattern, absPath, include, exclude, caseSensitive, maxResults, showContext, contextLines)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("Search failed: %v", err),
			UserContent: "❌ Failed to search: " + err.Error(),
			LLMContent:  "search_file_content failed: " + err.Error(),
		}, nil
	}

	// Sort results by file path and line number
	sort.Slice(results, func(i, j int) bool {
		if results[i].File != results[j].File {
			return results[i].File < results[j].File
		}
		return results[i].LineNumber < results[j].LineNumber
	})

	// Prepare metadata
	metadata := map[string]interface{}{
		"pattern":        pattern,
		"path":           absPath,
		"include":        include,
		"exclude":        exclude,
		"case_sensitive": caseSensitive,
		"total_results":  len(results),
		"truncated":      len(results) >= maxResults,
		"show_context":   showContext,
		"context_lines":  contextLines,
	}

	// Format content for display
	userContent := t.formatForUser(results, metadata)
	llmContentRaw := t.formatForLLM(results, metadata)

	// Wrap LLM content with isolation tags for grep results
	llmContent := wrapGrepContentForLLM(llmContentRaw, pattern)

	return &interfaces.ToolResult{
		Success:     true,
		Data:        results,
		Metadata:    metadata,
		LLMContent:  llmContent,
		UserContent: userContent,
	}, nil
}

func (t *GrepTool) validatePath(path string) (string, error) {
	// Clean the path
	cleaned := filepath.Clean(path)

	// Convert to absolute path
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %v", err)
	}

	// Check if path is within working directory (security check)
	workingDirAbs, err := filepath.Abs(t.workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to get working directory absolute path: %v", err)
	}

	// When working directory is the filesystem root, allow all paths.
	if workingDirAbs == string(filepath.Separator) {
		return absPath, nil
	}

	relPath, err := filepath.Rel(workingDirAbs, absPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path outside working directory not allowed: %s", path)
	}

	return absPath, nil
}

func (t *GrepTool) performSearch(ctx context.Context, pattern, path, include, exclude string, caseSensitive bool, maxResults int, showContext bool, contextLines int) ([]SearchResult, error) {
	// Try different search strategies in order of preference
	// Reorder strategies to avoid expensive zoekt indexing in daemon mode
	strategies := []func() ([]SearchResult, error){
		func() ([]SearchResult, error) {
			return t.searchWithRipgrep(ctx, pattern, path, include, exclude, caseSensitive, maxResults, showContext, contextLines)
		},
		func() ([]SearchResult, error) {
			return t.searchWithGitGrep(ctx, pattern, path, include, exclude, caseSensitive, maxResults, showContext, contextLines)
		},
		func() ([]SearchResult, error) {
			return t.searchWithSystemGrep(ctx, pattern, path, include, exclude, caseSensitive, maxResults, showContext, contextLines)
		},
		func() ([]SearchResult, error) {
			return t.searchWithGo(ctx, pattern, path, include, exclude, caseSensitive, maxResults, showContext, contextLines)
		},
		// Only try zoekt if other strategies fail and we have enough time
		func() ([]SearchResult, error) {
			// Check if we have enough time left for potentially expensive indexing
			deadline, ok := ctx.Deadline()
			if ok && time.Until(deadline) < 30*time.Second {
				return nil, fmt.Errorf("insufficient time for zoekt indexing")
			}
			return t.searchWithZoekt(ctx, pattern, path, include, exclude, caseSensitive, maxResults, showContext, contextLines)
		},
	}

	var lastErr error
	for _, strategy := range strategies {
		// Check context cancellation before trying each strategy
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		results, err := strategy()
		if err == nil {
			return results, nil
		}
		lastErr = err
	}

	// If all strategies fail, return the last error
	return nil, fmt.Errorf("all search strategies failed, last error: %w", lastErr)
}

func (t *GrepTool) searchWithRipgrep(ctx context.Context, pattern, path, include, exclude string, caseSensitive bool, maxResults int, showContext bool, contextLines int) ([]SearchResult, error) {
	args := []string{
		"--json",
		"--no-heading",
		"--line-number",
	}

	if !caseSensitive {
		args = append(args, "--ignore-case")
	}

	if showContext {
		args = append(args, fmt.Sprintf("--context=%d", contextLines))
	}

	if include != "" {
		args = append(args, "--glob", include)
	}

	if exclude != "" {
		args = append(args, "--glob", "!"+exclude)
	}

	if maxResults > 0 {
		args = append(args, fmt.Sprintf("--max-results=%d", maxResults))
	}

	args = append(args, "--", pattern, path)

	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = t.workingDir

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// ripgrep exits with 1 if no matches are found.
			if exitErr.ExitCode() == 1 {
				return []SearchResult{}, nil
			}
			return nil, fmt.Errorf("ripgrep failed with exit code %d: %v, stderr: %s", exitErr.ExitCode(), err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("ripgrep failed: %v, output: %s", err, string(output))
	}

	return t.parseRipgrepOutput(string(output))
}

func (t *GrepTool) searchWithGitGrep(ctx context.Context, pattern, path, include, exclude string, caseSensitive bool, maxResults int, showContext bool, contextLines int) ([]SearchResult, error) { //nolint:revive
	args := []string{"grep", "--line-number"}

	if !caseSensitive {
		args = append(args, "--ignore-case")
	}

	if showContext {
		args = append(args, fmt.Sprintf("--context=%d", contextLines))
	}

	args = append(args, pattern)

	// Add path specification
	if path != t.workingDir {
		relPath, err := filepath.Rel(t.workingDir, path)
		if err == nil {
			args = append(args, "--", relPath)
		}
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = t.workingDir

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git grep failed: %v", err)
	}

	return t.parseGrepOutput(string(output), maxResults)
}

func (t *GrepTool) searchWithSystemGrep(ctx context.Context, pattern, path, include, exclude string, caseSensitive bool, maxResults int, showContext bool, contextLines int) ([]SearchResult, error) {
	args := []string{"-rn"}

	if !caseSensitive {
		args = append(args, "-i")
	}

	if showContext {
		args = append(args, fmt.Sprintf("-C%d", contextLines))
	}

	if include != "" {
		args = append(args, "--include", include)
	}

	if exclude != "" {
		args = append(args, "--exclude", exclude)
	}

	args = append(args, pattern, path)

	cmd := exec.CommandContext(ctx, "grep", args...)
	cmd.Dir = t.workingDir

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("grep failed: %v", err)
	}

	return t.parseGrepOutput(string(output), maxResults)
}

func (t *GrepTool) searchWithGo(ctx context.Context, pattern, path, include, exclude string, caseSensitive bool, maxResults int, showContext bool, contextLines int) ([]SearchResult, error) {
	// Check context at the beginning
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Fallback Go implementation
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %v", err)
	}

	if !caseSensitive {
		regex, err = regexp.Compile("(?i)" + pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %v", err)
		}
	}

	var results []SearchResult
	fileCount := 0

	err = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		// Check context periodically during file traversal
		fileCount++
		if fileCount%100 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		if info.IsDir() {
			return nil
		}

		// Skip very large files to avoid timeout
		if info.Size() > 50*1024*1024 { // 50MB
			return nil
		}

		// Apply include/exclude filters
		if include != "" {
			if matched, _ := filepath.Match(include, info.Name()); !matched {
				return nil
			}
		}

		if exclude != "" {
			if matched, _ := filepath.Match(exclude, info.Name()); matched {
				return nil
			}
		}

		// Search in file
		fileResults, err := t.searchInFile(filePath, regex, showContext, contextLines)
		if err != nil {
			return nil // Skip files we can't read
		}

		results = append(results, fileResults...)

		// Stop if we've reached max results
		if len(results) >= maxResults {
			return fmt.Errorf("max results reached")
		}

		return nil
	})

	if err != nil && err.Error() != "max results reached" {
		return nil, err
	}

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results, nil
}

func (t *GrepTool) searchInFile(filePath string, regex *regexp.Regexp, showContext bool, contextLines int) ([]SearchResult, error) {
	var content []byte
	var err error

	content, err = os.ReadFile(filePath)

	if err != nil {
		return nil, err
	}

	// Skip binary files to avoid processing non-text content
	detection := DetectBinaryContent(content)
	if detection.IsBinary {
		// Skip binary files silently - they shouldn't be searched for text patterns
		return nil, nil
	}

	lines := strings.Split(string(content), "\n")
	var results []SearchResult

	for i, line := range lines {
		if regex.MatchString(line) {
			match := regex.FindString(line)
			result := SearchResult{
				File:       filePath,
				LineNumber: i + 1,
				Line:       line,
				Match:      match,
			}

			if showContext {
				start := i - contextLines
				end := i + contextLines + 1

				if start < 0 {
					start = 0
				}
				if end > len(lines) {
					end = len(lines)
				}

				result.Context = lines[start:end]
				result.StartLine = start + 1
				result.EndLine = end
			}

			results = append(results, result)
		}
	}

	return results, nil
}

func (t *GrepTool) parseRipgrepOutput(output string) ([]SearchResult, error) {
	var results []SearchResult
	decoder := json.NewDecoder(strings.NewReader(output))

	for decoder.More() {
		var entry struct {
			Type string `json:"type"`
			Data struct {
				Path struct {
					Text string `json:"text"`
				} `json:"path"`
				LineNumber int `json:"line_number"`
				Lines      struct {
					Text string `json:"text"`
				} `json:"lines"`
				Submatches []struct {
					Match struct {
						Text string `json:"text"`
					} `json:"match"`
				} `json:"submatches"`
				Context []struct {
					LineNumber int `json:"line_number"`
					Lines      struct {
						Text string `json:"text"`
					} `json:"lines"`
				} `json:"context"`
			} `json:"data"`
		}

		if err := decoder.Decode(&entry); err != nil {
			return nil, fmt.Errorf("failed to decode ripgrep JSON output: %v\nOutput: %s", err, output)
		}

		if entry.Type == "match" {
			result := SearchResult{
				File:       entry.Data.Path.Text,
				LineNumber: entry.Data.LineNumber,
				Line:       strings.TrimSuffix(entry.Data.Lines.Text, "\n"),
			}

			if len(entry.Data.Submatches) > 0 {
				result.Match = entry.Data.Submatches[0].Match.Text
			}

			if len(entry.Data.Context) > 0 {
				var contextLines []string
				for _, ctxLine := range entry.Data.Context {
					contextLines = append(contextLines, strings.TrimSuffix(ctxLine.Lines.Text, "\n"))
				}
				result.Context = contextLines
				result.StartLine = entry.Data.Context[0].LineNumber
				result.EndLine = entry.Data.Context[len(entry.Data.Context)-1].LineNumber
			}

			results = append(results, result)
		}
	}

	return results, nil
}

func (t *GrepTool) parseGrepOutput(output string, maxResults int) ([]SearchResult, error) {
	lines := strings.Split(output, "\n")
	var results []SearchResult

	for _, line := range lines {
		if line == "" {
			continue
		}

		// Parse format: file:line:content
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}

		file := parts[0]
		lineNumStr := parts[1]
		content := parts[2]

		// Parse line number
		var lineNum int
		if _, err := fmt.Sscanf(lineNumStr, "%d", &lineNum); err != nil {
			continue
		}

		result := SearchResult{
			File:       file,
			LineNumber: lineNum,
			Line:       content,
			Match:      content, // Simplified - would need to extract actual match
		}

		results = append(results, result)

		if len(results) >= maxResults {
			break
		}
	}

	return results, nil
}

func (t *GrepTool) formatForUser(results []SearchResult, metadata map[string]interface{}) string {
	var result strings.Builder

	pattern := metadata["pattern"].(string)
	path := metadata["path"].(string)
	totalResults := metadata["total_results"].(int)
	truncated := metadata["truncated"].(bool)

	fmt.Fprintf(&result, "🔍 Search: %s\n", pattern)
	fmt.Fprintf(&result, "📁 Path: %s\n", path)
	fmt.Fprintf(&result, "📊 Results: %d", totalResults)

	if truncated {
		result.WriteString(" (truncated)")
	}
	result.WriteString("\n")
	result.WriteString("─────────────────────────────────────\n")

	if len(results) == 0 {
		result.WriteString("❌ No matches found\n")
		return result.String()
	}

	currentFile := ""
	for _, res := range results {
		if res.File != currentFile {
			currentFile = res.File
			fmt.Fprintf(&result, "\n📄 %s\n", res.File)
		}

		fmt.Fprintf(&result, "   %d: %s\n", res.LineNumber, res.Line)

		// Show context if available
		if len(res.Context) > 0 {
			for j, contextLine := range res.Context {
				contextLineNum := res.StartLine + j
				if contextLineNum == res.LineNumber {
					fmt.Fprintf(&result, "→  %d: %s\n", contextLineNum, contextLine)
				} else {
					fmt.Fprintf(&result, "   %d: %s\n", contextLineNum, contextLine)
				}
			}
		}
	}

	return result.String()
}

func (t *GrepTool) formatForLLM(results []SearchResult, metadata map[string]interface{}) string {
	var result strings.Builder

	pattern := metadata["pattern"].(string)
	path := metadata["path"].(string)
	totalResults := metadata["total_results"].(int)

	fmt.Fprintf(&result, "Search pattern: %s\n", pattern)
	fmt.Fprintf(&result, "Search path: %s\n", path)
	fmt.Fprintf(&result, "Total results: %d\n\n", totalResults)

	if len(results) == 0 {
		result.WriteString("No matches found\n")
		return result.String()
	}

	for _, res := range results {
		fmt.Fprintf(&result, "File: %s\n", res.File)
		fmt.Fprintf(&result, "Line %d: %s\n", res.LineNumber, res.Line)

		if len(res.Context) > 0 {
			result.WriteString("Context:\n")
			for j, contextLine := range res.Context {
				contextLineNum := res.StartLine + j
				marker := "  "
				if contextLineNum == res.LineNumber {
					marker = "→ "
				}
				fmt.Fprintf(&result, "%s%d: %s\n", marker, contextLineNum, contextLine)
			}
		}
		result.WriteString("\n")
	}

	return result.String()
}

// wrapGrepContentForLLM wraps grep search result content with isolation tags
func wrapGrepContentForLLM(content, pattern string) string {
	// Escape pattern for safe XML attribute usage
	escapedPattern := html.EscapeString(pattern)
	return fmt.Sprintf("<external_data source=\"grep_search:%s\" type=\"search\">\n%s\n</external_data>", escapedPattern, content)
}
