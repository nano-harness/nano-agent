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
		"event":     acpEvent,
	}

	if err := b.transport.SendNotification("session/update", params); err != nil {
		logger.Errorf("ACP: Failed to send session/update notification: %v", err)
	}
}

// convertEvent converts a nano StreamEvent to ACP SessionUpdateEvent
func (b *EventBridge) convertEvent(se event.StreamEvent) *SessionUpdateEvent {
	switch se.Type {
	case event.EventTypeToolCall:
		// Convert tool call event
		if len(se.ToolCalls) > 0 && se.ToolCalls[0] != nil {
			return &SessionUpdateEvent{
				Type: "tool_call",
				Tool: se.ToolCalls[0].Name,
				Args: se.ToolCalls[0].Arguments,
			}
		}
		return nil

	case event.EventTypeToolResult:
		// Convert tool result event
		if se.ToolResult != nil {
			ev := &SessionUpdateEvent{
				Type: "tool_result",
				Tool: se.ToolResult.ID, // Use ID as tool identifier
			}
			if se.ToolResult.Error == "" {
				ev.Result = se.ToolResult.Content
			} else {
				ev.Error = se.ToolResult.Error
			}
			return ev
		}
		return nil

	case event.EventTypeContent, event.EventTypeStreamContent:
		// Convert text content event
		if se.Content != "" {
			return &SessionUpdateEvent{
				Type:    "text",
				Content: se.Content,
			}
		}
		return nil

	case event.EventTypeThinking:
		// Convert thinking event
		if se.Reasoning != "" {
			return &SessionUpdateEvent{
				Type:    "thinking",
				Content: se.Reasoning,
			}
		}
		return nil

	case event.EventTypeDone:
		// Convert done event
		return &SessionUpdateEvent{
			Type: "done",
		}

	case event.EventTypeError:
		// Convert error event
		return &SessionUpdateEvent{
			Type:  "error",
			Error: se.Error,
		}

	case event.EventTypeWaitingForUser:
		// This should trigger permission request, handled separately
		// Don't send as regular event
		return nil

	case event.EventTypeTokenStats:
		// Token stats - can be included in metadata or ignored
		if se.TokenStats != nil {
			return &SessionUpdateEvent{
				Type: "metadata",
				Metadata: map[string]interface{}{
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
		return &SessionUpdateEvent{
			Type: "session_info",
			Metadata: map[string]interface{}{
				"session_id": se.SessionID,
				"title":      se.Title,
			},
		}

	default:
		// Other event types - log and skip
		logger.Debugf("ACP: Skipping unsupported event type: %s", se.Type)
		return nil
	}
}
