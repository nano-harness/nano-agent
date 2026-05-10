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

	// Build permission request
	request := PermissionRequest{
		Type:   "tool_execution",
		Tool:   info.Name,
		Args:   info.Parameters,
		Reason: buildPermissionReason(info),
	}

	// Send session/request_permission notification
	params := map[string]interface{}{
		"sessionId": b.acpSessionID,
		"requestId": requestID,
		"request":   request,
	}

	if err := b.transport.SendNotification("session/request_permission", params); err != nil {
		return false, fmt.Errorf("send permission request: %w", err)
	}

	logger.Infof("ACP: Sent permission request %s for tool %s", requestID, info.Name)

	// Wait for response with timeout
	timeout := 60 * time.Second
	select {
	case result := <-responseChan:
		if result.Approved {
			logger.Infof("ACP: Permission request %s approved", requestID)
		} else {
			logger.Infof("ACP: Permission request %s denied", requestID)
		}
		return result.Approved, nil
	case <-ctx.Done():
		return false, fmt.Errorf("permission request cancelled: %w", ctx.Err())
	case <-time.After(timeout):
		return false, fmt.Errorf("permission request timeout after %v", timeout)
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
