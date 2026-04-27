package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileTool(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "readfile_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck

	// Create test file
	testFile := filepath.Join(tempDir, "test.txt")
	testContent := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"
	err = os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tool := NewReadFileTool(tempDir, nil, nil)

	tests := []struct {
		name     string
		params   map[string]interface{}
		wantErr  bool
		validate func(*testing.T, interface{})
	}{
		{
			name: "read full file",
			params: map[string]interface{}{
				"file_path": testFile,
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				if !strings.Contains(result.(string), "Line 1") {
					t.Error("Expected to find 'Line 1' in result")
				}
			},
		},
		{
			name: "read with line range",
			params: map[string]interface{}{
				"file_path":  testFile,
				"start_line": float64(2),
				"end_line":   float64(3),
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				content := result.(string)
				if !strings.Contains(content, "Line 2") {
					t.Error("Expected to find 'Line 2' in line range result")
				}
				if strings.Contains(content, "Line 1") {
					t.Error("Should not find 'Line 1' in line range 2-3")
				}
			},
		},
		{
			name: "file not found",
			params: map[string]interface{}{
				"file_path": filepath.Join(tempDir, "nonexistent.txt"),
			},
			wantErr: true,
		},
		{
			name: "invalid path",
			params: map[string]interface{}{
				"file_path": "../outside.txt",
			},
			wantErr: true,
		},
		{
			name: "missing file_path parameter",
			params: map[string]interface{}{
				"content": "test",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tt.params)

			if tt.wantErr {
				if err != nil || !result.Success {
					return // Expected error
				}
				t.Error("Expected error but got none")
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if !result.Success {
				t.Errorf("Tool execution failed: %s", result.Error)
				return
			}

			if tt.validate != nil {
				tt.validate(t, result.Data)
			}
		})
	}
}

func TestWriteFileTool(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "writefile_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck

	tool := NewWriteFileTool(tempDir, nil, nil)

	tests := []struct {
		name     string
		params   map[string]interface{}
		wantErr  bool
		validate func(*testing.T, string)
	}{
		{
			name: "create new file",
			params: map[string]interface{}{
				"file_path": filepath.Join(tempDir, "new.txt"),
				"content":   "Hello, World!",
			},
			wantErr: false,
			validate: func(t *testing.T, filePath string) {
				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Errorf("Failed to read created file: %v", err)
					return
				}
				if string(content) != "Hello, World!" {
					t.Errorf("Expected 'Hello, World!', got '%s'", string(content))
				}
			},
		},
		{
			name: "overwrite existing file",
			params: map[string]interface{}{
				"file_path": filepath.Join(tempDir, "existing.txt"),
				"content":   "Overwritten content",
			},
			wantErr: false,
			validate: func(t *testing.T, filePath string) {
				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Errorf("Failed to read overwritten file: %v", err)
					return
				}
				if string(content) != "Overwritten content" {
					t.Errorf("Expected 'Overwritten content', got '%s'", string(content))
				}
			},
		},
		{
			name: "create with directories",
			params: map[string]interface{}{
				"file_path":          filepath.Join(tempDir, "subdir", "file.txt"),
				"content":            "Content in subdirectory",
				"create_directories": true,
			},
			wantErr: false,
			validate: func(t *testing.T, filePath string) {
				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					t.Error("File should have been created in subdirectory")
				}
			},
		},
		{
			name: "invalid path",
			params: map[string]interface{}{
				"file_path": "../outside.txt",
				"content":   "test",
			},
			wantErr: true,
		},
		{
			name: "missing content parameter",
			params: map[string]interface{}{
				"file_path": filepath.Join(tempDir, "test.txt"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tt.params)

			if tt.wantErr {
				if err != nil || !result.Success {
					return // Expected error
				}
				t.Error("Expected error but got none")
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if !result.Success {
				t.Errorf("Tool execution failed: %s", result.Error)
				return
			}

			if tt.validate != nil {
				filePath := tt.params["file_path"].(string)
				tt.validate(t, filePath)
			}
		})
	}
}

