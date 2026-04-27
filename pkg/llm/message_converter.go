package llm

import (
	"encoding/json"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/openai/openai-go/v3"
)

// MessageConverter translates between the agent's internal Message/tool
// representation and the OpenAI-compatible wire format.
//
// MessageConverter is intentionally provider-agnostic at the call site so
// future Provider implementations (e.g. Gemini, Claude, local routing) can
// embed or wrap a converter rather than duplicating the conversion logic.
//
// The zero value is a usable converter; pass tools via WithTools when the
// caller needs ConvertTools to emit a tool list.
type MessageConverter struct {
	tools []interfaces.Tool
}

// NewMessageConverter returns a converter pre-populated with the supplied
// tool set. Passing nil is equivalent to the zero value.
func NewMessageConverter(tools []interfaces.Tool) *MessageConverter {
	return &MessageConverter{tools: tools}
}

// WithTools returns a copy of the converter with a different tool list.
// The original converter is not mutated, which keeps the type safe for
// concurrent reads when tools change at runtime.
func (mc *MessageConverter) WithTools(tools []interfaces.Tool) *MessageConverter {
	return &MessageConverter{tools: tools}
}

// ValidateMessageSequence verifies that every assistant message containing
// tool_calls is followed by at least one tool message before the next
// assistant message. Returns a descriptive error when the sequence would be
// rejected by the OpenAI API.
func (mc *MessageConverter) ValidateMessageSequence(messages []Message) error {
	for i, msg := range messages {
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		hasFollowingToolMessage := false
		for j := i + 1; j < len(messages); j++ {
			nextMsg := messages[j]
			if nextMsg.Role == "tool" {
				hasFollowingToolMessage = true
				break
			}
			if nextMsg.Role == "assistant" {
				break
			}
		}
		if !hasFollowingToolMessage {
			return fmt.Errorf("assistant message at index %d has tool_calls but no following tool messages", i)
		}
	}
	return nil
}

// CleanupMessages strips tool_calls from assistant messages whose tool
// invocations were never followed by tool result messages. The returned
// slice is safe to send to the OpenAI API even if the original sequence was
// interrupted mid-flight.
func (mc *MessageConverter) CleanupMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
	}

	cleaned := make([]Message, 0, len(messages))
	for i, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			hasFollowingToolMessage := false
			for j := i + 1; j < len(messages); j++ {
				nextMsg := messages[j]
				if nextMsg.Role == "tool" {
					hasFollowingToolMessage = true
					break
				}
				if nextMsg.Role == "assistant" {
					break
				}
			}
			if hasFollowingToolMessage {
				cleaned = append(cleaned, msg)
				continue
			}
			cleanedMsg := msg
			cleanedMsg.ToolCalls = nil
			cleaned = append(cleaned, cleanedMsg)
			logger.Warn("Removed incomplete tool calls from assistant message at index %d during message conversion", i)
			continue
		}
		cleaned = append(cleaned, msg)
	}
	return cleaned
}

// ConvertMessages translates internal messages into the OpenAI chat
// completion wire format. Invalid sequences are repaired via CleanupMessages
// before conversion to mirror the legacy Client behavior.
func (mc *MessageConverter) ConvertMessages(messages []Message) []openai.ChatCompletionMessageParamUnion {
	if err := mc.ValidateMessageSequence(messages); err != nil {
		logger.Warn("Invalid message sequence detected in convertMessages: %v", err)
		messages = mc.CleanupMessages(messages)
	}

	openaiMessages := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			if len(msg.Contents) > 0 {
				contentParts := []openai.ChatCompletionContentPartUnionParam{}
				for _, content := range msg.Contents {
					if content.Type == "text" {
						contentParts = append(contentParts, openai.TextContentPart(content.Text))
					} else if content.Type == "image_url" && content.ImageURL != nil {
						imageURLParam := openai.ChatCompletionContentPartImageImageURLParam{
							URL:    content.ImageURL.URL,
							Detail: content.ImageURL.Detail,
						}
						contentParts = append(contentParts, openai.ImageContentPart(imageURLParam))
					}
				}
				systemPrefix := "System: "
				contentParts = append([]openai.ChatCompletionContentPartUnionParam{
					openai.TextContentPart(systemPrefix),
				}, contentParts...)
				openaiMessages = append(openaiMessages, openai.UserMessage(contentParts))
			} else {
				openaiMessages = append(openaiMessages, openai.SystemMessage(msg.Content))
			}

		case "user":
			if len(msg.Contents) > 0 {
				contentParts := []openai.ChatCompletionContentPartUnionParam{}
				for _, content := range msg.Contents {
					if content.Type == "text" {
						contentParts = append(contentParts, openai.TextContentPart(content.Text))
					} else if content.Type == "image_url" && content.ImageURL != nil {
						imageURLParam := openai.ChatCompletionContentPartImageImageURLParam{
							URL:    content.ImageURL.URL,
							Detail: content.ImageURL.Detail,
						}
						contentParts = append(contentParts, openai.ImageContentPart(imageURLParam))
					}
				}
				openaiMessages = append(openaiMessages, openai.UserMessage(contentParts))
			} else {
				openaiMessages = append(openaiMessages, openai.UserMessage(msg.Content))
			}

		case "assistant":
			if len(msg.ToolCalls) > 0 {
				toolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, len(msg.ToolCalls))
				for i, tc := range msg.ToolCalls {
					argsJSON, _ := json.Marshal(tc.Arguments)
					toolCalls[i] = openai.ChatCompletionMessageToolCallUnionParam{
						OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
							ID: tc.ID,
							Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
								Name:      tc.Name,
								Arguments: string(argsJSON),
							},
						},
					}
				}
				assistantMsg := openai.ChatCompletionAssistantMessageParam{
					Content: openai.ChatCompletionAssistantMessageParamContentUnion{
						OfString: openai.String(msg.Content),
					},
					ToolCalls: toolCalls,
				}
				openaiMessages = append(openaiMessages, openai.ChatCompletionMessageParamUnion{
					OfAssistant: &assistantMsg,
				})
			} else {
				openaiMessages = append(openaiMessages, openai.AssistantMessage(msg.Content))
			}

		case "tool":
			openaiMessages = append(openaiMessages, openai.ToolMessage(msg.Content, msg.ToolCallID))
		}
	}

	return openaiMessages
}

// ConvertTools translates the converter's tool list into the OpenAI tool
// schema. Returns nil when no tools are configured, matching the previous
// Client behavior.
func (mc *MessageConverter) ConvertTools() []openai.ChatCompletionToolUnionParam {
	return NewToolSchemaConverter().ConvertTools(mc.tools)
}
