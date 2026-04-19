package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/skill"
)

// DiscoverSkillsTool provides on-demand lookup of skill details.
// This implements Layer 2 of the Progressive Disclosure pattern for skills.
type DiscoverSkillsTool struct {
	skillManager *skill.Manager
}

// NewDiscoverSkillsTool creates a DiscoverSkillsTool.
func NewDiscoverSkillsTool(sm *skill.Manager) *DiscoverSkillsTool {
	return &DiscoverSkillsTool{skillManager: sm}
}

// Name returns the tool name.
func (t *DiscoverSkillsTool) Name() string { return "discover_skills" }

// Description returns the tool description.
func (t *DiscoverSkillsTool) Description() string {
	return "Search skills or get full instructions for a specific skill. Use this before activating a skill to understand what it does."
}

// Category returns the tool category.
func (t *DiscoverSkillsTool) Category() interfaces.ToolCategory { return interfaces.CategoryAgent }

// RequiresConfirmation returns false.
func (t *DiscoverSkillsTool) RequiresConfirmation() bool { return false }

// ConcurrencySafe returns true.
func (t *DiscoverSkillsTool) ConcurrencySafe() bool { return true }

// Schema returns the JSON schema.
func (t *DiscoverSkillsTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"Search skills or get full instructions for a specific skill",
		map[string]*interfaces.PropertySchema{
			"query": {
				Type:        "string",
				Description: "Search keyword (skill name, description keyword). Leave empty to list all.",
			},
			"name": {
				Type:        "string",
				Description: "Exact skill name to get full instructions for.",
			},
		},
		[]string{},
	)
}

// Execute runs the discover action.
func (t *DiscoverSkillsTool) Execute(_ context.Context, args map[string]interface{}) (*interfaces.ToolResult, error) {
	if t.skillManager == nil {
		msg := "Skill manager not available"
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}

	// Get full instructions for a specific skill
	if name, ok := args["name"].(string); ok && name != "" {
		return t.getSkillDetail(name)
	}

	query, _ := args["query"].(string)
	return t.searchSkills(query)
}

func (t *DiscoverSkillsTool) searchSkills(query string) (*interfaces.ToolResult, error) {
	metadata := t.skillManager.ListMetadata()
	if len(metadata) == 0 {
		msg := "No skills available"
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	}

	q := strings.ToLower(query)
	var sb strings.Builder
	if query != "" {
		fmt.Fprintf(&sb, "Skills matching %q:\n\n", query)
	} else {
		sb.WriteString("All available skills:\n\n")
	}
	sb.WriteString("| Skill | Description | Scope | Active |\n")
	sb.WriteString("|-------|-------------|-------|--------|\n")

	found := 0
	for _, m := range metadata {
		if q != "" &&
			!strings.Contains(strings.ToLower(m.Name), q) &&
			!strings.Contains(strings.ToLower(m.Description), q) {
			continue
		}
		active := "no"
		if t.skillManager.IsActive(m.Name) {
			active = "yes"
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", m.Name, m.Description, m.Scope, active)
		found++
	}

	if found == 0 {
		msg := fmt.Sprintf("No skills found matching %q", query)
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	}

	sb.WriteString("\nUse `discover_skills` with `name=<skill>` to get full instructions.")
	content := sb.String()
	return &interfaces.ToolResult{Success: true, LLMContent: content, UserContent: content}, nil
}

func (t *DiscoverSkillsTool) getSkillDetail(name string) (*interfaces.ToolResult, error) {
	sk := t.skillManager.GetByName(name)
	if sk == nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Skill %q not found", name),
			UserContent: fmt.Sprintf("Skill %q not found", name),
		}, nil
	}

	active := "inactive"
	if t.skillManager.IsActive(name) {
		active = "active"
	}

	content := fmt.Sprintf(
		"Skill: %s\nDescription: %s\nScope: %s\nStatus: %s\nTriggers: %s\nGlobs: %s\n\n## Instructions\n\n%s",
		sk.Name,
		sk.Description,
		sk.Scope,
		active,
		strings.Join(sk.Triggers, ", "),
		strings.Join(sk.Globs, ", "),
		sk.Instructions,
	)
	return &interfaces.ToolResult{Success: true, LLMContent: content, UserContent: content}, nil
}
