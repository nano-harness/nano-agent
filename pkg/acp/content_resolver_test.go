package acp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/llm"
)

// TestProcessContentBlocks tests the processContentBlocks method
func TestProcessContentBlocks(t *testing.T) {
	srv := &Server{}

	tests := []struct {
		name         string
		blocks       []ContentBlock
		expectedText string
		expectedImgs int
		expectError  bool
		setupFiles   map[string]string // files to create for testing
	}{
		{
			name: "Text block only",
			blocks: []ContentBlock{
				{Type: "text", Text: "Hello, world!"},
			},
			expectedText: "Hello, world!",
			expectedImgs: 0,
		},
		{
			name: "Multiple text blocks",
			blocks: []ContentBlock{
				{Type: "text", Text: "First block"},
				{Type: "text", Text: "Second block"},
			},
			expectedText: "First block\nSecond block",
			expectedImgs: 0,
		},
		{
			name: "Embedded resource",
			blocks: []ContentBlock{
				{
					Type: "resource",
					Resource: &ResourceContent{
						URI:  "file:///test.go",
						Text: "package main\n\nfunc main() {}\n",
					},
				},
			},
			expectedText: "\n```go\n# File: file:///test.go\npackage main\n\nfunc main() {}\n```\n",
			expectedImgs: 0,
		},
		{
			name: "Image with base64 source",
			blocks: []ContentBlock{
				{
					Type: "image",
					Source: &ContentSource{
						Type:      "base64",
						Data:      "base64data",
						MediaType: "image/png",
					},
				},
			},
			expectedText: "",
			expectedImgs: 1,
		},
		{
			name: "Mixed content blocks",
			blocks: []ContentBlock{
				{Type: "text", Text: "Check this file:"},
				{
					Type: "resource",
					Resource: &ResourceContent{
						URI:  "test.py",
						Text: "print('hello')",
					},
				},
				{Type: "text", Text: "That's the code."},
			},
			expectedText: "Check this file:\n\n```python\n# File: test.py\nprint('hello')\n```\n\nThat's the code.",
			expectedImgs: 0,
		},
		{
			name: "Audio block (not supported)",
			blocks: []ContentBlock{
				{Type: "audio", Data: "audiodata"},
			},
			expectedText: "[Audio content not yet supported]",
			expectedImgs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test files if needed
			tempDir := t.TempDir()
			for path, content := range tt.setupFiles {
				fullPath := filepath.Join(tempDir, path)
				dir := filepath.Dir(fullPath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatalf("Failed to create test directory: %v", err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
			}

			text, images, err := srv.processContentBlocks(tt.blocks, tempDir)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if text != tt.expectedText {
				t.Errorf("Expected text %q, got %q", tt.expectedText, text)
			}
			if len(images) != tt.expectedImgs {
				t.Errorf("Expected %d images, got %d", tt.expectedImgs, len(images))
			}
		})
	}
}

// TestFormatResourceAsContext tests resource formatting
func TestFormatResourceAsContext(t *testing.T) {
	tests := []struct {
		name     string
		resource *ResourceContent
		expected string
	}{
		{
			name:     "Nil resource",
			resource: nil,
			expected: "",
		},
		{
			name: "Go file",
			resource: &ResourceContent{
				URI:  "main.go",
				Text: "package main",
			},
			expected: "\n```go\n# File: main.go\npackage main\n```\n",
		},
		{
			name: "Python file",
			resource: &ResourceContent{
				URI:  "script.py",
				Text: "print('hello')",
			},
			expected: "\n```python\n# File: script.py\nprint('hello')\n```\n",
		},
		{
			name: "Binary resource",
			resource: &ResourceContent{
				URI:      "image.png",
				MimeType: "image/png",
				Blob:     "base64data",
			},
			expected: "\n[Binary file: image.png (mime: image/png)]\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatResourceAsContext(tt.resource)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestResolveResourceLink tests resource link resolution
func TestResolveResourceLink(t *testing.T) {
	srv := &Server{}
	tempDir := t.TempDir()

	// Create test files
	testFile := filepath.Join(tempDir, "test.go")
	testContent := "package test\n\nfunc Hello() {}"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name        string
		uri         string
		expectError bool
		contains    string
	}{
		{
			name:        "Valid file URI",
			uri:         "file://" + testFile,
			expectError: false,
			contains:    "package test",
		},
		{
			name:        "Relative path",
			uri:         "test.go",
			expectError: false,
			contains:    "package test",
		},
		{
			name:        "Non-existent file",
			uri:         "nonexistent.go",
			expectError: true,
		},
		{
			name:        "Empty URI",
			uri:         "",
			expectError: true,
		},
		{
			name:        "Unsupported scheme",
			uri:         "http://example.com/file.txt",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := srv.resolveResourceLink(tt.uri, tempDir)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tt.contains != "" && len(content) > 0 {
					// Just check it has some content formatted as markdown
					if len(content) < len(tt.contains) {
						t.Errorf("Content too short, expected to contain %q", tt.contains)
					}
				}
			}
		})
	}
}

// TestConvertImageBlock tests image block conversion
func TestConvertImageBlock(t *testing.T) {
	tests := []struct {
		name        string
		block       ContentBlock
		expectError bool
		checkFunc   func(llm.MultimodalImage) bool
	}{
		{
			name: "Base64 image with Source",
			block: ContentBlock{
				Type: "image",
				Source: &ContentSource{
					Type:      "base64",
					Data:      "testdata",
					MediaType: "image/png",
				},
			},
			expectError: false,
			checkFunc: func(img llm.MultimodalImage) bool {
				return img.Base64 == "testdata" && img.MimeType == "image/png"
			},
		},
		{
			name: "URL image",
			block: ContentBlock{
				Type: "image",
				Source: &ContentSource{
					Type:      "url",
					URL:       "https://example.com/image.jpg",
					MediaType: "image/jpeg",
				},
			},
			expectError: false,
			checkFunc: func(img llm.MultimodalImage) bool {
				return img.URL == "https://example.com/image.jpg"
			},
		},
		{
			name: "Direct Data field",
			block: ContentBlock{
				Type:     "image",
				Data:     "directdata",
				MimeType: "image/gif",
			},
			expectError: false,
			checkFunc: func(img llm.MultimodalImage) bool {
				return img.Base64 == "directdata" && img.MimeType == "image/gif"
			},
		},
		{
			name: "Not an image block",
			block: ContentBlock{
				Type: "text",
				Text: "hello",
			},
			expectError: true,
		},
		{
			name: "Image block with no data",
			block: ContentBlock{
				Type: "image",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img, err := convertImageBlock(tt.block)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tt.checkFunc != nil && !tt.checkFunc(img) {
					t.Error("Image conversion validation failed")
				}
			}
		})
	}
}

