// Package attachment manages file attachments in the .nano/attachments/ directory
package attachment

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/llm"
)

// Manager handles file attachments stored in .nano/attachments/
type Manager struct {
	baseDir string // e.g., /path/to/project/.nano/attachments/
}

// NewManager creates a new attachment manager
func NewManager(projectRoot string) (*Manager, error) {
	baseDir := filepath.Join(projectRoot, ".nano", "attachments")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create attachments directory: %w", err)
	}

	return &Manager{
		baseDir: baseDir,
	}, nil
}

// SaveImage saves image data to the attachments directory and returns the file path.
// File name format: img_20260515_193650_a1b2c3.png
func (m *Manager) SaveImage(data []byte, mimeType string) (string, error) {
	ext := mimeTypeToExtension(mimeType)
	timestamp := time.Now().Format("20060102_150405")

	// Generate a random short ID
	randomBytes := make([]byte, 3)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random ID: %w", err)
	}
	randomID := hex.EncodeToString(randomBytes)

	filename := fmt.Sprintf("img_%s_%s%s", timestamp, randomID, ext)
	filePath := filepath.Join(m.baseDir, filename)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write image file: %w", err)
	}

	return filePath, nil
}

// SaveFile copies an external file to the attachments directory
func (m *Manager) SaveFile(srcPath string) (string, error) {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	// Get original filename and extension
	basename := filepath.Base(srcPath)
	ext := filepath.Ext(basename)
	nameWithoutExt := strings.TrimSuffix(basename, ext)

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s%s", nameWithoutExt, timestamp, ext)
	destPath := filepath.Join(m.baseDir, filename)

	destFile, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}

	return destPath, nil
}

// ToMultimodalImage converts a file path to a llm.MultimodalImage for sending to LLM.
// Supports image files (converted to base64 data URL) and text files (content inlined).
func (m *Manager) ToMultimodalImage(filePath string) (llm.MultimodalImage, error) {
	// Read file content
	data, err := os.ReadFile(filePath)
	if err != nil {
		return llm.MultimodalImage{}, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Detect MIME type based on extension
	ext := strings.ToLower(filepath.Ext(filePath))
	mimeType := extensionToMimeType(ext)

	// Only support image types for multimodal
	if !strings.HasPrefix(mimeType, "image/") {
		return llm.MultimodalImage{}, fmt.Errorf("file %s is not an image (type: %s)", filePath, mimeType)
	}

	// Encode to base64 data URL
	encoded := base64.StdEncoding.EncodeToString(data)
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)

	return llm.MultimodalImage{
		URL:      dataURL,
		MimeType: mimeType,
	}, nil
}

// CleanOldAttachments removes attachments older than the specified number of days
func (m *Manager) CleanOldAttachments(days int) error {
	cutoff := time.Now().AddDate(0, 0, -days)

	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		return fmt.Errorf("failed to read attachments directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			filePath := filepath.Join(m.baseDir, entry.Name())
			if err := os.Remove(filePath); err != nil {
				// Log but don't fail
				fmt.Fprintf(os.Stderr, "warning: failed to remove old attachment %s: %v\n", filePath, err)
			}
		}
	}

	return nil
}

// ParseFileReference parses @file references from user input.
// Supports: @/absolute/path.png, @./relative/path.png, @~/home/path.png
// Returns a list of file paths found in the input.
func ParseFileReference(input string) []string {
	// Match @filepath patterns (supporting @./, @/, @~/)
	re := regexp.MustCompile(`@(~?\.?/[^\s]+)`)
	matches := re.FindAllStringSubmatch(input, -1)

	var paths []string
	for _, match := range matches {
		if len(match) > 1 {
			path := match[1]
			// Expand ~ to home directory
			if strings.HasPrefix(path, "~/") {
				homeDir, err := os.UserHomeDir()
				if err == nil {
					path = filepath.Join(homeDir, path[2:])
				}
			}
			paths = append(paths, path)
		}
	}

	return paths
}

// IsImageFile checks if a file path is an image based on extension
func IsImageFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

// mimeTypeToExtension converts MIME type to file extension
func mimeTypeToExtension(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	default:
		return ".png" // default to PNG
	}
}

// extensionToMimeType converts file extension to MIME type
func extensionToMimeType(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return "application/octet-stream"
	}
}
