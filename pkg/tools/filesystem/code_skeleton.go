package filesystem //nolint:revive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// CodeSkeletonTool implements code structure parsing for better LLM understanding of large files
type CodeSkeletonTool struct {
	workingDir string
	config     map[string]interface{}
}

// NewCodeSkeletonTool creates a new CodeSkeletonTool instance
func NewCodeSkeletonTool(workingDir string, config map[string]interface{}, _ interface{}) *CodeSkeletonTool {
	if config == nil {
		config = make(map[string]interface{})
	}
	return &CodeSkeletonTool{
		workingDir: workingDir,
		config:     config,
	}
}

func (t *CodeSkeletonTool) Name() string { //nolint:revive
	return "code_skeleton"
}

func (t *CodeSkeletonTool) Description() string { //nolint:revive
	return "Advanced code skeleton parsing tool that uses sophisticated regex parsing techniques to precisely identify and extract various code elements (functions, methods, classes, structs, interfaces, constants, variables, etc.) from code files. Provides accurate line numbers, complete signatures, and detailed metadata with intelligent comment extraction and scope detection. Automatically calculates precise end line numbers and code line statistics."
}

func (t *CodeSkeletonTool) Category() interfaces.ToolCategory { //nolint:revive
	return interfaces.CategoryFileSystem
}

func (t *CodeSkeletonTool) RequiresConfirmation() bool { //nolint:revive
	return false // Reading and parsing files is safe
}

// ConcurrencySafe returns true: only reads files, no mutation.
func (t *CodeSkeletonTool) ConcurrencySafe() bool { return true }

func (t *CodeSkeletonTool) Schema() *interfaces.ToolSchema { //nolint:revive
	filePathProp := interfaces.NewStringProperty("Path to the code file to parse")
	filePathProp.Examples = []string{"/Users/user/project/main.go", "./src/utils.py", "/etc/config.js", "./app.rs", "./main.cpp"}
	filePathProp.Usage = "Provide absolute or relative path to source code file. Supported extensions: .go (Go), .js/.jsx (JavaScript), .ts/.tsx (TypeScript), .py (Python), .java (Java), .php (PHP). Other languages (.c/.h/.cpp/.rs/.rb) are detected but use generic parsing. Path validation ensures security."

	return interfaces.CreateSchema(
		"Parse code structure and extract skeleton elements (functions, classes, interfaces, structs) with precise metadata. Uses language-specific regex patterns to identify code elements with accurate line numbers, signatures, and optional comments. Currently supports: Go (.go), JavaScript/TypeScript (.js/.jsx/.ts/.tsx), Python (.py), Java (.java), PHP (.php). Returns structured data with start/end lines, line counts, and element signatures.",
		map[string]*interfaces.PropertySchema{
			"file_path": filePathProp,
		},
		[]string{"file_path"},
	)
}

// SkeletonElement represents a parsed code structure element
type SkeletonElement struct {
	Type      string `json:"type"`       // function, class, interface, struct, etc.
	Name      string `json:"name"`       // name of the element
	Signature string `json:"signature"`  // full signature/declaration
	StartLine int    `json:"start_line"` // starting line number (1-based)
	EndLine   int    `json:"end_line"`   // ending line number (1-based, accurately calculated)
	LineCount int    `json:"line_count"` // total number of lines for this element
	Comment   string `json:"comment"`    // associated comment/docstring
	Body      string `json:"body"`       // simplified body (if requested)
}

