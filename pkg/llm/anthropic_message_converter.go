package llm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

// cacheBoundaryMarker mirrors agent.CacheBoundaryMarker and is copied here to
// avoid an import cycle between pkg/llm and pkg/agent.
const cacheBoundaryMarker = "\n\n<!-- __SYSTEM_PROMPT_DYNAMIC_BOUNDARY__ -->\n\n"

// anthropicMessageConverter converts the agent's internal message representation
// to the Anthropic Messages API format.
type anthropicMessageConverter struct {
	tools []interfaces.Tool
}

func newAnthropicMessageConverter(tools []interfaces.Tool) *anthropicMessageConverter {
	return &anthropicMessageConverter{tools: tools}
}

// convertTools converts agent tools to Anthropic ToolUnionParam slice.
func (c *anthropicMessageConverter) convertTools() []anthropic.ToolUnionParam {
	if len(c.tools) == 0 {
		return nil
	}
	out := make([]anthropic.ToolUnionParam, 0, len(c.tools))
	for _, t := range c.tools {
		schema := t.Schema()
		if schema == nil {
			continue
		}
		inputSchema := anthropic.ToolInputSchemaParam{}
		if schema.Properties != nil {
			props := make(map[string]any, len(schema.Properties))
			for name, prop := range schema.Properties {
				props[name] = convertPropertySchema(prop)
			}
			inputSchema.Properties = props
		}
		if len(schema.Required) > 0 {
			inputSchema.Required = schema.Required
		}
		toolParam := anthropic.ToolParam{
			Name:        t.Name(),
			Description: anthropic.String(t.Description()),
			InputSchema: inputSchema,
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &toolParam})
	}
	return out
}

// convertToolsWithCache converts agent tools to Anthropic ToolUnionParam slice
// and applies cache_control to the last tool entry for prompt caching.
func (c *anthropicMessageConverter) convertToolsWithCache() []anthropic.ToolUnionParam {
	tools := c.convertTools()
	if len(tools) == 0 {
		return tools
	}
	last := &tools[len(tools)-1]
	if last.OfTool != nil {
		last.OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	return tools
}

// splitSystemByCacheBoundary splits the system prompt at CacheBoundaryMarker and
// applies cache_control to the cacheable prefix block.
func splitSystemByCacheBoundary(systemPrompt string) []anthropic.TextBlockParam {
	parts := strings.SplitN(systemPrompt, cacheBoundaryMarker, 2)
	blocks := []anthropic.TextBlockParam{}
	if parts[0] != "" {
		block := anthropic.TextBlockParam{
			Text:         parts[0],
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}
		blocks = append(blocks, block)
	}
	if len(parts) > 1 && parts[1] != "" {
		blocks = append(blocks, anthropic.TextBlockParam{Text: parts[1]})
	}
	if len(blocks) == 0 && systemPrompt != "" {
		blocks = append(blocks, anthropic.TextBlockParam{Text: systemPrompt})
	}
	return blocks
}

// convertMessages converts agent messages to Anthropic MessageParam slice and
// returns the system prompt separately (Anthropic places system as a top-level field).
func (c *anthropicMessageConverter) convertMessages(messages []Message) ([]anthropic.MessageParam, string) {
	var systemPrompt string
	var params []anthropic.MessageParam

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			// Collect system messages — only keep the first one (or concatenate)
			if systemPrompt == "" {
				systemPrompt = msg.Content
			} else {
				systemPrompt += "\n\n" + msg.Content
			}
		case "user":
			params = append(params, c.convertUserMessage(msg))
		case "assistant":
			params = append(params, c.convertAssistantMessage(msg))
		case "tool":
			// Tool results must be folded into a user message as tool_result blocks
			params = c.appendToolResultMessage(params, msg)
		}
	}

	// Anthropic requires messages to alternate user/assistant. Consecutive messages
	// of the same role need to be merged. We merge here conservatively.
	params = mergeConsecutiveRoles(params)

	return params, systemPrompt
}

// convertUserMessage converts a user message (including multimodal) to Anthropic format.
func (c *anthropicMessageConverter) convertUserMessage(msg Message) anthropic.MessageParam {
	if len(msg.Contents) > 0 {
		// Multimodal message
		var blocks []anthropic.ContentBlockParamUnion
		for _, mc := range msg.Contents {
			switch mc.Type {
			case "image_url":
				if mc.ImageURL != nil {
					if block, ok := convertImageURL(mc.ImageURL.URL); ok {
						blocks = append(blocks, block)
					}
				}
			default:
				if mc.Text != "" {
					blocks = append(blocks, anthropic.NewTextBlock(mc.Text))
				}
			}
		}
		if msg.Content != "" {
			blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
		}
		return anthropic.NewUserMessage(blocks...)
	}
	if msg.Content != "" {
		return anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content))
	}
	return anthropic.NewUserMessage(anthropic.NewTextBlock(""))
}

// convertAssistantMessage converts an assistant message to Anthropic format.
func (c *anthropicMessageConverter) convertAssistantMessage(msg Message) anthropic.MessageParam {
	var blocks []anthropic.ContentBlockParamUnion

	// Add reasoning as thinking block if present
	if msg.Reasoning != "" {
		blocks = append(blocks, anthropic.ContentBlockParamUnion{
			OfThinking: &anthropic.ThinkingBlockParam{
				Thinking: msg.Reasoning,
			},
		})
	}

	// Add text content
	if msg.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
	}

	// Add tool use blocks
	for _, tc := range msg.ToolCalls {
		inputJSON, _ := json.Marshal(tc.Arguments)
		toolUse := &anthropic.ToolUseBlockParam{
			ID:    tc.ID,
			Name:  tc.Name,
			Input: inputJSON,
		}
		blocks = append(blocks, anthropic.ContentBlockParamUnion{OfToolUse: toolUse})
	}

	if len(blocks) == 0 {
		blocks = append(blocks, anthropic.NewTextBlock(""))
	}

	return anthropic.NewAssistantMessage(blocks...)
}

