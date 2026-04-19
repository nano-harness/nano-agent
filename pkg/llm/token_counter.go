package llm

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

// TokenCounter provides accurate token counting using tiktoken
// Falls back to character-based estimation if tiktoken fails
// Supports multiple models with appropriate encodings
type TokenCounter struct {
	encoding *tiktoken.Tiktoken
	fallback bool
	mu       sync.RWMutex
}

// NewTokenCounter creates a new token counter for the specified model
func NewTokenCounter(model string) (*TokenCounter, error) {
	// Map common model names to tiktoken encodings
	encodingName := getEncodingForModel(model)

	encoding, err := tiktoken.GetEncoding(encodingName)
	if err != nil {
		// Fallback to character-based estimation
		return &TokenCounter{
			encoding: nil,
			fallback: true,
		}, nil
	}

	return &TokenCounter{
		encoding: encoding,
		fallback: false,
	}, nil
}

// CountTokens counts tokens for the given text
func (tc *TokenCounter) CountTokens(text string) int {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	if tc.fallback || tc.encoding == nil {
		// Fallback to character-based estimation (rough approximation)
		return len(text) / 4
	}

	// Use tiktoken for accurate counting
	tokens := tc.encoding.Encode(text, nil, nil)
	return len(tokens)
}

// CountMessagesTokens counts tokens for a slice of messages
func (tc *TokenCounter) CountMessagesTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		// Only count content; role labels are not part of tokenized content
		total += tc.CountTokens(msg.Content)

		// Count tool calls
		for _, toolCall := range msg.ToolCalls {
			// Count tool name once
			total += tc.CountTokens(toolCall.Name)
			// Convert arguments to JSON string for counting (more precise)
			argsJSON, _ := json.Marshal(toolCall.Arguments)
			total += tc.CountTokens(string(argsJSON))
		}

		// Count tool results
		for _, toolResult := range msg.ToolResults {
			total += tc.CountTokens(toolResult.Content)
		}
	}
	return total
}

// getEncodingForModel returns the appropriate encoding for the given model
func getEncodingForModel(model string) string {
	model = strings.ToLower(model)

	// GPT-4 and GPT-3.5 models use cl100k_base
	if strings.Contains(model, "gpt-4") || strings.Contains(model, "gpt-3.5") {
		return "cl100k_base"
	}

	// DeepSeek models (use GPT-4 encoding as approximation)
	if strings.Contains(model, "deepseek") {
		return "cl100k_base"
	}

	// Claude models (use GPT-4 encoding as approximation)
	if strings.Contains(model, "claude") {
		return "cl100k_base"
	}

	// Default to cl100k_base for unknown models
	return "cl100k_base"
}
