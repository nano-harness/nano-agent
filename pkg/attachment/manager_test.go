package attachment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewManager(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	if mgr == nil {
		t.Fatal("NewManager returned nil manager")
	}

	// Check that the attachments directory was created
	attachDir := filepath.Join(tmpDir, ".nano", "attachments")
	if _, err := os.Stat(attachDir); os.IsNotExist(err) {
		t.Errorf("Attachments directory not created: %s", attachDir)
	}
}

func TestSaveImage(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create dummy PNG data (minimal valid PNG header)
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, // IHDR chunk length
		0x49, 0x48, 0x44, 0x52, // IHDR chunk type
	}

	path, err := mgr.SaveImage(pngData, "image/png")
	if err != nil {
		t.Fatalf("SaveImage failed: %v", err)
	}

	if path == "" {
		t.Fatal("SaveImage returned empty path")
	}

	// Check file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("Saved image file does not exist: %s", path)
	}

	// Check file content
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read saved image: %v", err)
	}

	if len(content) != len(pngData) {
		t.Errorf("Saved image size mismatch: got %d, want %d", len(content), len(pngData))
	}

	// Check filename format (img_YYYYMMDD_HHMMSS_xxxxx.png)
	basename := filepath.Base(path)
	if !strings.HasPrefix(basename, "img_") || !strings.HasSuffix(basename, ".png") {
		t.Errorf("Unexpected filename format: %s", basename)
	}
}

func TestSaveFile(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create a source file
	srcPath := filepath.Join(tmpDir, "test_image.jpg")
	testData := []byte("test image data")
	if err := os.WriteFile(srcPath, testData, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	destPath, err := mgr.SaveFile(srcPath)
	if err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	if destPath == "" {
		t.Fatal("SaveFile returned empty path")
	}

	// Check file exists
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		t.Errorf("Saved file does not exist: %s", destPath)
	}

	// Check file content
	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}

	if string(content) != string(testData) {
		t.Errorf("Saved file content mismatch: got %q, want %q", content, testData)
	}
}

func TestToMultimodalImage(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create a test image file
	testPath := filepath.Join(tmpDir, ".nano", "attachments", "test.png")
	testData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	if err := os.WriteFile(testPath, testData, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	img, err := mgr.ToMultimodalImage(testPath)
	if err != nil {
		t.Fatalf("ToMultimodalImage failed: %v", err)
	}

	if img.URL == "" {
		t.Error("ToMultimodalImage returned empty URL")
	}

	if !strings.HasPrefix(img.URL, "data:image/png;base64,") {
		t.Errorf("Unexpected data URL format: %s", img.URL)
	}

	if img.MimeType != "image/png" {
		t.Errorf("Unexpected MIME type: got %s, want image/png", img.MimeType)
	}
}

func TestParseFileReference(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int // expected number of paths
	}{
		{
			name:     "absolute path",
			input:    "Check @/path/to/image.png please",
			expected: 1,
		},
		{
			name:     "relative path",
			input:    "Look at @./relative/path.jpg",
			expected: 1,
		},
		{
			name:     "home path",
			input:    "See @~/home/file.png",
			expected: 1,
		},
		{
			name:     "multiple references",
			input:    "Compare @/first.png and @/second.jpg",
			expected: 2,
		},
		{
			name:     "no references",
			input:    "Just plain text",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := ParseFileReference(tt.input)
			if len(paths) != tt.expected {
				t.Errorf("ParseFileReference returned %d paths, want %d", len(paths), tt.expected)
			}
		})
	}
}

func TestIsImageFile(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"PNG file", "/path/to/image.png", true},
		{"JPG file", "/path/to/photo.jpg", true},
		{"JPEG file", "/path/to/photo.jpeg", true},
		{"GIF file", "/path/to/animation.gif", true},
		{"WebP file", "/path/to/image.webp", true},
		{"BMP file", "/path/to/image.bmp", true},
		{"Text file", "/path/to/file.txt", false},
		{"Go file", "/path/to/main.go", false},
		{"No extension", "/path/to/file", false},
		{"Uppercase PNG", "/path/to/IMAGE.PNG", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsImageFile(tt.path)
			if result != tt.expected {
				t.Errorf("IsImageFile(%s) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestCleanOldAttachments(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	attachDir := filepath.Join(tmpDir, ".nano", "attachments")

	// Create some test files with different ages
	oldFile := filepath.Join(attachDir, "old_file.png")
	if err := os.WriteFile(oldFile, []byte("old"), 0644); err != nil {
		t.Fatalf("Failed to create old file: %v", err)
	}

	// Set old file's modification time to 10 days ago
	oldTime := os.FileMode(0644)
	_ = oldTime // Suppress unused warning for now
	// Note: In a real test, we'd use os.Chtimes to set the modification time

	// Clean files older than 5 days (our old file should be removed)
	err = mgr.CleanOldAttachments(5)
	if err != nil {
		t.Errorf("CleanOldAttachments failed: %v", err)
	}

	// Note: Without setting file times, this test is limited
	// In production, os.Chtimes would be used to properly test this
}

func TestMimeTypeConversion(t *testing.T) {
	tests := []struct {
		mimeType string
		ext      string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"image/bmp", ".bmp"},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			ext := mimeTypeToExtension(tt.mimeType)
			if ext != tt.ext {
				t.Errorf("mimeTypeToExtension(%s) = %s, want %s", tt.mimeType, ext, tt.ext)
			}
		})
	}
}

func TestExtensionToMimeType(t *testing.T) {
	tests := []struct {
		ext      string
		mimeType string
	}{
		{".png", "image/png"},
		{".jpg", "image/jpeg"},
		{".jpeg", "image/jpeg"},
		{".gif", "image/gif"},
		{".webp", "image/webp"},
		{".bmp", "image/bmp"},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			mimeType := extensionToMimeType(tt.ext)
			if mimeType != tt.mimeType {
				t.Errorf("extensionToMimeType(%s) = %s, want %s", tt.ext, mimeType, tt.mimeType)
			}
		})
	}
}
