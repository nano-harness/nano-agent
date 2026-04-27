package teamlist

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/mailbox"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	"github.com/nano-harness/nano-agent/pkg/team"
)

// Tool implements the team_list tool
type Tool struct {
	mailboxBackend mailbox.Backend
}

// New creates a new team_list tool
func New(mb mailbox.Backend) *Tool {
	return &Tool{mailboxBackend: mb}
}

// Name returns the tool name
func (t *Tool) Name() string {
	return "team_list"
}

// Description returns the tool description
func (t *Tool) Description() string {
	return "List all members in the current team with their status and unread message counts"
}

// Category returns the tool category
func (t *Tool) Category() interfaces.ToolCategory {
	return interfaces.CategoryAgent
}

// RequiresConfirmation returns false
func (t *Tool) RequiresConfirmation() bool {
	return false
}

// ConcurrencySafe returns true (read-only)
func (t *Tool) ConcurrencySafe() bool {
	return true
}

// Schema returns the JSON schema for tool parameters
func (t *Tool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"List all members in the current team with their status",
		map[string]*interfaces.PropertySchema{},
		[]string{},
	)
}

// Execute runs the tool with the provided parameters
func (t *Tool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// Get team name from context
	var teamName string
	if identity, ok := swarm.FromContext(ctx); ok {
		teamName = identity.TeamName
	} else if leadCtx, ok := swarm.TeamLeadFromContext(ctx); ok {
		teamName = leadCtx.TeamName
	} else {
		teamName = "default"
	}

	if teamName == "" {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "Cannot determine team name from context",
			UserContent: "Cannot determine team name from context",
		}, nil
	}

	// Read team configuration
	tm, err := team.ReadTeam(teamName)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Failed to read team: %v", err),
			UserContent: fmt.Sprintf("Failed to read team: %v", err),
		}, nil
	}

	// Build markdown table
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Team: %s\n\n", tm.Name))
	if tm.Description != "" {
		sb.WriteString(fmt.Sprintf("**Description:** %s\n\n", tm.Description))
	}

	sb.WriteString("| Name | Status | Kind | Session ID | Unread |\n")
	sb.WriteString("|------|--------|------|------------|--------|\n")

	// Add team-lead row
	leadStatus := "active"
	leadUnread := 0
	if t.mailboxBackend != nil {
		if mb, err := t.mailboxBackend.Open("team-lead@" + teamName); err == nil {
			if count, err := mb.Count(ctx); err == nil {
				leadUnread = count
			}
			_ = mb.Close()
		}
	}
	sb.WriteString(fmt.Sprintf("| team-lead | %s | lead | %s | %d |\n", leadStatus, tm.LeadSessionID, leadUnread))

	// Add teammate rows
	for _, member := range tm.Members {
		status := "inactive"
		if member.IsActive {
			status = "active"
		}

		// Get unread count
		unreadCount := 0
		if t.mailboxBackend != nil {
			if mb, err := t.mailboxBackend.Open(member.AgentID); err == nil {
				if count, err := mb.Count(ctx); err == nil {
					unreadCount = count
				}
				_ = mb.Close()
			}
		}

		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %d |\n",
			member.Name, status, member.Kind, member.SessionID, unreadCount))
	}

	result := sb.String()
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  result,
		UserContent: result,
	}, nil
}
