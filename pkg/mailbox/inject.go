package mailbox

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FormatMessagesAsAttachment formats messages as a markdown attachment for system prompt injection.
// This is called when an agent checks its mailbox and wants to present messages to the LLM.
//
// Example output:
//
//	# 📬 Mailbox Messages (2 new)
//
//	## Message from researcher@team-alpha
//	**Topic:** progress
//	**Sent:** 2 minutes ago
//
//	I've analyzed the authentication code and found 3 potential SQL injection vulnerabilities...
//
//	---
//
//	## Message from coder@team-alpha
//	**Topic:** finding
//	**Sent:** 5 minutes ago
//
//	[Type: security] The login endpoint at /api/auth/login is missing CSRF protection...
func FormatMessagesAsAttachment(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# 📬 Mailbox Messages (%d new)\n\n", len(messages)))

	for i, msg := range messages {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		sb.WriteString(formatSingleMessage(msg))
	}

	return sb.String()
}

// formatSingleMessage formats a single message as markdown
func formatSingleMessage(msg Message) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Message from %s\n", msg.From))
	sb.WriteString(fmt.Sprintf("**Topic:** %s\n", msg.Topic))
	sb.WriteString(fmt.Sprintf("**Sent:** %s\n", formatTimestamp(msg.Timestamp)))
	if msg.ReplyToID != "" {
		sb.WriteString(fmt.Sprintf("**Reply to:** %s\n", msg.ReplyToID))
	}
	sb.WriteString("\n")

	// Format body based on topic
	content := formatMessageBody(msg)
	sb.WriteString(content)

	return sb.String()
}

// formatMessageBody formats the message body based on its topic
func formatMessageBody(msg Message) string {
	switch msg.Topic {
	case TopicProgress:
		content, err := GetProgressContent(msg)
		if err != nil {
			return fmt.Sprintf("[Error: %v]", err)
		}
		return content

	case TopicFinding:
		findingType, content, err := GetFinding(msg)
		if err != nil {
			return fmt.Sprintf("[Error: %v]", err)
		}
		return fmt.Sprintf("[Type: %s] %s", findingType, content)

	case TopicAmendTask:
		taskID, instruction, err := GetAmendTask(msg)
		if err != nil {
			return fmt.Sprintf("[Error: %v]", err)
		}
		return fmt.Sprintf("**Task ID:** %s\n\n%s", taskID, instruction)

	case TopicPermissionRequest:
		tool, args, err := GetPermissionRequest(msg)
		if err != nil {
			return fmt.Sprintf("[Error: %v]", err)
		}
		return fmt.Sprintf("**Tool:** %s\n**Args:** %v", tool, args)

	case TopicPermissionGrant:
		requestID, _, err := GetPermissionResponse(msg)
		if err != nil {
			return fmt.Sprintf("[Error: %v]", err)
		}
		return fmt.Sprintf("Permission granted for request %s", requestID)

	case TopicPermissionDeny:
		requestID, reason, err := GetPermissionResponse(msg)
		if err != nil {
			return fmt.Sprintf("[Error: %v]", err)
		}
		return fmt.Sprintf("Permission denied for request %s\n**Reason:** %s", requestID, reason)

	default:
		// Generic formatting for unknown topics
		if content, ok := msg.Body["content"].(string); ok {
			return content
		}
		return fmt.Sprintf("%v", msg.Body)
	}
}

// formatTimestamp formats a Unix millisecond timestamp as a human-readable relative time
func formatTimestamp(unixMillis int64) string {
	t := time.UnixMilli(unixMillis)
	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return "just now"
	} else if diff < time.Hour {
		minutes := int(diff.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// DrainAndFormat is a convenience function that drains a mailbox and formats messages.
// This is the recommended way for agents to check their mailbox and inject messages.
func DrainAndFormat(ctx context.Context, mb Mailbox) (string, error) {
	if mb == nil {
		return "", fmt.Errorf("mailbox is nil")
	}

	messages, err := mb.DrainAll(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to drain mailbox: %w", err)
	}

	if len(messages) == 0 {
		return "", nil
	}

	return FormatMessagesAsAttachment(messages), nil
}
