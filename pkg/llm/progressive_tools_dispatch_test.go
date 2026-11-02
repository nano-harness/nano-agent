package llm

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

type dispatchTestTool struct {
	name     string
	category interfaces.ToolCategory
}

func (t dispatchTestTool) Name() string                      { return t.name }
func (t dispatchTestTool) Description() string               { return t.name + " description" }
func (t dispatchTestTool) Category() interfaces.ToolCategory { return t.category }
func (t dispatchTestTool) RequiresConfirmation() bool        { return false }
func (t dispatchTestTool) ConcurrencySafe() bool             { return true }
func (t dispatchTestTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(t.name, map[string]*interfaces.PropertySchema{"value": interfaces.NewStringProperty("value")}, []string{"value"})
}
func (t dispatchTestTool) Execute(context.Context, map[string]interface{}) (*interfaces.ToolResult, error) {
	return nil, nil
}

type dispatchGate map[string]bool

func (g dispatchGate) ShouldExpose(name string) bool { return g[name] }

func toolNames(tools []interfaces.Tool) map[string]bool {
	out := make(map[string]bool, len(tools))
	for _, tool := range tools {
		out[tool.Name()] = true
	}
	return out
}

func TestSelectToolsForLLM_OnlyCoreCategoriesAndDiscover(t *testing.T) {
	selected := selectToolsForLLM([]interfaces.Tool{
		dispatchTestTool{name: "read_file", category: interfaces.CategoryFileSystem},
		dispatchTestTool{name: "run_shell_command", category: interfaces.CategoryShell},
		dispatchTestTool{name: "spawn_teammate", category: interfaces.CategoryAgent},
		dispatchTestTool{name: "web_fetch", category: interfaces.CategoryWeb},
		dispatchTestTool{name: "mcp_server_lookup", category: interfaces.CategoryMCP},
		dispatchTestTool{name: "discover_tools", category: interfaces.CategoryAgent},
	}, nil)
	names := toolNames(selected)
	for _, want := range []string{"read_file", "run_shell_command", "spawn_teammate", "discover_tools"} {
		if !names[want] {
			t.Fatalf("expected %s to be selected; got %#v", want, names)
		}
	}
	for _, filtered := range []string{"web_fetch", "mcp_server_lookup"} {
		if names[filtered] {
			t.Fatalf("expected %s to be filtered; got %#v", filtered, names)
		}
	}
}

func TestSelectToolsForLLM_ExpandedToolsAreIncluded(t *testing.T) {
	selected := selectToolsForLLM([]interfaces.Tool{
		dispatchTestTool{name: "web_fetch", category: interfaces.CategoryWeb},
	}, dispatchGate{"web_fetch": true})
	if len(selected) != 1 || selected[0].Name() != "web_fetch" {
		t.Fatalf("expanded tool not selected: %#v", selected)
	}
}

func TestSelectToolsForLLM_DiscoverToolsAlwaysIncluded(t *testing.T) {
	selected := selectToolsForLLM([]interfaces.Tool{
		dispatchTestTool{name: "discover_tools", category: interfaces.CategoryAgent},
		dispatchTestTool{name: "discover_skills", category: interfaces.CategoryAgent},
	}, nil)
	if len(selected) != 2 {
		t.Fatalf("discover tools should always be included, got %d", len(selected))
	}
}
