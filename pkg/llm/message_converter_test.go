package llm

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

type converterTestTool struct{}

func (converterTestTool) Name() string        { return "converter_test_tool" }
func (converterTestTool) Description() string { return "converter test tool" }
func (converterTestTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryDevelopment
}
func (converterTestTool) RequiresConfirmation() bool { return false }
func (converterTestTool) ConcurrencySafe() bool      { return true }
func (converterTestTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"converter test schema",
		map[string]*interfaces.PropertySchema{
			"name": {
				Type:        "string",
				Description: "name to process",
				Enum:        []string{"a", "b", ""},
			},
			"items": {
				Type:  "array",
				Items: &interfaces.PropertySchema{Type: "string"},
			},
		},
		[]string{"name"},
	)
}
func (converterTestTool) Execute(_ context.Context, _ map[string]interface{}) (*interfaces.ToolResult, error) {
	return nil, nil
}

func TestMessageConverterValidatesAndCleansMessages(t *testing.T) {
	converter := NewMessageConverter(nil)
	messages := []Message{
		{Role: "user", Content: "hello"},
		{
			Role:    "assistant",
			Content: "calling tool",
			ToolCalls: []tools.ToolCall{
				{ID: "call-1", Name: "tool", Arguments: map[string]interface{}{"x": "y"}},
			},
		},
	}

	if err := converter.ValidateMessageSequence(messages); err == nil {
		t.Fatal("expected invalid sequence error")
	}
	cleaned := converter.CleanupMessages(messages)
	if got := len(cleaned[1].ToolCalls); got != 0 {
		t.Fatalf("expected incomplete tool calls to be removed, got %d", got)
	}
	if got := len(converter.ConvertMessages(messages)); got != 2 {
		t.Fatalf("expected conversion to keep message count after cleanup, got %d", got)
	}
}

func TestMessageConverterConvertsToolSchemas(t *testing.T) {
	converter := NewMessageConverter([]interfaces.Tool{converterTestTool{}})
	converted := converter.ConvertTools()
	if len(converted) != 1 {
		t.Fatalf("expected one converted tool, got %d", len(converted))
	}
}
