package tview

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/ui/spinnerverbs"
)

// AgentState represents the high-level agent execution state
type AgentState string

const (
	// AgentStateIdle Agent is idle, waiting for input
	AgentStateIdle AgentState = "idle" // Agent is idle, waiting for input
	// AgentStateProcessing Processing user input
	AgentStateProcessing AgentState = "processing" // Processing user input
	// AgentStateThinking LLM is generating response
	AgentStateThinking AgentState = "thinking"
	// AgentStateToolExecution Executing tools
	AgentStateToolExecution AgentState = "tool_execution"
	// AgentStateWaitingApproval Waiting for user approval
	AgentStateWaitingApproval AgentState = "waiting_approval"
	// AgentStateError Error state
	AgentStateError AgentState = "error"
	// AgentStateCompleted Task completed
	AgentStateCompleted AgentState = "completed"
)

// UIState represents the UI-specific state for rendering
type UIState struct {
	AgentState      AgentState
	CurrentActivity string
	ToolName        string
	Progress        float64 // 0.0 to 1.0
	TokenStats      *event.TokenStats
	ErrorMessage    string
	LastUpdate      time.Time
}

// StateTransition represents a state transition with context
type StateTransition struct {
	From      AgentState
	To        AgentState
	Trigger   string // What triggered the transition
	Timestamp time.Time
	Context   map[string]interface{}
}

// StateManager manages the agent's state and UI state synchronization
type StateManager struct {
	mu              sync.RWMutex
	currentState    UIState
	transitionLog   []StateTransition
	maxLogSize      int
	animationFrame  int
	animationTicker *time.Ticker
	updateCallback  func(UIState)
	callbackMu      sync.Mutex
	quit            chan struct{}
	stopOnce        sync.Once
	spinnerVerbs    []string // Effective list of spinner verbs
	selectedVerb    string   // verb selected at start of active cycle
}

// NewStateManager creates a new state manager with optional spinner verbs configuration
func NewStateManager(cfg *config.Config) *StateManager {
	var spinnerVerbsCfg *config.SpinnerVerbsConfig
	if cfg != nil {
		spinnerVerbsCfg = cfg.SpinnerVerbs
	}
	sm := &StateManager{
		currentState: UIState{
			AgentState: AgentStateIdle,
			LastUpdate: time.Now(),
		},
		maxLogSize:   100,
		quit:         make(chan struct{}),
		spinnerVerbs: spinnerverbs.EffectiveVerbs(spinnerVerbsCfg),
	}
	sm.startAnimation()
	return sm
}

// SetUpdateCallback sets the callback function for state updates
func (sm *StateManager) SetUpdateCallback(callback func(UIState)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.updateCallback = callback
}

// GetCurrentState returns the current UI state (thread-safe)
func (sm *StateManager) GetCurrentState() UIState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentState
}

// TransitionTo transitions to a new agent state with context
func (sm *StateManager) TransitionTo(newState AgentState, activity string, context map[string]interface{}) {
	sm.mu.Lock()

	oldState := sm.currentState.AgentState

	// Log the transition
	transition := StateTransition{
		From:      oldState,
		To:        newState,
		Trigger:   activity,
		Timestamp: time.Now(),
		Context:   context,
	}
	sm.addTransitionLog(transition)

	// Update state
	sm.currentState.AgentState = newState
	sm.currentState.CurrentActivity = activity
	sm.currentState.LastUpdate = time.Now()

	// Select or clear the spinner verb based on state
	switch newState {
	case AgentStateProcessing, AgentStateThinking, AgentStateToolExecution:
		if sm.selectedVerb == "" {
			sm.selectedVerb = spinnerverbs.RandomVerb(sm.spinnerVerbs)
		}
	default:
		sm.selectedVerb = ""
	}

	// Extract context information
	if toolName, ok := context["tool_name"].(string); ok {
		sm.currentState.ToolName = toolName
	}
	if progress, ok := context["progress"].(float64); ok {
		sm.currentState.Progress = progress
	}
	if errorMsg, ok := context["error"].(string); ok {
		sm.currentState.ErrorMessage = errorMsg
	}

	// Capture callback and state snapshot, then unlock before invoking callback
	cb := sm.updateCallback
	stateCopy := sm.currentState
	sm.mu.Unlock()

	if cb != nil {
		sm.invokeCallback(cb, stateCopy)
	}
}