func (t *CodeSkeletonTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) { //nolint:revive
	if params == nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "tool parameters are missing",
			UserContent: "❌ Failed to parse code skeleton: tool parameters are missing",
			LLMContent:  "code_skeleton failed: tool parameters are missing",
		}, nil
	}

	// Extract file path
	filePath, ok := params["file_path"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "file_path parameter is required and must be a string",
			UserContent: "❌ Failed to parse code skeleton: file_path parameter is required and must be a string",
			LLMContent:  "code_skeleton failed: file_path parameter is required and must be a string",
		}, nil
	}

	// Validate and clean path
	absPath, err := t.validatePath(filePath)
	if err != nil {
		msg := fmt.Sprintf("Invalid file path: %v", err)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       msg,
			UserContent: "❌ Failed to parse code skeleton: " + msg,
			LLMContent:  "code_skeleton failed: " + msg,
		}, nil
	}

	// Check if file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		msg := fmt.Sprintf("File does not exist: %s", absPath)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       msg,
			UserContent: "❌ Failed to parse code skeleton: " + msg,
			LLMContent:  "code_skeleton failed: " + msg,
		}, nil
	}

	// Hardcode default values since options are removed
	includeComments := true
	includeBody := false
	maxLinesPerFunction := 3

	// Read file content
	var data []byte
	data, err = os.ReadFile(absPath)
	if err != nil {
		msg := fmt.Sprintf("Failed to read file: %v", err)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       msg,
			UserContent: "❌ Failed to parse code skeleton: " + msg,
			LLMContent:  "code_skeleton failed: " + msg,
		}, nil
	}

	// Detect if file is binary before parsing
	detection := DetectBinaryContent(data)
	if detection.IsBinary {
		msg := fmt.Sprintf("Cannot parse code skeleton for binary file: %s (detected as %s, confidence: %.2f)", absPath, detection.Encoding, detection.Confidence)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       msg,
			UserContent: "❌ Failed to parse code skeleton: " + msg,
			LLMContent:  "code_skeleton failed: " + msg,
		}, nil
	}

	// Parse the code using regex patterns
	skeleton, err := t.parseCodeSkeleton(absPath, string(data), includeComments, includeBody, maxLinesPerFunction)
	if err != nil {
		msg := fmt.Sprintf("Failed to parse code structure: %v", err)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       msg,
			UserContent: "❌ Failed to parse code skeleton: " + msg,
			LLMContent:  "code_skeleton failed: " + msg,
		}, nil
	}

	// Format output
	userContent := t.formatSkeletonForUser(absPath, skeleton)
	llmContent := t.formatSkeletonForLLM(absPath, skeleton)

	// Calculate total lines for all elements
	totalElementLines := 0
	for _, element := range skeleton {
		totalElementLines += element.LineCount
	}

	// Prepare metadata
	metadata := map[string]interface{}{
		"file_path":           absPath,
		"total_elements":      len(skeleton),
		"total_element_lines": totalElementLines,
		"file_size":           len(data),
		"file_total_lines":    len(strings.Split(string(data), "\n")),
		"language":            t.detectLanguage(absPath),
	}

	return &interfaces.ToolResult{
		Success:     true,
		Data:        skeleton,
		Metadata:    metadata,
		UserContent: userContent,
		LLMContent:  llmContent,
	}, nil
}

func (t *CodeSkeletonTool) parseCodeSkeleton(filePath, content string, includeComments, includeBody bool, maxLinesPerFunction int) ([]SkeletonElement, error) {
	language := t.detectLanguage(filePath)
	lines := strings.Split(content, "\n")

	var elements []SkeletonElement

	switch language {
	case "go":
		elements = t.parseGoCode(lines, includeComments, includeBody, maxLinesPerFunction)
	case "javascript", "typescript":
		elements = t.parseJavaScriptCode(lines, includeComments, includeBody, maxLinesPerFunction)
	case "python":
		elements = t.parsePythonCode(lines, includeComments, includeBody, maxLinesPerFunction)
	case "java":
		elements = t.parseJavaCode(lines, includeComments, includeBody, maxLinesPerFunction)
	case "php":
		elements = t.parsePHPCode(lines, includeComments, includeBody, maxLinesPerFunction)
	default:
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	return elements, nil
}

func (t *CodeSkeletonTool) detectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".rs":
		return "rust"
	case ".php":
		return "php"
	case ".rb":
		return "ruby"
	default:
		return "generic"
	}
}

