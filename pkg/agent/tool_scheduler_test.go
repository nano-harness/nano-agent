package agent

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/hookservice"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/middleware"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
	"github.com/nano-harness/nano-agent/pkg/tools"
	"github.com/nano-harness/nano-agent/pkg/tools/system"
)

type testTool struct {
	name    string
	started chan struct{}
	done    <-chan struct{}
	wg      *sync.WaitGroup
}

func (t *testTool) Name() string {
	return t.name
}

func (t *testTool) Description() string {
	return "test tool"
}

func (t *testTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryDebug
}

func (t *testTool) RequiresConfirmation() bool {
	return false
}

func (t *testTool) ConcurrencySafe() bool { return true }

func (t *testTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema("test tool", map[string]*interfaces.PropertySchema{}, nil)
}

func (t *testTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	if t.wg != nil {
		t.wg.Add(1)
		defer t.wg.Done()
	}
	if t.started != nil {
		select {
		case <-t.started:
		default:
			close(t.started)
		}
	}
	<-t.done
	return nil, ctx.Err()
}

func ensureConfigLoaded(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	path := tmp + "/nano.yaml"
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	if _, err := config.LoadConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}
}

func TestToolScheduler_ToolNotFoundEmitsEventsAndResult(t *testing.T) {
	ensureConfigLoaded(t)
	tb := tools.NewToolbox(".", nil, nil)

	var mu sync.Mutex
	var events []event.StreamEvent
	handler := func(e event.StreamEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     handler,
		RecoveryStrategy: NewToolRecoveryStrategy(handler),
	})

	results, err := ts.ExecuteParallel(context.Background(), []ToolToExecute{{
		ID:         "call-1",
		Name:       "does_not_exist",
		Parameters: map[string]interface{}{"x": 1},
	}})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	tr := results["call-1"]
	if tr == nil {
		t.Fatalf("expected tool result for call-1")
	}
	if tr.Success {
		t.Fatalf("expected failure")
	}
	if tr.Metadata == nil || tr.Metadata["code"] != "tool_not_found" {
		t.Fatalf("expected metadata code tool_not_found, got %v", tr.Metadata)
	}

	var sawCall, sawResult, sawUse bool
	mu.Lock()
	defer mu.Unlock()
	for _, e := range events {
		switch e.Type {
		case event.EventTypeToolCall:
			sawCall = true
		case event.EventTypeToolResult:
			sawResult = true
		case event.EventTypeToolUse:
			sawUse = true
		}
	}
	if !sawCall || !sawResult || !sawUse {
		t.Fatalf("expected ToolCall/ToolResult/ToolUse events, got call=%v result=%v use=%v", sawCall, sawResult, sawUse)
	}
}