// UpdateTokenStats updates the token statistics
func (sm *StateManager) UpdateTokenStats(stats *event.TokenStats) {
	sm.mu.Lock()

	sm.currentState.TokenStats = stats
	sm.currentState.LastUpdate = time.Now()

	cb := sm.updateCallback
	stateCopy := sm.currentState
	sm.mu.Unlock()

	if cb != nil {
		sm.invokeCallback(cb, stateCopy)
	}
}

// SetError sets the error state with message
func (sm *StateManager) SetError(errorMsg string) {
	sm.TransitionTo(AgentStateError, "错误", map[string]interface{}{
		"error": errorMsg,
	})
}

// SetIdle sets the agent to idle state
func (sm *StateManager) SetIdle() {
	sm.TransitionTo(AgentStateIdle, "等待用户输入", nil)
}

// SetProcessing sets the agent to processing state
func (sm *StateManager) SetProcessing(activity string) {
	if activity == "" {
		activity = "处理用户输入"
	}
	sm.TransitionTo(AgentStateProcessing, activity, nil)
}

// SetThinking sets the agent to thinking state
func (sm *StateManager) SetThinking(activity string) {
	if activity == "" {
		activity = "AI正在思考"
	}
	sm.TransitionTo(AgentStateThinking, activity, nil)
}

// SetToolExecution sets the agent to tool execution state
func (sm *StateManager) SetToolExecution(toolName string, activity string) {
	if activity == "" {
		activity = fmt.Sprintf("执行工具: %s", toolName)
	}
	sm.TransitionTo(AgentStateToolExecution, activity, map[string]interface{}{
		"tool_name": toolName,
	})
}

// SetWaitingApproval sets the agent to waiting for approval state
func (sm *StateManager) SetWaitingApproval(toolName string) {
	activity := fmt.Sprintf("等待用户批准执行工具: %s", toolName)
	sm.TransitionTo(AgentStateWaitingApproval, activity, map[string]interface{}{
		"tool_name": toolName,
	})
}

// SetCompleted sets the agent to completed state
func (sm *StateManager) SetCompleted(activity string) {
	if activity == "" {
		activity = "任务完成"
	}
	sm.TransitionTo(AgentStateCompleted, activity, nil)
}

// IsAnimatedState returns true if the current state should show animation
func (sm *StateManager) IsAnimatedState() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	switch sm.currentState.AgentState {
	case AgentStateProcessing, AgentStateThinking, AgentStateToolExecution:
		return true
	default:
		return false
	}
}

// GetAnimationFrame returns the current animation frame
func (sm *StateManager) GetAnimationFrame() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.animationFrame
}

