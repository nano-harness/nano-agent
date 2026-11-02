package acp

import (
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// EventBridge converts nano StreamEvents to ACP session/update events
type EventBridge struct {
	acpSessionID string
	transport    *Transport
}

// NewEventBridge creates a new event bridge
func NewEventBridge(acpSessionID string, transport *Transport) *EventBridge {
	return &EventBridge{
		acpSessionID: acpSessionID,
		transport:    transport,
	}
}

// OnStreamEvent handles a nano StreamEvent and converts it to ACP format
func (b *EventBridge) OnStreamEvent(se event.StreamEvent) {
	acpEvent := b.convertEvent(se)
	if acpEvent == nil {
		// Event type not supported or should be filtered
		return
	}

	// Send as session/update notification
	params := map[string]interface{}{
		"sessionId": b.acpSessionID,
		"update":    acpEvent,
	}

	if err := b.transport.SendNotification("session/update", params); err != nil {
		logger.Errorf("ACP: Failed to send session/update notification: %v", err)
	}
}

// convertEvent converts a nano StreamEvent to ACP SessionUpdateEvent
func (b *EventBridge) convertEvent(se event.StreamEvent) SessionUpdateEvent {
	switch se.Type {
	case event.EventTypeToolCall:
		// Convert tool call event
		if len(se.ToolCalls) > 0 && se.ToolCalls[0] != nil {
			toolCall := se.ToolCalls[0]
			return SessionUpdateEvent{
				"sessionUpdate": "tool_call",
				"toolCallId":    toolCall.ID,
				"title":         toolCall.Name,
				"kind":          inferToolKind(toolCall.Name),
				"status":        "pending",
			}
		}
		return nil

	case event.EventTypeToolResult:
		// Convert tool result event
		if se.ToolResult != nil {
			status := "completed"
			if se.ToolResult.Error != "" {
				status = "failed"
			}

			contentBlocks := []ContentBlock{}
			if se.ToolResult.Content != "" {
				contentBlocks = append(contentBlocks, ContentBlock{
					Type: "text",
					Text: se.ToolResult.Content,
				})
			}

			return SessionUpdateEvent{
				"sessionUpdate": "tool_call_update",
				"toolCallId":    se.ToolResult.ID,
				"status":        status,
				"content":       contentBlocks,
			}
		}
		return nil

	case event.EventTypeContent, event.EventTypeStreamContent:
		// Convert text content event
		if se.Content != "" {
			return SessionUpdateEvent{
				"sessionUpdate": "agent_message_chunk",
				"content": map[string]interface{}{
					"type": "text",
					"text": se.Content,
				},
			}
		}
		return nil

	case event.EventTypeThinking:
		// Convert thinking event as thought_message_chunk
		if se.Reasoning != "" {
			return SessionUpdateEvent{
				"sessionUpdate": "thought_message_chunk",
				"content": map[string]interface{}{
					"type": "text",
					"text": se.Reasoning,
				},
			}
		}
		return nil

	case event.EventTypeDone:
		// Done event is no longer sent via notification, handled in session/prompt response
		return nil

	case event.EventTypeError:
		// Convert error event
		return SessionUpdateEvent{
			"sessionUpdate": "agent_message_chunk",
			"content": map[string]interface{}{
				"type": "text",
				"text": se.Error,
			},
			"metadata": map[string]interface{}{
				"error": true,
			},
		}

	case event.EventTypeWaitingForUser:
		// This should trigger permission request, handled separately
		// Don't send as regular event
		return nil

	case event.EventTypeTokenStats:
		// Token stats - can be included in metadata or ignored
		if se.TokenStats != nil {
			return SessionUpdateEvent{
				"sessionUpdate": "agent_message_chunk",
				"metadata": map[string]interface{}{
					"token_stats": map[string]interface{}{
						"total_tokens":  se.TokenStats.TotalTokens,
						"input_tokens":  se.TokenStats.InputTokens,
						"output_tokens": se.TokenStats.OutputTokens,
					},
				},
			}
		}
		return nil

	case event.EventTypeSessionInfo:
		// Session info event
		return SessionUpdateEvent{
			"sessionUpdate": "agent_message_chunk",
			"metadata": map[string]interface{}{
				"session_id": se.SessionID,
				"title":      se.Title,
			},
		}

	case event.EventTypeTodoListUpdate:
		// Convert todo list to ACP plan format
		if se.TaskList != nil {
			entries := convertTodoListToPlanEntries(se.TaskList, se.Metadata)
			return SessionUpdateEvent{
				"sessionUpdate": "plan",
				"entries":       entries,
			}
		}
		return nil

	default:
		// Other event types - log and skip
		logger.Debugf("ACP: Skipping unsupported event type: %s", se.Type)
		return nil
	}
}

// convertTodoListToPlanEntries converts nano-agent TaskList to ACP PlanEntry format
func convertTodoListToPlanEntries(taskList interface{}, metadata map[string]interface{}) []map[string]interface{} {
	// Try to extract entries from TaskList interface{}
	// The structure depends on how nano-agent defines TaskList
	entries := []map[string]interface{}{}

	// If metadata contains todo items, use those
	if metadata != nil {
		if items, ok := metadata["items"].([]interface{}); ok {
			for _, item := range items {
				if itemMap, ok := item.(map[string]interface{}); ok {
					entry := map[string]interface{}{
						"content": itemMap["content"],
						"status":  convertTodoStatus(itemMap["status"]),
					}
					// Add priority if available
					if priority, ok := itemMap["priority"].(string); ok && priority != "" {
						entry["priority"] = priority
					}
					entries = append(entries, entry)
				}
			}
		}
	}

	// Fallback: try to convert taskList directly
	if len(entries) == 0 && taskList != nil {
		if items, ok := taskList.([]interface{}); ok {
			for _, item := range items {
				if itemMap, ok := item.(map[string]interface{}); ok {
					entry := map[string]interface{}{
						"content": itemMap["content"],
						"status":  convertTodoStatus(itemMap["status"]),
					}
					entries = append(entries, entry)
				}
			}
		}
	}

	return entries
}

// convertTodoStatus converts nano-agent todo status to ACP plan status
func convertTodoStatus(status interface{}) string {
	if statusStr, ok := status.(string); ok {
		switch statusStr {
		case "pending":
			return "pending"
		case "in_progress", "active":
			return "in_progress"
		case "completed", "done":
			return "completed"
		default:
			return "pending"
		}
	}
	return "pending"
}

// inferToolKind infers the tool kind from the tool name
func inferToolKind(toolName string) string {
	switch toolName {
	case "read_file", "view_file", "list_dir":
		return "read"
	case "edit_file", "write_file", "create_file":
		return "edit"
	case "delete_file", "remove_file":
		return "delete"
	case "move_file", "rename_file":
		return "move"
	case "search", "grep", "find":
		return "search"
	case "bash", "shell", "run_command":
		return "execute"
	case "think", "reasoning":
		return "think"
	case "fetch", "http_request", "api_call":
		return "fetch"
	default:
		return "other"
	}
}