// appendToolResultMessage appends a tool result as a tool_result block inside
// a user message. If the previous message is already a user message that contains
// only tool_result blocks, the result is added there; otherwise a new user
// message is created.
func (c *anthropicMessageConverter) appendToolResultMessage(params []anthropic.MessageParam, msg Message) []anthropic.MessageParam {
	// Build the tool_result content block
	var resultContent []anthropic.ToolResultBlockParamContentUnion
	if msg.Content != "" {
		resultContent = append(resultContent, anthropic.ToolResultBlockParamContentUnion{
			OfText: &anthropic.TextBlockParam{Text: msg.Content},
		})
	}

	// Also handle ToolResults field (may carry multiple results)
	for _, tr := range msg.ToolResults {
		content := tr.Content
		if tr.Error != "" {
			content = fmt.Sprintf("Error: %s", tr.Error)
		}
		resultContent = append(resultContent, anthropic.ToolResultBlockParamContentUnion{
			OfText: &anthropic.TextBlockParam{Text: content},
		})
	}

	toolResultBlock := &anthropic.ToolResultBlockParam{
		ToolUseID: msg.ToolCallID,
		Content:   resultContent,
	}
	block := anthropic.ContentBlockParamUnion{OfToolResult: toolResultBlock}

	// Append to existing trailing user message if it only has tool_result blocks
	if len(params) > 0 {
		last := params[len(params)-1]
		if last.Role == anthropic.MessageParamRoleUser && allToolResults(last) {
			last.Content = append(last.Content, block)
			params[len(params)-1] = last
			return params
		}
	}

	// Otherwise create a new user message
	return append(params, anthropic.NewUserMessage(block))
}

// allToolResults returns true if all content blocks in msg are tool_result blocks.
func allToolResults(msg anthropic.MessageParam) bool {
	for _, b := range msg.Content {
		if b.OfToolResult == nil {
			return false
		}
	}
	return true
}

// convertImageURL converts an image URL to an Anthropic image content block.
// Returns (block, true) on success or (zero, false) when the URL is unsupported.
func convertImageURL(url string) (anthropic.ContentBlockParamUnion, bool) {
	if strings.HasPrefix(url, "data:") {
		// Data URI: data:<mediatype>;base64,<data>
		withoutData := strings.TrimPrefix(url, "data:")
		parts := strings.SplitN(withoutData, ",", 2)
		if len(parts) != 2 {
			return anthropic.ContentBlockParamUnion{}, false
		}
		metaPart := parts[0]
		dataPart := parts[1]
		isBase64 := strings.HasSuffix(metaPart, ";base64")
		mediaType := strings.TrimSuffix(metaPart, ";base64")
		if !isBase64 {
			// Encode plain data as base64
			dataPart = base64.StdEncoding.EncodeToString([]byte(dataPart))
		}
		block := anthropic.NewImageBlockBase64(mediaType, dataPart)
		return block, true
	}
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		block := anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: url})
		return block, true
	}
	return anthropic.ContentBlockParamUnion{}, false
}

// mergeConsecutiveRoles merges consecutive messages of the same role into one.
// This is needed because Anthropic requires strict alternating user/assistant turns.
func mergeConsecutiveRoles(params []anthropic.MessageParam) []anthropic.MessageParam {
	if len(params) == 0 {
		return params
	}
	out := []anthropic.MessageParam{params[0]}
	for i := 1; i < len(params); i++ {
		last := &out[len(out)-1]
		cur := params[i]
		if last.Role == cur.Role {
			// Merge content blocks
			last.Content = append(last.Content, cur.Content...)
		} else {
			out = append(out, cur)
		}
	}
	return out
}

// applyConversationCache applies cache_control to the last user message (before
// the current turn) so the conversation history prefix is cached on repeated
// calls.
func applyConversationCache(params []anthropic.MessageParam) []anthropic.MessageParam {
	if len(params) < 2 {
		return params
	}
	// Mark the last complete turn (the message before the final user message)
	targetIdx := len(params) - 2
	target := &params[targetIdx]
	if len(target.Content) > 0 {
		applyEphemeralToLastBlock(target)
	}
	return params
}

// applyEphemeralToLastBlock applies CacheControlEphemeral to the last content block of a message.
func applyEphemeralToLastBlock(msg *anthropic.MessageParam) {
	if len(msg.Content) == 0 {
		return
	}
	last := &msg.Content[len(msg.Content)-1]
	cacheControl := anthropic.NewCacheControlEphemeralParam()
	switch {
	case last.OfText != nil:
		last.OfText.CacheControl = cacheControl
	case last.OfToolResult != nil:
		last.OfToolResult.CacheControl = cacheControl
	case last.OfToolUse != nil:
		last.OfToolUse.CacheControl = cacheControl
	case last.OfImage != nil:
		last.OfImage.CacheControl = cacheControl
	}
}

// convertToolResults converts a slice of ToolResult to synthetic tool role messages.
// This can be used to bridge ToolResult slices into the agent's Message format.
func toolResultsAsMessages(results []tools.ToolResult) []Message {
	msgs := make([]Message, 0, len(results))
	for _, r := range results {
		content := r.Content
		if r.Error != "" {
			content = fmt.Sprintf("Error: %s", r.Error)
		}
		msgs = append(msgs, Message{
			Role:       "tool",
			Content:    content,
			ToolCallID: r.ID,
		})
	}
	return msgs
}
