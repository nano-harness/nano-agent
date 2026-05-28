package toolruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/middleware"
)

type mockHookDispatcher struct {
	calls []struct {
		event    middleware.HookEvent
		toolName string
	}
}

func (m *mockHookDispatcher) Execute(_ context.Context, event middleware.HookEvent, toolName string, _ map[string]interface{}) (*middleware.Decision, error) {
	m.calls = append(m.calls, struct {
		event    middleware.HookEvent
		toolName string
	}{event, toolName})
	return &middleware.Decision{Action: middleware.ActionAllow}, nil
}

type mockRecoveryExecutor struct {
	result *ToolExecutionResult
}

func (m *mockRecoveryExecutor) ExecuteToolWithRecovery(_ context.Context, req ToolRequest) *ToolExecutionResult {
	if m.result != nil {
		return m.result
	}
	return &ToolExecutionResult{
		Result:   &interfaces.ToolResult{Content: "ok"},
		Attempts: 1,
	}
}

func newTestRuntime() *Runtime {
	reg := NewRegistry()
	return NewRuntime(reg, nil, nil)
}

func TestExecuteWithHooks_DispatchesPreAndPostHooks(t *testing.T) {
	rt := newTestRuntime()
	hooks := &mockHookDispatcher{}
	recovery := &mockRecoveryExecutor{}

	req := ToolRequest{
		ID:         "call-1",
		Name:       "test_tool",
		Parameters: map[string]interface{}{"key": "value"},
		SessionID:  "sess-1",
	}

	result := rt.ExecuteWithHooks(context.Background(), req, hooks, recovery)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Result.Content != "ok" {
		t.Fatalf("expected content 'ok', got %q", result.Result.Content)
	}
	if len(hooks.calls) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(hooks.calls))
	}
	if hooks.calls[0].event != middleware.HookPreToolUse {
		t.Errorf("first hook should be PreToolUse, got %v", hooks.calls[0].event)
	}
	if hooks.calls[1].event != middleware.HookPostToolUse {
		t.Errorf("second hook should be PostToolUse, got %v", hooks.calls[1].event)
	}
}

func TestExecuteWithHooks_PostToolUseFailureOnError(t *testing.T) {
	rt := newTestRuntime()
	hooks := &mockHookDispatcher{}
	recovery := &mockRecoveryExecutor{
		result: &ToolExecutionResult{
			Error:    errors.New("tool failed"),
			Attempts: 2,
		},
	}

	req := ToolRequest{
		ID:   "call-2",
		Name: "failing_tool",
	}

	result := rt.ExecuteWithHooks(context.Background(), req, hooks, recovery)

	if result.Error == nil {
		t.Fatal("expected error")
	}
	if len(hooks.calls) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(hooks.calls))
	}
	if hooks.calls[1].event != middleware.HookPostToolUseFailure {
		t.Errorf("expected PostToolUseFailure, got %v", hooks.calls[1].event)
	}
}

func TestExecuteWithHooks_NilHooksStillExecutes(t *testing.T) {
	rt := newTestRuntime()
	recovery := &mockRecoveryExecutor{
		result: &ToolExecutionResult{
			Result:    &interfaces.ToolResult{Content: "no hooks"},
			Attempts:  1,
			TotalTime: 10 * time.Millisecond,
		},
	}

	req := ToolRequest{ID: "call-3", Name: "tool"}
	result := rt.ExecuteWithHooks(context.Background(), req, nil, recovery)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Result.Content != "no hooks" {
		t.Errorf("expected 'no hooks', got %q", result.Result.Content)
	}
}
