package llm

import (
	"github.com/openai/openai-go/v3"
)

// This file retains thin shims around MessageConverter so that existing
// callers (including tests) continue to compile and behave identically.
// New code should depend on MessageConverter directly.

func (c *Client) messageConverter() *MessageConverter {
	return NewMessageConverter(c.tools)
}

func (c *Client) validateMessageSequence(messages []Message) error {
	return c.messageConverter().ValidateMessageSequence(messages)
}

func (c *Client) cleanupMessages(messages []Message) []Message {
	return c.messageConverter().CleanupMessages(messages)
}

func (c *Client) convertMessages(messages []Message) []openai.ChatCompletionMessageParamUnion {
	return c.messageConverter().ConvertMessages(messages)
}

func (c *Client) convertTools() []openai.ChatCompletionToolUnionParam {
	return c.messageConverter().ConvertTools()
}
