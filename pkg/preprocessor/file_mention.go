package preprocessor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/tools"
	"github.com/google/uuid"
)

var hashFileMentionRe = regexp.MustCompile(`(?m)(^|\s)#([A-Za-z0-9_./\-*]+)`)

const (
	MaxFileSizeBytes  = 20 * 1024 * 1024
	MaxFileLines      = 2000
	MaxImageSizeBytes = 5 * 1024 * 1024
)

// FileMentionResult contains synthetic read_file messages and multimodal images
// produced from #path references in a user message.
type FileMentionResult struct {
	Messages []llm.Message
	Images   []llm.MultimodalImage
}

// FileMentionMessages resolves #path references in userInput against workingDir
// and returns assistant tool_call + tool_result message pairs to insert before
// the actual user message.
func FileMentionMessages(userInput, workingDir string) (FileMentionResult, error) {
	paths := ExtractFileMentions(userInput)
	if len(paths) == 0 {
		return FileMentionResult{}, nil
	}
	if workingDir == "" {
		workingDir = "."
	}
	root, err := filepath.Abs(workingDir)
	if err != nil {
		return FileMentionResult{}, err
	}

	var result FileMentionResult
	for _, mention := range paths {
		expanded, err := expandMention(root, mention)
		if err != nil {
			result.Messages = append(result.Messages, syntheticReadFilePair(mention, fmt.Sprintf("Error reading file: %v", err))...)
			continue
		}
		for _, rel := range expanded {
			content, image, err := readMentionFile(root, rel)
			if image != nil {
				result.Images = append(result.Images, *image)
			}
			if err != nil {
				content = fmt.Sprintf("Error reading file: %v", err)
			}
			result.Messages = append(result.Messages, syntheticReadFilePair(rel, content)...)
		}
	}
	return result, nil
}

// ExtractFileMentions returns unique #path references in first-seen order.
func ExtractFileMentions(input string) []string {
	matches := hashFileMentionRe.FindAllStringSubmatch(input, -1)
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		path := strings.TrimSpace(match[2])
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func expandMention(root, mention string) ([]string, error) {
	cleaned, err := cleanMentionPath(mention)
	if err != nil {
		return nil, err
	}
	if strings.Contains(cleaned, "*") {
		pattern := filepath.Join(root, filepath.FromSlash(cleaned))
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		sort.Strings(matches)
		out := make([]string, 0, len(matches))
		for _, match := range matches {
			rel, err := safeRel(root, match)
			if err != nil {
				return nil, err
			}
			if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil && !info.IsDir() {
				out = append(out, rel)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("no files matched %s", mention)
		}
		return out, nil
	}
	rel, err := safeRel(root, filepath.Join(root, filepath.FromSlash(cleaned)))
	if err != nil {
		return nil, err
	}
	return []string{rel}, nil
}

func cleanMentionPath(mention string) (string, error) {
	if filepath.IsAbs(mention) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", mention)
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(mention)))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path escapes workspace: %s", mention)
	}
	return cleaned, nil
}

func safeRel(root, candidate string) (string, error) {
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.Clean(rel)
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes workspace: %s", candidate)
	}
	return filepath.ToSlash(rel), nil
}

func readMentionFile(root, rel string) (string, *llm.MultimodalImage, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(full)
	if err != nil {
		return "", nil, err
	}
	if info.IsDir() {
		return "", nil, fmt.Errorf("%s is a directory", rel)
	}
	if info.Size() > MaxFileSizeBytes {
		return "", nil, fmt.Errorf("file too large: %s exceeds %d bytes", rel, MaxFileSizeBytes)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", nil, err
	}
	mimeType := detectMimeType(rel, data)
	if strings.HasPrefix(mimeType, "image/") {
		if len(data) > MaxImageSizeBytes {
			return "", nil, fmt.Errorf("image too large: %s exceeds %d bytes", rel, MaxImageSizeBytes)
		}
		image := llm.MultimodalImage{
			Base64:   base64.StdEncoding.EncodeToString(data),
			MimeType: mimeType,
		}
		return fmt.Sprintf("Image file %s attached as multimodal content (%s, %d bytes).", rel, mimeType, len(data)), &image, nil
	}
	if isBinary(data) {
		return "", nil, fmt.Errorf("binary files are not supported: %s", rel)
	}
	return truncateFileLines(string(data), MaxFileLines), nil, nil
}

func syntheticReadFilePair(rel, content string) []llm.Message {
	callID := "call_" + uuid.NewString()
	args := map[string]interface{}{
		"relative_workspace_path": rel,
		"should_read_entire_file": true,
	}
	argsJSON, _ := json.Marshal(args)
	return []llm.Message{
		{
			Role: "assistant",
			ToolCalls: []tools.ToolCall{{
				ID:        callID,
				Name:      "read_file",
				Arguments: args,
			}},
		},
		{
			Role:       "tool",
			ToolCallID: callID,
			Content:    fmt.Sprintf("read_file(%s)\n%s", string(argsJSON), content),
		},
	}
}

func detectMimeType(path string, data []byte) string {
	if ext := filepath.Ext(path); ext != "" {
		if typ := mime.TypeByExtension(ext); typ != "" {
			return strings.Split(typ, ";")[0]
		}
	}
	if len(data) > 512 {
		data = data[:512]
	}
	return http.DetectContentType(data)
}

func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	if !utf8.Valid(data) {
		return true
	}
	return false
}

func truncateFileLines(content string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.SplitAfter(content, "\n")
	if len(lines) <= maxLines {
		return content
	}
	return strings.Join(lines[:maxLines], "") + fmt.Sprintf("\n[File truncated after %d lines]", maxLines)
}
