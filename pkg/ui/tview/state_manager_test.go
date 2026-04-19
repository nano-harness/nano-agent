package tview

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
)

func TestStateManager(t *testing.T) {
	// Create a new state manager
	sm := NewStateManager()
	defer sm.Stop()

	// Test initial state
	initialState := sm.GetCurrentState()
	if initialState.AgentState != AgentStateIdle {
		t.Errorf("Expected initial state to be Idle, got %v", initialState.AgentState)
	}

	// Test state transition
	sm.TransitionTo(AgentStateThinking, "正在思考问题", nil)
	currentState := sm.GetCurrentState()
	if currentState.AgentState != AgentStateThinking {
		t.Errorf("Expected state to be Thinking, got %v", currentState.AgentState)
	}
	if currentState.CurrentActivity != "正在思考问题" {
		t.Errorf("Expected activity to be '正在思考问题', got %v", currentState.CurrentActivity)
	}

	// Test token stats update
	tokenStats := &event.TokenStats{
		InputTokens:  100,
		OutputTokens: 50,
	}
	sm.UpdateTokenStats(tokenStats)
	updatedState := sm.GetCurrentState()
	if updatedState.TokenStats == nil {
		t.Error("Expected token stats to be set")
	}
	if updatedState.TokenStats.InputTokens != 100 {
		t.Errorf("Expected input tokens to be 100, got %d", updatedState.TokenStats.InputTokens)
	}

	// Test status text formatting
	statusText := sm.FormatStatusText()
	if statusText == "" {
		t.Error("Expected non-empty status text")
	}
	t.Logf("Status text: %s", statusText)

	// Note: State history functionality can be added later if needed

	// Test callback mechanism
	callbackCalled := false
	sm.SetUpdateCallback(func(state UIState) {
		callbackCalled = true
	})
	sm.TransitionTo(AgentStateProcessing, "处理请求", nil)
	if !callbackCalled {
		t.Error("Expected callback to be called")
	}
}

func TestStateTransitions(t *testing.T) {
	sm := NewStateManager()
	defer sm.Stop()

	// Test all state transitions
	states := []struct {
		state    AgentState
		activity string
	}{
		{AgentStateIdle, "空闲状态"},
		{AgentStateProcessing, "处理中"},
		{AgentStateThinking, "思考中"},
		{AgentStateToolExecution, "执行工具"},
		{AgentStateError, "错误状态"},
	}

	for _, test := range states {
		sm.TransitionTo(test.state, test.activity, nil)
		currentState := sm.GetCurrentState()
		if currentState.AgentState != test.state {
			t.Errorf("Expected state %v, got %v", test.state, currentState.AgentState)
		}
		if currentState.CurrentActivity != test.activity {
			t.Errorf("Expected activity '%s', got '%s'", test.activity, currentState.CurrentActivity)
		}
	}
}

func TestFormatStatusText(t *testing.T) {
	sm := NewStateManager()
	defer sm.Stop()

	// Test different states and their formatting
	tests := []struct {
		state    AgentState
		activity string
		expected string
	}{
		{AgentStateIdle, "", "空闲"},
		{AgentStateThinking, "分析问题", "思考中"},
		{AgentStateProcessing, "生成回复", "处理输入中"},
		{AgentStateToolExecution, "执行搜索", "执行工具中"},
		{AgentStateError, "连接失败", "错误"},
	}

	for _, test := range tests {
		sm.TransitionTo(test.state, test.activity, nil)
		statusText := sm.FormatStatusText()
		if !contains(statusText, test.expected) {
			t.Errorf("Expected status text to contain '%s', got '%s'", test.expected, statusText)
		}
		// Also check if activity is included when provided
		if test.activity != "" && !contains(statusText, test.activity) {
			t.Errorf("Expected status text to contain activity '%s', got '%s'", test.activity, statusText)
		}
	}
}

func TestTokenStatsFormatting(t *testing.T) {
	sm := NewStateManager()
	defer sm.Stop()

	// Test with token stats
	stats := &event.TokenStats{
		InputTokens:  1000,
		OutputTokens: 1250,
		TotalTokens:  2250,
	}
	sm.UpdateTokenStats(stats)

	statusText := sm.FormatStatusText()
	t.Logf("Status text with tokens: '%s'", statusText)

	// Check that status text contains token information
	if !contains(statusText, "令牌") {
		t.Errorf("Expected status text to contain '令牌', got '%s'", statusText)
	}

	// Check breakdown labels are present
	if !contains(statusText, "输入") || !contains(statusText, "输出") || !contains(statusText, "总计") {
		t.Errorf("Expected status text to contain 输入/输出/总计 breakdown, got '%s'", statusText)
	}

	// Check for total token count - should not be 0
	if contains(statusText, "令牌: 0") {
		t.Errorf("Expected status text to contain non-zero tokens, got '%s'", statusText)
	}

	// Check that it contains formatted total tokens (2250 -> 2.2K)
	if !contains(statusText, "2.2K") && !contains(statusText, "2250") {
		t.Errorf("Expected status text to contain formatted total tokens, got '%s'", statusText)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			containsInMiddle(s, substr))))
}

func containsInMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
