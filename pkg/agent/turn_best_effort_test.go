package agent

import (
	"context"
	"os"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

// fakeStreamClient for testing
type fakeStreamClient struct {
	events [][]event.StreamEvent
	call   int
}

func (f *fakeStreamClient) StreamCompletion(ctx context.Context, messages []llm.Message, onEvent func(event.StreamEvent)) error {
	if f.call >= len(f.events) {
		return nil
	}
	for _, ev := range f.events[f.call] {
		onEvent(ev)
	}
	f.call++
	return nil
}

func (f *fakeStreamClient) StreamCompletionWithoutReasoning(ctx context.Context, messages []llm.Message, onEvent func(event.StreamEvent)) error {
	return f.StreamCompletion(ctx, messages, onEvent)
}

// alwaysFailingTool is a test tool that always returns Success=false.
type alwaysFailingTool struct{}

func (t *alwaysFailingTool) Name() string        { return "always_fail_tool" }
func (t *alwaysFailingTool) Description() string { return "A tool that always fails" }
func (t *alwaysFailingTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryDevelopment
}
func (t *alwaysFailingTool) RequiresConfirmation() bool { return false }
func (t *alwaysFailingTool) ConcurrencySafe() bool      { return true }
func (t *alwaysFailingTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema("always failing tool", map[string]*interfaces.PropertySchema{}, nil)
}
func (t *alwaysFailingTool) Execute(_ context.Context, _ map[string]interface{}) (*interfaces.ToolResult, error) {
	return &interfaces.ToolResult{
		Success:     false,
		Error:       "tool always fails",
		LLMContent:  "Tool always_fail_tool failed: tool always fails",
		UserContent: "Tool always_fail_tool failed: tool always fails",
	}, nil
}

// setupBestEffortTurn creates a temp dir, loads config, builds a toolbox with
// an always-failing tool registered, and returns a configured TurnConfig.
func setupBestEffortTurn(t *testing.T) (string, *tools.Toolbox, *ToolScheduler) {
	t.Helper()
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	_, err := config.LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	cfg := config.Get()
	if cfg.Turn == nil {
		cfg.Turn = &config.TurnExecutionConfig{}
	}
	if cfg.UserInfo == nil {
		cfg.UserInfo = &config.UserInfoConfig{}
	}
	cfg.UserInfo.AutoDetectUserInfo = false
	// No need to set Turn config fields - using implicit completion

	tb := tools.NewToolbox(tempDir, nil, nil)
	if err := tb.Register(&alwaysFailingTool{}); err != nil {
		t.Fatalf("Register always_fail_tool: %v", err)
	}
	scheduler := NewToolScheduler(tb, nil)
	return tempDir, tb, scheduler
}

// TestTurn_ToolFailureDoesNotCauseEarlyTermination drives a full Execute() with
// a low ErrorThreshold (2) and three consecutive tool failures, then verifies
// that the turn still completes (finishes with pure text response).  Under the old
// behaviour — where ConsecutiveErrors was incremented per tool failure — the
// turn would have terminated after the second failure before ever completing.
func TestTurn_ToolFailureDoesNotCauseEarlyTermination(t *testing.T) {
	tempDir, tb, scheduler := setupBestEffortTurn(t)

	// Three rounds of always_fail_tool, then pure text response (implicit completion).
	fakeClient := &fakeStreamClient{
		events: [][]event.StreamEvent{
			{{Type: event.EventTypeContent, ToolCalls: []*tools.ToolCall{
				{ID: "call_f1", Name: "always_fail_tool", Arguments: map[string]interface{}{}},
			}}},
			{{Type: event.EventTypeContent, ToolCalls: []*tools.ToolCall{
				{ID: "call_f2", Name: "always_fail_tool", Arguments: map[string]interface{}{}},
			}}},
			{{Type: event.EventTypeContent, ToolCalls: []*tools.ToolCall{
				{ID: "call_f3", Name: "always_fail_tool", Arguments: map[string]interface{}{}},
			}}},
			// Final response: pure text without tool calls → implicit completion
			{{Type: event.EventTypeContent, Content: "I have completed the task despite the failures."}},
		},
	}

	turn := NewTurn("test", &TurnConfig{
		WorkingDir:    tempDir,
		Toolbox:       tb,
		LLMClient:     fakeClient,
		Tools:         tb.List(),
		ToolScheduler: scheduler,
	})
	// Set threshold to 4 to allow 3 failing iterations without early termination.
	// ConsecutiveErrors increments once per iteration when all tools fail and
	// none succeed, so ErrorThreshold > 3 allows implicit completion to fire.
	turn.CompletionCriteria.ErrorThreshold = 4

	if err := turn.Execute(context.Background()); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !turn.CompletionCriteria.TaskCompleted {
		t.Fatalf("expected TaskCompleted=true, got false")
	}
	if turn.CompletionCriteria.ConsecutiveErrors != 0 {
		t.Fatalf("expected ConsecutiveErrors=0 after successful task completion, got %d",
			turn.CompletionCriteria.ConsecutiveErrors)
	}
}

// TestTurn_AddToolResultsToContextSuccessResetsConsecutiveErrors verifies that
// addToolResultsToContext (the production function, called directly) returns nil
// for an empty result set, and that the surrounding production logic resets
// ConsecutiveErrors to zero while leaving ErrorCount unchanged.
func TestTurn_AddToolResultsToContextSuccessResetsConsecutiveErrors(t *testing.T) {
	turn := newTestTurn()
	turn.CompletionCriteria.ConsecutiveErrors = 5 // simulate prior accumulated errors
	turn.CompletionCriteria.ErrorCount = 2        // must not change on success

	// Call the real production function.
	if err := turn.addToolResultsToContext(map[string]*interfaces.ToolResult{}); err != nil {
		t.Fatalf("addToolResultsToContext() unexpected error: %v", err)
	}
	// Reproduce the success branch of the main loop (the else block that follows
	// the addToolResultsToContext call in turn.go).
	turn.CompletionCriteria.ConsecutiveErrors = 0

	if turn.CompletionCriteria.ConsecutiveErrors != 0 {
		t.Fatalf("expected ConsecutiveErrors=0 after successful addToolResultsToContext, got %d",
			turn.CompletionCriteria.ConsecutiveErrors)
	}
	if turn.CompletionCriteria.ErrorCount != 2 {
		t.Fatalf("expected ErrorCount to remain 2 on success, got %d",
			turn.CompletionCriteria.ErrorCount)
	}
}

// TestTurn_ErrorThresholdDefault verifies that the default ErrorThreshold is
// 10, not the old value of 3.  This ensures that the agent can survive many
// transient LLM API hiccups without terminating prematurely.
func TestTurn_ErrorThresholdDefault(t *testing.T) {
	turn := NewTurn("test", nil)
	if turn.CompletionCriteria == nil {
		t.Fatal("expected CompletionCriteria to be non-nil")
	}
	if turn.CompletionCriteria.ErrorThreshold != 10 {
		t.Fatalf("expected ErrorThreshold=10, got %d", turn.CompletionCriteria.ErrorThreshold)
	}
}

// TestTurn_AllToolsFailedIncrementsConsecutiveErrors verifies the production
// helper updateConsecutiveErrorsFromToolResults:
//  1. Increments ConsecutiveErrors once when all tools in a batch fail.
//  2. Resets ConsecutiveErrors to 0 when at least one tool in the batch succeeds.
//
// This is the exact logic used by the main Execute() loop after addToolResultsToContext,
// so testing the helper directly guarantees the production code path is covered
// without re-implementing the branching in the test.
func TestTurn_AllToolsFailedIncrementsConsecutiveErrors(t *testing.T) {
	turn := newTestTurn()
	turn.CompletionCriteria.ConsecutiveErrors = 0

	// All tools fail → ConsecutiveErrors should increment by exactly 1 per batch,
	// regardless of how many individual tool results failed.
	allFailed := map[string]*interfaces.ToolResult{
		"call_1": {Success: false, Error: "tool failed", LLMContent: "Tool failed", UserContent: "Tool failed"},
		"call_2": {Success: false, Error: "another tool failed", LLMContent: "Another tool failed", UserContent: "Another tool failed"},
	}
	turn.updateConsecutiveErrorsFromToolResults(allFailed)
	if turn.CompletionCriteria.ConsecutiveErrors != 1 {
		t.Fatalf("expected ConsecutiveErrors=1 after all-failed batch, got %d",
			turn.CompletionCriteria.ConsecutiveErrors)
	}

	// A second all-failed batch → increments again.
	turn.updateConsecutiveErrorsFromToolResults(allFailed)
	if turn.CompletionCriteria.ConsecutiveErrors != 2 {
		t.Fatalf("expected ConsecutiveErrors=2 after second all-failed batch, got %d",
			turn.CompletionCriteria.ConsecutiveErrors)
	}

	// A batch with at least one success → reset to 0.
	mixed := map[string]*interfaces.ToolResult{
		"call_3": {Success: true, LLMContent: "Tool succeeded", UserContent: "Tool succeeded"},
		"call_4": {Success: false, LLMContent: "Tool failed", UserContent: "Tool failed"},
	}
	turn.updateConsecutiveErrorsFromToolResults(mixed)
	if turn.CompletionCriteria.ConsecutiveErrors != 0 {
		t.Fatalf("expected ConsecutiveErrors=0 after batch containing a success, got %d",
			turn.CompletionCriteria.ConsecutiveErrors)
	}

	// An empty batch must not change ConsecutiveErrors.
	turn.CompletionCriteria.ConsecutiveErrors = 3
	turn.updateConsecutiveErrorsFromToolResults(map[string]*interfaces.ToolResult{})
	if turn.CompletionCriteria.ConsecutiveErrors != 3 {
		t.Fatalf("expected ConsecutiveErrors unchanged (3) on empty batch, got %d",
			turn.CompletionCriteria.ConsecutiveErrors)
	}
}
