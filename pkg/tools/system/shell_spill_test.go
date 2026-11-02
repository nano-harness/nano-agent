package system

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/middleware"
)

func allowCtx() context.Context {
	decision := &middleware.Decision{Action: middleware.ActionAllow}
	return middleware.WithSecurityDecision(context.Background(), decision)
}

func TestShellTool_InlineBudgetDefaults(t *testing.T) {
	tool := NewShellTool("/tmp", nil, nil)
	if tool.inlineOutputMaxBytes != DefaultShellInlineOutputBytes {
		t.Errorf("Expected default inline budget %d, got %d", DefaultShellInlineOutputBytes, tool.inlineOutputMaxBytes)
	}
	if tool.captureOutputMaxBytes != MaxShellOutputBytes {
		t.Errorf("Expected default capture limit %d, got %d", MaxShellOutputBytes, tool.captureOutputMaxBytes)
	}

	// Zero or negative values fall back to defaults
	tool = NewShellTool("/tmp", map[string]interface{}{
		"shell_inline_output_max_bytes":  0,
		"shell_capture_output_max_bytes": -1,
	}, nil)
	if tool.inlineOutputMaxBytes != DefaultShellInlineOutputBytes {
		t.Errorf("Expected default inline budget for zero value, got %d", tool.inlineOutputMaxBytes)
	}
	if tool.captureOutputMaxBytes != MaxShellOutputBytes {
		t.Errorf("Expected default capture limit for negative value, got %d", tool.captureOutputMaxBytes)
	}
}

func TestShellTool_OutputSpillsToFile(t *testing.T) {
	tool := NewShellTool("/tmp", map[string]interface{}{
		"shell_inline_output_max_bytes": 4096,
	}, nil)

	// ~100KB of numbered lines, well above the 4KB inline budget
	params := map[string]interface{}{
		"command":         "seq 1 12000",
		"timeout_seconds": float64(15),
	}

	result, err := tool.Execute(allowCtx(), params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	if spilled, ok := result.Metadata["output_spilled"].(bool); !ok || !spilled {
		t.Fatal("Expected output_spilled metadata to be true")
	}

	total, ok := result.Metadata["total_bytes"].(int)
	if !ok || total <= 4096 {
		t.Errorf("Expected total_bytes > 4096, got %v", result.Metadata["total_bytes"])
	}

	path, ok := result.Metadata["output_file"].(string)
	if !ok || path == "" {
		t.Fatal("Expected output_file metadata")
	}
	defer os.Remove(path)

	// The spill file must contain the full output (head and tail)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read spill file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "=== STDOUT ===") {
		t.Error("Expected STDOUT section in spill file")
	}
	if !strings.Contains(content, "1\n2\n3") {
		t.Error("Expected beginning of output in spill file")
	}
	if !strings.Contains(content, "12000") {
		t.Error("Expected end of output in spill file")
	}
	if len(content) != total+len("=== STDOUT ===\n")+len("\n=== STDERR ===\n") {
		t.Errorf("Spill file size %d does not match total_bytes %d plus headers", len(content), total)
	}

	// Inline content must be bounded and carry the truncation hint
	for name, inline := range map[string]string{"LLMContent": result.LLMContent, "UserContent": result.UserContent} {
		if len(inline) > 8192 {
			t.Errorf("%s length %d exceeds bounded inline budget", name, len(inline))
		}
		if !strings.Contains(inline, "[output truncated: full output written to "+path) {
			t.Errorf("Expected truncation hint with spill path in %s, got: %.200s...", name, inline)
		}
	}

	// Head and tail of the output should be present inline
	if !strings.Contains(result.LLMContent, "1\n2\n3") {
		t.Error("Expected head preview in LLMContent")
	}
	if !strings.Contains(result.LLMContent, "12000") {
		t.Error("Expected tail preview in LLMContent")
	}
}

func TestShellTool_NoSpillBelowInlineBudget(t *testing.T) {
	tool := NewShellTool("/tmp", nil, nil)

	params := map[string]interface{}{
		"command":         "echo hello",
		"timeout_seconds": float64(5),
	}

	result, err := tool.Execute(allowCtx(), params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}
	if _, ok := result.Metadata["output_spilled"]; ok {
		t.Error("Did not expect output_spilled metadata for small output")
	}
	if !strings.Contains(result.LLMContent, "hello") {
		t.Error("Expected full output inline")
	}
}

func TestSplitSpillBudget(t *testing.T) {
	tests := []struct {
		name                   string
		budget, stdout, stderr int
		wantStdout, wantStderr int
	}{
		{"stdout only", 1000, 5000, 0, 1000, 0},
		{"stderr only", 1000, 0, 5000, 0, 1000},
		{"proportional", 1000, 3000, 1000, 750, 250},
		{"minimum share for tiny stderr", 1000, 100000, 10, 900, 100},
		{"minimum share for tiny stdout", 1000, 10, 100000, 100, 900},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOut, gotErr := splitSpillBudget(tt.budget, tt.stdout, tt.stderr)
			if gotOut != tt.wantStdout || gotErr != tt.wantStderr {
				t.Errorf("splitSpillBudget(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tt.budget, tt.stdout, tt.stderr, gotOut, gotErr, tt.wantStdout, tt.wantStderr)
			}
		})
	}
}

func TestSpillPreview(t *testing.T) {
	s := strings.Repeat("a", 1000)
	got := spillPreview(s, 100)
	if len(got) >= len(s) {
		t.Errorf("Expected preview shorter than original, got %d bytes", len(got))
	}
	if !strings.HasPrefix(got, strings.Repeat("a", 80)) {
		t.Error("Expected head preview (80%) at start")
	}
	if !strings.HasSuffix(got, strings.Repeat("a", 20)) {
		t.Error("Expected tail preview (20%) at end")
	}
	if !strings.Contains(got, "bytes omitted") {
		t.Error("Expected omission marker")
	}
	if unchanged := spillPreview("short", 100); unchanged != "short" {
		t.Error("Expected short content to pass through unchanged")
	}
}
