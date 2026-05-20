package acp

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// processContentBlocks processes all ContentBlock types and returns unified text + images
func (s *Server) processContentBlocks(blocks []ContentBlock, cwd string) (string, []llm.MultimodalImage, error) {
	var textParts []string
	var images []llm.MultimodalImage

	for _, block := range blocks {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)

		case "resource":
			// Embedded resource - content is inline
			if block.Resource != nil {
				resourceText := formatResourceAsContext(block.Resource)
				textParts = append(textParts, resourceText)
			}

		case "resource_link":
			// Resource link - read content via filesystem
			content, err := s.resolveResourceLink(block.URI, cwd)
			if err != nil {
				// Include error as context rather than failing
				logger.Warnf("ACP: Failed to resolve resource_link %s: %v", block.URI, err)
				textParts = append(textParts, fmt.Sprintf("[Failed to read %s: %v]", block.URI, err))
			} else {
				textParts = append(textParts, content)
			}

		case "image":
			// Convert image to multimodal format
			img, err := convertImageBlock(block)
			if err != nil {
				logger.Warnf("ACP: Failed to convert image block: %v", err)
			} else {
				images = append(images, img)
			}

		case "audio":
			// Audio not yet supported in LLM layer
			logger.Warnf("ACP: Audio content not yet supported")
			textParts = append(textParts, "[Audio content not yet supported]")
		}
	}

	return strings.Join(textParts, "\n"), images, nil
}

// formatResourceAsContext formats an embedded resource as context text
func formatResourceAsContext(resource *ResourceContent) string {
	if resource == nil {
		return ""
	}

	var sb strings.Builder

	// Extract filename from URI if available
	uri := resource.URI
	filename := filepath.Base(uri)
	if filename == "." || filename == "/" {
		filename = uri
	}

	// Infer language from URI
	lang := inferLanguageFromURI(uri)

	// Format as markdown code block for text resources
	if resource.Text != "" {
		sb.WriteString(fmt.Sprintf("\n```%s\n", lang))
		sb.WriteString(fmt.Sprintf("# File: %s\n", uri))
		sb.WriteString(resource.Text)
		if !strings.HasSuffix(resource.Text, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
	} else if resource.Blob != "" {
		// Binary content - just note its presence
		sb.WriteString(fmt.Sprintf("\n[Binary file: %s (mime: %s)]\n", uri, resource.MimeType))
	} else {
		sb.WriteString(fmt.Sprintf("\n[Resource: %s]\n", uri))
	}

	return sb.String()
}

// resolveResourceLink resolves a resource_link URI and reads its content
func (s *Server) resolveResourceLink(uri string, cwd string) (string, error) {
	if uri == "" {
		return "", fmt.Errorf("empty URI")
	}

	// Parse URI
	parsedURI, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("invalid URI: %w", err)
	}

	// Only support file:// scheme for now
	if parsedURI.Scheme != "file" && parsedURI.Scheme != "" {
		return "", fmt.Errorf("unsupported URI scheme: %s", parsedURI.Scheme)
	}

	// Get file path
	filePath := parsedURI.Path
	if parsedURI.Scheme == "" {
		// Relative path
		filePath = uri
	}

	// Make path absolute relative to cwd
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(cwd, filePath)
	}

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Format as context with file path
	lang := inferLanguageFromURI(filePath)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n```%s\n", lang))
	sb.WriteString(fmt.Sprintf("# File: %s\n", filePath))
	sb.WriteString(string(content))
	if !strings.HasSuffix(string(content), "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("```\n")

	return sb.String(), nil
}

// convertImageBlock converts an image ContentBlock to MultimodalImage
func convertImageBlock(block ContentBlock) (llm.MultimodalImage, error) {
	if block.Type != "image" {
		return llm.MultimodalImage{}, fmt.Errorf("not an image block")
	}

	// Check for base64 data in Source field
	if block.Source != nil {
		if block.Source.Type == "base64" && block.Source.Data != "" {
			return llm.MultimodalImage{
				Base64:   block.Source.Data,
				MimeType: block.Source.MediaType,
			}, nil
		}
		if block.Source.Type == "url" && block.Source.URL != "" {
			return llm.MultimodalImage{
				URL:      block.Source.URL,
				MimeType: block.Source.MediaType,
			}, nil
		}
	}

	// Check for direct Data field (alternative format)
	if block.Data != "" {
		return llm.MultimodalImage{
			Base64:   block.Data,
			MimeType: block.MimeType,
		}, nil
	}

	return llm.MultimodalImage{}, fmt.Errorf("no valid image data found")
}

// inferLanguageFromURI infers the programming language from a file URI
func inferLanguageFromURI(uri string) string {
	ext := strings.ToLower(filepath.Ext(uri))
	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".c":
		return "c"
	case ".cpp", ".cc", ".cxx":
		return "cpp"
	case ".h", ".hpp":
		return "cpp"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".scala":
		return "scala"
	case ".sh", ".bash":
		return "bash"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".xml":
		return "xml"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".sql":
		return "sql"
	case ".md", ".markdown":
		return "markdown"
	case ".txt", ".text":
		return "text"
	default:
		return ""
	}
}
