package filesystem

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

// ReadPDFTool extracts text from a PDF file. It prefers the system pdftotext
// binary (Poppler) when available; otherwise it falls back to a minimal
// in-process extractor that handles the common subset of PDF objects used by
// modern documents (FlateDecode-compressed text streams). The fallback is
// best-effort and intended to give the LLM enough signal to summarise.
type ReadPDFTool struct {
	workingDir  string
	pathChecker *sandbox.PathChecker
}

// NewReadPDFTool constructs a new ReadPDFTool.
func NewReadPDFTool(workingDir string, checker *sandbox.PathChecker) *ReadPDFTool {
	pc := checker
	if pc == nil {
		pc = sandbox.NewPathChecker(nil)
	}
	return &ReadPDFTool{workingDir: workingDir, pathChecker: pc}
}

// Name implements interfaces.Tool.
func (t *ReadPDFTool) Name() string { return "read_pdf" }

// Description implements interfaces.Tool.
func (t *ReadPDFTool) Description() string {
	return "Extract text from a PDF file. Uses pdftotext when available (best quality). Falls back to a minimal in-process extractor that handles uncompressed and FlateDecode text streams. Returns the extracted text alongside basic metadata."
}

// Category implements interfaces.Tool.
func (t *ReadPDFTool) Category() interfaces.ToolCategory { return interfaces.CategoryFileSystem }

// RequiresConfirmation implements interfaces.Tool.
func (t *ReadPDFTool) RequiresConfirmation() bool { return false }

// ConcurrencySafe implements interfaces.Tool.
func (t *ReadPDFTool) ConcurrencySafe() bool { return true }

// Schema implements interfaces.Tool.
func (t *ReadPDFTool) Schema() *interfaces.ToolSchema {
	pathProp := interfaces.NewStringProperty("Absolute or workspace-relative path to a .pdf file")
	pathProp.Examples = []string{"./report.pdf", "/tmp/spec.pdf"}

	maxCharsProp := interfaces.NewNumberProperty("Maximum number of characters to return (default: 200000)")
	maxCharsProp.Examples = []string{"50000", "100000"}

	return interfaces.CreateSchema(
		"Extract text from a PDF file with safe truncation.",
		map[string]*interfaces.PropertySchema{
			"file_path": pathProp,
			"max_chars": maxCharsProp,
		},
		[]string{"file_path"},
	)
}

// Execute reads the PDF at file_path and returns extracted text.
func (t *ReadPDFTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	filePath, ok := params["file_path"].(string)
	if !ok || strings.TrimSpace(filePath) == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "file_path is required",
			UserContent: "❌ read_pdf: file_path is required",
			LLMContent:  "read_pdf failed: file_path is required",
		}, nil
	}
	maxChars := 200_000
	if v, ok := params["max_chars"].(float64); ok && v > 0 {
		maxChars = int(v)
	}

	absPath, err := validatePathCommon(t.workingDir, filePath, t.pathChecker.AllowedPaths())
	if err != nil {
		return errResult(fmt.Sprintf("invalid file path: %v", err)), nil
	}
	if err := t.pathChecker.Check(absPath, sandbox.OpRead); err != nil {
		return errResult(fmt.Sprintf("access denied: %v", err)), nil
	}
	if !strings.EqualFold(strings.TrimSpace(filepathExt(absPath)), ".pdf") {
		return errResult("file does not have a .pdf extension"), nil
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return errResult(fmt.Sprintf("stat: %v", err)), nil
	}

	text, source, extractErr := extractPDFText(ctx, absPath)
	if extractErr != nil {
		return errResult(fmt.Sprintf("extract: %v", extractErr)), nil
	}
	truncated := false
	if len(text) > maxChars {
		text = text[:maxChars]
		truncated = true
	}
	header := fmt.Sprintf("📄 PDF: %s (%d bytes, source=%s)\n", absPath, info.Size(), source)
	if truncated {
		header += fmt.Sprintf("⚠️  truncated to %d chars\n", maxChars)
	}
	body := header + "\n" + text
	return &interfaces.ToolResult{
		Success:     true,
		UserContent: body,
		LLMContent:  body,
	}, nil
}

func errResult(msg string) *interfaces.ToolResult {
	return &interfaces.ToolResult{
		Success:     false,
		Error:       msg,
		UserContent: "❌ read_pdf: " + msg,
		LLMContent:  "read_pdf failed: " + msg,
	}
}

// filepathExt returns lowercase extension including the dot.
func filepathExt(p string) string {
	idx := strings.LastIndex(p, ".")
	if idx < 0 {
		return ""
	}
	return strings.ToLower(p[idx:])
}

// extractPDFText returns (text, source, error). source is "pdftotext" or
// "fallback" so the caller can attribute quality.
func extractPDFText(ctx context.Context, absPath string) (string, string, error) {
	if _, err := exec.LookPath("pdftotext"); err == nil {
		var buf bytes.Buffer
		cmd := exec.CommandContext(ctx, "pdftotext", "-layout", "-q", absPath, "-")
		cmd.Stdout = &buf
		if err := cmd.Run(); err == nil {
			return buf.String(), "pdftotext", nil
		}
		// fall through to in-process fallback
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", "", err
	}
	text := decodePDFTextFallback(data)
	return text, "fallback", nil
}
