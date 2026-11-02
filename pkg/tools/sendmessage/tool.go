package sendmessage

import (
	"context"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/mailbox"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	"github.com/nano-harness/nano-agent/pkg/team"
)

// Tool implements the send_message tool for agent-to-agent communication
type Tool struct {
	mailboxBackend mailbox.Backend
}

// New creates a new send_message tool
func New(mb mailbox.Backend) *Tool {
	return &Tool{mailboxBackend: mb}
}

// Name returns the tool name
func (t *Tool) Name() string {
	return "send_message"
}

// Description returns the tool description
func (t *Tool) Description() string {
	return "Send a message to another agent in your team (team-lead or teammate)"
}

// Category returns the tool category
func (t *Tool) Category() interfaces.ToolCategory {
	return interfaces.CategoryAgent
}

// RequiresConfirmation returns false
func (t *Tool) RequiresConfirmation() bool {
	return false
}

// ConcurrencySafe returns true (sending messages is safe)
func (t *Tool) ConcurrencySafe() bool {
	return true
}

// Schema returns the JSON schema for tool parameters
func (t *Tool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"Send a message to another agent in your team",
		map[string]*interfaces.PropertySchema{
			"to": {
				Type:        "string",
				Description: "Recipient agent name (e.g., 'team-lead', 'researcher') or '*' for broadcast",
			},
			"text": {
				Type:        "string",
				Description: "Message content (required for most message types)",
			},
			"topic": {
				Type:        "string",
				Description: "Message topic (e.g., 'progress', 'finding', 'permission_request')",
			},
			"body": {
				Type:        "object",
				Description: "Structured message payload (optional, for advanced use)",
			},
		},
		[]string{"to", "text"},
	)
}

// Execute runs the tool with the provided parameters
func (t *Tool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// Parse parameters
	to, _ := params["to"].(string)
	text, _ := params["text"].(string)
	topic, _ := params["topic"].(string)
	body, _ := params["body"].(map[string]interface{})

	// Validate input
	if to == "" {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "'to' field is required",
			UserContent: "'to' field is required",
		}, nil
	}
	if text == "" {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "'text' field is required",
			UserContent: "'text' field is required",
		}, nil
	}

	// Get sender identity from context
	identity, ok := swarm.FromContext(ctx)
	var fromName string
	var teamName string
	if ok {
		// This is a teammate
		fromName = identity.AgentName
		teamName = identity.TeamName
	} else {
		// This is team-lead
		fromName = "team-lead"
		if leadCtx, ok := swarm.TeamLeadFromContext(ctx); ok {
			teamName = leadCtx.TeamName
		}
	}

	if teamName == "" {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "Cannot determine team name from context",
			UserContent: "Cannot determine team name from context",
		}, nil
	}

	// Validate recipient exists in team (unless broadcast)
	if to != "*" {
		tm, err := team.ReadTeam(teamName)
		if err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				LLMContent:  fmt.Sprintf("Failed to read team: %v", err),
				UserContent: fmt.Sprintf("Failed to read team: %v", err),
			}, nil
		}

		// Check if recipient is team-lead or a known member
		if to != "team-lead" {
			found := false
			for _, member := range tm.Members {
				if member.Name == to {
					found = true
					break
				}
			}
			if !found {
				return &interfaces.ToolResult{
					Success:     false,
					LLMContent:  fmt.Sprintf("Recipient '%s' not found in team '%s'", to, teamName),
					UserContent: fmt.Sprintf("Recipient '%s' not found in team '%s'", to, teamName),
				}, nil
			}
		}
	}

	// Prepare message body
	msgBody := body
	if msgBody == nil {
		msgBody = make(map[string]interface{})
	}
	msgBody["text"] = text

	// Default topic
	if topic == "" {
		topic = "message"
	}

	// Create mailbox message
	msg := mailbox.Message{
		From:  fromName,
		To:    to,
		Topic: topic,
		Body:  msgBody,
	}

	// Get recipient's mailbox and send
	recipientMailbox, err := t.mailboxBackend.Open(to + "@" + teamName)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Failed to open recipient mailbox: %v", err),
			UserContent: fmt.Sprintf("Failed to open recipient mailbox: %v", err),
		}, nil
	}
	defer func() { _ = recipientMailbox.Close() }()

	if err := recipientMailbox.Send(ctx, msg); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Failed to send message: %v", err),
			UserContent: fmt.Sprintf("Failed to send message: %v", err),
		}, nil
	}

	// Return success
	result := fmt.Sprintf("Message sent to '%s' in team '%s'", to, teamName)
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  result,
		UserContent: result,
	}, nil
}