func TestToolSchedulerBridgesSandboxEvents(t *testing.T) {
	ensureConfigLoaded(t)
	tempDir := t.TempDir()
	tb := tools.NewToolbox(tempDir, nil, nil)

	var mu sync.Mutex
	var events []event.StreamEvent
	handler := func(e event.StreamEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     handler,
		RecoveryStrategy: NewToolRecoveryStrategy(handler),
	})
	results, err := ts.ExecuteParallel(context.Background(), []ToolToExecute{{
		ID:   "shell-1",
		Name: "run_shell_command",
		Parameters: map[string]interface{}{
			"command":         "echo scheduler",
			"directory":       tempDir,
			"timeout_seconds": float64(5),
			"capture_output":  true,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if results["shell-1"] == nil || !results["shell-1"].Success {
		t.Fatalf("shell result = %#v", results["shell-1"])
	}

	mu.Lock()
	defer mu.Unlock()
	seenStarted := false
	seenFinished := false
	for _, ev := range events {
		switch ev.Type {
		case event.EventType(sandbox.EventTypeSandboxCommandStarted):
			seenStarted = true
			if ev.Metadata["sandbox"] == nil {
				t.Fatalf("started event missing sandbox metadata: %#v", ev)
			}
		case event.EventType(sandbox.EventTypeSandboxCommandFinished):
			seenFinished = true
			if ev.Metadata["exit_code"] != 0 {
				t.Fatalf("finished exit_code = %#v, want 0", ev.Metadata["exit_code"])
			}
		}
	}
	if !seenStarted || !seenFinished {
		t.Fatalf("missing sandbox command events: started=%t finished=%t events=%#v", seenStarted, seenFinished, events)
	}
}

type deadlineOnlyContext struct {
	deadline time.Time
	done     chan struct{}
}

func (c *deadlineOnlyContext) Deadline() (time.Time, bool) {
	return c.deadline, true
}

func (c *deadlineOnlyContext) Done() <-chan struct{} {
	return c.done
}

func (c *deadlineOnlyContext) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}

func (c *deadlineOnlyContext) Value(key interface{}) interface{} {
	return nil
}

func TestToolScheduler_ExecutionTimeoutEmitsToolResultAndUpdatesStatus(t *testing.T) {
	ensureConfigLoaded(t)
	tb := tools.NewToolbox(".", nil, nil)

	done := make(chan struct{})
	var wg sync.WaitGroup
	slow := &testTool{
		name:    "slow_tool",
		started: make(chan struct{}),
		done:    done,
		wg:      &wg,
	}
	if err := tb.Register(slow); err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}

	var mu sync.Mutex
	var events []event.StreamEvent
	handler := func(e event.StreamEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     handler,
		RecoveryStrategy: NewToolRecoveryStrategy(handler),
	})

	ctx := &deadlineOnlyContext{
		deadline: time.Now().Add(20 * time.Millisecond),
		done:     make(chan struct{}),
	}

	resultCh := make(chan map[string]*interfaces.ToolResult, 1)
	errCh := make(chan error, 1)
	go func() {
		results, err := ts.ExecuteParallel(ctx, []ToolToExecute{{
			ID:   "call-2",
			Name: "slow_tool",
		}})
		resultCh <- results
		errCh <- err
	}()

	select {
	case <-slow.started:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("tool did not start")
	}

	var results map[string]*interfaces.ToolResult
	select {
	case err := <-errCh:
		// Timeout no longer propagates as an error; ExecuteParallel returns partial results.
		if err != nil {
			t.Fatalf("expected nil error on timeout (partial results returned), got: %v", err)
		}
		results = <-resultCh
	case <-time.After(2 * time.Second):
		t.Fatalf("expected ExecuteParallel to return")
	}

	mu.Lock()
	sawTimeoutResult := false
	for _, e := range events {
		if e.Type == event.EventTypeToolResult && e.ToolResult != nil && e.ToolResult.Error == "execution timeout" {
			sawTimeoutResult = true
			break
		}
	}
	mu.Unlock()
	if !sawTimeoutResult {
		t.Fatalf("expected ToolResult event with execution timeout")
	}

	close(done)
	close(ctx.done)
	wg.Wait()

	// Verify the timeout result is present in the returned map (not in internal state,
	// which may have been cleared by the async ClearSpecificToolCalls).
	tr, ok := results["call-2"]
	if !ok || tr == nil || tr.Metadata == nil {
		t.Fatalf("expected timeout result in returned map, got: %v", results)
	}
	if tr.Metadata["code"] != "execution_timeout" {
		t.Fatalf("expected code execution_timeout, got %v", tr.Metadata["code"])
	}
}

// ─── Security pre-analysis tests ─────────────────────────────────────────────

// newShellToolbox creates a Toolbox backed by a real ShellTool rooted at
// workDir, replacing the default shell tool registration.
func newShellToolbox(t *testing.T, workDir string) *tools.Toolbox {
	t.Helper()
	tb := tools.NewToolbox(".", nil, nil)
	// The default toolbox registers run_shell_command; swap it for one rooted
	// at the test's working directory.
	if err := tb.Unregister("run_shell_command"); err != nil {
		t.Fatalf("unregister default ShellTool: %v", err)
	}
	shellTool := system.NewShellTool(workDir, nil, nil)
	if err := tb.Register(shellTool); err != nil {
		t.Fatalf("register ShellTool: %v", err)
	}
	return tb
}