// FormatStatusText formats the current state into a status bar text
func (sm *StateManager) FormatStatusText() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var status strings.Builder

	// Animation characters for animated states
	animationChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	animChar := animationChars[sm.animationFrame%len(animationChars)]

	// Get spinner verb for current cycle
	spinnerVerb := sm.selectedVerb

	// Format based on agent state
	switch sm.currentState.AgentState {
	case AgentStateIdle:
		status.WriteString("[green]🟢 空闲[white]")
	case AgentStateProcessing:
		if spinnerVerb != "" {
			fmt.Fprintf(&status, "[yellow]%s %s 处理输入中...[white]", animChar, spinnerVerb)
		} else {
			fmt.Fprintf(&status, "[yellow]%s 处理输入中...[white]", animChar)
		}
	case AgentStateThinking:
		if spinnerVerb != "" {
			fmt.Fprintf(&status, "[yellow]%s %s 思考中...[white]", animChar, spinnerVerb)
		} else {
			fmt.Fprintf(&status, "[yellow]%s 思考中...[white]", animChar)
		}
	case AgentStateToolExecution:
		if spinnerVerb != "" {
			fmt.Fprintf(&status, "[orange]%s %s 执行工具中...[white]", animChar, spinnerVerb)
		} else {
			fmt.Fprintf(&status, "[orange]%s 执行工具中...[white]", animChar)
		}
	case AgentStateWaitingApproval:
		status.WriteString("[magenta]🤔 等待用户批准[white] | [yellow]使用↑/↓选择，回车确认,ESC取消[white]")
	case AgentStateError:
		status.WriteString("[red]❌ 错误[white]")
	case AgentStateCompleted:
		status.WriteString("[green]✅ 完成[white]")
	}

	// Add current activity if available
	if sm.currentState.CurrentActivity != "" {
		fmt.Fprintf(&status, " | [white]%s[white]", sm.currentState.CurrentActivity)
	}

	// Add token statistics (输入/输出/总计细分)
	if sm.currentState.TokenStats != nil {
		in := sm.formatTokens(sm.currentState.TokenStats.InputTokens)
		out := sm.formatTokens(sm.currentState.TokenStats.OutputTokens)
		total := sm.formatTokens(sm.currentState.TokenStats.TotalTokens)
		fmt.Fprintf(&status, " | [cyan]令牌: 输入 %s | 输出 %s | 总计 %s[white]", in, out, total)
		if sm.currentState.TokenStats.PeakTokensPerSecond > 0 {
			fmt.Fprintf(&status, " | [cyan]峰值速率: %.2f t/s[white]", sm.currentState.TokenStats.PeakTokensPerSecond)
		}
	}

	return status.String()
}

// GetTransitionHistory returns the recent state transitions
func (sm *StateManager) GetTransitionHistory() []StateTransition {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Return a copy to avoid race conditions
	history := make([]StateTransition, len(sm.transitionLog))
	copy(history, sm.transitionLog)
	return history
}

// startAnimation starts the animation ticker
func (sm *StateManager) startAnimation() {
	sm.animationTicker = time.NewTicker(50 * time.Millisecond) // 20 FPS
	go func() {
		ticker := sm.animationTicker
		if ticker == nil {
			return
		}
		for {
			select {
			case <-ticker.C:
				// Update animation frame under write lock and capture state/callback
				sm.mu.Lock()
				sm.animationFrame++

				// Compute animated state without nested locking
				var isAnimated bool
				switch sm.currentState.AgentState {
				case AgentStateProcessing, AgentStateThinking, AgentStateToolExecution:
					isAnimated = true
				default:
					isAnimated = false
				}

				cb := sm.updateCallback
				stateCopy := sm.currentState
				sm.mu.Unlock()

				if isAnimated && cb != nil {
					sm.invokeCallback(cb, stateCopy)
				}
			case <-sm.quit:
				return
			}
		}
	}()
}

// Stop stops the state manager and cleans up resources
func (sm *StateManager) Stop() {
	sm.stopOnce.Do(func() {
		close(sm.quit)
		sm.mu.Lock()
		ticker := sm.animationTicker
		sm.mu.Unlock()
		if ticker != nil {
			ticker.Stop()
		}
	})
}

func (sm *StateManager) invokeCallback(cb func(UIState), state UIState) {
	// Serialize callbacks because they update tview primitives. Callbacks must
	// not call back into StateManager while running.
	sm.callbackMu.Lock()
	defer sm.callbackMu.Unlock()
	cb(state)
}

// addTransitionLog adds a transition to the log (internal, assumes lock held)
func (sm *StateManager) addTransitionLog(transition StateTransition) {
	sm.transitionLog = append(sm.transitionLog, transition)

	// Keep log size under control
	if len(sm.transitionLog) > sm.maxLogSize {
		sm.transitionLog = sm.transitionLog[len(sm.transitionLog)-sm.maxLogSize:]
	}
}

// formatTokens formats token count for display
func (sm *StateManager) formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1000000)
}