func (t *CodeSkeletonTool) parseGoCode(lines []string, includeComments, includeBody bool, maxLinesPerFunction int) []SkeletonElement {
	var elements []SkeletonElement

	// Regex patterns for Go code structures
	patterns := map[string]*regexp.Regexp{
		"package":   regexp.MustCompile(`^package\s+(\w+)`),
		"import":    regexp.MustCompile(`^import\s+(.+)`),
		"const":     regexp.MustCompile(`^const\s+(.+)`),
		"var":       regexp.MustCompile(`^var\s+(.+)`),
		"type":      regexp.MustCompile(`^type\s+(\w+)\s+(.+)`),
		"func":      regexp.MustCompile(`^func\s+(?:\([^)]*\)\s+)?(\w+)\s*\([^)]*\)(?:\s*\([^)]*\))?\s*(?:\w+\s*)?{?`),
		"interface": regexp.MustCompile(`^type\s+(\w+)\s+interface\s*{`),
		"struct":    regexp.MustCompile(`^type\s+(\w+)\s+struct\s*{`),
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		for elementType, pattern := range patterns {
			if matches := pattern.FindStringSubmatch(trimmed); matches != nil {
				element := SkeletonElement{
					Type:      elementType,
					StartLine: i + 1,
					Signature: trimmed,
				}

				// Extract name
				if len(matches) > 1 {
					element.Name = matches[1]
				}

				// Extract comment if requested
				if includeComments {
					element.Comment = t.extractComment(lines, i)
				}

				// Extract body and calculate accurate end line
				if elementType == "func" || elementType == "type" || elementType == "interface" || elementType == "struct" {
					body, endLine := t.extractGoBody(lines, i, maxLinesPerFunction, includeBody)
					if includeBody {
						element.Body = body
					}
					element.EndLine = endLine
				} else {
					element.EndLine = i + 1
				}

				// Calculate line count
				element.LineCount = element.EndLine - element.StartLine + 1

				elements = append(elements, element)
				break
			}
		}
	}

	return elements
}

func (t *CodeSkeletonTool) parsePHPCode(lines []string, includeComments, includeBody bool, maxLinesPerFunction int) []SkeletonElement {
	var elements []SkeletonElement

	patterns := map[string]*regexp.Regexp{
		"class":     regexp.MustCompile(`^(?:abstract\s+)?(?:final\s+)?class\s+(\w+)(?:\s+extends\s+\w+)?(?:\s+implements\s+[\w,\s]+)?\s*{`),
		"interface": regexp.MustCompile(`^interface\s+(\w+)(?:\s+extends\s+[\w,\s]+)?\s*{`),
		"trait":     regexp.MustCompile(`^trait\s+(\w+)\s*{`),
		"function":  regexp.MustCompile(`^(?:public|private|protected|static|abstract|final)?\s*function\s+(\w+)\s*\([^)]*\)(?:\s*:\s*[\w\\|]+)?\s*([;{])?$`),
		"include":   regexp.MustCompile(`^(?:include|require|include_once|require_once)\s+(.+);?`),
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		for elementType, pattern := range patterns {
			if matches := pattern.FindStringSubmatch(trimmed); matches != nil {
				element := SkeletonElement{
					Type:      elementType,
					StartLine: i + 1,
					Signature: trimmed,
				}

				if len(matches) > 1 {
					element.Name = matches[1]
				}

				if includeComments {
					element.Comment = t.extractComment(lines, i)
				}

				if elementType == "function" || elementType == "class" || elementType == "interface" || elementType == "trait" {
					// Check if function declaration ends with semicolon (interface method)
					if elementType == "function" && strings.HasSuffix(strings.TrimSpace(line), ";") {
						element.EndLine = i + 1
					} else {
						body, endLine := t.extractGoBody(lines, i, maxLinesPerFunction, includeBody)
						if includeBody {
							element.Body = body
						}
						element.EndLine = endLine
					}
				} else {
					element.EndLine = i + 1
				}

				element.LineCount = element.EndLine - element.StartLine + 1

				elements = append(elements, element)
				break
			}
		}
	}

	return elements
}