// TestToolScheduler_BlockedShellCommand_NoApproval verifies that a command
// classified as ActionBlock is rejected immediately without invoking the
// approval handler (user confirmation must not be triggered for blocked cmds).
func TestToolScheduler_BlockedShellCommand_NoApproval(t *testing.T) {
	ensureConfigLoaded(t)

	tempDir := t.TempDir()
	tb := newShellToolbox(t, tempDir)

	var mu sync.Mutex
	var events []event.StreamEvent
	handler := func(e event.StreamEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	approvalCalled := false
	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     handler,
		RecoveryStrategy: NewToolRecoveryStrategy(handler),
		ApprovalHandler: func(_ *ToolCallInfo) bool {
			approvalCalled = true
			return true
		},
	})

	results, err := ts.ExecuteParallel(context.Background(), []ToolToExecute{{
		ID:         "blocked-1",
		Name:       "run_shell_command",
		Parameters: map[string]interface{}{"command": "mkfs /dev/sda"},
	}})
	if err != nil {
		t.Fatalf("ExecuteParallel: unexpected error: %v", err)
	}

	tr := results["blocked-1"]
	if tr == nil {
		t.Fatal("expected a result for blocked-1")
	}
	if tr.Success {
		t.Error("expected failure for blocked command")
	}
	if tr.Metadata == nil || tr.Metadata["code"] != "security_blocked" {
		t.Errorf("expected code=security_blocked, got metadata: %v", tr.Metadata)
	}
	if approvalCalled {
		t.Error("approval handler must not be called for blocked commands")
	}
}

// TestToolScheduler_ConfirmShellCommand_TriggersApproval verifies that a
// command classified as ActionConfirm still triggers the approval handler.
func TestToolScheduler_ConfirmShellCommand_TriggersApproval(t *testing.T) {
	ensureConfigLoaded(t)

	tempDir := t.TempDir()
	tb := newShellToolbox(t, tempDir)

	var mu sync.Mutex
	var events []event.StreamEvent
	handler := func(e event.StreamEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	approvalCalled := false
	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     handler,
		RecoveryStrategy: NewToolRecoveryStrategy(handler),
		ApprovalHandler: func(_ *ToolCallInfo) bool {
			approvalCalled = true
			return true // sync-approve
		},
	})

	// "cmd1 && cmd2" is a compound command → ActionConfirm.
	_, err := ts.ExecuteParallel(context.Background(), []ToolToExecute{{
		ID:         "confirm-1",
		Name:       "run_shell_command",
		Parameters: map[string]interface{}{"command": "cmd1 && cmd2"},
	}})
	if err != nil {
		t.Fatalf("ExecuteParallel: unexpected error: %v", err)
	}

	if !approvalCalled {
		t.Error("approval handler should have been called for ActionConfirm command")
	}
}

func TestToolScheduler_ContextApprovalHandler(t *testing.T) {
	ensureConfigLoaded(t)

	tempDir := t.TempDir()
	tb := newShellToolbox(t, tempDir)

	globalApprovalCalled := false
	contextApprovalCalled := false
	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     func(_ event.StreamEvent) {},
		RecoveryStrategy: NewToolRecoveryStrategy(nil),
		ApprovalHandler: func(_ *ToolCallInfo) bool {
			globalApprovalCalled = true
			return false
		},
	})

	ctx := WithApprovalHandler(context.Background(), func(_ *ToolCallInfo) bool {
		contextApprovalCalled = true
		return true
	})
	if _, err := ts.ExecuteParallel(ctx, []ToolToExecute{{
		ID:         "context-approval-1",
		Name:       "run_shell_command",
		Parameters: map[string]interface{}{"command": "sleep 0 && sleep 0"},
	}}); err != nil {
		t.Fatalf("ExecuteParallel: %v", err)
	}

	if !contextApprovalCalled {
		t.Fatal("context approval handler should have been called")
	}
	if globalApprovalCalled {
		t.Fatal("global approval handler should not be called when context handler exists")
	}
}

