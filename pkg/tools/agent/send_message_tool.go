package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/mailbox"
	"github.com/oklog/ulid/v2"
)

// SendMessageTool allows sub-agents to send structured messages to their parent agent
type SendMessageTool struct {
	agent          AgentRef
	callCount      int
	callCountMu    sync.Mutex
	maxCallsPerRun int
	eventHandler   func(event.StreamEvent) // event handler for observability
}

// AgentRef is an interface to access agent's mailbox capabilities without importing cycle
type AgentRef interface {
	ID() string
	ParentMailbox() mailbox.Mailbox
	IsSubAgent() bool
	GetEventHandler() func(event.StreamEvent) // access to agent's event handler
	EmitEvent(ev event.StreamEvent)           // emit event via agent's active turn
}

// NewSendMessageTool creates a new send message tool
func NewSendMessageTool(agent AgentRef) *SendMessageTool {
	return &SendMessageTool{
		agent:          agent,
		maxCallsPerRun: 20,
		eventHandler:   agent.GetEventHandler(),
	}
}

func (t *SendMessageTool) Name() string {
	return "send_message"
}

func (t *SendMessageTool) Description() string {
	return `Send a structured message to the parent agent. Use this to report intermediate findings, progress updates, or request clarification without waiting for task completion.

Guidelines:
- Use topic "progress" for status updates
- Use topic "finding" for discoveries/insights
- Use topic "amend_task" to request task clarification (parent will see on next turn)
- Keep messages concise and structured
- Rate limit: 20 messages per agent run`
}

func (t *SendMessageTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryBuild // Agent communication fits Build category
}

func (t *SendMessageTool) Schema() *interfaces.ToolSchema {
	toProp := interfaces.NewStringProperty(`Recipient of the message. Currently only "parent" is supported.`)
	topicProp := interfaces.NewStringPropertyWithEnum(`Message topic. Valid values: "progress", "finding", "amend_task"`, []string{"progress", "finding", "amend_task"})
	bodyProp := interfaces.NewStringProperty("Structured message payload (key-value pairs)")
	bodyProp.Type = "object"
	priorityProp := interfaces.NewStringProperty("Message priority (0=normal, 1=high). Default: 0")
	priorityProp.Type = "integer"

	return interfaces.CreateSchema(
		t.Description(),
		map[string]*interfaces.PropertySchema{
			"to":       toProp,
			"topic":    topicProp,
			"body":     bodyProp,
			"priority": priorityProp,
		},
		[]string{"to", "topic", "body"},
	)
}

func (t *SendMessageTool) Parameters() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"to": map[string]interface{}{
				"type":        "string",
				"description": `Recipient of the message. Currently only "parent" is supported.`,
			},
			"topic": map[string]interface{}{
				"type":        "string",
				"description": `Message topic. Valid values: "progress", "finding", "amend_task"`,
				"enum":        []string{"progress", "finding", "amend_task"},
			},
			"body": map[string]interface{}{
				"type":        "object",
				"description": "Structured message payload (key-value pairs)",
			},
			"priority": map[string]interface{}{
				"type":        "integer",
				"description": "Message priority (0=normal, 1=high). Default: 0",
			},
		},
		"required": []string{"to", "topic", "body"},
	}
	data, _ := json.Marshal(schema)
	return data
}

func (t *SendMessageTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// Rate limiting
	t.callCountMu.Lock()
	if t.callCount >= t.maxCallsPerRun {
		t.callCountMu.Unlock()
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("send_message rate limit reached (%d calls per run)", t.maxCallsPerRun),
			LLMContent:  fmt.Sprintf("Rate limit exceeded. You have sent %d messages in this run.", t.maxCallsPerRun),
			UserContent: "",
		}, nil
	}
	t.callCount++
	t.callCountMu.Unlock()

	// Parse parameters
	to, ok := params["to"].(string)
	if !ok || to == "" {
		return errResult("missing or invalid 'to' parameter"), nil
	}

	topic, ok := params["topic"].(string)
	if !ok || topic == "" {
		return errResult("missing or invalid 'topic' parameter"), nil
	}

	body, ok := params["body"].(map[string]interface{})
	if !ok {
		return errResult("missing or invalid 'body' parameter (must be an object)"), nil
	}

	priority := 0
	if p, ok := params["priority"].(float64); ok {
		priority = int(p)
	}

	// Validate recipient
	var targetMbox mailbox.Mailbox
	if to == "parent" {
		targetMbox = t.agent.ParentMailbox()
		if targetMbox == nil {
			return errResult("no parent mailbox available (not a sub-agent or mailbox disabled)"), nil
		}
	} else {
		return errResult(fmt.Sprintf("unsupported recipient %q (only 'parent' is supported in v1)", to)), nil
	}

	// Create message
	msg := mailbox.Message{
		ID:        ulid.Make().String(),
		From:      t.agent.ID(),
		To:        to,
		Topic:     topic,
		Body:      body,
		Timestamp: time.Now().UnixMilli(),
	}

	// Send message
	if err := targetMbox.Send(ctx, msg); err != nil {
		return errResult(fmt.Sprintf("failed to send message: %v", err)), nil
	}

	// DEBUG: Log successful send
	logger.Infof("DEBUG send_message: sent msg %s from %s to mailbox (recipient=%s, topic=%s, priority=%d)",
		msg.ID, msg.From, msg.To, msg.Topic, priority)

	// Emit event for observability
	emitMailboxSentEvent(t.agent, msg)

	return &interfaces.ToolResult{
		Success:     true,
		Data:        map[string]interface{}{"message_id": msg.ID},
		LLMContent:  fmt.Sprintf("Message delivered to %s (ID: %s, topic: %s)", to, msg.ID, topic),
		UserContent: "",
	}, nil
}

func (t *SendMessageTool) RequiresConfirmation() bool {
	return false
}

func (t *SendMessageTool) ConcurrencySafe() bool {
	return true
}

// errResult creates an error tool result
func errResult(msg string) *interfaces.ToolResult {
	return &interfaces.ToolResult{
		Success:     false,
		Error:       msg,
		LLMContent:  msg,
		UserContent: "",
	}
}

// emitMailboxSentEvent emits an event for observability
func emitMailboxSentEvent(agent AgentRef, msg mailbox.Message) {
	if agent == nil {
		return
	}
	evt := event.NewStreamEvent(event.EventTypeMailboxSent, "agent_tool").
		WithMetadata("message_id", msg.ID).
		WithMetadata("topic", msg.Topic).
		WithMetadata("from", msg.From).
		WithMetadata("to", msg.To).
		WithContent(fmt.Sprintf("Message sent: %s -> %s (topic: %s)", msg.From, msg.To, msg.Topic))
	agent.EmitEvent(evt)
}
