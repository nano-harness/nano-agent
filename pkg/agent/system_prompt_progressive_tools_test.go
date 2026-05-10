package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

type promptTestTool struct {
	name        string
	description string
	category    interfaces.ToolCategory
}

func (t promptTestTool) Name() string                      { return t.name }
func (t promptTestTool) Description() string               { return t.description }
func (t promptTestTool) Category() interfaces.ToolCategory { return t.category }
func (t promptTestTool) RequiresConfirmation() bool        { return false }
func (t promptTestTool) ConcurrencySafe() bool             { return true }
func (t promptTestTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema("schema for "+t.name, map[string]*interfaces.PropertySchema{
		"secret_param": interfaces.NewStringProperty("full schema marker"),
	}, []string{"secret_param"})
}
func (t promptTestTool) Execute(context.Context, map[string]interface{}) (*interfaces.ToolResult, error) {
	return nil, nil
}

func TestSystemPromptProgressiveTools_MCPToolDoesNotRenderFullSchema(t *testing.T) {
	spb := NewSystemPromptBuilder(t.TempDir(), []interfaces.Tool{
		promptTestTool{name: "mcp_demo_lookup", description: "lookup demo", category: interfaces.CategoryMCP},
	}, nil, &config.Config{IsSubAgent: true})
	section := spb.buildToolsSection()
	if !strings.Contains(section, "mcp_demo_lookup") && !strings.Contains(section, "lookup") {
		t.Fatalf("expected MCP tool directory entry, got %q", section)
	}
	if strings.Contains(section, "secret_param") || strings.Contains(section, "full schema marker") {
		t.Fatalf("MCP full schema should not be rendered: %q", section)
	}
}

func TestSystemPromptProgressiveTools_IncludesDiscoverToolsGuidance(t *testing.T) {
	spb := NewSystemPromptBuilder(t.TempDir(), nil, nil, &config.Config{IsSubAgent: true})
	section := spb.buildToolsSection()
	if !strings.Contains(section, "Before using non-core tools, you MUST call `discover_tools` first") {
		t.Fatalf("missing discover_tools guidance: %q", section)
	}
}
