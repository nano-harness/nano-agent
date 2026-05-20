package acp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// PermissionBridge bridges nano-agent permission requests to ACP session/request_permission
type PermissionBridge struct {
	acpSessionID string
	transport    *Transport
	mu           sync.Mutex
	pending      map[string]chan *PermissionResult
	nextID       int
}

// NewPermissionBridge creates a new permission bridge
func NewPermissionBridge(acpSessionID string, transport *Transport) *PermissionBridge {
	return &PermissionBridge{
		acpSessionID: acpSessionID,
		transport:    transport,
		pending:      make(map[string]chan *PermissionResult),
	}
}

// RequestApproval sends a permission request to the ACP client and waits for response
func (b *PermissionBridge) RequestApproval(ctx context.Context, info *agent.ToolCallInfo) (bool, error) {
	decision, err := b.RequestApprovalV2(ctx, info)
	if err != nil {
		return false, err
	}
	return decision != agent.ApprovalReject, nil
}

// K2.3: RequestApprovalV2 sends a permission request to the ACP client and returns V2 approval decision
func (b *PermissionBridge) RequestApprovalV2(ctx context.Context, info *agent.ToolCallInfo) (agent.ApprovalDecision, error) {
	b.mu.Lock()
	requestID := fmt.Sprintf("perm-%s-%d", b.acpSessionID, b.nextID)
	b.nextID++
	responseChan := make(chan *PermissionResult, 1)
	b.pending[requestID] = responseChan
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.pending, requestID)
		b.mu.Unlock()
	}()

	// Build ToolCallUpdate for permission request (new ACP format)
	toolCallUpdate := &ToolCallUpdate{
		ToolCallID: info.ID,
		Title:      info.Name,
		Kind:       inferToolKindForPermission(info.Name),
		Status:     "pending",
		Content:    nil,
		Locations:  nil,
		Metadata: map[string]interface{}{
			"parameters": info.Parameters,
			"reason":     buildPermissionReason(info),
		},
	}

	// Build permission options (new ACP format)
	options := []map[string]interface{}{
		{
			"optionId": "allow_once",
			"name":     "Allow once",
			"kind":     "allow_once",
		},
		{
			"optionId": "allow_always",
			"name":     "Always allow",
			"kind":     "allow_always",
		},
		{
			"optionId": "reject_once",
			"name":     "Reject once",
			"kind":     "reject_once",
		},
		{
			"optionId": "reject_always",
			"name":     "Always reject",
			"kind":     "reject_always",
		},
	}

	// Send session/request_permission notification (new ACP format)
	params := map[string]interface{}{
		"sessionId": b.acpSessionID,
		"requestId": requestID,
		"toolCall":  toolCallUpdate,
		"options":   options,
	}

	if err := b.transport.SendNotification("session/request_permission", params); err != nil {
		return agent.ApprovalReject, fmt.Errorf("send permission request: %w", err)
	}

	logger.Infof("ACP: Sent permission request %s for tool %s", requestID, info.Name)

	// Wait for response with timeout
	timeout := 60 * time.Second
	select {
	case result := <-responseChan:
		if result.Approved {
			logger.Infof("ACP: Permission request %s approved (always=%v)", requestID, result.ApproveAlways)
			if result.ApproveAlways {
				return agent.ApprovalApproveAlways, nil
			}
			return agent.ApprovalApproveOnce, nil
		}
		logger.Infof("ACP: Permission request %s denied", requestID)
		return agent.ApprovalReject, nil
	case <-ctx.Done():
		return agent.ApprovalReject, fmt.Errorf("permission request cancelled: %w", ctx.Err())
	case <-time.After(timeout):
		return agent.ApprovalReject, fmt.Errorf("permission request timeout after %v", timeout)
	}
}

// inferToolKindForPermission infers the tool kind for permission requests
func inferToolKindForPermission(toolName string) string {
	switch toolName {
	case "read_file", "view_file", "list_dir", "glob", "grep":
		return "read"
	case "edit_file", "write_file", "create_file":
		return "edit"
	case "delete_file", "remove_file":
		return "delete"
	case "move_file", "rename_file":
		return "move"
	case "search", "find":
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

// HandlePermissionResponse handles an incoming permission response from the client
func (b *PermissionBridge) HandlePermissionResponse(requestID string, result *PermissionResult) {
	b.mu.Lock()
	ch, ok := b.pending[requestID]
	b.mu.Unlock()

	if !ok {
		logger.Warnf("ACP: Received permission response for unknown request: %s", requestID)
		return
	}

	select {
	case ch <- result:
		// Response delivered
	default:
		logger.Warnf("ACP: Permission response channel full for request: %s", requestID)
	}
}

// buildPermissionReason generates a human-readable reason for the permission request
func buildPermissionReason(info *agent.ToolCallInfo) string {
	reasons := []string{}

	// Check for specific dangerous operations
	if info.Name == "edit_file" || info.Name == "write_file" || info.Name == "create_file" {
		if path, ok := info.Parameters["path"].(string); ok {
			reasons = append(reasons, fmt.Sprintf("Writing to file: %s", path))
		}
	}

	if info.Name == "delete_file" {
		if path, ok := info.Parameters["path"].(string); ok {
			reasons = append(reasons, fmt.Sprintf("Deleting file: %s", path))
		}
	}

	if info.Name == "run_shell_command" || info.Name == "bash" {
		if cmd, ok := info.Parameters["command"].(string); ok {
			if len(cmd) > 100 {
				cmd = cmd[:100] + "..."
			}
			reasons = append(reasons, fmt.Sprintf("Executing command: %s", cmd))
		}
	}

	if len(reasons) == 0 {
		return fmt.Sprintf("Tool %s requires approval", info.Name)
	}

	return fmt.Sprintf("%s requires approval", reasons[0])
}
