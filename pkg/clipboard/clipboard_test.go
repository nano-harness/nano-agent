package clipboard

import (
	"testing"
)

func TestClipboardContentType(t *testing.T) {
	tests := []struct {
		name     string
		expected ClipboardContentType
	}{
		{
			name:     "ContentText",
			expected: ContentText,
		},
		{
			name:     "ContentImage",
			expected: ContentImage,
		},
		{
			name:     "ContentFilePath",
			expected: ContentFilePath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expected < ContentText || tt.expected > ContentFilePath {
				t.Errorf("Invalid content type value: %d", tt.expected)
			}
		})
	}
}

func TestDetectContentType(t *testing.T) {
	// This test just ensures the function doesn't panic
	// Actual functionality depends on clipboard state which is hard to mock
	contentType := DetectContentType()
	if contentType < ContentText || contentType > ContentFilePath {
		t.Errorf("DetectContentType returned invalid value: %d", contentType)
	}
}

func TestReadImage(t *testing.T) {
	// This test just ensures the function doesn't panic
	// It will likely return an error if no image is in clipboard
	_, err := ReadImage()
	// We expect either success (if image is in clipboard) or a specific error
	// Just ensure it doesn't panic
	_ = err
}

func TestReadFilePaths(t *testing.T) {
	// This test just ensures the function doesn't panic
	// It will likely return an error or empty slice if no files are in clipboard
	paths, err := ReadFilePaths()
	// We expect either success (if files are in clipboard) or a specific error
	// Just ensure it doesn't panic and returns valid types
	_ = paths
	_ = err
}