func TestToolScheduler_ApproveAlways_AddsToAllowlist(t *testing.T) {
	ensureConfigLoaded(t)

	tempDir := t.TempDir()
	tb := newShellToolbox(t, tempDir)
	pm := permission.NewManager(permission.ModeDefault, nil)

	approvalCalls := 0
	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     func(_ event.StreamEvent) {},
		RecoveryStrategy: NewToolRecoveryStrategy(nil),
		ApprovalHandlerV2: func(_ *ToolCallInfo) ApprovalDecision {
			approvalCalls++
			return ApprovalApproveAlways
		},
	})
	ts.SetPermissionManager(pm)

	params := map[string]interface{}{"command": "sleep 0 && sleep 0"}
	if _, err := ts.ExecuteParallel(context.Background(), []ToolToExecute{{
		ID:         "always-1",
		Name:       "run_shell_command",
		Parameters: params,
	}}); err != nil {
		t.Fatalf("first ExecuteParallel: %v", err)
	}
	if approvalCalls != 1 {
		t.Fatalf("expected first approval call, got %d", approvalCalls)
	}

	if _, err := ts.ExecuteParallel(context.Background(), []ToolToExecute{{
		ID:         "always-2",
		Name:       "run_shell_command",
		Parameters: params,
	}}); err != nil {
		t.Fatalf("second ExecuteParallel: %v", err)
	}
	if approvalCalls != 1 {
		t.Fatalf("approve-always allowlist should skip second approval, got %d calls", approvalCalls)
	}
}

// TestToolScheduler_AllowShellCommand_NoApproval verifies that a read-only
// command (ActionAllow) is executed without triggering the approval handler.
func TestToolScheduler_AllowShellCommand_NoApproval(t *testing.T) {
	ensureConfigLoaded(t)

	tempDir := t.TempDir()
	tb := newShellToolbox(t, tempDir)

	var mu sync.Mutex
	var events []event.StreamEvent
	handler := func(e event.StreamEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	approvalCalled := false
	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     handler,
		RecoveryStrategy: NewToolRecoveryStrategy(handler),
		ApprovalHandler: func(_ *ToolCallInfo) bool {
			approvalCalled = true
			return true
		},
	})

	results, err := ts.ExecuteParallel(context.Background(), []ToolToExecute{{
		ID:         "allow-1",
		Name:       "run_shell_command",
		Parameters: map[string]interface{}{"command": "echo hello"},
	}})
	if err != nil {
		t.Fatalf("ExecuteParallel: unexpected error: %v", err)
	}

	tr := results["allow-1"]
	if tr == nil {
		t.Fatal("expected result for allow-1")
	}
	if !tr.Success {
		t.Errorf("expected success for allowed command, got error: %s", tr.Error)
	}
	if approvalCalled {
		t.Error("approval handler must not be called for ActionAllow commands")
	}
}

func TestExecuteShell_FirewallBlocksDangerous(t *testing.T) {
	ensureConfigLoaded(t)

	tempDir := t.TempDir()
	tb := newShellToolbox(t, tempDir)
	firewallConfig := permission.DefaultFirewallConfig()
	firewallConfig.FailurePolicy = "block"
	hookEngine := middleware.NewHookEngine(nil)
	hookEngine.RegisterProgrammaticHook(permission.NewFirewallHook(firewallConfig))

	approvalCalled := false
	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     func(_ event.StreamEvent) {},
		RecoveryStrategy: NewToolRecoveryStrategy(nil),
		ApprovalHandler: func(_ *ToolCallInfo) bool {
			approvalCalled = true
			return true
		},
	})
	ts.SetHookEngine(hookEngine)

	results, err := ts.ExecuteParallel(context.Background(), []ToolToExecute{{
		ID:         "firewall-blocked",
		Name:       "run_shell_command",
		Parameters: map[string]interface{}{"command": "rm -rf " + filepath.Join(tempDir, "target")},
	}})
	if err != nil {
		t.Fatalf("ExecuteParallel: %v", err)
	}
	tr := results["firewall-blocked"]
	if tr == nil {
		t.Fatal("expected firewall result")
	}
	if tr.Success {
		t.Fatalf("expected firewall block, got %#v", tr)
	}
	if tr.Metadata == nil || tr.Metadata["code"] != "hook_blocked" {
		t.Fatalf("expected hook_blocked metadata, got %#v", tr.Metadata)
	}
	if approvalCalled {
		t.Fatal("approval handler should not run for firewall block")
	}
}

