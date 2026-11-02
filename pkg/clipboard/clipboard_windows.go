//go:build windows

package clipboard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func detectContentType() ClipboardContentType {
	return detectContentTypeWindows()
}

func readImage() ([]byte, error) {
	return readImageWindows()
}

func readFilePaths() ([]string, error) {
	return readFilePathsWindows()
}

func detectContentTypeWindows() ClipboardContentType {
	// Use PowerShell to check clipboard content type
	script := `
		$img = Get-Clipboard -Format Image -ErrorAction SilentlyContinue
		$files = Get-Clipboard -Format FileDropList -ErrorAction SilentlyContinue
		if ($img -ne $null) { Write-Output "image" }
		elseif ($files -ne $null) { Write-Output "files" }
		else { Write-Output "text" }
	`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return ContentText
	}

	result := strings.TrimSpace(string(out))
	switch result {
	case "image":
		return ContentImage
	case "files":
		return ContentFilePath
	default:
		return ContentText
	}
}

func readImageWindows() ([]byte, error) {
	// Create a temporary file for the image
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("nano-clipboard-%d.png", os.Getpid()))
	defer os.Remove(tmpFile)

	// Use PowerShell to save clipboard image to file
	script := fmt.Sprintf(`
		$img = Get-Clipboard -Format Image -ErrorAction Stop
		$img.Save('%s', [System.Drawing.Imaging.ImageFormat]::Png)
	`, tmpFile)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to read image from clipboard: %w", err)
	}

	// Read the saved PNG file
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read temporary image file: %w", err)
	}

	return data, nil
}

func readFilePathsWindows() ([]string, error) {
	// Use PowerShell to get file paths from clipboard
	script := `
		$files = Get-Clipboard -Format FileDropList -ErrorAction Stop
		$files | ForEach-Object { Write-Output $_.FullName }
	`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read file paths from clipboard: %w", err)
	}

	paths := strings.Split(strings.TrimSpace(string(out)), "\r\n")
	var result []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}

	return result, nil
}
