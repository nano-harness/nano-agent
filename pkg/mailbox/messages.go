package mailbox

import (
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// Structured message constructors for common message types.
// These provide type-safe ways to create messages with well-defined Body schemas.

// NewProgressMessage creates a progress update message from a teammate to the lead
func NewProgressMessage(from, to, content string) Message {
	return Message{
		ID:        ulid.Make().String(),
		From:      from,
		To:        to,
		Topic:     TopicProgress,
		Body:      map[string]interface{}{"content": content},
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewFindingMessage creates a finding message (structured discovery from a teammate)
func NewFindingMessage(from, to, findingType, content string) Message {
	return Message{
		ID:        ulid.Make().String(),
		From:      from,
		To:        to,
		Topic:     TopicFinding,
		Body:      map[string]interface{}{"type": findingType, "content": content},
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewAmendTaskMessage creates a task amendment message from lead to teammate
func NewAmendTaskMessage(from, to, taskID, instruction string) Message {
	return Message{
		ID:        ulid.Make().String(),
		From:      from,
		To:        to,
		Topic:     TopicAmendTask,
		Body:      map[string]interface{}{"task_id": taskID, "instruction": instruction},
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewPermissionRequestMessage creates a permission request from teammate to lead
func NewPermissionRequestMessage(from, to, tool string, args map[string]interface{}) Message {
	return Message{
		ID:        ulid.Make().String(),
		From:      from,
		To:        to,
		Topic:     TopicPermissionRequest,
		Body:      map[string]interface{}{"tool": tool, "args": args},
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewPermissionGrantMessage creates a permission grant response from lead to teammate
func NewPermissionGrantMessage(from, to, requestID string) Message {
	return Message{
		ID:        ulid.Make().String(),
		From:      from,
		To:        to,
		Topic:     TopicPermissionGrant,
		Body:      map[string]interface{}{"request_id": requestID},
		Timestamp: time.Now().UnixMilli(),
		ReplyToID: requestID,
	}
}

// NewPermissionDenyMessage creates a permission deny response from lead to teammate
func NewPermissionDenyMessage(from, to, requestID, reason string) Message {
	return Message{
		ID:        ulid.Make().String(),
		From:      from,
		To:        to,
		Topic:     TopicPermissionDeny,
		Body:      map[string]interface{}{"request_id": requestID, "reason": reason},
		Timestamp: time.Now().UnixMilli(),
		ReplyToID: requestID,
	}
}

// Body field extractors for type-safe access

// GetProgressContent extracts content from a progress message
func GetProgressContent(msg Message) (string, error) {
	if msg.Topic != TopicProgress {
		return "", fmt.Errorf("message is not a progress message (topic=%s)", msg.Topic)
	}
	content, ok := msg.Body["content"].(string)
	if !ok {
		return "", fmt.Errorf("progress message missing 'content' field")
	}
	return content, nil
}

// GetFinding extracts type and content from a finding message
func GetFinding(msg Message) (findingType, content string, err error) {
	if msg.Topic != TopicFinding {
		return "", "", fmt.Errorf("message is not a finding message (topic=%s)", msg.Topic)
	}
	findingType, ok := msg.Body["type"].(string)
	if !ok {
		return "", "", fmt.Errorf("finding message missing 'type' field")
	}
	content, ok = msg.Body["content"].(string)
	if !ok {
		return "", "", fmt.Errorf("finding message missing 'content' field")
	}
	return findingType, content, nil
}

// GetAmendTask extracts task_id and instruction from an amend_task message
func GetAmendTask(msg Message) (taskID, instruction string, err error) {
	if msg.Topic != TopicAmendTask {
		return "", "", fmt.Errorf("message is not an amend_task message (topic=%s)", msg.Topic)
	}
	taskID, ok := msg.Body["task_id"].(string)
	if !ok {
		return "", "", fmt.Errorf("amend_task message missing 'task_id' field")
	}
	instruction, ok = msg.Body["instruction"].(string)
	if !ok {
		return "", "", fmt.Errorf("amend_task message missing 'instruction' field")
	}
	return taskID, instruction, nil
}

// GetPermissionRequest extracts tool and args from a permission_request message
func GetPermissionRequest(msg Message) (tool string, args map[string]interface{}, err error) {
	if msg.Topic != TopicPermissionRequest {
		return "", nil, fmt.Errorf("message is not a permission_request message (topic=%s)", msg.Topic)
	}
	tool, ok := msg.Body["tool"].(string)
	if !ok {
		return "", nil, fmt.Errorf("permission_request message missing 'tool' field")
	}
	args, ok = msg.Body["args"].(map[string]interface{})
	if !ok {
		return "", nil, fmt.Errorf("permission_request message missing 'args' field")
	}
	return tool, args, nil
}

// GetPermissionResponse extracts request_id and optional reason from permission grant/deny messages
func GetPermissionResponse(msg Message) (requestID, reason string, err error) {
	if msg.Topic != TopicPermissionGrant && msg.Topic != TopicPermissionDeny {
		return "", "", fmt.Errorf("message is not a permission response (topic=%s)", msg.Topic)
	}
	requestID, ok := msg.Body["request_id"].(string)
	if !ok {
		return "", "", fmt.Errorf("permission response message missing 'request_id' field")
	}
	// Reason is only present in deny messages
	if msg.Topic == TopicPermissionDeny {
		reason, _ = msg.Body["reason"].(string)
	}
	return requestID, reason, nil
}
