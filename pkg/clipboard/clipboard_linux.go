//go:build linux

package clipboard

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func detectContentType() ClipboardContentType {
	return detectContentTypeLinux()
}

func readImage() ([]byte, error) {
	return readImageLinux()
}

func readFilePaths() ([]string, error) {
	return readFilePathsLinux()
}

func detectContentTypeLinux() ClipboardContentType {
	// Check for Wayland first
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		cmd := exec.Command("wl-paste", "--list-types")
		out, err := cmd.Output()
		if err == nil {
			types := string(out)
			if strings.Contains(types, "image/") {
				return ContentImage
			}
			if strings.Contains(types, "text/uri-list") || strings.Contains(types, "x-special/gnome-copied-files") {
				return ContentFilePath
			}
		}
		return ContentText
	}

	// Try X11
	cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
	out, err := cmd.Output()
	if err != nil {
		return ContentText
	}

	targets := string(out)
	if strings.Contains(targets, "image/") {
		return ContentImage
	}
	if strings.Contains(targets, "text/uri-list") || strings.Contains(targets, "x-special/gnome-copied-files") {
		return ContentFilePath
	}

	return ContentText
}

func readImageLinux() ([]byte, error) {
	var cmd *exec.Cmd
	var buf bytes.Buffer

	// Check for Wayland
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		// Try PNG first
		cmd = exec.Command("wl-paste", "--type", "image/png")
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			return out, nil
		}

		// Try JPEG
		cmd = exec.Command("wl-paste", "--type", "image/jpeg")
		out, err = cmd.Output()
		if err == nil && len(out) > 0 {
			return out, nil
		}

		return nil, fmt.Errorf("no image data in clipboard (wl-paste)")
	}

	// Try X11 with xclip
	cmd = exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
	out, err := cmd.Output()
	if err == nil && len(out) > 0 {
		return out, nil
	}

	// Try JPEG
	cmd = exec.Command("xclip", "-selection", "clipboard", "-t", "image/jpeg", "-o")
	out, err = cmd.Output()
	if err == nil && len(out) > 0 {
		return out, nil
	}

	return buf.Bytes(), fmt.Errorf("no image data in clipboard (install xclip or wl-clipboard)")
}

func readFilePathsLinux() ([]string, error) {
	var cmd *exec.Cmd

	// Check for Wayland
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		cmd = exec.Command("wl-paste", "--type", "text/uri-list")
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to read file paths from clipboard: %w", err)
		}
		return parseURIList(string(out)), nil
	}

	// Try X11
	cmd = exec.Command("xclip", "-selection", "clipboard", "-t", "text/uri-list", "-o")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read file paths from clipboard: %w", err)
	}

	return parseURIList(string(out)), nil
}

func parseURIList(uriList string) []string {
	lines := strings.Split(strings.TrimSpace(uriList), "\n")
	var result []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Remove file:// prefix
		if strings.HasPrefix(line, "file://") {
			line = strings.TrimPrefix(line, "file://")
		}

		result = append(result, line)
	}

	return result
}
