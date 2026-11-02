package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

type extensionTestRegistry struct {
	tools []interfaces.Tool
}

func (r *extensionTestRegistry) Register(tool interfaces.Tool) error {
	r.tools = append(r.tools, tool)
	return nil
}
func (r *extensionTestRegistry) Unregister(name string) error { return nil }
func (r *extensionTestRegistry) Get(name string) (interfaces.Tool, bool) {
	for _, tool := range r.tools {
		if tool.Name() == name {
			return tool, true
		}
	}
	return nil, false
}
func (r *extensionTestRegistry) List() []interfaces.Tool { return r.tools }
func (r *extensionTestRegistry) ListByCategory(category interfaces.ToolCategory) []interfaces.Tool {
	var out []interfaces.Tool
	for _, tool := range r.tools {
		if tool.Category() == category {
			out = append(out, tool)
		}
	}
	return out
}
func (r *extensionTestRegistry) Schemas() map[string]*interfaces.ToolSchema {
	out := map[string]*interfaces.ToolSchema{}
	for _, tool := range r.tools {
		out[tool.Name()] = tool.Schema()
	}
	return out
}
func (r *extensionTestRegistry) Execute(ctx context.Context, name string, params map[string]interface{}) (*interfaces.ToolResult, error) {
	tool, ok := r.Get(name)
	if !ok {
		return &interfaces.ToolResult{Success: false, Error: "not found"}, nil
	}
	return tool.Execute(ctx, params)
}

