package search

import (
	"context"
	"os"

	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGrepTool(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "grep_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test files with different content
	files := map[string]string{
		"file1.txt": "Hello World\nThis is a test file\nContains function definition\nEnd of file",
		"file2.go":  "package main\n\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n\nfunc helper() string {\n    return \"test\"\n}",
		"file3.py":  "def function_name():\n    print('Python function')\n    return True\n\nclass MyClass:\n    pass",
	}

	for filename, content := range files {
		err := os.WriteFile(filepath.Join(tempDir, filename), []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	t.Logf("Temp dir for grep test: %s", tempDir)
	walkErr := filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		t.Logf("Found file/dir: %s", path)
		if !info.IsDir() {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Logf("Error reading file %s: %v", path, readErr)
			} else {
				t.Logf("Content of %s:\n%s", path, string(content))
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("Failed to walk temp dir: %v", walkErr)
	}

	tool := NewGrepTool(tempDir, nil, nil)

	tests := []struct {
		name     string
		params   map[string]interface{}
		wantErr  bool
		validate func(*testing.T, interface{})
	}{
		{
			name: "search for function pattern",
			params: map[string]interface{}{
				"pattern": "func.*",
				"path":    tempDir,
				"include": "*.go",
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				results, ok := result.([]SearchResult)
				if !ok {
					t.Error("Expected result to be []SearchResult")
					return
				}
				if len(results) == 0 {
					t.Error("Expected to find function definitions")
				}
				// Should find Go functions
				foundGo := false
				for _, res := range results {
					if strings.Contains(res.File, "file2.go") {
						foundGo = true
						break
					}
				}
				if !foundGo {
					t.Error("Expected to find Go function definitions")
				}
			},
		},
		{
			name: "search with file include pattern",
			params: map[string]interface{}{
				"pattern": "Hello",
				"path":    tempDir,
				"include": "*.txt",
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				results, ok := result.([]SearchResult)
				if !ok {
					t.Error("Expected result to be []SearchResult")
					return
				}
				// Should only find matches in .txt files
				for _, res := range results {
					if !strings.HasSuffix(res.File, ".txt") {
						t.Errorf("Found match in non-txt file: %s", res.File)
					}
				}
			},
		},
		{
			name: "case insensitive search",
			params: map[string]interface{}{
				"pattern":        "HELLO",
				"path":           tempDir,
				"case_sensitive": false,
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				results, ok := result.([]SearchResult)
				if !ok {
					t.Error("Expected result to be []SearchResult")
					return
				}
				if len(results) == 0 {
					t.Error("Expected to find case insensitive matches")
				}
			},
		},
		{
			name: "limit max results",
			params: map[string]interface{}{
				"pattern":     "function",
				"path":        tempDir,
				"max_results": float64(1),
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				results, ok := result.([]SearchResult)
				if !ok {
					t.Error("Expected result to be []SearchResult")
					return
				}
				if len(results) > 1 {
					t.Errorf("Expected max 1 result, got %d", len(results))
				}
			},
		},
		{
			name: "invalid regex pattern",
			params: map[string]interface{}{
				"pattern": "[invalid",
				"path":    tempDir,
			},
			wantErr: true,
		},
		{
			name: "search outside working directory",
			params: map[string]interface{}{
				"pattern": "test",
				"path":    "/tmp",
			},
			wantErr: true,
		},
		{
			name: "missing pattern parameter",
			params: map[string]interface{}{
				"path": tempDir,
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

func TestGrepTool_ContentStandardization(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "grep_std_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	tool := NewGrepTool(tempDir, nil, nil)

	t.Run("missing params", func(t *testing.T) {
		res, _ := tool.Execute(context.Background(), nil)
		if res.Success {
			t.Fatalf("expected failure")
		}
		if res.UserContent == "" || res.LLMContent == "" {
			t.Fatalf("expected UserContent and LLMContent to be set on error")
		}
	})

	t.Run("missing pattern", func(t *testing.T) {
		res, _ := tool.Execute(context.Background(), map[string]interface{}{"path": tempDir})
		if res.Success {
			t.Fatalf("expected failure")
		}
		if !strings.Contains(res.UserContent, "Failed to search") || !strings.Contains(res.LLMContent, "search_file_content failed") {
			t.Fatalf("expected standardized contents, got user=%q llm=%q", res.UserContent, res.LLMContent)
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		res, _ := tool.Execute(context.Background(), map[string]interface{}{"pattern": "x", "path": "../.."})
		if res.Success {
			t.Fatalf("expected failure")
		}
		if !strings.Contains(res.UserContent, "Invalid path") || !strings.Contains(res.LLMContent, "Invalid path") {
			t.Fatalf("expected standardized contents for invalid path")
		}
	})

	t.Run("invalid regex", func(t *testing.T) {
		res, _ := tool.Execute(context.Background(), map[string]interface{}{"pattern": "[", "path": tempDir})
		if res.Success {
			t.Fatalf("expected failure")
		}
		if !strings.Contains(res.UserContent, "Invalid regex pattern") || !strings.Contains(res.LLMContent, "Invalid regex pattern") {
			t.Fatalf("expected standardized contents for invalid regex")
		}
	})
}

func TestZoektSearch(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "zoekt_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test files
	files := map[string]string{
		"file1.txt": "hello world",
		"file2.txt": "another file",
	}
	for filename, content := range files {
		err := os.WriteFile(filepath.Join(tempDir, filename), []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	tool := NewGrepTool(tempDir, nil, nil)

	// Build the index
	indexDir := filepath.Join(tempDir, ".zoekt")
	err = tool.buildZoektIndex(context.Background(), indexDir, tempDir)
	if err != nil {
		t.Fatalf("Failed to build zoekt index: %v", err)
	}

	// Test search
	params := map[string]interface{}{
		"pattern": "hello",
		"path":    tempDir,
	}
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Tool execution failed: %s", result.Error)
	}

	searchResults, ok := result.Data.([]SearchResult)
	if !ok {
		t.Fatalf("Expected result to be []SearchResult")
	}

	if len(searchResults) != 1 {
		t.Errorf("Expected 1 search result, got %d", len(searchResults))
	}

	// Check if the file path ends with file1.txt (could be absolute or relative)
	if !strings.HasSuffix(searchResults[0].File, "file1.txt") {
		t.Errorf("Expected to find file1.txt, got %s", searchResults[0].File)
	}
}

func TestGlobTool(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "glob_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test directory structure
	subDir := filepath.Join(tempDir, "subdir")
	_ = os.Mkdir(subDir, 0755)

	// Create test files with different extensions and timestamps
	files := []string{
		"file1.txt",
		"file2.go",
		"file3.py",
		"README.md",
		".hidden",
		"subdir/nested.txt",
		"subdir/nested.go",
	}

	for i, filename := range files {
		fullPath := filepath.Join(tempDir, filename)
		// Ensure directory exists
		_ = os.MkdirAll(filepath.Dir(fullPath), 0755)

		err := os.WriteFile(fullPath, []byte("content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}

		// Set different modification times for sorting tests
		modTime := time.Now().Add(-time.Duration(i) * time.Hour)
		_ = os.Chtimes(fullPath, modTime, modTime)
	}

	tool := NewGlobTool(tempDir, nil, nil)

	tests := []struct {
		name     string
		params   map[string]interface{}
		wantErr  bool
		validate func(*testing.T, interface{})
	}{
		{
			name: "match all go files",
			params: map[string]interface{}{
				"pattern": "*.go",
				"path":    tempDir,
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				results, ok := result.([]GlobResult)
				if !ok {
					t.Error("Expected result to be []GlobResult")
					return
				}

				// Should find at least file2.go
				found := false
				for _, res := range results {
					if strings.HasSuffix(res.Path, "file2.go") {
						found = true
						break
					}
				}
				if !found {
					t.Error("Expected to find file2.go")
				}

				// All results should be .go files
				for _, res := range results {
					if !strings.HasSuffix(res.Name, ".go") {
						t.Errorf("Found non-go file: %s", res.Name)
					}
				}
			},
		},
		{
			name: "recursive pattern matching",
			params: map[string]interface{}{
				"pattern": "**/*.txt",
				"path":    tempDir,
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				results, ok := result.([]GlobResult)
				if !ok {
					t.Error("Expected result to be []GlobResult")
					return
				}

				// Should find nested.txt
				found := false
				for _, res := range results {
					if strings.Contains(res.RelativePath, "nested.txt") {
						found = true
						break
					}
				}
				if !found {
					t.Error("Expected to find nested.txt in recursive search")
				}
			},
		},
		{
			name: "include hidden files",
			params: map[string]interface{}{
				"pattern":        ".*",
				"path":           tempDir,
				"include_hidden": true,
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				results, ok := result.([]GlobResult)
				if !ok {
					t.Error("Expected result to be []GlobResult")
					return
				}

				// Should find .hidden file
				found := false
				for _, res := range results {
					if res.Name == ".hidden" {
						found = true
						break
					}
				}
				if !found {
					t.Error("Expected to find .hidden file when include_hidden=true")
				}
			},
		},
		{
			name: "sort by modification time",
			params: map[string]interface{}{
				"pattern": "*.txt",
				"path":    tempDir,
				"sort_by": "modified",
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				results, ok := result.([]GlobResult)
				if !ok {
					t.Error("Expected result to be []GlobResult")
					return
				}

				if len(results) < 2 {
					return // Cannot test sorting with less than 2 results
				}

				// Results should be sorted by modification time (newest first)
				for i := 1; i < len(results); i++ {
					if results[i].ModTime.After(results[i-1].ModTime) {
						t.Error("Results not sorted by modification time (newest first)")
						break
					}
				}
			},
		},
		{
			name: "limit max results",
			params: map[string]interface{}{
				"pattern":     "*",
				"path":        tempDir,
				"max_results": float64(2),
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				results, ok := result.([]GlobResult)
				if !ok {
					t.Error("Expected result to be []GlobResult")
					return
				}

				if len(results) > 2 {
					t.Errorf("Expected max 2 results, got %d", len(results))
				}
			},
		},
		{
			name: "case insensitive matching",
			params: map[string]interface{}{
				"pattern":        "*.GO",
				"path":           tempDir,
				"case_sensitive": false,
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				results, ok := result.([]GlobResult)
				if !ok {
					t.Error("Expected result to be []GlobResult")
					return
				}

				// Should find .go files even with uppercase pattern
				found := false
				for _, res := range results {
					if strings.HasSuffix(res.Name, ".go") {
						found = true
						break
					}
				}
				if !found {
					t.Error("Expected to find .go files with case insensitive matching")
				}
			},
		},
		{
			name: "search outside working directory",
			params: map[string]interface{}{
				"pattern": "*",
				"path":    "/tmp",
			},
			wantErr: true,
		},
		{
			name: "missing pattern parameter",
			params: map[string]interface{}{
				"path": tempDir,
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
