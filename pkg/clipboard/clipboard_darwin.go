//go:build darwin

package clipboard

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func detectContentType() ClipboardContentType {
	return detectContentTypeMacOS()
}

func readImage() ([]byte, error) {
	return readImageMacOS()
}

func readFilePaths() ([]string, error) {
	return readFilePathsMacOS()
}

func detectContentTypeMacOS() ClipboardContentType {
	// Use osascript to check clipboard content type
	cmd := exec.Command("osascript", "-e", "clipboard info")
	out, err := cmd.Output()
	if err != nil {
		return ContentText
	}

	info := string(out)
	// Check for image types (PNG, TIFF are common clipboard formats on macOS)
	if strings.Contains(info, "«class PNGf»") ||
		strings.Contains(info, "«class TIFF»") ||
		strings.Contains(info, "public.png") ||
		strings.Contains(info, "public.tiff") {
		return ContentImage
	}

	// Check for file URLs
	if strings.Contains(info, "«class furl»") || strings.Contains(info, "public.file-url") {
		return ContentFilePath
	}

	return ContentText
}

func readImageMacOS() ([]byte, error) {
	// Try pngpaste first (if installed, it's the most reliable)
	cmd := exec.Command("pngpaste", "-")
	out, err := cmd.Output()
	if err == nil && len(out) > 0 {
		return out, nil
	}

	// Fallback to osascript
	script := `
		set pngData to the clipboard as «class PNGf»
		set pngBytes to pngData as list
		set output to ""
		repeat with b in pngBytes
			set output to output & (b as text) & " "
		end repeat
		return output
	`
	cmd = exec.Command("osascript", "-e", script)
	out, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read image from clipboard: %w (install pngpaste for better support: brew install pngpaste)", err)
	}

	// Parse space-separated byte values
	bytesStr := strings.TrimSpace(string(out))
	if bytesStr == "" {
		return nil, fmt.Errorf("clipboard does not contain image data")
	}

	var buf bytes.Buffer
	for _, s := range strings.Fields(bytesStr) {
		var b byte
		_, err := fmt.Sscanf(s, "%d", &b)
		if err != nil {
			continue
		}
		buf.WriteByte(b)
	}

	if buf.Len() == 0 {
		return nil, fmt.Errorf("failed to parse image data from clipboard")
	}

	return buf.Bytes(), nil
}

func readFilePathsMacOS() ([]string, error) {
	// Read file URLs from clipboard
	script := `
		try
			set fileList to the clipboard as «class furl»
			set output to ""
			repeat with f in fileList
				set output to output & POSIX path of f & linefeed
			end repeat
			return output
		on error
			return ""
		end try
	`
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read file paths from clipboard: %w", err)
	}

	paths := strings.Split(strings.TrimSpace(string(out)), "\n")
	var result []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}

	return result, nil
}