func TestManageExtensionTool_ListAndManifest(t *testing.T) {
	mgr := newTestSkillManager(t)
	root := t.TempDir()
	cmdDir := filepath.Join(root, ".nano", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "deploy.md"), []byte(`---
description: Deploy safely
allowed-tools: [run_shell_command]
permission-profile: acceptEdits
---
Deploy $ARGUMENTS
`), 0o644); err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(root, ".nano", "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "reviewer.yaml"), []byte(`description: Review code
permission_mode: acceptEdits
allowed_tools: [read_file]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, ".nano.yaml")
	cfg := &config.Config{MCP: &config.MCPConfig{Servers: []config.MCPServerConfig{{
		Name:      "fs",
		Transport: "stdio",
		Command:   []string{"npx", "server"},
		Enabled:   true,
	}}}}
	testRegistry := &extensionTestRegistry{tools: []interfaces.Tool{testBuiltinTool{name: "agent_helper", category: interfaces.CategoryAgent}}}
	tool := NewManageExtensionTool(mgr, cfg, configPath, testRegistry, nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !strings.Contains(result.LLMContent, "skill/my-skill") || !strings.Contains(result.LLMContent, "mcp/fs") || !strings.Contains(result.LLMContent, "command/deploy") || !strings.Contains(result.LLMContent, "agent/reviewer") {
		t.Fatalf("unexpected list result: %+v", result)
	}

	result, err = tool.Execute(context.Background(), map[string]interface{}{"action": "manifest", "kind": "skill", "name": "my-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !strings.Contains(result.LLMContent, `"kind": "skill"`) {
		t.Fatalf("unexpected manifest result: %s", result.LLMContent)
	}

	result, err = tool.Execute(context.Background(), map[string]interface{}{"action": "manifest", "kind": "command", "name": "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !strings.Contains(result.LLMContent, `"kind": "command"`) || !strings.Contains(result.LLMContent, `"permission_profile": "acceptEdits"`) {
		t.Fatalf("unexpected command manifest result: %s", result.LLMContent)
	}

	result, err = tool.Execute(context.Background(), map[string]interface{}{"action": "manifest", "kind": "agent", "name": "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !strings.Contains(result.LLMContent, `"kind": "agent"`) || !strings.Contains(result.LLMContent, `"permission_mode": "acceptEdits"`) {
		t.Fatalf("unexpected agent manifest result: %s", result.LLMContent)
	}
}

func TestManageExtensionTool_EnableDisableSkill(t *testing.T) {
	mgr := newTestSkillManager(t)
	tool := NewManageExtensionTool(mgr, &config.Config{}, "", nil, nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{"action": "enable", "kind": "skill", "name": "my-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !mgr.IsActive("my-skill") {
		t.Fatalf("enable failed: %+v", result)
	}
	result, err = tool.Execute(context.Background(), map[string]interface{}{"action": "disable", "kind": "skill", "name": "my-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || mgr.IsActive("my-skill") {
		t.Fatalf("disable failed: %+v", result)
	}
}

func TestManageExtensionTool_InstallMCPAndUpdate(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".nano.yaml")
	if err := os.WriteFile(configPath, []byte("model: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	tool := NewManageExtensionTool(nil, cfg, configPath, nil, func(string) bool { return true })

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":      "install",
		"kind":        "mcp",
		"name":        "fs",
		"description": "filesystem",
		"transport":   "stdio",
		"command":     []interface{}{"npx", "server"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || cfg.MCP == nil || len(cfg.MCP.Servers) != 1 {
		t.Fatalf("install failed: %+v cfg=%+v", result, cfg.MCP)
	}

	result, err = tool.Execute(context.Background(), map[string]interface{}{
		"action":      "update",
		"kind":        "mcp",
		"name":        "fs",
		"description": "updated",
		"transport":   "streamable",
		"source":      "https://example.invalid/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || len(cfg.MCP.Servers) != 1 || cfg.MCP.Servers[0].Transport != "streamable" {
		t.Fatalf("update failed: %+v cfg=%+v", result, cfg.MCP.Servers)
	}

	result, err = tool.Execute(context.Background(), map[string]interface{}{
		"action": "remove",
		"kind":   "mcp",
		"name":   "fs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || len(cfg.MCP.Servers) != 0 {
		t.Fatalf("remove failed: %+v cfg=%+v", result, cfg.MCP.Servers)
	}
}

func TestManageExtensionTool_InstallSkillCancelled(t *testing.T) {
	mgr := newTestSkillManager(t)
	tool := NewManageExtensionTool(mgr, &config.Config{}, "", nil, func(string) bool { return false })
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "install",
		"kind":   "skill",
		"source": "https://example.com/SKILL.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Fatal("expected cancelled install to fail")
	}
}

func TestManageExtensionTool_RemoteInstallRequiresConfirmationHandler(t *testing.T) {
	cfg := &config.Config{}
	tool := NewManageExtensionTool(nil, cfg, filepath.Join(t.TempDir(), ".nano.yaml"), nil, nil)
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":    "install",
		"kind":      "mcp",
		"name":      "remote",
		"transport": "streamable",
		"source":    "https://example.invalid/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || !strings.Contains(result.LLMContent, "requires explicit user confirmation") {
		t.Fatalf("expected remote install without confirm handler to fail, got %+v", result)
	}
}

func TestManageExtensionTool_RemoveSkill(t *testing.T) {
	mgr := newTestSkillManager(t)
	tool := NewManageExtensionTool(mgr, &config.Config{}, "", nil, func(string) bool { return true })

	result, err := tool.Execute(context.Background(), map[string]interface{}{"action": "remove", "kind": "skill", "name": "my-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("remove skill failed: %+v", result)
	}
	if mgr.GetByName("my-skill") != nil {
		t.Fatal("expected skill to be removed from manager")
	}
}

func TestManageExtensionTool_DoctorTrustAndAudit(t *testing.T) {
	mgr := newTestSkillManager(t)
	cfg := &config.Config{MCP: &config.MCPConfig{Servers: []config.MCPServerConfig{{
		Name:      "remote",
		Transport: "streamable",
		URL:       "https://example.invalid/mcp",
		Enabled:   true,
	}}}}
	tool := NewManageExtensionTool(mgr, cfg, filepath.Join(t.TempDir(), ".nano.yaml"), nil, nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{"action": "doctor"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !strings.Contains(result.LLMContent, `"trust_level": "remote"`) {
		t.Fatalf("unexpected doctor result: %s", result.LLMContent)
	}

	result, err = tool.Execute(context.Background(), map[string]interface{}{"action": "trust", "kind": "mcp", "name": "remote"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !strings.Contains(result.LLMContent, `"trusted": false`) {
		t.Fatalf("unexpected trust result: %s", result.LLMContent)
	}

	result, err = tool.Execute(context.Background(), map[string]interface{}{"action": "audit", "kind": "mcp", "name": "remote"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !strings.Contains(result.LLMContent, `"permissions"`) || !strings.Contains(result.LLMContent, `"network"`) {
		t.Fatalf("unexpected audit result: %s", result.LLMContent)
	}
}

type testBuiltinTool struct {
	name     string
	category interfaces.ToolCategory
}

func (t testBuiltinTool) Name() string        { return t.name }
func (t testBuiltinTool) Description() string { return "test builtin tool" }
func (t testBuiltinTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema("test", nil, nil)
}
func (t testBuiltinTool) RequiresConfirmation() bool        { return false }
func (t testBuiltinTool) Category() interfaces.ToolCategory { return t.category }
func (t testBuiltinTool) ConcurrencySafe() bool             { return true }
func (t testBuiltinTool) Execute(context.Context, map[string]interface{}) (*interfaces.ToolResult, error) {
	return &interfaces.ToolResult{Success: true}, nil
}
