package teamcreate

import (
	"context"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	"github.com/nano-harness/nano-agent/pkg/team"
)

// Tool implements the team_create tool
type Tool struct{}

// New creates a new team_create tool
func New() *Tool {
	return &Tool{}
}

// Name returns the tool name
func (t *Tool) Name() string {
	return "team_create"
}

// Description returns the tool description
func (t *Tool) Description() string {
	return "Create a new multi-agent team (team-lead only)"
}

// Category returns the tool category
func (t *Tool) Category() interfaces.ToolCategory {
	return interfaces.CategoryAgent
}

// RequiresConfirmation returns false
func (t *Tool) RequiresConfirmation() bool {
	return false
}

// ConcurrencySafe returns false (creates team files)
func (t *Tool) ConcurrencySafe() bool {
	return false
}

// Schema returns the JSON schema for tool parameters
func (t *Tool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"Create a new multi-agent team",
		map[string]*interfaces.PropertySchema{
			"name": {
				Type:        "string",
				Description: "Unique team name (alphanumeric, lowercase recommended)",
			},
			"description": {
				Type:        "string",
				Description: "Description of the team's purpose",
			},
		},
		[]string{"name"},
	)
}

// Execute runs the tool with the provided parameters
func (t *Tool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// Check if caller is a teammate (teammates cannot create teams)
	if swarm.IsTeammate(ctx) {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "Teammates cannot create teams (team-lead only)",
			UserContent: "Teammates cannot create teams (team-lead only)",
		}, nil
	}

	// Parse parameters
	name, _ := params["name"].(string)
	description, _ := params["description"].(string)

	// Validate input
	if name == "" {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "'name' field is required",
			UserContent: "'name' field is required",
		}, nil
	}

	leadAgentID := "team-lead@" + name
	leadSessionID := "default" // Placeholder
	if leadCtx, ok := swarm.TeamLeadFromContext(ctx); ok && leadCtx.SessionID != "" {
		leadSessionID = leadCtx.SessionID
	}

	// Create the team
	createdTeam, err := team.CreateTeam(name, description, leadAgentID, leadSessionID)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Failed to create team: %v", err),
			UserContent: fmt.Sprintf("Failed to create team: %v", err),
		}, nil
	}

	// Return success message
	result := fmt.Sprintf("Created team '%s' successfully\nTeam directory: ~/.nano/teams/%s/\nYou can now spawn teammates using spawn_teammate tool.",
		createdTeam.Name, createdTeam.Name)

	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  result,
		UserContent: result,
	}, nil
}
