package filesystem

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// validatePathCommon validates a given path and returns its absolute, cleaned,
// symlink-resolved path. Path access control is delegated to sandbox.PathChecker;
// this function blocks relative path traversal outside the working directory and
// normalizes the path for subsequent sandbox checks.
func validatePathCommon(workingDir, path string) (string, error) {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" || cleaned == "." {
		return "", fmt.Errorf("path is empty")
	}

	// Resolve working dir to an absolute, symlink-free base for relative path validation.
	base, err := filepath.Abs(workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute working directory: %v", err)
	}
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("failed to resolve working directory symlinks: %v", err)
	}

	// Convert to absolute, resolving relative paths against workingDir.
	absPath := cleaned
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(baseReal, absPath)
		// Reject relative path traversal attempts (e.g. "../outside.txt").
		if rel, err := filepath.Rel(baseReal, absPath); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path escapes working directory: %q", path)
		}
	}

	// Resolve all symlinks to prevent traversal attacks (e.g., a symlink
	// inside the working dir pointing to /etc).
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File or some parent directories don't exist yet (write target).
			// Walk up to the first existing ancestor and resolve from there.
			realPath = resolveNonExistentPath(absPath)
		} else {
			return "", fmt.Errorf("symlink resolution failed: %v", err)
		}
	}

	// If the caller provided a relative path, ensure the resolved realPath is still
	// within the working directory (covers symlink escape via existing parents).
	if !filepath.IsAbs(cleaned) {
		if rel, err := filepath.Rel(baseReal, realPath); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path escapes working directory via symlink resolution: %q", path)
		}
	}

	return realPath, nil
}

// resolveNonExistentPath walks up the directory tree until it finds an existing
// ancestor, resolves its symlinks, then reconstructs the full path beneath it.
// This handles cases like /tmp/new-dir/file.txt where /tmp exists but new-dir doesn't.
func resolveNonExistentPath(absPath string) string {
	// Collect non-existing tail segments.
	tail := []string{filepath.Base(absPath)}
	parent := filepath.Dir(absPath)
	for parent != absPath {
		real, err := filepath.EvalSymlinks(parent)
		if err == nil {
			// Found an existing ancestor – rebuild the path.
			for i := len(tail) - 1; i >= 0; i-- {
				real = filepath.Join(real, tail[i])
			}
			return real
		}
		if !os.IsNotExist(err) {
			break // Unexpected error; fall back to original.
		}
		tail = append(tail, filepath.Base(parent))
		absPath = parent
		parent = filepath.Dir(parent)
	}
	return absPath
}

// generateDiffCommon generates a diff string showing the changes between oldContent and newContent
// contextDiff=true provides context around changes (used by edit_file), false provides simple line diff (used by write_file)
func generateDiffCommon(oldContent, newContent, filePath string, isNewFile bool, config map[string]interface{}, contextDiff bool) string {
	var diff strings.Builder

	if isNewFile {
		fmt.Fprintf(&diff, "+ Creating new file: %s\n", filePath)
		diff.WriteString("+" + strings.Repeat("-", 60) + "\n")
		lines := strings.Split(newContent, "\n")
		maxLines := 20 // Default value
		if maxConfig, ok := config["file_diff_max_lines"].(int); ok && maxConfig > 0 {
			maxLines = maxConfig
		}
		if len(lines) > maxLines {
			for i := 0; i < maxLines; i++ {
				fmt.Fprintf(&diff, "+ %s\n", lines[i])
			}
			fmt.Fprintf(&diff, "+ ... (%d more lines)\n", len(lines)-maxLines)
		} else {
			for _, line := range lines {
				fmt.Fprintf(&diff, "+ %s\n", line)
			}
		}
		return diff.String()
	}

	// Use go-diff library for reliable cross-platform diff generation
	dmp := diffmatchpatch.New()

	if contextDiff {
		// Generate unified diff format for context diff
		diffs := dmp.DiffMain(oldContent, newContent, false)
		diffs = dmp.DiffCleanupSemantic(diffs)

		// Convert to unified diff format
		patches := dmp.PatchMake(oldContent, diffs)
		unifiedDiff := dmp.PatchToText(patches)

		if unifiedDiff == "" {
			return fmt.Sprintf("~ No changes detected in file: %s\n", filePath)
		}

		fmt.Fprintf(&diff, "~ Modifying file: %s\n", filePath)
		diff.WriteString("~" + strings.Repeat("-", 60) + "\n")

		// Process and format the unified diff
		lines := strings.Split(unifiedDiff, "\n")
		maxLines := 100
		if maxConfig, ok := config["file_diff_max_lines"].(int); ok && maxConfig > 0 {
			maxLines = maxConfig
		}

		if len(lines) > maxLines {
			for i := 0; i < maxLines; i++ {
				if lines[i] != "" {
					diff.WriteString(lines[i] + "\n")
				}
			}
			diff.WriteString("... (diff truncated)\n")
		} else {
			diff.WriteString(unifiedDiff)
		}
	} else {
		// Simple line-by-line diff for write_file
		fmt.Fprintf(&diff, "~ Modifying file: %s\n", filePath)
		diff.WriteString("~" + strings.Repeat("-", 60) + "\n")

		// Use go-diff to find line differences
		diffs := dmp.DiffMain(oldContent, newContent, false)
		diffs = dmp.DiffCleanupSemantic(diffs)

		diffLines := 0
		maxDiffLines := 20 // Default value
		if maxConfig, ok := config["file_diff_max_lines"].(int); ok && maxConfig > 0 {
			maxDiffLines = maxConfig
		}

		for _, d := range diffs {
			if diffLines >= maxDiffLines {
				break
			}

			switch d.Type {
			case diffmatchpatch.DiffDelete:
				lines := strings.Split(d.Text, "\n")
				for _, line := range lines {
					if line != "" && diffLines < maxDiffLines {
						fmt.Fprintf(&diff, "- %s\n", line)
						diffLines++
					}
				}
			case diffmatchpatch.DiffInsert:
				lines := strings.Split(d.Text, "\n")
				for _, line := range lines {
					if line != "" && diffLines < maxDiffLines {
						fmt.Fprintf(&diff, "+ %s\n", line)
						diffLines++
					}
				}
			}
		}

		if diffLines >= maxDiffLines {
			diff.WriteString("... (diff truncated)\n")
		}
	}

	return diff.String()
}