func TestEditFileTool(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "editfile_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck

	tool := NewEditTool(tempDir, nil, nil)

	// Create test file
	testFile := filepath.Join(tempDir, "edit_test.txt")
	originalContent := "Hello World\nThis is a test\nEnd of file"
	err = os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name     string
		params   map[string]interface{}
		wantErr  bool
		validate func(*testing.T, string)
	}{
		{
			name: "simple replacement",
			params: map[string]interface{}{
				"command": "str_replace",
				"path":    testFile,
				"old_str": "Hello World",
				"new_str": "Greetings Universe",
			},
			wantErr: false,
			validate: func(t *testing.T, filePath string) {
				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Errorf("Failed to read edited file: %v", err)
					return
				}
				if !strings.Contains(string(content), "Greetings Universe") {
					t.Error("Expected to find 'Greetings Universe' in edited file")
				}
				if strings.Contains(string(content), "Hello World") {
					t.Error("Should not find 'Hello World' in edited file after replacement")
				}
			},
		},
		{
			name: "create new file with empty old_string",
			params: map[string]interface{}{
				"command": "create",
				"path":    filepath.Join(tempDir, "new_edit.txt"),
				"new_str": "New file content",
			},
			wantErr: true,
		},
		{
			name: "string not found",
			params: map[string]interface{}{
				"command": "str_replace",
				"path":    testFile,
				"old_str": "Nonexistent string",
				"new_str": "Replacement",
			},
			wantErr: true,
		},
		{
			name: "same old and new string",
			params: map[string]interface{}{
				"command": "str_replace",
				"path":    testFile,
				"old_str": "test",
				"new_str": "test",
			},
			wantErr: true,
		},
		{
			name: "missing required parameters",
			params: map[string]interface{}{
				"command": "str_replace",
				"path":    testFile,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Restore original content before each test
			os.WriteFile(testFile, []byte(originalContent), 0644) //nolint:errcheck

			result, err := tool.Execute(context.Background(), tt.params)

			if tt.wantErr {
				if err != nil || !result.Success {
					return // Expected error
				}
				t.Error("Expected error but got none")
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if !result.Success {
				t.Errorf("Tool execution failed: %s", result.Error)
				return
			}

			if tt.validate != nil {
				filePath := tt.params["path"].(string)
				tt.validate(t, filePath)
			}
		})
	}
}