func (t *CodeSkeletonTool) parseJavaScriptCode(lines []string, includeComments, includeBody bool, maxLinesPerFunction int) []SkeletonElement {
	var elements []SkeletonElement

	patterns := map[string]*regexp.Regexp{
		"function":   regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*\([^)]*\)`),
		"arrow_func": regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s*)?\([^)]*\)\s*=>`),
		"class":      regexp.MustCompile(`^(?:export\s+)?class\s+(\w+)(?:\s+extends\s+\w+)?\s*{`),
		"method":     regexp.MustCompile(`^\s*(?:async\s+)?(\w+)\s*\([^)]*\)\s*{`),
		"import":     regexp.MustCompile(`^import\s+(.+)`),
		"export":     regexp.MustCompile(`^export\s+(.+)`),
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		for elementType, pattern := range patterns {
			if matches := pattern.FindStringSubmatch(trimmed); matches != nil {
				element := SkeletonElement{
					Type:      elementType,
					StartLine: i + 1,
					Signature: trimmed,
				}

				if len(matches) > 1 {
					element.Name = matches[1]
				}

				if includeComments {
					element.Comment = t.extractComment(lines, i)
				}

				if elementType == "function" || elementType == "arrow_func" || elementType == "class" || elementType == "method" {
					body, endLine := t.extractJavaScriptBody(lines, i, maxLinesPerFunction, includeBody)
					if includeBody {
						element.Body = body
					}
					element.EndLine = endLine
				} else {
					element.EndLine = i + 1
				}

				// Calculate line count
				element.LineCount = element.EndLine - element.StartLine + 1

				elements = append(elements, element)
				break
			}
		}
	}

	return elements
}

func (t *CodeSkeletonTool) parsePythonCode(lines []string, includeComments, includeBody bool, maxLinesPerFunction int) []SkeletonElement {
	var elements []SkeletonElement

	patterns := map[string]*regexp.Regexp{
		"import":   regexp.MustCompile(`^(?:from\s+\w+\s+)?import\s+(.+)`),
		"class":    regexp.MustCompile(`^class\s+(\w+)(?:\([^)]*\))?\s*:`),
		"function": regexp.MustCompile(`^def\s+(\w+)\s*\([^)]*\)\s*(?:->\s*[^:]+)?\s*:`),
		"async":    regexp.MustCompile(`^async\s+def\s+(\w+)\s*\([^)]*\)\s*(?:->\s*[^:]+)?\s*:`),
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		for elementType, pattern := range patterns {
			if matches := pattern.FindStringSubmatch(trimmed); matches != nil {
				element := SkeletonElement{
					Type:      elementType,
					StartLine: i + 1,
					Signature: trimmed,
				}

				if len(matches) > 1 {
					element.Name = matches[1]
				}

				if includeComments {
					element.Comment = t.extractComment(lines, i)
				}

				if elementType == "function" || elementType == "async" || elementType == "class" {
					body, endLine := t.extractPythonBody(lines, i, maxLinesPerFunction, includeBody)
					if includeBody {
						element.Body = body
					}
					element.EndLine = endLine
				} else {
					element.EndLine = i + 1
				}

				// Calculate line count
				element.LineCount = element.EndLine - element.StartLine + 1

				elements = append(elements, element)
				break
			}
		}
	}

	return elements
}

