package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestManageMCPTool_List_Empty(t *testing.T) {
	cfg := &config.Config{}
	tool := NewManageMCPTool(cfg, "", nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.LLMContent)
	}
}

func TestManageMCPTool_List_WithServers(t *testing.T) {
	cfg := &config.Config{
		MCP: &config.MCPConfig{
			Servers: []config.MCPServerConfig{
				{Name: "fs", Description: "filesystem", Transport: "stdio", Enabled: true},
			},
		},
	}
	tool := NewManageMCPTool(cfg, "", nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.LLMContent)
	}
}

func TestManageMCPTool_AddServer(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".nano.yaml")

	// Write minimal initial config
	if err := os.WriteFile(configPath, []byte("model: deepseek-chat\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	tool := NewManageMCPTool(cfg, configPath, nil) // nil confirm = auto-confirm

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":      "add",
		"name":        "filesystem",
		"description": "Local filesystem access",
		"transport":   "stdio",
		"command":     []interface{}{"npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.LLMContent)
	}

	// Verify config was written
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty config file")
	}
}

func TestManageMCPTool_AddServer_Cancelled(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".nano.yaml")
	cfg := &config.Config{}
	tool := NewManageMCPTool(cfg, configPath, func(_ string) bool { return false })

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "add",
		"name":   "filesystem",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Error("expected failure when user cancels")
	}
}

func TestManageMCPTool_AddServer_NoConfigPath(t *testing.T) {
	cfg := &config.Config{}
	tool := NewManageMCPTool(cfg, "", nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "add",
		"name":   "filesystem",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Error("expected failure when configPath is empty")
	}
}

func TestManageMCPTool_UnknownAction(t *testing.T) {
	cfg := &config.Config{}
	tool := NewManageMCPTool(cfg, "", nil)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "unknown",
	})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}
