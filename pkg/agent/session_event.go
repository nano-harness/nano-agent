package agent

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

// SessionEvent represents a single event in the JSONL session log.
// Each line in the .jsonl file is one SessionEvent.
type SessionEvent struct {
	Type        string                 `json:"type"`                   // "user_message", "assistant_message", "tool_call", "tool_result"
	Content     string                 `json:"content,omitempty"`      // Text content for messages
	Contents    []llm.MessageContent   `json:"contents,omitempty"`     // Multimodal content support
	Role        string                 `json:"role,omitempty"`         // "user", "assistant", "tool"
	ToolCalls   []tools.ToolCall       `json:"tool_calls,omitempty"`   // Tool calls in assistant message
	ToolResults []tools.ToolResult     `json:"tool_results,omitempty"` // Tool results
	ToolCallID  string                 `json:"tool_call_id,omitempty"` // Tool call ID for tool results
	Reasoning   string                 `json:"reasoning,omitempty"`    // Reasoning tokens from the model
	Timestamp   int64                  `json:"timestamp"`              // Unix timestamp
	Metadata    map[string]interface{} `json:"metadata,omitempty"`     // Additional metadata
}

// SessionIndexEntry represents metadata for a single session in sessions-index.json
type SessionIndexEntry struct {
	ID           string `json:"id"`
	Summary      string `json:"summary,omitempty"` // First user message or AI-generated summary
	MessageCount int    `json:"message_count"`
	CreatedAt    int64  `json:"created_at"`  // Unix timestamp
	ModifiedAt   int64  `json:"modified_at"` // Unix timestamp
	WorkingDir   string `json:"working_dir"` // Project working directory
}

// SessionEventsToMessages converts a slice of SessionEvent to llm.Message slice
func SessionEventsToMessages(events []SessionEvent) []llm.Message {
	messages := make([]llm.Message, 0, len(events))

	for _, event := range events {
		msg := llm.Message{
			Role:        event.Role,
			Content:     event.Content,
			Contents:    event.Contents,
			ToolCalls:   event.ToolCalls,
			ToolResults: event.ToolResults,
			ToolCallID:  event.ToolCallID,
			Reasoning:   event.Reasoning,
		}
		messages = append(messages, msg)
	}

	return messages
}

// MessagesToSessionEvents converts llm.Message slice to SessionEvent slice
func MessagesToSessionEvents(messages []llm.Message) []SessionEvent {
	events := make([]SessionEvent, 0, len(messages))

	for _, msg := range messages {
		eventType := "message"
		switch msg.Role {
		case "user":
			eventType = "user_message"
		case "assistant":
			eventType = "assistant_message"
		case "tool":
			eventType = "tool_result"
		}

		event := SessionEvent{
			Type:        eventType,
			Content:     msg.Content,
			Contents:    msg.Contents,
			Role:        msg.Role,
			ToolCalls:   msg.ToolCalls,
			ToolResults: msg.ToolResults,
			ToolCallID:  msg.ToolCallID,
			Reasoning:   msg.Reasoning,
			Timestamp:   time.Now().Unix(), // Set timestamp per message
		}
		events = append(events, event)
	}

	return events
}

// ToJSONL converts a SessionEvent to a JSONL line (JSON + newline)
func (e *SessionEvent) ToJSONL() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	// Append newline for JSONL format
	return append(data, '\n'), nil
}

// ParseJSONL parses a JSONL line into a SessionEvent
func ParseJSONL(line []byte) (*SessionEvent, error) {
	var event SessionEvent
	if err := json.Unmarshal(line, &event); err != nil {
		return nil, err
	}
	return &event, nil
}

// encodeProjectPathWithHash encodes a project path with a hash suffix to avoid collisions
// Example: /Users/name/project -> Users-name-project-a1b2c3d4
func encodeProjectPathWithHash(absPath string) string {
	// Calculate hash of the absolute path
	hash := sha256.Sum256([]byte(absPath))
	hashStr := base64.URLEncoding.EncodeToString(hash[:8])[:8] // Use first 8 chars of base64 hash

	// Clean the path for filesystem safety
	cleaned := absPath
	// Remove leading slash on Unix paths
	cleaned = cleaned[1:] // Remove leading /

	// Replace path separators and other unsafe characters
	unsafe := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range unsafe {
		cleaned = replaceAll(cleaned, char, "-")
	}

	// Combine with hash to ensure uniqueness
	return fmt.Sprintf("%s-%s", cleaned, hashStr)
}

func replaceAll(s, old, new string) string {
	result := ""
	for _, ch := range s {
		if string(ch) == old {
			result += new
		} else {
			result += string(ch)
		}
	}
	return result
}
