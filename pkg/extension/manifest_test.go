package extension

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agentprofile"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/skill"
)

type testTool struct {
	name     string
	category interfaces.ToolCategory
	confirm  bool
}

func (t testTool) Name() string                      { return t.name }
func (t testTool) Description() string               { return "test tool" }
func (t testTool) Schema() *interfaces.ToolSchema    { return interfaces.CreateSchema("test", nil, nil) }
func (t testTool) RequiresConfirmation() bool        { return t.confirm }
func (t testTool) Category() interfaces.ToolCategory { return t.category }
func (t testTool) ConcurrencySafe() bool             { return true }
func (t testTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	return &interfaces.ToolResult{Success: true}, nil
}

func TestRegistryListsSkillMCPToolAndAgentExtensions(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: review
description: Review code
triggers: ["review"]
---
Review instructions.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sm := skill.NewManager(root, filepath.Join(root, "skills"), "", 0, 0, 0, true)
	if err := sm.Discover(); err != nil {
		t.Fatal(err)
	}
	if err := sm.ActivateSkill("review"); err != nil {
		t.Fatal(err)
	}

	registry := NewRegistry(sm, &config.MCPConfig{
		Servers: []config.MCPServerConfig{{
			Name:      "fs",
			Command:   []string{"npx", "server"},
			Transport: "stdio",
			Enabled:   true,
		}},
	}, []interfaces.Tool{
		testTool{name: "read_file", category: interfaces.CategoryFileSystem},
		testTool{name: "primary_agent_controller", category: interfaces.CategoryAgent},
	})

	manifests := registry.List()
	if len(manifests) != 4 {
		t.Fatalf("List returned %d manifests, want 4", len(manifests))
	}
	skillManifest, ok := registry.Get(KindSkill, "review")
	if !ok || !skillManifest.Enabled || skillManifest.Health.Status != HealthHealthy {
		t.Fatalf("unexpected skill manifest: %+v ok=%v", skillManifest, ok)
	}
	mcpManifest, ok := registry.Get(KindMCP, "fs")
	if !ok || len(mcpManifest.Permissions) == 0 || mcpManifest.Permissions[0].Type != "process_spawn" {
		t.Fatalf("unexpected mcp manifest: %+v ok=%v", mcpManifest, ok)
	}
	if _, ok := registry.Get(KindAgent, "primary_agent_controller"); !ok {
		t.Fatal("expected agent extension for CategoryAgent tool")
	}
	if !skillManifest.Trust.Trusted || skillManifest.Trust.Level == "" {
		t.Fatalf("unexpected trust metadata: %+v", skillManifest.Trust)
	}
}

func TestRegistryListsCommandExtensions(t *testing.T) {
	registry := NewRegistryWithCommands(nil, nil, nil, []*skill.CommandDef{{
		Name:              "deploy",
		Description:       "Deploy safely",
		AllowedTools:      []string{"run_shell_command"},
		PermissionProfile: "acceptEdits",
		Source:            "project:nano",
	}}, agentprofile.AgentProfile{
		Name:           "reviewer",
		Description:    "Review code",
		PermissionMode: "acceptEdits",
		AllowedTools:   []string{"read_file"},
		Kind:           "in_process",
		Source:         filepath.Join(t.TempDir(), ".nano", "agents", "reviewer.yaml"),
	})

	manifest, ok := registry.Get(KindCommand, "deploy")
	if !ok {
		t.Fatal("expected command manifest")
	}
	if manifest.Kind != KindCommand || !manifest.Trust.Trusted {
		t.Fatalf("unexpected command manifest: %+v", manifest)
	}
	if len(manifest.Permissions) != 3 {
		t.Fatalf("expected prompt, tool, and permission profile permissions, got %+v", manifest.Permissions)
	}
	if got := manifest.Metadata["permission_profile"]; got != "acceptEdits" {
		t.Fatalf("permission profile metadata = %#v", got)
	}
	agentManifest, ok := registry.Get(KindAgent, "reviewer")
	if !ok {
		t.Fatal("expected agent profile manifest")
	}
	if agentManifest.Health.Status != HealthHealthy || !agentManifest.Trust.Trusted {
		t.Fatalf("unexpected agent profile manifest: %+v", agentManifest)
	}
	if len(agentManifest.Permissions) != 3 {
		t.Fatalf("expected spawn, permission, and tool permissions, got %+v", agentManifest.Permissions)
	}
}

func TestParseKind(t *testing.T) {
	if got, err := ParseKind("skill"); err != nil || got != KindSkill {
		t.Fatalf("ParseKind(skill)=%q err=%v", got, err)
	}
	if _, err := ParseKind("bad"); err == nil {
		t.Fatal("expected invalid kind error")
	}
	if got, err := ParseKind("command"); err != nil || got != KindCommand {
		t.Fatalf("ParseKind(command)=%q err=%v", got, err)
	}
}

func TestTrustForSourceMarksHTTPRemoteInsecure(t *testing.T) {
	trust := trustForSource("http://example.invalid/agent.yaml")
	if trust.Trusted || trust.Level != "remote_insecure" {
		t.Fatalf("unexpected trust for plain HTTP source: %+v", trust)
	}
}
