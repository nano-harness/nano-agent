package llm

import "strings"

// ReasoningBlockType identifies the type of a reasoning block.
type ReasoningBlockType string

const (
	// ReasoningBlockThinking represents a thinking block with text and signature.
	ReasoningBlockThinking ReasoningBlockType = "thinking"
	// ReasoningBlockRedactedThinking represents a redacted thinking block with encrypted data.
	ReasoningBlockRedactedThinking ReasoningBlockType = "redacted_thinking"
)

// ReasoningBlock represents a structured reasoning/thinking block aligned with
// Anthropic's content block format.
//   - thinking:          {Text, Signature}
//   - redacted_thinking: {Data}
type ReasoningBlock struct {
	Type      ReasoningBlockType `json:"type"`
	Text      string             `json:"text,omitempty"`      // type=thinking: the thinking text
	Signature string             `json:"signature,omitempty"` // type=thinking: base64 signature for verification
	Data      string             `json:"data,omitempty"`      // type=redacted_thinking: base64 encrypted blob
}

// IsRedacted returns true if this is a redacted_thinking block.
func (b ReasoningBlock) IsRedacted() bool {
	return b.Type == ReasoningBlockRedactedThinking
}

// Empty returns true if the block has no meaningful content.
func (b ReasoningBlock) Empty() bool {
	switch b.Type {
	case ReasoningBlockThinking:
		return b.Text == ""
	case ReasoningBlockRedactedThinking:
		return b.Data == ""
	default:
		return true
	}
}

// BlocksToText aggregates reasoning blocks into a single plain text string.
// Redacted thinking blocks are represented as "[redacted]" placeholder.
// Used for UI display, OpenAI-compatible providers, and human-readable transcript.
func BlocksToText(blocks []ReasoningBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, b := range blocks {
		if i > 0 {
			sb.WriteString("\n")
		}
		switch b.Type {
		case ReasoningBlockThinking:
			sb.WriteString(b.Text)
		case ReasoningBlockRedactedThinking:
			sb.WriteString("[redacted]")
		}
	}
	return sb.String()
}

// HasSignature returns true if any block in the slice carries a non-empty signature.
func HasSignature(blocks []ReasoningBlock) bool {
	for _, b := range blocks {
		if b.Signature != "" {
			return true
		}
	}
	return false
}