func TestReadFileTool_ContentStandardization(t *testing.T) {
	// Setup temp dir
	tempDir, err := os.MkdirTemp("", "readfile_std_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tool := NewReadFileTool(tempDir, nil, nil)

	t.Run("missing params", func(t *testing.T) {
		res, _ := tool.Execute(context.Background(), nil)
		if res.Success {
			t.Fatalf("expected failure")
		}
		if res.UserContent == "" || res.LLMContent == "" {
			t.Fatalf("expected UserContent and LLMContent to be set on error")
		}
	})

	t.Run("missing file_path", func(t *testing.T) {
		res, _ := tool.Execute(context.Background(), map[string]interface{}{"foo": "bar"})
		if res.Success {
			t.Fatalf("expected failure")
		}
		if !strings.Contains(res.UserContent, "Failed to read file") {
			t.Errorf("unexpected UserContent: %s", res.UserContent)
		}
		if !strings.Contains(res.LLMContent, "read_file failed") {
			t.Errorf("unexpected LLMContent: %s", res.LLMContent)
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		res, _ := tool.Execute(context.Background(), map[string]interface{}{"file_path": "../etc/passwd"})
		if res.Success {
			t.Fatalf("expected failure")
		}
		if !strings.Contains(res.UserContent, "Failed to read file") || !strings.Contains(res.LLMContent, "read_file failed") {
			t.Fatalf("expected standardized contents, got user=%q llm=%q", res.UserContent, res.LLMContent)
		}
	})
}

func TestDetectBinaryFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "binary_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	textFile := filepath.Join(tempDir, "text.txt")
	binaryFile := filepath.Join(tempDir, "binary.bin")

	os.WriteFile(textFile, []byte("Hello text"), 0644)
	os.WriteFile(binaryFile, []byte{0, 1, 2, 3}, 0644)

	tests := []struct {
		filePath     string
		expectBinary bool
	}{
		{textFile, false},
		{binaryFile, true},
	}

	for _, tt := range tests {
		result := DetectBinaryFile(tt.filePath)
		if result.IsBinary != tt.expectBinary {
			t.Errorf("For %s, expected isBinary %v, got %v", tt.filePath, tt.expectBinary, result.IsBinary)
		}
	}
}

// TestEditTool_ReadBeforeEdit verifies that EditTool with a ReadFileState rejects edits on
// files that haven't been read first, and allows edits after reading.
func TestEditTool_ReadBeforeEdit(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "editstate_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck

	testFile := filepath.Join(tempDir, "guarded.txt")
	if err := os.WriteFile(testFile, []byte("original content"), 0644); err != nil {
		t.Fatal(err)
	}

	state := NewReadFileState()
	editTool := NewEditToolWithState(tempDir, nil, nil, state)
	readTool := NewReadFileToolWithState(tempDir, nil, nil, state)

	// Attempt to edit before reading – should fail.
	result, err := editTool.Execute(context.Background(), map[string]interface{}{
		"command": "str_replace",
		"path":    testFile,
		"old_str": "original content",
		"new_str": "modified content",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Error("expected edit to fail for unread file, but it succeeded")
	}
	if !strings.Contains(result.Error, "has not been read") {
		t.Errorf("unexpected error message: %s", result.Error)
	}

	// Read the file.
	readResult, err := readTool.Execute(context.Background(), map[string]interface{}{
		"file_path": testFile,
	})
	if err != nil || !readResult.Success {
		t.Fatalf("read_file failed: %v / %v", err, readResult)
	}

	// Now the edit should succeed.
	result, err = editTool.Execute(context.Background(), map[string]interface{}{
		"command": "str_replace",
		"path":    testFile,
		"old_str": "original content",
		"new_str": "modified content",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected edit to succeed after reading, got error: %s", result.Error)
	}

	// After a successful mutation the cache must be invalidated; editing again without
	// another read should fail so the model cannot keep editing from stale memory.
	result, err = editTool.Execute(context.Background(), map[string]interface{}{
		"command": "str_replace",
		"path":    testFile,
		"old_str": "modified content",
		"new_str": "modified again",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Error("expected second edit to fail until the file is re-read")
	}
	if !strings.Contains(result.Error, "has not been read") {
		t.Errorf("unexpected error message after invalidation: %s", result.Error)
	}
}

// TestEditTool_AmbiguousMatch verifies that str_replace fails when old_str matches multiple
// locations and instructs the caller to provide more context.
func TestEditTool_AmbiguousMatch(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "editamb_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck

	testFile := filepath.Join(tempDir, "dup.txt")
	if err := os.WriteFile(testFile, []byte("foo\nfoo\nfoo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	state := NewReadFileState()
	state.Mark(testFile) // pre-mark so the read-guard doesn't interfere
	editTool := NewEditToolWithState(tempDir, nil, nil, state)

	result, err := editTool.Execute(context.Background(), map[string]interface{}{
		"command": "str_replace",
		"path":    testFile,
		"old_str": "foo",
		"new_str": "bar",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Error("expected ambiguous match to fail, but it succeeded")
	}
	if !strings.Contains(result.Error, "matches 3 locations") {
		t.Errorf("unexpected error message: %s", result.Error)
	}
}

// TestWriteTool_InvalidatesReadCache verifies that non-edit filesystem mutations also
// invalidate the read cache so later edit_file calls require a fresh read.
func TestWriteTool_InvalidatesReadCache(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "writeinvalidate_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir) //nolint:errcheck

	testFile := filepath.Join(tempDir, "guarded.txt")
	if err := os.WriteFile(testFile, []byte("before write"), 0644); err != nil {
		t.Fatal(err)
	}

	state := NewReadFileState()
	readTool := NewReadFileToolWithState(tempDir, nil, nil, state)
	writeTool := NewWriteFileToolWithState(tempDir, nil, nil, state)
	editTool := NewEditToolWithState(tempDir, nil, nil, state)

	readResult, err := readTool.Execute(context.Background(), map[string]interface{}{
		"file_path": testFile,
	})
	if err != nil || !readResult.Success {
		t.Fatalf("read_file failed: %v / %v", err, readResult)
	}

	writeResult, err := writeTool.Execute(context.Background(), map[string]interface{}{
		"file_path": testFile,
		"content":   "after write",
	})
	if err != nil {
		t.Fatalf("unexpected Go error from write_file: %v", err)
	}
	if !writeResult.Success {
		t.Fatalf("write_file failed: %s", writeResult.Error)
	}

	result, err := editTool.Execute(context.Background(), map[string]interface{}{
		"command": "str_replace",
		"path":    testFile,
		"old_str": "after write",
		"new_str": "after write and edit",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Error("expected edit to fail after write_file invalidated the read cache")
	}
	if !strings.Contains(result.Error, "has not been read") {
		t.Errorf("unexpected error message: %s", result.Error)
	}
}
