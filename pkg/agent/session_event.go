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
	Type            string                 `json:"type"`                       // "user_message", "assistant_message", "tool_call", "tool_result"
	Content         string                 `json:"content,omitempty"`          // Text content for messages
	Contents        []llm.MessageContent   `json:"contents,omitempty"`         // Multimodal content support
	Role            string                 `json:"role,omitempty"`             // "user", "assistant", "tool"
	ToolCalls       []tools.ToolCall       `json:"tool_calls,omitempty"`       // Tool calls in assistant message
	ToolResults     []tools.ToolResult     `json:"tool_results,omitempty"`     // Tool results
	ToolCallID      string                 `json:"tool_call_id,omitempty"`     // Tool call ID for tool results
	Reasoning       string                 `json:"reasoning,omitempty"`        // Deprecated: use ReasoningBlocks; populated from BlocksToText for display only
	ReasoningBlocks []llm.ReasoningBlock   `json:"reasoning_blocks,omitempty"` // Structured reasoning blocks
	Timestamp       int64                  `json:"timestamp"`                  // Unix timestamp
	Metadata        map[string]interface{} `json:"metadata,omitempty"`         // Additional metadata
	Seq             int64                  `json:"seq,omitempty"`              // Monotonic sequence number for resume

	StateTransition *StateTransition  `json:"state_transition,omitempty"`  // State machine transition event
	Compaction      *CompactionMarker `json:"compaction_marker,omitempty"` // Autocompact checkpoint marker
}

const (
	SessionEventTypeCompactionMarker = "compaction_marker"
	SessionEventTypeStateTransition  = "state_transition"
	SessionEventTypeCheckpoint       = "checkpoint"
)

// StateTransition describes a session lifecycle state transition.
type StateTransition struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason,omitempty"`
}

// CompactionMarker records a durable context compaction checkpoint.
type CompactionMarker struct {
	OriginalMessageCount   int    `json:"original_message_count"`
	CompressedMessageCount int    `json:"compressed_message_count"`
	OriginalTokens         int    `json:"original_tokens"`
	CompressedTokens       int    `json:"compressed_tokens"`
	SummaryHash            string `json:"summary_hash"`
	LastSeqBeforeCompact   int64  `json:"last_seq_before_compact"`
}

// SessionIndexEntry represents metadata for a single session in sessions-index.json
type SessionIndexEntry struct {
	ID                string     `json:"id"`
	Summary           string     `json:"summary,omitempty"` // First user message or AI-generated summary
	MessageCount      int        `json:"message_count"`
	CreatedAt         int64      `json:"created_at"`  // Unix timestamp
	ModifiedAt        int64      `json:"modified_at"` // Unix timestamp
	WorkingDir        string     `json:"working_dir"` // Project working directory
	LastSeq           int64      `json:"last_seq,omitempty"`
	LastCompactionSeq int64      `json:"last_compaction_seq,omitempty"`
	State             string     `json:"state,omitempty"`
	Goal              *GoalState `json:"goal,omitempty"`
}

// SessionEventsToMessages converts a slice of SessionEvent to llm.Message slice
func SessionEventsToMessages(events []SessionEvent) []llm.Message {
	messages := make([]llm.Message, 0, len(events))

	for _, event := range events {
		switch event.Type {
		case SessionEventTypeCompactionMarker, SessionEventTypeStateTransition, SessionEventTypeCheckpoint:
			continue
		}
		msg := llm.Message{
			Role:            event.Role,
			Content:         event.Content,
			Contents:        event.Contents,
			ToolCalls:       event.ToolCalls,
			ToolResults:     event.ToolResults,
			ToolCallID:      event.ToolCallID,
			Reasoning:       event.Reasoning,
			ReasoningBlocks: event.ReasoningBlocks,
		}
		messages = append(messages, msg)
	}

	return messages
}

// MessagesToSessionEvents converts llm.Message slice to SessionEvent slice
func MessagesToSessionEvents(messages []llm.Message) []SessionEvent {
	events := make([]SessionEvent, 0, len(messages))

	for i, msg := range messages {
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
			Type:            eventType,
			Content:         msg.Content,
			Contents:        msg.Contents,
			Role:            msg.Role,
			ToolCalls:       msg.ToolCalls,
			ToolResults:     msg.ToolResults,
			ToolCallID:      msg.ToolCallID,
			Reasoning:       msg.Reasoning,
			ReasoningBlocks: msg.ReasoningBlocks,
			Timestamp:       time.Now().Unix(), // Set timestamp per message
			Seq:             int64(i + 1),
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
