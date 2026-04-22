package agent

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

// recoverTool is a test tool that succeeds, used to simulate recovery after failures.
type recoverTool struct{}

func (t *recoverTool) Name() string        { return "recover_tool" }
func (t *recoverTool) Description() string { return "A tool that succeeds" }
func (t *recoverTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryDevelopment
}
func (t *recoverTool) RequiresConfirmation() bool { return false }
func (t *recoverTool) ConcurrencySafe() bool      { return true }
func (t *recoverTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema("recover tool", map[string]*interfaces.PropertySchema{}, nil)
}
func (t *recoverTool) Execute(_ context.Context, _ map[string]interface{}) (*interfaces.ToolResult, error) {
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  "Tool recover_tool succeeded",
		UserContent: "Tool recover_tool succeeded",
	}, nil
}

// TestTurn_RecoverAfterConsecutiveToolFailuresDoesNotTriggerLowGain is the
// end-to-end regression test for the diminishing-returns pollution bug.
//
// Scenario:
// 1. Three consecutive tool failures (always_fail_tool) accumulate low token gain samples
// 2. One successful recovery (recover_tool) resets ConsecutiveErrors and clears history
// 3. Final text response (implicit completion)
//
// Expected: Task completes successfully without triggering diminishing-returns termination
// Bug (before fix): Low-gain samples from error recovery period pollute history,
//                   causing hasDiminishingReturns() to trigger after recovery.
func TestTurn_RecoverAfterConsecutiveToolFailuresDoesNotTriggerLowGain(t *testing.T) {
	tempDir, tb, scheduler := setupBestEffortTurn(t)

	// Register the recovery tool
	if err := tb.Register(&recoverTool{}); err != nil {
		t.Fatalf("Register recover_tool: %v", err)
	}

	// LLM sequence:
	// - 3 rounds of always_fail_tool (error recovery period)
	// - 1 round of recover_tool (successful recovery)
	// - 1 pure text response (implicit completion)
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
			// Recovery: successful tool call
			{{Type: event.EventTypeContent, ToolCalls: []*tools.ToolCall{
				{ID: "call_r1", Name: "recover_tool", Arguments: map[string]interface{}{}},
			}}},
			// Final response: pure text without tool calls → implicit completion
			{{Type: event.EventTypeContent, Content: "Task completed successfully after recovery."}},
		},
	}

	turn := NewTurn("test_low_gain_recovery", &TurnConfig{
		WorkingDir:    tempDir,
		Toolbox:       tb,
		LLMClient:     fakeClient,
		Tools:         tb.List(),
		ToolScheduler: scheduler,
	})

	// Enable diminishing-returns detection with aggressive thresholds
	// to ensure the bug would trigger if not fixed
	turn.CompletionCriteria.DiminishingReturnsEnabled = true
	turn.CompletionCriteria.DiminishingReturnsWindow = 3
	turn.CompletionCriteria.DiminishingReturnsMinGain = 500

	// Set high error threshold to allow error recovery
	turn.CompletionCriteria.ErrorThreshold = 10

	// Execute the turn
	if err := turn.Execute(context.Background()); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Verify task completed successfully
	if !turn.CompletionCriteria.TaskCompleted {
		t.Fatalf("expected TaskCompleted=true, got false")
	}

	// Verify ConsecutiveErrors was reset after recovery
	if turn.CompletionCriteria.ConsecutiveErrors != 0 {
		t.Fatalf("expected ConsecutiveErrors=0 after recovery, got %d",
			turn.CompletionCriteria.ConsecutiveErrors)
	}

	// Verify the turn completed all iterations (should be at least 5)
	// If diminishing-returns was incorrectly triggered, the turn would
	// have terminated early (around iteration 3-4)
	if turn.CompletionCriteria.CurrentIteration < 5 {
		t.Fatalf("expected at least 5 iterations (3 failures + 1 recovery + 1 completion), got %d",
			turn.CompletionCriteria.CurrentIteration)
	}
}