func TestExecuteShell_FirewallAllowsSafe(t *testing.T) {
	ensureConfigLoaded(t)

	tempDir := t.TempDir()
	tb := newShellToolbox(t, tempDir)
	hookEngine := middleware.NewHookEngine(nil)
	hookEngine.RegisterProgrammaticHook(permission.NewFirewallHook(permission.DefaultFirewallConfig()))

	approvalCalled := false
	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     func(_ event.StreamEvent) {},
		RecoveryStrategy: NewToolRecoveryStrategy(nil),
		ApprovalHandler: func(_ *ToolCallInfo) bool {
			approvalCalled = true
			return true
		},
	})
	ts.SetHookEngine(hookEngine)

	results, err := ts.ExecuteParallel(context.Background(), []ToolToExecute{{
		ID:         "firewall-allowed",
		Name:       "run_shell_command",
		Parameters: map[string]interface{}{"command": "ls"},
	}})
	if err != nil {
		t.Fatalf("ExecuteParallel: %v", err)
	}
	tr := results["firewall-allowed"]
	if tr == nil || !tr.Success {
		t.Fatalf("expected successful safe command, got %#v", tr)
	}
	if approvalCalled {
		t.Fatal("approval handler should not run for safe command")
	}
}

// TestToolScheduler_SecurityDecisionInjectedInContext verifies that the
// pre-computed security decision is injected into the execution context so
// downstream layers (SecurityMiddleware, ShellTool.Execute) can skip redundant
// analysis.
func TestToolScheduler_SecurityDecisionInjectedInContext(t *testing.T) {
	ensureConfigLoaded(t)

	tempDir := t.TempDir()

	// Create a spy ShellTool that records whether a security decision was in
	// the context when Execute was called.
	spyTool := &spyShellTool{
		ShellTool: system.NewShellTool(tempDir, nil, nil),
	}
	tb := tools.NewToolbox(".", nil, nil)
	if err := tb.Unregister("run_shell_command"); err != nil {
		t.Fatalf("unregister default ShellTool: %v", err)
	}
	if err := tb.Register(spyTool); err != nil {
		t.Fatalf("register spy tool: %v", err)
	}

	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     func(_ event.StreamEvent) {},
		RecoveryStrategy: NewToolRecoveryStrategy(nil),
	})

	_, err := ts.ExecuteParallel(context.Background(), []ToolToExecute{{
		ID:         "spy-1",
		Name:       "run_shell_command",
		Parameters: map[string]interface{}{"command": "echo hello"},
	}})
	if err != nil {
		t.Fatalf("ExecuteParallel: %v", err)
	}

	if !spyTool.hadDecision {
		t.Error("expected security decision to be present in execution context")
	}
}

// spyShellTool wraps ShellTool and records whether a security decision was
// present in the context when Execute was called.
type spyShellTool struct {
	*system.ShellTool
	hadDecision bool
}

func (s *spyShellTool) Name() string { return "run_shell_command" }

func (s *spyShellTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	s.hadDecision = middleware.HasSecurityDecision(ctx)
	return s.ShellTool.Execute(ctx, params)
}

// captureHook 实现 middleware.ProgrammaticHook，记录最近一次 Execute 调用的 params。
type captureHook struct {
	event      hookservice.Event
	name       string
	mu         sync.Mutex
	lastParams map[string]interface{}
	lastTool   string
}

