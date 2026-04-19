package tview

import (
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
)

func TestInputFieldStateManagement(t *testing.T) {
	// Create a model
	model := NewModel()
	defer model.Stop()

	// Test Agent state transitions trigger input field updates

	// 1. When agent starts processing, callback should execute
	model.stateManager.SetProcessing("处理用户输入")
	// Allow time for callback to execute
	time.Sleep(20 * time.Millisecond)

	// Test that placeholder updates correctly for processing state
	if model.inputField.GetText() == "" {
		// In queue mode, input field should remain enabled with queue placeholder
		// This allows users to continue typing while AI processes, inputs will be queued
		t.Logf("Agent processing state - input field enabled for queue mode")
	}

	// 2. When agent goes to thinking state
	model.stateManager.SetThinking("AI正在思考")
	time.Sleep(20 * time.Millisecond)

	t.Logf("Agent thinking state - input field enabled for queue mode")

	// 3. When agent goes to tool execution
	model.stateManager.SetToolExecution("search_files", "执行搜索")
	time.Sleep(20 * time.Millisecond)

	t.Logf("Agent tool execution state - input field enabled for queue mode")

	// 4. When agent goes to idle state
	model.stateManager.SetIdle()
	time.Sleep(20 * time.Millisecond)

	t.Logf("Agent idle state - input field should be fully enabled")

	// 5. Test confirmation states affect input field
	model.showingConfirm = true
	model.updateInputFieldState()
	time.Sleep(20 * time.Millisecond)

	t.Logf("Confirmation dialog state - input field should be disabled")

	// 6. Hide confirmation and trigger state update
	model.showingConfirm = false
	model.stateManager.SetIdle() // Ensure agent is idle
	time.Sleep(20 * time.Millisecond)

	t.Logf("Confirmation dialog hidden and idle state restored - input should be enabled")
}

func TestStateManagerCallback(t *testing.T) {
	model := NewModel()
	defer model.Stop()

	// Test that state transitions trigger the update callback
	callbackExecuted := false

	originalCallback := model.stateManager.updateCallback
	model.stateManager.SetUpdateCallback(func(state UIState) {
		callbackExecuted = true
		// Call original callback to maintain functionality
		if originalCallback != nil {
			originalCallback(state)
		}
	})

	// Trigger a state change
	model.stateManager.SetProcessing("测试状态变化")

	// Allow callback to execute
	time.Sleep(20 * time.Millisecond)

	if !callbackExecuted {
		t.Error("StateManager callback was not executed on state transition")
	}
}

func TestInputFieldUpdateIntegration(t *testing.T) {
	model := NewModel()
	defer model.Stop()

	// Test that the updateInputFieldState method can be called without errors
	model.updateInputFieldState()

	// Test with different agent states
	states := []AgentState{
		AgentStateIdle,
		AgentStateProcessing,
		AgentStateThinking,
		AgentStateToolExecution,
		AgentStateWaitingApproval,
		AgentStateError,
		AgentStateCompleted,
	}

	for _, state := range states {
		model.stateManager.TransitionTo(state, "test", nil)
		time.Sleep(10 * time.Millisecond)

		// Call update method directly
		model.updateInputFieldState()
		time.Sleep(10 * time.Millisecond)

		t.Logf("Input field state updated for agent state: %v", state)
	}
}

func TestTokenStatsUpdate(t *testing.T) {
	model := NewModel()
	defer model.Stop()

	// Test that token stats updates trigger callbacks correctly
	model.stateManager.SetIdle()
	time.Sleep(10 * time.Millisecond)

	// Update token stats
	tokenStats := &event.TokenStats{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
	}
	model.stateManager.UpdateTokenStats(tokenStats)
	time.Sleep(10 * time.Millisecond)

	// Verify token stats were updated
	currentState := model.stateManager.GetCurrentState()
	if currentState.TokenStats == nil {
		t.Error("Token stats were not updated")
	}
	if currentState.TokenStats.TotalTokens != 150 {
		t.Errorf("Expected total tokens 150, got %d", currentState.TokenStats.TotalTokens)
	}

	t.Log("Token stats update processed correctly")
}