// BinaryDetectionResult contains the result of binary file detection
type BinaryDetectionResult struct {
	IsBinary   bool
	IsText     bool
	Encoding   string
	Confidence float64
	Error      error
}

// isBinaryContent checks if the given content appears to be binary
func isBinaryContent(content []byte) bool {
	if len(content) == 0 {
		return false
	}

	// MIME type detection
	sampleSize := 512
	if len(content) < sampleSize {
		sampleSize = len(content)
	}
	mimeType := http.DetectContentType(content[:sampleSize])
	if !strings.HasPrefix(mimeType, "text/") {
		return true
	}

	// Check for null bytes (common in binary files)
	if bytes.Contains(content, []byte{0}) {
		return true
	}

	// Check for high ratio of non-printable characters
	if len(content) >= 100 {
		nonPrintable := 0
		for _, b := range content {
			if b < 32 && b != 9 && b != 10 && b != 13 { // Not tab, newline, or carriage return
				nonPrintable++
			}
		}

		// If more than 20% of bytes are non-printable, consider it binary (adjusted threshold)
		if float64(nonPrintable)/float64(len(content)) > 0.2 {
			return true
		}
	}

	return false
}

// isValidUTF8 checks if the content is valid UTF-8
func isValidUTF8(content []byte) bool {
	return utf8.Valid(content)
}

// detectFileEncoding detects the encoding of file content
func detectFileEncoding(content []byte) (string, float64) {
	if len(content) == 0 {
		return "utf-8", 1.0
	}

	// Check for BOM markers
	if len(content) >= 3 && bytes.Equal(content[:3], []byte{0xEF, 0xBB, 0xBF}) {
		return "utf-8-bom", 1.0
	}
	if len(content) >= 2 && bytes.Equal(content[:2], []byte{0xFF, 0xFE}) {
		return "utf-16le", 1.0
	}
	if len(content) >= 2 && bytes.Equal(content[:2], []byte{0xFE, 0xFF}) {
		return "utf-16be", 1.0
	}

	// Check if valid UTF-8
	if isValidUTF8(content) {
		return "utf-8", 0.9
	}

	// Check for ASCII (subset of UTF-8)
	isASCII := true
	for _, b := range content {
		if b > 127 {
			isASCII = false
			break
		}
	}
	if isASCII {
		return "ascii", 0.8
	}

	return "unknown", 0.1
}

// DetectBinaryFile analyzes a file to determine if it's binary and its encoding
func DetectBinaryFile(filePath string) *BinaryDetectionResult {
	result := &BinaryDetectionResult{}

	// Check file extension first for known binary types
	if IsBinaryFileExtension(filePath) {
		result.IsBinary = true
		result.IsText = false
		result.Encoding = "binary"
		result.Confidence = 1.0
		return result
	}

	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		result.Error = fmt.Errorf("failed to open file: %v", err)
		return result
	}
	// Read first 8KB for analysis (sufficient for most detection)
	buffer := make([]byte, 8192)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		result.Error = fmt.Errorf("failed to read file: %v", err)
		return result
	}

	content := buffer[:n]

	// Detect if binary
	result.IsBinary = isBinaryContent(content)
	result.IsText = !result.IsBinary

	// Detect encoding
	encoding, confidence := detectFileEncoding(content)
	result.Encoding = encoding
	result.Confidence = confidence

	return result
}

// DetectBinaryContent analyzes content to determine if it's binary
func DetectBinaryContent(content []byte) *BinaryDetectionResult {
	result := &BinaryDetectionResult{}

	// Detect if binary
	result.IsBinary = isBinaryContent(content)
	result.IsText = !result.IsBinary

	// Detect encoding
	encoding, confidence := detectFileEncoding(content)
	result.Encoding = encoding
	result.Confidence = confidence

	return result
}

// SafeReadFile reads a file with binary detection and encoding validation
func SafeReadFile(filePath string) ([]byte, *BinaryDetectionResult, error) {
	// First detect if file is binary
	detection := DetectBinaryFile(filePath)
	if detection.Error != nil {
		return nil, detection, detection.Error
	}

	// Read the full file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, detection, fmt.Errorf("failed to read file: %v", err)
	}

	// Re-analyze with full content for better accuracy
	fullDetection := DetectBinaryContent(content)
	fullDetection.Error = detection.Error

	return content, fullDetection, nil
}

// IsBinaryFileExtension checks if file extension suggests binary content
func IsBinaryFileExtension(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	binaryExtensions := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".dylib": true,
		".bin": true, ".dat": true, ".db": true, ".sqlite": true,
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true, ".ico": true,
		".mp3": true, ".mp4": true, ".avi": true, ".mov": true, ".wav": true,
		".zip": true, ".tar": true, ".gz": true, ".rar": true, ".7z": true,
		".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
		".ppt": true, ".pptx": true, ".odt": true, ".ods": true, ".odp": true,
		".class": true, ".jar": true, ".war": true, ".ear": true,
		".o": true, ".obj": true, ".lib": true, ".a": true,
		".pyc": true, ".pyo": true, ".pyd": true,
		".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	}
	return binaryExtensions[ext]
}