func (h *captureHook) Name() string             { return h.name }
func (h *captureHook) Event() hookservice.Event { return h.event }
func (h *captureHook) Execute(_ context.Context, _ hookservice.Event, toolName string, params map[string]interface{}) (*hookservice.Decision, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastTool = toolName
	h.lastParams = make(map[string]interface{}, len(params))
	for k, v := range params {
		h.lastParams[k] = v
	}
	return &hookservice.Decision{Action: hookservice.ActionAllow}, nil
}

func (h *captureHook) snapshot() (string, map[string]interface{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]interface{}, len(h.lastParams))
	for k, v := range h.lastParams {
		out[k] = v
	}
	return h.lastTool, out
}

func TestPreToolUseHook_IncludesSessionID(t *testing.T) {
	ensureConfigLoaded(t)

	tempDir := t.TempDir()
	tb := newShellToolbox(t, tempDir)
	hookEngine := middleware.NewHookEngine(nil)
	cap := &captureHook{event: hookservice.EventPreToolUse, name: "capture-pre"}
	hookEngine.RegisterProgrammaticHook(cap)

	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     func(_ event.StreamEvent) {},
		RecoveryStrategy: NewToolRecoveryStrategy(nil),
	})
	ts.SetHookEngine(hookEngine)

	const wantSessionID = "session-pre-123"
	ctx := context.WithValue(
		context.Background(),
		interfaces.TurnContextKey{},
		interfaces.TurnContext{SessionID: wantSessionID},
	)

	if _, err := ts.ExecuteParallel(ctx, []ToolToExecute{{
		ID:         "pre-hook-session",
		Name:       "run_shell_command",
		Parameters: map[string]interface{}{"command": "ls"},
	}}); err != nil {
		t.Fatalf("ExecuteParallel: %v", err)
	}

	_, params := cap.snapshot()
	got, _ := params["session_id"].(string)
	if got != wantSessionID {
		t.Fatalf("PreToolUse params[session_id] = %q, want %q", got, wantSessionID)
	}
}

func TestPostToolUseHook_IncludesSessionID(t *testing.T) {
	ensureConfigLoaded(t)

	tempDir := t.TempDir()
	tb := newShellToolbox(t, tempDir)
	hookEngine := middleware.NewHookEngine(nil)
	capPost := &captureHook{event: hookservice.EventPostToolUse, name: "capture-post"}
	capFail := &captureHook{event: hookservice.EventPostToolUseFailure, name: "capture-post-fail"}
	hookEngine.RegisterProgrammaticHook(capPost)
	hookEngine.RegisterProgrammaticHook(capFail)

	ts := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          tb,
		EventHandler:     func(_ event.StreamEvent) {},
		RecoveryStrategy: NewToolRecoveryStrategy(nil),
	})
	ts.SetHookEngine(hookEngine)

	const wantSessionID = "session-post-456"
	ctx := context.WithValue(
		context.Background(),
		interfaces.TurnContextKey{},
		interfaces.TurnContext{SessionID: wantSessionID},
	)

	// 成功路径 → PostToolUse
	if _, err := ts.ExecuteParallel(ctx, []ToolToExecute{{
		ID:         "post-hook-success",
		Name:       "run_shell_command",
		Parameters: map[string]interface{}{"command": "ls"},
	}}); err != nil {
		t.Fatalf("ExecuteParallel(success): %v", err)
	}
	if _, params := capPost.snapshot(); params["session_id"] != wantSessionID {
		t.Fatalf("PostToolUse session_id = %v, want %q", params["session_id"], wantSessionID)
	}

	// 失败路径 → PostToolUseFailure
	// Use an invalid command that will fail
	if _, err := ts.ExecuteParallel(ctx, []ToolToExecute{{
		ID:         "post-hook-failure",
		Name:       "run_shell_command",
		Parameters: map[string]interface{}{"command": "exit 1"},
	}}); err != nil {
		// ExecuteParallel doesn't return error, failure is in result
		_ = err
	}
	if _, params := capFail.snapshot(); params["session_id"] != wantSessionID {
		t.Fatalf("PostToolUseFailure session_id = %v, want %q", params["session_id"], wantSessionID)
	}
}
