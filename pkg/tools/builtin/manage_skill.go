// Package builtin provides conversational management tools for skills, MCP
// servers, and scheduled tasks.  All mutating actions require an external
// confirmation step before they are applied.
package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/skill"
)

// ManageSkillTool allows the agent to install, activate, deactivate, and list skills.
// The "install" action returns a preview that the caller (TUI) must confirm before
// the skill is actually written to disk.
type ManageSkillTool struct {
	skillManager *skill.Manager
	// confirmFn is called with a human-readable summary of the proposed change.
	// It should return true if the user confirmed, false otherwise.
	confirmFn func(summary string) bool
}

// NewManageSkillTool creates a ManageSkillTool.
// confirmFn may be nil, in which case all actions are automatically confirmed
// (useful in non-interactive contexts like tests).
func NewManageSkillTool(sm *skill.Manager, confirmFn func(string) bool) *ManageSkillTool {
	return &ManageSkillTool{skillManager: sm, confirmFn: confirmFn}
}

// Name returns the tool name.
func (t *ManageSkillTool) Name() string { return "manage_skill" }

// Description returns the tool description.
func (t *ManageSkillTool) Description() string {
	return "Manage skills: install from URL/local path, activate, deactivate, list, or get info. Install requires user confirmation."
}

// Category returns the tool category.
func (t *ManageSkillTool) Category() interfaces.ToolCategory { return interfaces.CategoryAgent }

// RequiresConfirmation returns false because confirmation is handled internally
// via the confirmFn callback rather than through the standard tool mechanism.
func (t *ManageSkillTool) RequiresConfirmation() bool { return false }

// ConcurrencySafe returns false.
func (t *ManageSkillTool) ConcurrencySafe() bool { return false }

// Schema returns the JSON schema for the tool parameters.
func (t *ManageSkillTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"Manage skills: install, activate, deactivate, list, or get info",
		map[string]*interfaces.PropertySchema{
			"action": {
				Type:        "string",
				Description: "Action to perform",
				Enum:        []string{"install", "activate", "deactivate", "list", "info"},
			},
			"source": {
				Type:        "string",
				Description: "Install source: URL (http/https), local SKILL.md path, local directory, or local archive (.zip/.tar.gz)",
			},
			"name": {
				Type:        "string",
				Description: "Skill name for activate, deactivate, or info actions",
			},
		},
		[]string{"action"},
	)
}

// Execute runs the skill management action.
func (t *ManageSkillTool) Execute(ctx context.Context, args map[string]interface{}) (*interfaces.ToolResult, error) {
	action, ok := args["action"].(string)
	if !ok || action == "" {
		return nil, fmt.Errorf("action is required")
	}

	if t.skillManager == nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "Skill manager is not available",
			UserContent: "Skill manager is not available",
		}, nil
	}

	switch action {
	case "list":
		listing := t.skillManager.ListSkillNames()
		return &interfaces.ToolResult{
			Success:     true,
			LLMContent:  listing,
			UserContent: listing,
		}, nil

	case "info":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name is required for info action")
		}
		sk := t.skillManager.GetByName(name)
		if sk == nil {
			return &interfaces.ToolResult{
				Success:     false,
				LLMContent:  fmt.Sprintf("Skill %q not found", name),
				UserContent: fmt.Sprintf("Skill %q not found", name),
			}, nil
		}
		info := fmt.Sprintf("Name: %s\nDescription: %s\nScope: %s\nActive: %t\nPriority: %d\nTriggers: %s\nGlobs: %s\n\nInstructions:\n%s",
			sk.Name, sk.Description, sk.Scope, t.skillManager.IsActive(sk.Name), sk.Priority,
			strings.Join(sk.Triggers, ", "), strings.Join(sk.Globs, ", "), sk.Instructions)
		return &interfaces.ToolResult{
			Success:     true,
			LLMContent:  info,
			UserContent: info,
		}, nil

	case "activate":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name is required for activate action")
		}
		if err := t.skillManager.ActivateSkill(name); err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				LLMContent:  fmt.Sprintf("Failed to activate skill %q: %v", name, err),
				UserContent: fmt.Sprintf("Failed to activate skill %q: %v", name, err),
			}, nil
		}
		msg := fmt.Sprintf("Skill %q activated successfully", name)
		return &interfaces.ToolResult{
			Success:     true,
			LLMContent:  msg,
			UserContent: msg,
		}, nil

	case "deactivate":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name is required for deactivate action")
		}
		if err := t.skillManager.DeactivateSkill(name); err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				LLMContent:  fmt.Sprintf("Failed to deactivate skill %q: %v", name, err),
				UserContent: fmt.Sprintf("Failed to deactivate skill %q: %v", name, err),
			}, nil
		}
		msg := fmt.Sprintf("Skill %q deactivated", name)
		return &interfaces.ToolResult{
			Success:     true,
			LLMContent:  msg,
			UserContent: msg,
		}, nil

	case "install":
		source, _ := args["source"].(string)
		if source == "" {
			return nil, fmt.Errorf("source is required for install action")
		}

		// Show confirmation before installing
		summary := fmt.Sprintf("Install skill from: %s", source)
		if t.confirmFn != nil && !t.confirmFn(summary) {
			return &interfaces.ToolResult{
				Success:     false,
				LLMContent:  "Skill installation cancelled by user",
				UserContent: "Skill installation cancelled by user",
			}, nil
		}

		installed, err := t.skillManager.InstallSkill(ctx, source)
		if err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				LLMContent:  fmt.Sprintf("Failed to install skill from %q: %v", source, err),
				UserContent: fmt.Sprintf("Failed to install skill from %q: %v", source, err),
			}, nil
		}

		msg := fmt.Sprintf("Skill %q installed successfully.\nDescription: %s\nUse `/skill:use %s` or activate with manage_skill to start using it.",
			installed.Name, installed.Description, installed.Name)
		return &interfaces.ToolResult{
			Success:     true,
			LLMContent:  msg,
			UserContent: msg,
			Data: map[string]interface{}{
				"name":        installed.Name,
				"description": installed.Description,
				"scope":       string(installed.Scope),
			},
		}, nil

	default:
		return nil, fmt.Errorf("unknown action %q; valid actions: install, activate, deactivate, list, info", action)
	}
}
