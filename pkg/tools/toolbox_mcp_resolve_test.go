package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// mockMCPTool implements a simple mock tool for testing
type mockMCPTool struct {
	name        string
	description string
	category    interfaces.ToolCategory
}

func (m *mockMCPTool) Name() string {
	return m.name
}

func (m *mockMCPTool) Description() string {
	return m.description
}

func (m *mockMCPTool) Category() interfaces.ToolCategory {
	return m.category
}

func (m *mockMCPTool) Schema() *interfaces.ToolSchema {
	return &interfaces.ToolSchema{
		Type:        "object",
		Description: m.description,
		Properties:  make(map[string]*interfaces.PropertySchema),
		Required:    []string{},
	}
}

func (m *mockMCPTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	return &interfaces.ToolResult{
		Success:     true,
		Data:        "mock execution successful",
		UserContent: "Mock tool executed successfully",
	}, nil
}

func (m *mockMCPTool) RequiresConfirmation() bool {
	return false
}

func (m *mockMCPTool) ConcurrencySafe() bool {
	return true
}

// TestToolboxExecuteMCPNameResolution verifies that Toolbox.Execute() preserves
// existing behavior when MCP tools are executed by their registered names.
func TestToolboxExecuteMCPNameResolution(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "toolbox_mcp_execute_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a toolbox
	toolbox := NewToolbox(tempDir, nil, nil)

	// Test 1: Executing a tool that doesn't exist should return an error
	t.Run("non-existent tool returns error", func(t *testing.T) {
		ctx := context.Background()
		_, err := toolbox.Execute(ctx, "nonexistent.tool", nil)
		if err == nil {
			t.Error("Expected error for non-existent tool, got nil")
		}
		if err != nil && err.Error() != "tool 'nonexistent.tool' not found" {
			t.Errorf("Expected 'tool 'nonexistent.tool' not found', got: %v", err)
		}
	})

	// Test 2: Executing a built-in tool should work normally
	t.Run("built-in tool executes normally", func(t *testing.T) {
		ctx := context.Background()
		testFile := filepath.Join(tempDir, "test.txt")

		// First create a file
		result, err := toolbox.Execute(ctx, "write_file", map[string]interface{}{
			"file_path": testFile,
			"content":   "test content",
		})
		if err != nil {
			t.Fatalf("Failed to execute write_file: %v", err)
		}
		if !result.Success {
			t.Fatalf("write_file failed: %s", result.Error)
		}
	})

	// Test 3: MCP tools registered with full name should execute
	t.Run("MCP tool with registered name executes", func(t *testing.T) {
		// Register an MCP tool with its full registered name
		mockTool := &mockMCPTool{
			name:        "mcp_symphony_symphony.status",
			description: "Get status",
			category:    interfaces.CategoryMCP,
		}

		toolbox.registry.Register(mockTool)

		// Execute using the full registered name
		ctx := context.Background()
		result, err := toolbox.Execute(ctx, "mcp_symphony_symphony.status", nil)
		if err != nil {
			t.Fatalf("Failed to execute MCP tool by registered name: %v", err)
		}
		if !result.Success {
			t.Fatalf("MCP tool execution failed: %s", result.Error)
		}
	})

	// Test 4: MCP tool name resolution when MCP client is nil should fall through to registry
	t.Run("MCP tool name without client returns not found", func(t *testing.T) {
		// Ensure mcpClient is nil (it should be by default)
		ctx := context.Background()
		_, err := toolbox.Execute(ctx, "symphony.fetch_issue", nil)

		// Should fail with tool not found
		if err == nil {
			t.Error("Expected error when MCP client is nil, got nil")
		}
		if err != nil && err.Error() != "tool 'symphony.fetch_issue' not found" {
			t.Errorf("Expected 'tool 'symphony.fetch_issue' not found', got: %v", err)
		}
	})
}

// TestToolboxExecuteWithRegistry verifies that Execute() correctly uses the registry
func TestToolboxExecuteWithRegistry(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "toolbox_execute_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	toolbox := NewToolbox(tempDir, nil, nil)

	// Register multiple mock tools
	tool1 := &mockMCPTool{
		name:        "test_tool_1",
		description: "Test tool 1",
		category:    interfaces.CategoryMCP,
	}
	tool2 := &mockMCPTool{
		name:        "test_tool_2",
		description: "Test tool 2",
		category:    interfaces.CategoryMCP,
	}

	toolbox.registry.Register(tool1)
	toolbox.registry.Register(tool2)

	ctx := context.Background()

	t.Run("execute registered tool 1", func(t *testing.T) {
		result, err := toolbox.Execute(ctx, "test_tool_1", nil)
		if err != nil {
			t.Fatalf("Failed to execute test_tool_1: %v", err)
		}
		if !result.Success {
			t.Fatalf("test_tool_1 execution failed: %s", result.Error)
		}
	})

	t.Run("execute registered tool 2", func(t *testing.T) {
		result, err := toolbox.Execute(ctx, "test_tool_2", nil)
		if err != nil {
			t.Fatalf("Failed to execute test_tool_2: %v", err)
		}
		if !result.Success {
			t.Fatalf("test_tool_2 execution failed: %s", result.Error)
		}
	})

	t.Run("execute non-existent tool", func(t *testing.T) {
		_, err := toolbox.Execute(ctx, "test_tool_3", nil)
		if err == nil {
			t.Error("Expected error for non-existent tool, got nil")
		}
	})
}