func (t *CodeSkeletonTool) parseJavaCode(lines []string, includeComments, includeBody bool, maxLinesPerFunction int) []SkeletonElement {
	var elements []SkeletonElement

	patterns := map[string]*regexp.Regexp{
		"package":     regexp.MustCompile(`^package\s+([a-zA-Z_][a-zA-Z0-9_.]*)\s*;`),
		"import":      regexp.MustCompile(`^import\s+(?:static\s+)?([a-zA-Z_][a-zA-Z0-9_.*]*)\s*;`),
		"class":       regexp.MustCompile(`^(?:public\s+|private\s+|protected\s+)?(?:abstract\s+|final\s+)?class\s+(\w+)(?:\s+extends\s+\w+)?(?:\s+implements\s+[\w,\s]+)?\s*{`),
		"interface":   regexp.MustCompile(`^(?:public\s+|private\s+|protected\s+)?interface\s+(\w+)(?:\s+extends\s+[\w,\s]+)?\s*{`),
		"enum":        regexp.MustCompile(`^(?:public\s+|private\s+|protected\s+)?enum\s+(\w+)\s*{`),
		"method":      regexp.MustCompile(`^(?:\s*)(?:public\s+|private\s+|protected\s+|static\s+|final\s+|abstract\s+|synchronized\s+)*(?:\w+\s+)*(\w+)\s*\([^)]*\)(?:\s+throws\s+[\w,\s]+)?\s*[{;]`),
		"constructor": regexp.MustCompile(`^(?:\s*)(?:public\s+|private\s+|protected\s+)?(\w+)\s*\([^)]*\)\s*(?:throws\s+[\w,\s]+)?\s*{`),
		"field":       regexp.MustCompile(`^(?:\s*)(?:public\s+|private\s+|protected\s+|static\s+|final\s+)*(?:\w+(?:<[^>]*>)?(?:\[\])?\s+)+(\w+)(?:\s*=\s*[^;]+)?\s*;`),
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		for elementType, pattern := range patterns {
			if matches := pattern.FindStringSubmatch(line); matches != nil {
				element := SkeletonElement{
					Type:      elementType,
					StartLine: i + 1,
					Signature: trimmed,
				}

				if len(matches) > 1 {
					element.Name = matches[1]
				}

				if includeComments {
					element.Comment = t.extractJavaComment(lines, i)
				}

				if elementType == "method" || elementType == "constructor" || elementType == "class" || elementType == "interface" || elementType == "enum" {
					body, endLine := t.extractJavaBody(lines, i, maxLinesPerFunction, includeBody)
					if includeBody {
						element.Body = body
					}
					element.EndLine = endLine
				} else {
					element.EndLine = i + 1
				}

				// Calculate line count
				element.LineCount = element.EndLine - element.StartLine + 1

				elements = append(elements, element)
				break
			}
		}
	}

	return elements
}

func (t *CodeSkeletonTool) extractComment(lines []string, lineIndex int) string {
	var comments []string

	// Look for comments in the lines before the current line
	for i := lineIndex - 1; i >= 0 && i >= lineIndex-5; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "/*") {
			comments = append([]string{line}, comments...)
		} else if line == "" {
			continue // Skip empty lines
		} else {
			break // Stop at non-comment, non-empty line
		}
	}

	return strings.Join(comments, "\n")
}

func (t *CodeSkeletonTool) extractJavaComment(lines []string, lineIndex int) string {
	var comments []string

	// Look for Javadoc or regular comments in the lines before the current line
	for i := lineIndex - 1; i >= 0 && i >= lineIndex-10; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "/**") {
			comments = append([]string{line}, comments...)
		} else if line == "" {
			continue // Skip empty lines
		} else {
			break // Stop at non-comment, non-empty line
		}
	}

	return strings.Join(comments, "\n")
}

func (t *CodeSkeletonTool) extractGoBody(lines []string, startIndex, maxLines int, includeBody bool) (string, int) {
	var bodyLines []string
	braceCount := 0
	inBody := false
	endLine := startIndex + 1

	// Always calculate the accurate end line, regardless of includeBody
	for i := startIndex; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if !inBody && strings.Contains(line, "{") {
			inBody = true
			braceCount += strings.Count(line, "{") - strings.Count(line, "}")
			if braceCount > 0 {
				endLine = i + 1 // Update endLine when we enter the body
				continue        // Skip the opening brace line
			}
		}

		if inBody {
			braceCount += strings.Count(line, "{") - strings.Count(line, "}")

			// Only collect body lines if includeBody is true and we haven't exceeded maxLines
			if includeBody && len(bodyLines) < maxLines && trimmed != "" && !strings.HasPrefix(trimmed, "}") {
				bodyLines = append(bodyLines, fmt.Sprintf("    %d: %s", i+1, trimmed))
			}

			endLine = i + 1 // Always update endLine when in body
			if braceCount <= 0 {
				break
			}
		}
	}

	// Add truncation indicator if we collected the maximum lines
	if includeBody && len(bodyLines) >= maxLines {
		bodyLines = append(bodyLines, "    // ... more code ...")
	}

	var bodyContent string
	if includeBody {
		bodyContent = strings.Join(bodyLines, "\n")
	}

	return bodyContent, endLine
}

