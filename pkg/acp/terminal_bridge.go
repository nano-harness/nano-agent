package acp

import (
	"context"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// TerminalBridge bridges nano-agent shell commands to ACP terminal/* RPC operations
// Unlike the previous implementation, this makes RPC calls to the Client (Agent→Client direction)
type TerminalBridge struct {
	acpSessionID      string
	transport         *Transport
	clientHasTermCaps bool
}

// TerminalExitStatus represents terminal exit status
type TerminalExitStatus struct {
	ExitCode int    `json:"exitCode"`
	Signal   string `json:"signal,omitempty"`
}

// NewTerminalBridge creates a new terminal bridge
func NewTerminalBridge(acpSessionID string, transport *Transport, clientHasTermCaps bool) *TerminalBridge {
	return &TerminalBridge{
		acpSessionID:      acpSessionID,
		transport:         transport,
		clientHasTermCaps: clientHasTermCaps,
	}
}

// Create sends terminal/create RPC to the Client
func (b *TerminalBridge) Create(ctx context.Context, cmd string, args []string, cwd string, env []EnvVariable) (string, error) {
	if !b.clientHasTermCaps {
		return "", fmt.Errorf("client does not support terminal capabilities")
	}

	logger.Infof("ACP: Creating terminal via client RPC: %s %v", cmd, args)

	params := map[string]interface{}{
		"sessionId": b.acpSessionID,
		"command":   cmd,
		"args":      args,
		"cwd":       cwd,
		"env":       env,
	}

	resp, err := b.transport.SendRPCRequest("terminal/create", params)
	if err != nil {
		return "", fmt.Errorf("RPC call failed: %w", err)
	}

	// Extract terminal ID from response
	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid response format")
	}

	terminalID, ok := resultMap["terminalId"].(string)
	if !ok {
		return "", fmt.Errorf("terminalId field missing or not a string")
	}

	return terminalID, nil
}

// Output sends terminal/output RPC to the Client
func (b *TerminalBridge) Output(ctx context.Context, terminalID string) (string, bool, *TerminalExitStatus, error) {
	if !b.clientHasTermCaps {
		return "", false, nil, fmt.Errorf("client does not support terminal capabilities")
	}

	logger.Debugf("ACP: Getting terminal output via client RPC: %s", terminalID)

	params := map[string]interface{}{
		"sessionId":  b.acpSessionID,
		"terminalId": terminalID,
	}

	resp, err := b.transport.SendRPCRequest("terminal/output", params)
	if err != nil {
		return "", false, nil, fmt.Errorf("RPC call failed: %w", err)
	}

	// Extract output from response
	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		return "", false, nil, fmt.Errorf("invalid response format")
	}

	output, _ := resultMap["output"].(string)
	running, _ := resultMap["running"].(bool)

	var exitStatus *TerminalExitStatus
	if statusMap, ok := resultMap["exitStatus"].(map[string]interface{}); ok {
		exitCode, _ := statusMap["exitCode"].(float64)
		signal, _ := statusMap["signal"].(string)
		exitStatus = &TerminalExitStatus{
			ExitCode: int(exitCode),
			Signal:   signal,
		}
	}

	return output, running, exitStatus, nil
}

// WaitForExit sends terminal/wait_for_exit RPC to the Client
func (b *TerminalBridge) WaitForExit(ctx context.Context, terminalID string) (int, string, error) {
	if !b.clientHasTermCaps {
		return 0, "", fmt.Errorf("client does not support terminal capabilities")
	}

	logger.Infof("ACP: Waiting for terminal exit via client RPC: %s", terminalID)

	params := map[string]interface{}{
		"sessionId":  b.acpSessionID,
		"terminalId": terminalID,
	}

	resp, err := b.transport.SendRPCRequest("terminal/wait_for_exit", params)
	if err != nil {
		return 0, "", fmt.Errorf("RPC call failed: %w", err)
	}

	// Extract exit info from response
	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		return 0, "", fmt.Errorf("invalid response format")
	}

	exitCode, _ := resultMap["exitCode"].(float64)
	signal, _ := resultMap["signal"].(string)

	return int(exitCode), signal, nil
}

// Kill sends terminal/kill RPC to the Client
func (b *TerminalBridge) Kill(ctx context.Context, terminalID string) error {
	if !b.clientHasTermCaps {
		return fmt.Errorf("client does not support terminal capabilities")
	}

	logger.Infof("ACP: Killing terminal via client RPC: %s", terminalID)

	params := map[string]interface{}{
		"sessionId":  b.acpSessionID,
		"terminalId": terminalID,
	}

	resp, err := b.transport.SendRPCRequest("terminal/kill", params)
	if err != nil {
		return fmt.Errorf("RPC call failed: %w", err)
	}

	// Check success
	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid response format")
	}

	success, _ := resultMap["success"].(bool)
	if !success {
		return fmt.Errorf("kill operation failed")
	}

	return nil
}

// Release sends terminal/release RPC to the Client
func (b *TerminalBridge) Release(ctx context.Context, terminalID string) error {
	if !b.clientHasTermCaps {
		return fmt.Errorf("client does not support terminal capabilities")
	}

	logger.Infof("ACP: Releasing terminal via client RPC: %s", terminalID)

	params := map[string]interface{}{
		"sessionId":  b.acpSessionID,
		"terminalId": terminalID,
	}

	resp, err := b.transport.SendRPCRequest("terminal/release", params)
	if err != nil {
		return fmt.Errorf("RPC call failed: %w", err)
	}

	// Check success
	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid response format")
	}

	success, _ := resultMap["success"].(bool)
	if !success {
		return fmt.Errorf("release operation failed")
	}

	return nil
}
