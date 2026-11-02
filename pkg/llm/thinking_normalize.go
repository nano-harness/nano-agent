package llm

// FilterTrailingThinkingFromLastAssistant removes trailing thinking/redacted_thinking
// blocks from the last assistant message's ReasoningBlocks. Anthropic API rejects
// messages where the last assistant message ends with only thinking blocks and no
// text or tool_use content.
//
// This function should be called just before sending messages to the API.
func FilterTrailingThinkingFromLastAssistant(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
	}

	// Find the last assistant message
	lastIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastIdx = i
			break
		}
	}
	if lastIdx < 0 {
		return messages
	}

	msg := &messages[lastIdx]

	// If the assistant message has text content or tool calls, reasoning blocks are fine
	if msg.Content != "" || len(msg.ToolCalls) > 0 {
		return messages
	}

	// The message has no text content and no tool calls - it's thinking-only.
	// Clear reasoning blocks to avoid API rejection.
	if len(msg.ReasoningBlocks) > 0 {
		msg.ReasoningBlocks = nil
	}

	return messages
}

// FilterOrphanedThinkingOnlyMessages removes assistant messages that contain only
// reasoning blocks (no text content, no tool calls) and are not followed by a
// tool_result message. These can appear after context compression cuts a message
// in a way that leaves only thinking blocks.
func FilterOrphanedThinkingOnlyMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
	}

	result := make([]Message, 0, len(messages))
	for i, msg := range messages {
		if msg.Role == "assistant" && msg.Content == "" && len(msg.ToolCalls) == 0 && len(msg.ReasoningBlocks) > 0 {
			// Check if the next message is a tool_result (which would mean this
			// assistant message is part of a tool use sequence)
			hasFollowingToolResult := false
			if i+1 < len(messages) {
				next := messages[i+1]
				if next.Role == "tool" || (next.Role == "user" && len(next.ToolResults) > 0) {
					hasFollowingToolResult = true
				}
			}
			if !hasFollowingToolResult {
				// Skip this orphaned thinking-only message
				continue
			}
		}
		result = append(result, msg)
	}
	return result
}

// StripSignatureBearingBlocks clears signatures from thinking blocks and removes
// redacted_thinking blocks entirely. This should be called when credentials change
// (API key rotation) because signatures are tied to the credential that generated them.
//
// Thinking blocks retain their Text for context continuity, but lose their Signature
// (which would be invalid with new credentials anyway). Redacted thinking blocks
// have no displayable text substitute so they must be removed entirely.
func StripSignatureBearingBlocks(messages []Message) []Message {
	for i := range messages {
		if len(messages[i].ReasoningBlocks) == 0 {
			continue
		}
		filtered := make([]ReasoningBlock, 0, len(messages[i].ReasoningBlocks))
		for _, rb := range messages[i].ReasoningBlocks {
			switch rb.Type {
			case ReasoningBlockThinking:
				// Keep the text but clear signature
				rb.Signature = ""
				filtered = append(filtered, rb)
			case ReasoningBlockRedactedThinking:
				// Remove entirely - no text substitute available
				continue
			default:
				filtered = append(filtered, rb)
			}
		}
		messages[i].ReasoningBlocks = filtered
	}
	return messages
}
