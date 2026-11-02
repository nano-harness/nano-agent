// Package clipboard provides cross-platform clipboard access for reading images and file paths
package clipboard

// ClipboardContentType represents the type of content in the clipboard
type ClipboardContentType int

const (
	// ContentText represents text content in the clipboard
	ContentText ClipboardContentType = iota
	// ContentImage represents image content in the clipboard
	ContentImage
	// ContentFilePath represents file path(s) in the clipboard
	ContentFilePath
)

// DetectContentType explores the clipboard and returns the type of content present.
// It prioritizes image and file content over plain text.
func DetectContentType() ClipboardContentType {
	return detectContentType()
}

// ReadImage reads image data from the clipboard and returns it as PNG bytes.
// Returns an error if no image is available or the platform is unsupported.
func ReadImage() ([]byte, error) {
	return readImage()
}

// ReadFilePaths reads file paths from the clipboard (e.g., files copied in Finder/Explorer).
// Returns an empty slice if no file paths are available.
func ReadFilePaths() ([]string, error) {
	return readFilePaths()
}