// TestInferLanguageFromURI tests language inference
func TestInferLanguageFromURI(t *testing.T) {
	tests := []struct {
		uri      string
		expected string
	}{
		{"main.go", "go"},
		{"script.py", "python"},
		{"index.js", "javascript"},
		{"app.tsx", "typescript"},
		{"Main.java", "java"},
		{"lib.rs", "rust"},
		{"config.yaml", "yaml"},
		{"data.json", "json"},
		{"README.md", "markdown"},
		{"style.css", "css"},
		{"test.unknown", ""},
		{"/path/to/file.go", "go"},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			result := inferLanguageFromURI(tt.uri)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestBuildAvailableCommands tests slash command advertising
func TestBuildAvailableCommands(t *testing.T) {
	srv := &Server{}
	tempDir := t.TempDir()

	commands := srv.buildAvailableCommands(tempDir)

	// Should have at least the built-in commands
	if len(commands) == 0 {
		t.Error("Expected at least some built-in commands")
	}

	// Check that common commands are present
	foundYolo := false
	foundClear := false
	for _, cmd := range commands {
		if cmd.Name == "yolo" {
			foundYolo = true
		}
		if cmd.Name == "clear" {
			foundClear = true
		}
	}

	if !foundYolo {
		t.Error("Expected to find 'yolo' command")
	}
	if !foundClear {
		t.Error("Expected to find 'clear' command")
	}

	// Verify structure
	for _, cmd := range commands {
		if cmd.Name == "" {
			t.Error("Command should have a name")
		}
		if cmd.Description == "" {
			t.Error("Command should have a description")
		}
	}
}
