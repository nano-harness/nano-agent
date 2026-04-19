package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/skill"
)

const testSkillMD = `---
name: my-skill
description: "A test skill"
triggers:
  - "test"
auto_invoke: false
priority: 1
---

# My Skill

Instructions here.
`

func newTestSkillManager(t *testing.T) *skill.Manager {
	t.Helper()
	personalDir := t.TempDir()
	// Pre-install a skill
	skillDir := filepath.Join(personalDir, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(testSkillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := skill.NewManager(".", personalDir, "", skill.DefaultMaxSkillSize, 50, 5, true)
	if err := mgr.Discover(); err != nil {
		t.Fatal(err)
	}
	return mgr
}

func TestManageSkillTool_List(t *testing.T) {
	mgr := newTestSkillManager(t)
	tool := NewManageSkillTool(mgr, nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Errorf("expected success, got failure: %s", result.LLMContent)
	}
	if result.LLMContent == "" {
		t.Error("expected non-empty listing")
	}
}

func TestManageSkillTool_ActivateDeactivate(t *testing.T) {
	mgr := newTestSkillManager(t)
	tool := NewManageSkillTool(mgr, nil)

	// Activate
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "activate",
		"name":   "my-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Errorf("activate failed: %s", result.LLMContent)
	}
	if !mgr.IsActive("my-skill") {
		t.Error("skill should be active after activate")
	}

	// Deactivate
	result, err = tool.Execute(context.Background(), map[string]interface{}{
		"action": "deactivate",
		"name":   "my-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Errorf("deactivate failed: %s", result.LLMContent)
	}
	if mgr.IsActive("my-skill") {
		t.Error("skill should not be active after deactivate")
	}
}

func TestManageSkillTool_Info(t *testing.T) {
	mgr := newTestSkillManager(t)
	tool := NewManageSkillTool(mgr, nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "info",
		"name":   "my-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Errorf("info failed: %s", result.LLMContent)
	}
}

func TestManageSkillTool_InfoNotFound(t *testing.T) {
	mgr := newTestSkillManager(t)
	tool := NewManageSkillTool(mgr, nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "info",
		"name":   "nonexistent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Error("expected failure for nonexistent skill")
	}
}

func TestManageSkillTool_InstallCancelled(t *testing.T) {
	mgr := newTestSkillManager(t)
	// Confirm function that always denies
	tool := NewManageSkillTool(mgr, func(_ string) bool { return false })

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "install",
		"source": "https://example.com/SKILL.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Error("expected failure when user cancels install")
	}
}

func TestManageSkillTool_UnknownAction(t *testing.T) {
	mgr := newTestSkillManager(t)
	tool := NewManageSkillTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"action": "unknown",
	})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestManageSkillTool_MissingAction(t *testing.T) {
	mgr := newTestSkillManager(t)
	tool := NewManageSkillTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing action")
	}
}
