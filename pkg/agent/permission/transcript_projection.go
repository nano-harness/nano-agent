package permission

import (
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/llm"
)

// TranscriptEntry is a single entry in the compact session projection sent to
// the classifier.  Only user text and tool_use blocks are included; assistant
// text blocks and tool_result blocks are intentionally omitted to prevent
// self-influence and prompt-injection via untrusted tool output.
type TranscriptEntry struct {
	Role    string `json:"role"`    // "user" | "tool_use"
	Content string `json:"content"` // user text excerpt or "toolName(paramSummary)"
}

const (
	// defaultMaxTranscriptEntries is the maximum number of entries kept in the
	// compact transcript to bound classifier prompt size.
	defaultMaxTranscriptEntries = 20
	// maxEntryLength is the maximum character length of a single entry's content.
	maxEntryLength = 200
)

// BuildCompactTranscript projects messages into a classifier-safe compact
// transcript.  It keeps at most maxEntries of the most recent entries.
// Pass maxEntries <= 0 to use the default (20).
func BuildCompactTranscript(messages []llm.Message, maxEntries int) []TranscriptEntry {
	if maxEntries <= 0 {
		maxEntries = defaultMaxTranscriptEntries
	}
	var entries []TranscriptEntry
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if text := extractUserText(msg); text != "" {
				entries = append(entries, TranscriptEntry{
					Role:    "user",
					Content: truncateString(text, maxEntryLength),
				})
			}
		case "assistant":
			// Only include tool_use blocks; skip assistant text to prevent
			// the classifier from being influenced by the model's own reasoning.
			for _, tc := range msg.ToolCalls {
				summary := projectToolCallParams(tc.Name, tc.Arguments)
				entries = append(entries, TranscriptEntry{
					Role:    "tool_use",
					Content: truncateString(fmt.Sprintf("%s(%s)", tc.Name, summary), maxEntryLength),
				})
			}
			// "tool" / "system" roles are intentionally excluded:
			//   - "tool" contains untrusted output (prompt injection risk)
			//   - "system" is irrelevant to risk classification
		}
	}
	// Return the most recent maxEntries entries.
	if len(entries) > maxEntries {
		entries = entries[len(entries)-maxEntries:]
	}
	return entries
}

// extractUserText pulls plain text out of a user message, skipping image and
// tool_result content blocks.
func extractUserText(msg llm.Message) string {
	if msg.Content != "" {
		return strings.TrimSpace(msg.Content)
	}
	var parts []string
	for _, mc := range msg.Contents {
		if mc.Type == "text" && strings.TrimSpace(mc.Text) != "" {
			parts = append(parts, strings.TrimSpace(mc.Text))
		}
	}
	return strings.Join(parts, " ")
}

// projectToolCallParams extracts a small, safe summary of the parameters for a
// tool call.  Only the most security-relevant fields are surfaced; large content
// blobs are truncated to keep the transcript compact.
func projectToolCallParams(toolName string, params map[string]interface{}) string {
	if len(params) == 0 {
		return ""
	}
	// For file-system tools surface the path.
	switch toolName {
	case "write_file", "read_file", "edit_file", "delete_file", "patch_file":
		if p, ok := pathFromParams(params); ok {
			return truncateString(p, 80)
		}
	case "run_shell_command", "bash", "shell":
		if cmd, ok := params["command"].(string); ok {
			return truncateString(cmd, 100)
		}
	}
	// Generic fallback: list at most 2 keys.
	var parts []string
	for k, v := range params {
		if len(parts) >= 2 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s=%v", k, truncateString(fmt.Sprintf("%v", v), 40)))
	}
	return strings.Join(parts, ", ")
}

func pathFromParams(params map[string]interface{}) (string, bool) {
	for _, key := range []string{"file_path", "path"} {
		if v, ok := params[key].(string); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
