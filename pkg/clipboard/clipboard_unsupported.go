//go:build !darwin && !linux && !windows

package clipboard

import "fmt"

func detectContentType() ClipboardContentType {
	return ContentText
}

func readImage() ([]byte, error) {
	return nil, fmt.Errorf("clipboard image reading not supported on this platform")
}

func readFilePaths() ([]string, error) {
	return nil, fmt.Errorf("clipboard file path reading not supported on this platform")
}
