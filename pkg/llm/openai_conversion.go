package llm

import (
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
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
	return NewToolSchemaConverter().ConvertTools(selectToolsForLLM(c.tools, c.toolGate))
}

var coreExposedCategories = map[interfaces.ToolCategory]bool{ //nolint:gochecknoglobals
	interfaces.CategoryFileSystem: true,
	interfaces.CategoryShell:      true,
	interfaces.CategoryAgent:      true,
}

var alwaysExposedToolNames = map[string]bool{ //nolint:gochecknoglobals
	"discover_tools":  true,
	"discover_skills": true,
}

func selectToolsForLLM(allTools []interfaces.Tool, gate interfaces.ToolGate) []interfaces.Tool {
	if len(allTools) == 0 {
		return nil
	}
	selected := make([]interfaces.Tool, 0, len(allTools))
	for _, tool := range allTools {
		if tool == nil {
			continue
		}
		name := tool.Name()
		if alwaysExposedToolNames[name] {
			selected = append(selected, tool)
			continue
		}
		if !strings.HasPrefix(name, "mcp_") && coreExposedCategories[tool.Category()] {
			selected = append(selected, tool)
			continue
		}
		if gate != nil && gate.ShouldExpose(name) {
			selected = append(selected, tool)
		}
	}
	return selected
}
