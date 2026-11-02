package system

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/middleware"
)

func TestShellTool_StreamingOutput(t *testing.T) {
	tool := NewShellTool("/tmp", nil, nil)

	var receivedChunks []string
	var mu sync.Mutex

	callback := func(stream, chunk string) {
		mu.Lock()
		receivedChunks = append(receivedChunks, chunk)
		mu.Unlock()
	}

	// Add security decision to context to allow command execution in tests
	decision := &middleware.Decision{Action: middleware.ActionAllow}
	ctx := middleware.WithSecurityDecision(context.Background(), decision)
	ctx = WithOutputCallback(ctx, callback)

	params := map[string]interface{}{
		"command":         "echo line1 && sleep 0.1 && echo line2 && echo line3",
		"timeout_seconds": float64(5),
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Verify we received chunks via callback
	mu.Lock()
	chunkCount := len(receivedChunks)
	allOutput := strings.Join(receivedChunks, "")
	mu.Unlock()

	if chunkCount == 0 {
		t.Error("Expected to receive streaming chunks")
	}

	if !strings.Contains(allOutput, "line1") {
		t.Errorf("Expected output to contain 'line1', got: %s", allOutput)
	}

	if !strings.Contains(allOutput, "line2") {
		t.Errorf("Expected output to contain 'line2', got: %s", allOutput)
	}

	if !strings.Contains(allOutput, "line3") {
		t.Errorf("Expected output to contain 'line3', got: %s", allOutput)
	}
}

func TestShellTool_OutputSizeLimit(t *testing.T) {
	tool := NewShellTool("/tmp", nil, nil)

	// Add security decision to context to allow command execution in tests
	decision := &middleware.Decision{Action: middleware.ActionAllow}
	ctx := middleware.WithSecurityDecision(context.Background(), decision)

	// Generate output larger than 16MB limit
	// 20MB = 20 * 1024 * 1024 bytes
	params := map[string]interface{}{
		"command":         "dd if=/dev/zero bs=1M count=20 | base64",
		"timeout_seconds": float64(30),
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Check that output was truncated
	outputLen := len(result.UserContent)
	if outputLen > MaxShellOutputBytes+1024 { // Allow some buffer for formatting
		t.Errorf("Output size %d exceeds max %d", outputLen, MaxShellOutputBytes)
	}

	// Should contain truncation message
	if !strings.Contains(result.UserContent, "truncated") && !strings.Contains(result.LLMContent, "truncated") {
		t.Error("Expected truncation message in output")
	}

	// Verify metadata indicates truncation
	if truncated, ok := result.Metadata["output_truncated"].(bool); !ok || !truncated {
		t.Error("Expected output_truncated metadata to be true")
	}
}

func TestShellTool_GracefulShutdown(t *testing.T) {
	tool := NewShellTool("/tmp", nil, nil)

	// Add security decision to context to allow command execution in tests
	decision := &middleware.Decision{Action: middleware.ActionAllow}
	ctx := middleware.WithSecurityDecision(context.Background(), decision)

	// Start long-running process with short timeout
	params := map[string]interface{}{
		"command":         "trap 'echo caught sigterm; exit 0' TERM; sleep 60",
		"timeout_seconds": float64(1),
	}

	start := time.Now()
	result, err := tool.Execute(ctx, params)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Should timeout and kill process gracefully
	if !result.Success {
		t.Errorf("Expected successful timeout handling, got error: %s", result.Error)
	}

	// Check if the command data indicates it timed out
	if cmdResult, ok := result.Data.(*CommandResult); ok {
		if !cmdResult.TimedOut {
			t.Error("Expected command to be marked as timed out")
		}
	}

	// Should complete quickly (timeout + grace period)
	// 1s timeout + 200ms grace period + some overhead
	if duration > 2*time.Second {
		t.Errorf("Expected quick completion, took %v", duration)
	}
}

func TestShellTool_BackgroundMode(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewShellToolWithBgManager("/tmp", nil, nil, bgManager)

	// Add security decision to context to allow command execution in tests
	decision := &middleware.Decision{Action: middleware.ActionAllow}
	ctx := middleware.WithSecurityDecision(context.Background(), decision)

	params := map[string]interface{}{
		"command":       "echo starting && sleep 1 && echo done",
		"is_background": true,
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Should return task ID
	taskID, ok := result.Metadata["task_id"].(string)
	if !ok || taskID == "" {
		t.Fatal("Expected task_id in metadata")
	}

	// Should indicate background execution
	if !strings.Contains(strings.ToLower(result.UserContent), "background") {
		t.Errorf("Expected background indication in output: %s", result.UserContent)
	}

	// Verify task is running
	time.Sleep(100 * time.Millisecond)
	tasks := bgManager.List("default")
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 background task, got %d", len(tasks))
	}

	if tasks[0].ID != taskID {
		t.Errorf("Expected task ID %s, got %s", taskID, tasks[0].ID)
	}

	// Wait and verify completion
	time.Sleep(1200 * time.Millisecond)
	output, _, status, _ := bgManager.ReadOutput(taskID, 0, 0, 0)

	if status != BgStatusCompleted {
		t.Errorf("Expected status %s, got %s", BgStatusCompleted, status)
	}

	if !strings.Contains(output, "starting") || !strings.Contains(output, "done") {
		t.Errorf("Expected full output, got: %s", output)
	}
}

func TestShellTool_AutoBackground(t *testing.T) {
	tempDir := t.TempDir()
	bgManager := NewBackgroundTaskManager(tempDir)
	tool := NewShellToolWithBgManager("/tmp", nil, nil, bgManager)

	// Add security decision to context to allow command execution in tests
	decision := &middleware.Decision{Action: middleware.ActionAllow}
	ctx := middleware.WithSecurityDecision(context.Background(), decision)

	// Command that takes longer than timeout should auto-convert to background
	params := map[string]interface{}{
		"command":         "echo starting && sleep 3 && echo done",
		"timeout_seconds": float64(1), // 1 second timeout
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Should return task ID after auto-conversion
	taskID, ok := result.Metadata["task_id"].(string)
	if !ok || taskID == "" {
		t.Fatal("Expected task_id in metadata after auto-background")
	}

	// Should indicate auto-background conversion
	contentLower := strings.ToLower(result.UserContent)
	if !strings.Contains(contentLower, "converted") || !strings.Contains(contentLower, "background") {
		t.Errorf("Expected auto-background indication in output: %s", result.UserContent)
	}

	// Verify task is running in background
	tasks := bgManager.List("default")
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 background task, got %d", len(tasks))
	}

	// Wait for completion
	time.Sleep(3500 * time.Millisecond)
	output, _, status, _ := bgManager.ReadOutput(taskID, 0, 0, 0)

	if status != BgStatusCompleted {
		t.Errorf("Expected status %s, got %s", BgStatusCompleted, status)
	}

	if !strings.Contains(output, "done") {
		t.Errorf("Expected command to complete, got: %s", output)
	}
}

func TestShellTool_StreamingWithSizeLimit(t *testing.T) {
	tool := NewShellTool("/tmp", nil, nil)

	var totalReceived int64
	var mu sync.Mutex
	var truncated bool

	callback := func(stream, chunk string) {
		mu.Lock()
		totalReceived += int64(len(chunk))
		mu.Unlock()
	}

	// Add security decision to context to allow command execution in tests
	decision := &middleware.Decision{Action: middleware.ActionAllow}
	ctx := middleware.WithSecurityDecision(context.Background(), decision)
	ctx = WithOutputCallback(ctx, callback)

	// Generate ~20MB of output
	params := map[string]interface{}{
		"command":         "head -c 20971520 /dev/zero | tr '\\0' 'x'",
		"timeout_seconds": float64(10),
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Check truncation
	if val, ok := result.Metadata["output_truncated"].(bool); ok {
		truncated = val
	}

	mu.Lock()
	received := totalReceived
	mu.Unlock()

	if !truncated {
		t.Error("Expected output to be truncated")
	}

	// Received should not exceed limit by much
	if received > MaxShellOutputBytes+100*1024 {
		t.Errorf("Received %d bytes exceeds max %d by too much", received, MaxShellOutputBytes)
	}
}

func TestOutputCallbackContext(t *testing.T) {
	// Test storing and retrieving callback from context
	called := false
	callback := func(stream, chunk string) {
		called = true
	}

	ctx := WithOutputCallback(context.Background(), callback)

	// Retrieve callback
	value := ctx.Value(outputCallbackKey{})
	if value == nil {
		t.Fatal("Expected callback in context")
	}

	cb, ok := value.(OutputCallback)
	if !ok {
		t.Fatal("Expected OutputCallback type")
	}

	// Test callback
	cb("stdout", "test")
	if !called {
		t.Error("Callback was not called")
	}
}

func TestStreamPipe_Integration(t *testing.T) {
	tool := NewShellTool("/tmp", nil, nil)

	var stdoutChunks []string
	var stderrChunks []string
	var mu sync.Mutex

	callback := func(stream, chunk string) {
		mu.Lock()
		defer mu.Unlock()
		if stream == "stdout" {
			stdoutChunks = append(stdoutChunks, chunk)
		} else if stream == "stderr" {
			stderrChunks = append(stderrChunks, chunk)
		}
	}

	// Add security decision to context to allow command execution in tests
	decision := &middleware.Decision{Action: middleware.ActionAllow}
	ctx := middleware.WithSecurityDecision(context.Background(), decision)
	ctx = WithOutputCallback(ctx, callback)

	// Command that writes to both stdout and stderr
	params := map[string]interface{}{
		"command":         "echo stdout1 && echo stderr1 >&2 && echo stdout2 && echo stderr2 >&2",
		"timeout_seconds": float64(5),
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	mu.Lock()
	stdoutCount := len(stdoutChunks)
	stderrCount := len(stderrChunks)
	stdoutAll := strings.Join(stdoutChunks, "")
	stderrAll := strings.Join(stderrChunks, "")
	mu.Unlock()

	if stdoutCount == 0 {
		t.Error("Expected stdout chunks")
	}

	if stderrCount == 0 {
		t.Error("Expected stderr chunks")
	}

	if !strings.Contains(stdoutAll, "stdout1") || !strings.Contains(stdoutAll, "stdout2") {
		t.Errorf("Expected stdout content, got: %s", stdoutAll)
	}

	if !strings.Contains(stderrAll, "stderr1") || !strings.Contains(stderrAll, "stderr2") {
		t.Errorf("Expected stderr content, got: %s", stderrAll)
	}
}