func (t *CodeSkeletonTool) extractJavaScriptBody(lines []string, startIndex, maxLines int, includeBody bool) (string, int) {
	var bodyLines []string
	braceCount := 0
	inBody := false
	endLine := startIndex + 1

	// Always calculate the accurate end line, regardless of includeBody
	for i := startIndex; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if !inBody && strings.Contains(line, "{") {
			inBody = true
			braceCount += strings.Count(line, "{") - strings.Count(line, "}")
			if braceCount > 0 {
				endLine = i + 1 // Update endLine when we enter the body
				continue
			}
		}

		if inBody {
			braceCount += strings.Count(line, "{") - strings.Count(line, "}")

			// Only collect body lines if includeBody is true and we haven't exceeded maxLines
			if includeBody && len(bodyLines) < maxLines && trimmed != "" && !strings.HasPrefix(trimmed, "}") {
				bodyLines = append(bodyLines, fmt.Sprintf("    %d: %s", i+1, trimmed))
			}

			endLine = i + 1 // Always update endLine when in body
			if braceCount <= 0 {
				break
			}
		}
	}

	// Add truncation indicator if we collected the maximum lines
	if includeBody && len(bodyLines) >= maxLines {
		bodyLines = append(bodyLines, "    // ... more code ...")
	}

	var bodyContent string
	if includeBody {
		bodyContent = strings.Join(bodyLines, "\n")
	}

	return bodyContent, endLine
}

func (t *CodeSkeletonTool) extractPythonBody(lines []string, startIndex, maxLines int, includeBody bool) (string, int) {
	var bodyLines []string
	baseIndent := -1
	endLine := startIndex + 1
	foundBody := false

	// Always calculate the accurate end line, regardless of includeBody
	for i := startIndex + 1; i < len(lines); i++ {
		line := lines[i]

		if strings.TrimSpace(line) == "" {
			endLine = i + 1
			continue // Skip empty lines
		}

		// Calculate indentation
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		if baseIndent == -1 && strings.TrimSpace(line) != "" {
			baseIndent = indent
			foundBody = true
		}

		// If we found body content and current line has less or equal indentation than base, we've reached the end
		if foundBody && indent <= baseIndent && strings.TrimSpace(line) != "" {
			// Check if this line starts with def, class, import, etc. (new top-level construct)
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "def ") || strings.HasPrefix(trimmed, "class ") ||
				strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ") ||
				strings.HasPrefix(trimmed, "@") || !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				break
			}
		}

		// Only collect body lines if includeBody is true and we haven't exceeded maxLines
		if includeBody && len(bodyLines) < maxLines && strings.TrimSpace(line) != "" {
			trimmed := strings.TrimSpace(line)
			bodyLines = append(bodyLines, fmt.Sprintf("    %d: %s", i+1, trimmed))
		}

		endLine = i + 1
	}

	// Add truncation indicator if we collected the maximum lines
	if includeBody && len(bodyLines) >= maxLines {
		bodyLines = append(bodyLines, "    # ... more code ...")
	}

	var bodyContent string
	if includeBody {
		bodyContent = strings.Join(bodyLines, "\n")
	}

	return bodyContent, endLine
}

func (t *CodeSkeletonTool) extractJavaBody(lines []string, startIndex, maxLines int, includeBody bool) (string, int) {
	var bodyLines []string
	braceCount := 0
	inBody := false
	endLine := startIndex + 1

	// Always calculate the accurate end line, regardless of includeBody
	for i := startIndex; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if !inBody && strings.Contains(line, "{") {
			inBody = true
			braceCount += strings.Count(line, "{") - strings.Count(line, "}")
			if braceCount > 0 {
				continue // Skip the opening brace line
			}
		}

		if inBody {
			braceCount += strings.Count(line, "{") - strings.Count(line, "}")

			// Only collect body lines if includeBody is true and we haven't exceeded maxLines
			if includeBody && len(bodyLines) < maxLines && trimmed != "" && !strings.HasPrefix(trimmed, "}") && !strings.HasPrefix(trimmed, "//") {
				bodyLines = append(bodyLines, fmt.Sprintf("    %d: %s", i+1, trimmed))
			}

			if braceCount <= 0 {
				endLine = i + 1
				break
			}
		}
		endLine = i + 1
	}

	// Add truncation indicator if we collected the maximum lines
	if includeBody && len(bodyLines) >= maxLines {
		bodyLines = append(bodyLines, "    // ... more code ...")
	}

	var bodyContent string
	if includeBody {
		bodyContent = strings.Join(bodyLines, "\n")
	}

	return bodyContent, endLine
}

func (t *CodeSkeletonTool) formatSkeletonForUser(filePath string, skeleton []SkeletonElement) string {
	var result strings.Builder

	fmt.Fprintf(&result, "📋 Code Skeleton: %s\n", filePath)
	result.WriteString(strings.Repeat("=", 60) + "\n\n")

	if len(skeleton) == 0 {
		result.WriteString("No significant code structures found.\n")
		return result.String()
	}

	for _, element := range skeleton {
		// Type icon
		icon := t.getElementIcon(element.Type)

		fmt.Fprintf(&result, "%s %d: %s", icon, element.StartLine, element.Signature)
		if element.Name != "" && !strings.Contains(element.Signature, element.Name) {
			fmt.Fprintf(&result, " (%s)", element.Name)
		}
		fmt.Fprintf(&result, " [L%d", element.StartLine)
		if element.EndLine > element.StartLine {
			fmt.Fprintf(&result, "-%d", element.EndLine)
		}
		fmt.Fprintf(&result, ", %d lines]\n", element.LineCount)

		// Add comment if present
		if element.Comment != "" {
			commentLines := strings.Split(element.Comment, "\n")
			for _, commentLine := range commentLines {
				fmt.Fprintf(&result, "  💬 %s\n", commentLine)
			}
		}

		// Add simplified body if present
		if element.Body != "" {
			bodyLines := strings.Split(element.Body, "\n")
			for _, bodyLine := range bodyLines {
				if strings.TrimSpace(bodyLine) != "" {
					fmt.Fprintf(&result, "  %s\n", bodyLine)
				}
			}
		}

		result.WriteString("\n")
	}

	return result.String()
}

func (t *CodeSkeletonTool) formatSkeletonForLLM(filePath string, skeleton []SkeletonElement) string {
	var result strings.Builder

	fmt.Fprintf(&result, "Code skeleton for %s:\n\n", filePath)

	if len(skeleton) == 0 {
		result.WriteString("No significant code structures found.\n")
		return result.String()
	}

	for _, element := range skeleton {
		fmt.Fprintf(&result, "%s: %s", element.Type, element.Signature)
		if element.Name != "" {
			fmt.Fprintf(&result, " (name: %s)", element.Name)
		}
		fmt.Fprintf(&result, " (lines %d", element.StartLine)
		if element.EndLine > element.StartLine {
			fmt.Fprintf(&result, "-%d", element.EndLine)
		}
		fmt.Fprintf(&result, ", total: %d lines)\n", element.LineCount)

		if element.Comment != "" {
			fmt.Fprintf(&result, "  Comment: %s\n", strings.ReplaceAll(element.Comment, "\n", " "))
		}

		if element.Body != "" {
			fmt.Fprintf(&result, "  Body: %s\n", strings.ReplaceAll(element.Body, "\n", "; "))
		}

		result.WriteString("\n")
	}

	return result.String()
}

func (t *CodeSkeletonTool) getElementIcon(elementType string) string {
	icons := map[string]string{
		"package":     "📦",
		"import":      "📥",
		"const":       "📌",
		"var":         "📦",
		"type":        "📝",
		"func":        "🔧",
		"function":    "🔧",
		"arrow_func":  "🏹",
		"method":      "⚙️",
		"constructor": "🏗️",
		"class":       "🏛️",
		"interface":   "🔌",
		"struct":      "🏗️",
		"enum":        "📋",
		"field":       "🏷️",
		"async":       "⚡",
		"export":      "📤",
	}

	if icon, ok := icons[elementType]; ok {
		return icon
	}
	return "📄"
}

func (t *CodeSkeletonTool) validatePath(path string) (string, error) {
	return validatePathCommon(t.workingDir, path, nil)
}
