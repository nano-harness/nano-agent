// Package tui provides the terminal user interface for nano-agent.
package tview

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/slash"
	uirender "github.com/nano-harness/nano-agent/pkg/ui/render"
	"github.com/atotto/clipboard"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

//go:embed assets/*.mp3
var soundsFS embed.FS

// MessageType represents the type of message
type MessageType string

// Supported message types.
const (
	MessageTypeUser         MessageType = "user"
	MessageTypeLLMResponse  MessageType = "llm_response"
	MessageTypeToolUse      MessageType = "tool_use"
	MessageTypeSystem       MessageType = "system"
	MessageTypeError        MessageType = "error"
	MessageTypeConfirmation MessageType = "confirmation"
	MessageTypeThinking     MessageType = "thinking"
)

const minMarkdownViewportWidth = 20

// MessageInterface defines the common interface for all message types
type MessageInterface interface {
	GetID() string
	GetTimestamp() time.Time
	GetContent() string
	GetType() MessageType
	SetContent(string)
}

// BaseMessage represents the common fields for all message types
type BaseMessage struct {
	ID        string      `json:"id"`
	Timestamp time.Time   `json:"timestamp"`
	Type      MessageType `json:"type"`
}

// GetID returns the message ID
func (b *BaseMessage) GetID() string {
	return b.ID
}

// GetTimestamp returns the message timestamp
func (b *BaseMessage) GetTimestamp() time.Time {
	return b.Timestamp
}

// GetType returns the message type
func (b *BaseMessage) GetType() MessageType {
	return b.Type
}

// UserMessage represents a user input message
type UserMessage struct {
	BaseMessage
	Content string `json:"content"`
}

// GetContent returns the user message content
func (u *UserMessage) GetContent() string {
	return u.Content
}

// SetContent sets the user message content
func (u *UserMessage) SetContent(content string) {
	u.Content = content
}

// LLMResponseMessage represents an LLM response message
type LLMResponseMessage struct {
	BaseMessage
	Content   string `json:"content"`
	Response  string `json:"response"` // For backward compatibility
	Streaming bool   `json:"streaming"`
}

// GetContent returns the LLM response content
func (l *LLMResponseMessage) GetContent() string {
	return l.Content
}

// SetContent sets the LLM response content
func (l *LLMResponseMessage) SetContent(content string) {
	l.Content = content
}

// SystemMessage represents a system message
type SystemMessage struct {
	BaseMessage
	Content string `json:"content"`
}

// GetContent returns the system message content
func (s *SystemMessage) GetContent() string {
	return s.Content
}

// SetContent sets the system message content
func (s *SystemMessage) SetContent(content string) {
	s.Content = content
}

// ToolUseMessage represents a tool usage message
type ToolUseMessage struct {
	BaseMessage
	ToolCallID string                 `json:"tool_call_id"`
	ToolName   string                 `json:"tool_name"`
	Parameters map[string]interface{} `json:"parameters"`
	Result     string                 `json:"result"`
	Error      string                 `json:"error,omitempty"`
	Status     string                 `json:"status"` // "calling", "completed", "error"
}

// GetContent returns the tool use message content (formatted)
func (t *ToolUseMessage) GetContent() string {
	statusIcon := ""
	switch t.Status {
	case "executing":
		statusIcon = "🏃"
	case "success":
		statusIcon = "✅"
	case "error":
		statusIcon = "❌"
	default:
		statusIcon = ""
	}

	var content strings.Builder
	fmt.Fprintf(&content, "%s [yellow]Tool:[-] %s\n", statusIcon, t.ToolName)
	fmt.Fprintf(&content, "   [yellow]Status:[-] %s\n", t.Status)

	if len(t.Parameters) > 0 {
		content.WriteString("   [yellow]Parameters:[-]\n")
		for key, value := range t.Parameters {
			valueStr := fmt.Sprintf("%v", value)
			fmt.Fprintf(&content, "     - %s: %s\n", key, truncate(valueStr, 300))
		}
	}

	if t.Result != "" {
		fmt.Fprintf(&content, "   [yellow]Result:[-] %s\n", truncate(t.Result, 1000))
	}

	if t.Error != "" {
		fmt.Fprintf(&content, "   [red]Error:[-] %s\n", truncate(t.Error, 1000))
	}

	return content.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// SetContent sets the tool use message content
func (t *ToolUseMessage) SetContent(_ string) {
	// Not used for ToolUseMessage; content is derived
}

// ErrorMessage represents an error message
type ErrorMessage struct {
	BaseMessage
	Error     string `json:"error"`
	ErrorType string `json:"error_type"`
}

// GetContent returns the error message content
func (e *ErrorMessage) GetContent() string {
	return fmt.Sprintf("[red]Error (%s):[-] %s", e.ErrorType, e.Error)
}

// SetContent sets the error message content
func (e *ErrorMessage) SetContent(content string) {
	e.Error = content
}

// ConfirmationMessage represents a confirmation message
type ConfirmationMessage struct {
	BaseMessage
	Prompt   string                 `json:"prompt"`
	ToolInfo map[string]interface{} `json:"tool_info"`
	Status   string                 `json:"status"` // "pending", "approved", "rejected"
	Response string                 `json:"response,omitempty"`
}

// GetContent returns the confirmation message content
func (c *ConfirmationMessage) GetContent() string {
	var content strings.Builder
	fmt.Fprintf(&content, "[yellow]Confirmation Required:[-] %s\n", c.Prompt)

	if len(c.ToolInfo) > 0 {
		content.WriteString("[cyan]Tool Information:[-]\n")
		for key, value := range c.ToolInfo {
			fmt.Fprintf(&content, "  - %s: %v\n", key, value)
		}
	}

	if c.Status != "" {
		fmt.Fprintf(&content, "[yellow]Status:[-] %s\n", c.Status)
	}

	if c.Response != "" {
		fmt.Fprintf(&content, "[green]Response:[-] %s\n", c.Response)
	}

	return content.String()
}

// SetContent sets the confirmation message content
func (c *ConfirmationMessage) SetContent(content string) {
	c.Prompt = content
}

// ThinkingMessage represents a reasoning block that can be expanded/collapsed.
type ThinkingMessage struct {
	BaseMessage
	Title     string `json:"title"`
	Reasoning string `json:"reasoning,omitempty"`
	Collapsed bool   `json:"collapsed"`
	Completed bool   `json:"completed"`
}

func NewThinkingMessage(id, title, reasoning string) *ThinkingMessage {
	if strings.TrimSpace(title) == "" {
		title = "🧠 正在思考..."
	}
	return &ThinkingMessage{
		BaseMessage: BaseMessage{
			ID:        id,
			Timestamp: time.Now(),
			Type:      MessageTypeThinking,
		},
		Title:     title,
		Reasoning: reasoning,
		Collapsed: true,
	}
}

func (t *ThinkingMessage) GetContent() string {
	return t.Title
}

func (t *ThinkingMessage) SetContent(content string) {
	t.Title = content
}

// Model manages the terminal UI state and widgets.
type Model struct {
	app        *tview.Application
	chatView   *ChatView
	inputField *InputField
	layout     *tview.Flex
	pages      *tview.Pages
	statusBar  *tview.TextView
	swarmBar   *tview.TextView

	// State
	messages         []MessageInterface
	isLoading        bool //nolint:unused
	activeView       string
	connectionState  string
	connectionDetail string
	swarmRoster      string

	// New state management
	stateManager *StateManager

	// 串行渲染控制
	renderMutex       sync.Mutex
	lastRenderTime    time.Time
	minRenderInterval time.Duration
	// 自适应节流：根据状态更新动态调整批量渲染频率
	normalMinRenderInterval time.Duration
	activeMinRenderInterval time.Duration
	lastStateUpdate         time.Time
	pendingRender           bool
	renderThrottler         *time.Timer
	// Accumulate pending UI updates between renders to avoid dropping updates
	pendingUpdates []func()

	// Confirmation state
	pendingConfirmation *ConfirmationMessage
	confirmationHandler func(bool)
	// allowlistHandler is called (with toolName and params) when the user picks
	// "同意并不再询问".  It is set by root.go before the TUI loop starts.
	allowlistHandler func(toolName string, params map[string]interface{})

	// Confirmation components
	confirmSelect  *tview.List
	confirmInfo    *tview.TextView
	confirmLayout  *tview.Flex
	showingConfirm bool

	// Simple user confirmation component
	userConfirmDialog  *tview.Modal
	userConfirmHandler func(bool)
	showingUserConfirm bool

	// Agent integration
	inputHandler  func(string)
	cancelHandler func() bool
	// newSessionHandler is invoked when the user requests a new session via Ctrl+R.
	newSessionHandler func()

	// Event channel
	eventChan chan func()

	// Track if app is running to prevent QueueUpdateDraw deadlock
	appRunning bool

	// inUIUpdate indicates we are currently inside a UI update callback to avoid nested QueueUpdateDraw
	inUIUpdate int32

	// Global styles
	styles *Styles

	// Terminal mapping: optional rune used as alternate send trigger (set via env NANO_TUI_SHIFT_ENTER_RUNE)
	shiftSendRune rune

	// Whether SIGINT (Ctrl+C) has been ignored during TUI run
	ignoredSigInt bool
	soundMutex    sync.Mutex
	inputStateMu  sync.Mutex

	// Commands view
	commandsList     *tview.List
	commandsPreview  *tview.TextView
	commandsLayout   *tview.Flex
	commandsProvider func() []slash.Command
}

// NewModel creates a TUI model with initialized components and defaults.
func NewModel() *Model {
	m := &Model{
		app:               tview.NewApplication(),
		messages:          make([]MessageInterface, 0),
		activeView:        "chat",
		eventChan:         make(chan func(), 256),
		minRenderInterval: 50 * time.Millisecond, // 最小渲染间隔50ms，限制最大刷新率为20fps
		pendingRender:     false,
	}

	// 初始化自适应节流参数
	m.normalMinRenderInterval = 50 * time.Millisecond
	m.activeMinRenderInterval = 33 * time.Millisecond // 状态更新/动画时更高刷新频率（约30fps）

	// Initialize new state manager
	m.stateManager = NewStateManager()
	m.stateManager.SetUpdateCallback(func(_ UIState) {
		// 标记最近状态更新时间以触发更高频批量渲染
		m.renderMutex.Lock()
		m.lastStateUpdate = time.Now()
		m.renderMutex.Unlock()

		// 统一通过批量渲染队列更新状态栏，确保立即可见
		m.setStatusBarText(m.stateManager.FormatStatusText())

		// 同步更新输入框状态 - 当Agent状态变化时自动同步输入框的启用/禁用状态
		m.updateInputFieldState()
	})

	// Initialize styles and apply default theme
	m.styles = newStyles()
	m.styles.ApplyTheme("dark")
	m.styles.ConfigureTViewStyles()

	// Read optional terminal-mapped rune for alternate send trigger
	if env := os.Getenv("NANO_TUI_SHIFT_ENTER_RUNE"); env != "" {
		// Supported formats:
		// 1) Single character (e.g. set env to the exact rune)
		// 2) Hex codepoint like "U+E000" or "0xE000" or "E000"
		parseHex := func(s string) (rune, bool) {
			s = strings.TrimSpace(strings.ToUpper(s))
			s = strings.TrimPrefix(s, "U+")
			s = strings.TrimPrefix(s, "0X")
			if s == "" {
				return 0, false
			}
			var val uint32
			for _, ch := range s {
				val *= 16
				switch {
				case ch >= '0' && ch <= '9':
					val += uint32(ch - '0')
				case ch >= 'A' && ch <= 'F':
					val += 10 + uint32(ch-'A')
				default:
					return 0, false
				}
			}
			return rune(val), true
		}
		if r, ok := parseHex(env); ok {
			m.shiftSendRune = r
		} else {
			rr := []rune(env)
			if len(rr) == 1 {
				m.shiftSendRune = rr[0]
			}
		}
	}

	// Enable mouse support to allow clicking/scrolling
	m.app.EnableMouse(true)

	// Create components
	m.createComponents()
	m.setupLayout()
	m.setupKeyBindings()

	// 启动时后台预热音频缓存（不阻塞 UI）
	go m.PrewarmSoundCache()

	return m
}

// createComponents creates all tview components
func (m *Model) createComponents() {
	// Create new component-based architecture
	chatViewComponent := NewChatView()
	inputFieldComponent := NewInputField()
	statusBarComponent := NewStatusBar()

	// Extract the underlying tview primitives for compatibility with existing code
	m.chatView = chatViewComponent
	m.inputField = inputFieldComponent
	m.statusBar = statusBarComponent.GetPrimitive().(*tview.TextView)
	m.swarmBar = tview.NewTextView().SetDynamicColors(true)

	// Apply styles to status bar
	if m.styles != nil {
		bg, fg := m.styles.GetStatusBarColors()
		m.statusBar.SetBackgroundColor(bg)
		m.statusBar.SetTextColor(fg)
	}

	// Apply styles to input field (TextArea) to ensure consistent look
	if m.styles != nil {
		_, ifg := m.styles.GetInputBoxColors()
		// Set background and text colors for TextArea
		m.inputField.SetBackgroundColor(tcell.ColorDefault)
		m.inputField.SetTextColor(ifg)
		// Border uses a muted color for subtle separation
		m.inputField.SetBorderColor(m.styles.GetMuted())
	}

	// Set up mouse capture for chat view
	m.chatView.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		// Handle any mouse button press
		if action == tview.MouseLeftClick || action == tview.MouseLeftDown {
			// Set focus to chat view
			m.app.SetFocus(m.chatView.GetPrimitive())
			// Return the event to allow normal processing
			return action, event
		}
		if event.Buttons()&tcell.WheelUp != 0 {
			m.chatView.ScrollBy(-1)
			return action, event
		}
		if event.Buttons()&tcell.WheelDown != 0 {
			m.chatView.ScrollBy(1)
			return action, event
		}
		return action, event
	})

	// Set up mouse capture for input field
	m.inputField.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftClick || action == tview.MouseLeftDown {
			// Only set focus if not already focused
			if m.app.GetFocus() != m.inputField {
				// Use goroutine to avoid potential event conflicts
				go func() {
					m.runUI(func() {
						m.app.SetFocus(m.inputField)
					})
				}()
			}
			// Return the event to allow normal processing
			return action, event
		}
		return action, event
	})

	// NEW: Confirm components (List + Info)
	m.confirmInfo = tview.NewTextView().
		SetDynamicColors(true).
		SetWordWrap(true)
	m.confirmInfo.SetBorder(true).SetTitle("工具信息")

	m.confirmSelect = tview.NewList()
	m.confirmSelect.ShowSecondaryText(false)
	m.confirmSelect.SetBorder(true).SetTitle("⚠️ 危险操作确认（↑↓选择，回车确认）")

	// container for confirm components to place between input and status bar when needed
	m.confirmLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(m.confirmInfo, 0, 3, false).
		AddItem(m.confirmSelect, 0, 2, true)
	m.showingConfirm = false

	// Simple user confirmation dialog
	m.userConfirmDialog = tview.NewModal().
		SetText("请确认您的选择").
		AddButtons([]string{"同意", "拒绝"}).
		SetDoneFunc(func(buttonIndex int, _ string) {
			approved := buttonIndex == 0 // 0 = "同意", 1 = "拒绝"
			m.hideUserConfirmation()
			if m.userConfirmHandler != nil {
				go m.userConfirmHandler(approved)
			}
		})
	m.showingUserConfirm = false

	// Commands components
	m.commandsList = tview.NewList()
	m.commandsList.SetBorder(true).SetTitle("命令列表（Enter插入，ESC返回）")
	m.commandsList.ShowSecondaryText(true)
	m.commandsPreview = tview.NewTextView().SetDynamicColors(true).SetWordWrap(true)
	m.commandsPreview.SetBorder(true).SetTitle("命令预览")
	m.commandsLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(m.commandsList, 0, 2, true).
		AddItem(m.commandsPreview, 0, 3, false)
}

// setupLayout creates the main layout
func (m *Model) setupLayout() {
	// Create pages for different views
	m.pages = tview.NewPages()

	// Chat page layout
	// We set focus manually in the Run() method, so we don't give it focus here.
	chatLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(m.chatView.GetPrimitive(), 0, 1, true).
		AddItem(m.inputField, 3, 0, true).
		AddItem(m.swarmBar, 1, 0, false).
		AddItem(m.statusBar, 1, 0, false)

	// Add pages
	m.pages.AddPage("chat", chatLayout, true, true)

	// Commands page layout
	cmdLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(m.commandsLayout, 0, 1, true).
		AddItem(m.statusBar, 1, 0, false)
	m.pages.AddPage("commands", cmdLayout, true, false)

	m.layout = tview.NewFlex().AddItem(m.pages, 0, 1, true)
}

// setupKeyBindings sets up global key bindings
func (m *Model) setupKeyBindings() {
	// Track last Enter key press time for double-click detection
	var lastEnterTime time.Time
	const doubleClickInterval = 800 * time.Millisecond
	const minHumanInterval = 80 * time.Millisecond // below this, it's almost certainly not human double-press

	// Simple burst detector to identify paste-like input (many events in short time)
	var recentEvents int
	var windowStart time.Time
	const windowSize = 60 * time.Millisecond
	const burstThreshold = 30    // if >=30 events within windowSize, treat as paste burst
	var pasteModeUntil time.Time // while now < pasteModeUntil, disable double-enter sending

	// Track if the last key press was Enter for double-press detection
	var enterPressedOnce bool

	// Reliable paste detection using content change characteristics (works with EventPaste and TextArea clipboard insert)
	var prevText string
	var lastChange time.Time
	m.inputField.SetChangedFunc(func() {
		now := time.Now()
		text := m.inputField.GetText()
		deltaLen := len(text) - len(prevText)
		if deltaLen < 0 {
			// on deletion or replacement, ignore negative delta for paste detection
			deltaLen = 0
		}
		addedNL := strings.Count(text, "\n") - strings.Count(prevText, "\n")

		// Heuristics:
		// - Large insertion (>=32 chars)
		// - Multiple newlines added at once (>=2)
		// - Rapid growth within 20ms with delta >=8 (burst typing/paste)
		if deltaLen >= 32 || addedNL >= 2 || (now.Sub(lastChange) <= 20*time.Millisecond && deltaLen >= 8) {
			pasteModeUntil = now.Add(2 * time.Second)
			enterPressedOnce = false
		}

		prevText = text
		lastChange = now
	})

	// Input field key bindings - support multi-line input
	m.inputField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		now := time.Now()
		// Update burst window
		if windowStart.IsZero() || now.Sub(windowStart) > windowSize {
			windowStart = now
			recentEvents = 0
		}
		recentEvents++
		if recentEvents >= burstThreshold {
			// Enter paste-protect mode briefly
			pasteModeUntil = now.Add(250 * time.Millisecond)
		}

		// Ctrl+C: only handle if input field has focus and has text, otherwise let global handler deal with it
		if event.Key() == tcell.KeyCtrlC {
			// Check if input field has focus and has text
			if m.app.GetFocus() == m.inputField {
				text := m.inputField.GetText()
				if text != "" {
					_ = clipboard.WriteAll(text)
					return nil // consume the event
				}
			}
			// Let the event pass through to global handler for chat view copy
			return event
		}

		// Optional terminal-mapped send rune (from env)
		if m.shiftSendRune != 0 && event.Key() == tcell.KeyRune && event.Rune() == m.shiftSendRune {
			text := m.inputField.GetText()
			if text != "" && m.inputHandler != nil {
				m.inputField.SetText("")
				go func(input string) {
					if m.stateManager != nil {
						m.stateManager.TransitionTo(AgentStateProcessing, "处理用户输入", nil)
					}
					m.inputHandler(input)
				}(text)
			}
			enterPressedOnce = false // Reset double-enter detection
			return nil               // consume; do not insert the rune into the textarea
		}

		// If we're in paste mode (detected burst or by ChangedFunc), never treat Enter as send
		if !pasteModeUntil.IsZero() && now.Before(pasteModeUntil) {
			if event.Key() == tcell.KeyEnter {
				enterPressedOnce = false
				return event // always newline during paste bursts
			}
			// Other keys: proceed as normal but block double-enter
			enterPressedOnce = false
			return event
		}

		// Enter key: handle double-press for sending, single for newline
		if event.Key() == tcell.KeyEnter {
			if enterPressedOnce {
				delta := now.Sub(lastEnterTime)
				if delta >= minHumanInterval && delta < doubleClickInterval {
					// Double Enter: send message
					text := strings.TrimSpace(m.inputField.GetText())
					if text != "" && m.inputHandler != nil {
						// Clear input field immediately to provide user feedback
						m.inputField.SetText("")

						// Use goroutine to avoid blocking tview's event handling
						go func(input string) {
							// Use StateManager directly for state transitions
							if m.stateManager != nil {
								m.stateManager.TransitionTo(AgentStateProcessing, "处理用户输入", nil)
							}
							m.inputHandler(input)
						}(text)
					}
					enterPressedOnce = false // Reset
					return nil               // Consume the event
				}
				// If delta is too small or too large, treat as normal newline and reset detection
				enterPressedOnce = false
				return event
			}
			// First Enter press: record time and pass through for newline
			enterPressedOnce = true
			lastEnterTime = now
			return event
		}

		// Any other key press resets the double-enter detection
		enterPressedOnce = false

		// All other keys: pass through to TextArea for normal editing
		return event
	})

	// Global key bindings
	m.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Ctrl+C: copy selected text if any, and prevent app exit
		if event.Key() == tcell.KeyCtrlC {
			focused := m.app.GetFocus()

			// If chat view has focus, try to copy selection
			if focused == m.chatView.GetPrimitive() && m.chatView != nil {
				text, _, _ := m.chatView.area.GetSelection()
				if text != "" {
					_ = clipboard.WriteAll(text)
				}
				return nil // consume to avoid exiting app
			}

			// If input field has focus, try to copy selection
			if focused == m.inputField && m.inputField != nil {
				text, _, _ := m.inputField.GetSelection()
				if text != "" {
					_ = clipboard.WriteAll(text)
				}
				return nil // consume to avoid exiting app
			}

			if m.cancelHandler != nil && m.cancelHandler() {
				if m.stateManager != nil {
					m.stateManager.TransitionTo(AgentStateIdle, "任务已取消，等待用户输入", nil)
				}
			}
			return nil
		}

		// Ctrl+A: copy all chat content regardless of focus
		if event.Key() == tcell.KeyCtrlA {
			if m.chatView != nil {
				text := m.chatView.area.GetText()
				if text != "" {
					_ = clipboard.WriteAll(text)
				}
			}
			return nil
		}

		// Q key: quit application (only when input field is not focused)
		if event.Key() == tcell.KeyRune && (event.Rune() == 'q' || event.Rune() == 'Q') {
			if m.app.GetFocus() != m.inputField {
				m.Stop()
				return nil
			}
			return event
		}

		// Ctrl+Z triggers cancel handler if available
		if event.Key() == tcell.KeyCtrlZ {
			if m.cancelHandler != nil {
				if m.cancelHandler() {
					// Reset state after successful cancellation using StateManager
					if m.stateManager != nil {
						m.stateManager.TransitionTo(AgentStateIdle, "任务已取消，等待用户输入", nil)
					}
					return nil // Suppress default behavior
				}
			}
			return event
		}

		// Tab to switch views
		if event.Key() == tcell.KeyTAB {
			m.switchView()
			return nil
		}

		// Ctrl+P: open commands palette
		if event.Key() == tcell.KeyCtrlP {
			m.openCommandsView()
			return nil
		}

		// Ctrl+R: start a new session (clear context)
		if event.Key() == tcell.KeyCtrlR {
			if m.newSessionHandler != nil {
				m.newSessionHandler()
			}
			return nil
		}

		// T/t: toggle the latest thinking block (expand/collapse reasoning)
		if event.Key() == tcell.KeyRune && (event.Rune() == 't' || event.Rune() == 'T') {
			m.toggleLatestThinking()
			return nil
		}

		return event
	})
}

// Run starts the TUI application
func (m *Model) Run() error {
	// Ignore SIGINT (Ctrl+C) while TUI is running so that Ctrl+C is handled as a key event
	signal.Ignore(os.Interrupt)
	m.ignoredSigInt = true

	m.appRunning = true
	go m.eventLoop()
	return m.app.SetRoot(m.layout, true).SetFocus(m.inputField).Run()
}

// eventLoop processes events from the event channel
func (m *Model) eventLoop() {
	for event := range m.eventChan {
		event()
	}
}

// runUI ensures UI updates are executed on the correct goroutine with throttling.
// If the application is running, it schedules the update via throttledRender;
// otherwise, it executes the update immediately (safe before app.Run()).
func (m *Model) runUI(fn func()) {
	if !m.appRunning {
		// Before app.Run(), tview components aren't running yet; direct updates are safe.
		fn()
		return
	}

	// 委托给节流/批处理渲染，内部会感知正在渲染中的状态（pendingRender）并避免嵌套的 QueueUpdateDraw
	m.throttledRender(fn)
}

// SetInputHandler sets the handler for processing user input text.
func (m *Model) SetInputHandler(handler func(string)) {
	m.inputHandler = handler
}

// SetCancelHandler sets the handler invoked when user presses Ctrl+Z to cancel.
func (m *Model) SetCancelHandler(handler func() bool) {
	m.cancelHandler = handler
}

func (m *Model) SetConnectionStatus(state, detail string) {
	m.connectionState = state
	m.connectionDetail = detail
	m.updateStatusBarDirect()
}

func (m *Model) UpdateSwarmRoster(roster string) {
	m.swarmRoster = roster
	if m.swarmBar != nil {
		m.swarmBar.SetText("[gray]团队动态:[-] " + roster)
	}
}

// SetNewSessionHandler sets the handler invoked when user presses Ctrl+R to start a new session.
func (m *Model) SetNewSessionHandler(handler func()) {
	m.newSessionHandler = handler
}

// ShowConfirmation displays a confirmation UI with tool information and options.
func (m *Model) ShowConfirmation(message string, toolInfo map[string]interface{}, callback func(bool)) {
	m.eventChan <- func() {
		// Create and store pending confirmation message
		confirmMsg := NewConfirmationMessage(
			fmt.Sprintf("confirm_%d", time.Now().UnixNano()),
			message,
			toolInfo,
		)
		m.messages = append(m.messages, confirmMsg)
		m.pendingConfirmation = confirmMsg
		m.confirmationHandler = callback
		m.showingConfirm = true
		go m.PlaySound("didi")

		// Build tool info text
		var infoBuilder strings.Builder
		reset := m.styles.GetResetTag()
		fmt.Fprintf(&infoBuilder, "%s确认信息%s\n", m.styles.GetColorTag("warning"), reset)
		infoBuilder.WriteString(message)
		fmt.Fprintf(&infoBuilder, "\n\n%s工具参数:%s\n", m.styles.GetColorTag("success"), reset)
		for k, v := range toolInfo {
			fmt.Fprintf(&infoBuilder, "%s- %s: %s%v\n", m.styles.GetColorTag("white"), k, reset, v)
		}

		// Update UI components in UI goroutine
		m.runUI(func() {
			// Update confirmation components
			if m.confirmInfo != nil {
				m.confirmInfo.SetText(infoBuilder.String())
			}
			if m.confirmSelect != nil {
				m.confirmSelect.Clear()
				m.confirmSelect.AddItem("同意", "", 'y', func() {
					m.handleConfirmationResponse(true)
				})
				m.confirmSelect.AddItem("拒绝", "", 'n', func() {
					m.handleConfirmationResponse(false)
				})
				m.confirmSelect.AddItem("同意并不再询问", "", 'a', func() {
					m.handleConfirmationResponseWithAllowlist(toolInfo)
				})
			}

			// Disable input while confirming
			if m.inputField != nil {
				m.inputField.SetDisabled(true)
			}
		})

		// Insert confirmation UI into layout
		m.insertConfirmationIntoLayout()

		// Refresh chat/status views
		m.updateChatView()
		m.updateStatusBarDirect()
	}
}

// SetCommandsProvider registers the provider used to populate the commands view.
func (m *Model) SetCommandsProvider(p func() []slash.Command) {
	m.commandsProvider = p
}

func (m *Model) openCommandsView() {
	// Load commands via provider
	var items []slash.Command
	if m.commandsProvider != nil {
		items = m.commandsProvider()
	}
	m.populateCommands(items)
	m.showView("commands")
}

func (m *Model) populateCommands(items []slash.Command) {
	m.eventChan <- func() {
		m.commandsList.Clear()
		// ESC to return
		m.commandsList.SetDoneFunc(func() {
			m.showView("chat")
		})
		for _, it := range items {
			secondary := fmt.Sprintf("[%s] %s", it.Source, it.Description)
			name := fmt.Sprintf("/%s", it.Name)
			// Capture for closure
			item := it
			m.commandsList.AddItem(name, secondary, 0, func() {
				// Insert into input and return to chat view
				m.inputField.SetText(fmt.Sprintf("/%s ", item.Name))
				m.showView("chat")
				m.app.SetFocus(m.inputField)
			})
		}
		// Preview on change
		m.commandsList.SetChangedFunc(func(i int, _ string, _ string, _ rune) {
			if i < 0 || i >= len(items) {
				return
			}
			m.commandsPreview.SetText(m.buildCommandPreview(items[i]))
		})
		// Initialize preview
		if len(items) > 0 {
			m.commandsList.SetCurrentItem(0)
			m.commandsPreview.SetText(m.buildCommandPreview(items[0]))
		} else {
			m.commandsPreview.SetText("暂无命令。")
		}
	}
}

// buildCommandPreview returns the detail text shown in the commands preview panel.
func (m *Model) buildCommandPreview(it slash.Command) string {
	var b strings.Builder
	reset := m.styles.GetResetTag()
	fmt.Fprintf(&b, "%s/%s%s  %s%s%s\n", m.styles.GetColorTag("secondary"), it.Name, reset, m.styles.GetColorTag("muted"), it.Source, reset)
	if it.Usage != "" {
		fmt.Fprintf(&b, "用法: %s\n", it.Usage)
	}
	if it.Namespace != "" {
		fmt.Fprintf(&b, "命名空间: %s\n", it.Namespace)
	}
	if it.Description != "" {
		fmt.Fprintf(&b, "描述: %s\n", it.Description)
	}
	if len(it.AllowedTools) > 0 {
		fmt.Fprintf(&b, "允许工具: %s\n", strings.Join(it.AllowedTools, ", "))
	}
	if it.Category == slash.CategoryCustom {
		b.WriteString("\n前置命令: 支持 ! 行，受 allowed-tools 中 run_shell_command 控制\n")
	}
	b.WriteString("\n提示：按 Enter 将命令插入输入框，继续填写参数；按 ESC 返回聊天视图。")
	return b.String()
}

// throttledRender 实现节流渲染，避免过度刷新
func (m *Model) throttledRender(fn func()) {
	m.renderMutex.Lock()

	// Accumulate this update for the next render batch
	m.pendingUpdates = append(m.pendingUpdates, fn)

	now := time.Now()
	timeSinceLastRender := now.Sub(m.lastRenderTime)

	// 根据动画状态与最近状态更新动态选择有效的最小渲染间隔
	effectiveMin := m.normalMinRenderInterval
	recentStateUpdate := false
	if !m.lastStateUpdate.IsZero() {
		recentStateUpdate = now.Sub(m.lastStateUpdate) <= 200*time.Millisecond
	}
	isAnimated := m.stateManager != nil && m.stateManager.IsAnimatedState()
	if isAnimated || recentStateUpdate {
		effectiveMin = m.activeMinRenderInterval
	}

	// Determine if we should render immediately
	shouldRenderNow := timeSinceLastRender >= effectiveMin && !m.pendingRender

	if shouldRenderNow {
		// Mark that a render is pending and release the lock BEFORE executing batch
		m.pendingRender = true
		m.renderMutex.Unlock()
		m.executeRenderBatch()
		return
	}

	// Otherwise, schedule a delayed batched render if not already scheduled
	if !m.pendingRender {
		m.pendingRender = true
		delay := effectiveMin - timeSinceLastRender
		if delay < 0 {
			delay = 0
		}

		// Stop previous timer if any
		if m.renderThrottler != nil {
			m.renderThrottler.Stop()
		}

		// Do not hold the mutex while scheduling and executing the batch
		m.renderThrottler = time.AfterFunc(delay, func() {
			m.executeRenderBatch()
		})
	}

	m.renderMutex.Unlock()
}

// executeRenderBatch 执行批量的 UI 渲染，确保期间累积的更新不会丢失
func (m *Model) executeRenderBatch() {
	m.app.QueueUpdateDraw(func() {
		atomic.StoreInt32(&m.inUIUpdate, 1)
		defer atomic.StoreInt32(&m.inUIUpdate, 0)

		// Drain all accumulated updates at execution time to include updates
		// that arrived after scheduling but before this callback runs.
		m.renderMutex.Lock()
		updates := m.pendingUpdates
		m.pendingUpdates = nil
		m.renderMutex.Unlock()

		// Execute all accumulated updates in order
		for _, u := range updates {
			if u != nil {
				u()
			}
		}

		now := time.Now()

		// Update render timing and pending flags
		m.renderMutex.Lock()
		m.lastRenderTime = now
		// Stop and clear any existing throttler since we've just rendered
		if m.renderThrottler != nil {
			m.renderThrottler.Stop()
			m.renderThrottler = nil
		}
		// Mark current batch as completed
		m.pendingRender = false
		// Check if more updates arrived during execution; if so, schedule another batch
		hasMore := len(m.pendingUpdates) > 0
		m.renderMutex.Unlock()

		if hasMore {
			// Schedule a follow-up batch immediately to flush remaining updates
			m.renderMutex.Lock()
			if !m.pendingRender {
				m.pendingRender = true
				if m.renderThrottler != nil {
					m.renderThrottler.Stop()
				}
				// Do not lock inside the timer; executeRenderBatch will manage locking
				m.renderThrottler = time.AfterFunc(0, func() {
					m.executeRenderBatch()
				})
			}
			m.renderMutex.Unlock()
		}
	})
}

// batchUIUpdate 批量执行多个UI更新，减少渲染次数
func (m *Model) batchUIUpdate(updates ...func()) { //nolint:unused
	m.runUI(func() {
		for _, update := range updates {
			update()
		}
	})
}

// updateChatAndStatus 同时更新聊天视图和状态栏，避免分别渲染
func (m *Model) updateChatAndStatus() { //nolint:unused
	m.batchUIUpdate(
		func() {
			// 更新聊天视图
			var content strings.Builder
			for _, msg := range m.messages {
				content.WriteString(m.formatMessage(msg))
			}
			m.chatView.SetText(content.String())
			m.chatView.ScrollToEnd()
		},
		func() {
			// 更新状态栏
			m.updateStatusBarDirect()
		},
	)
}

// PlaySound plays the named UI sound after ensuring it is cached locally.
func (m *Model) PlaySound(soundType string) {
	logger.Infof("🔊 PlaySound被调用，声音类型: %s", soundType)
	m.soundMutex.Lock()
	defer m.soundMutex.Unlock()

	// Map sound types to URLs and cache file names
	soundSources := getSoundSources()

	src, ok := soundSources[soundType]
	if !ok {
		logger.Errorf("🔊 声音类型 '%s' 未找到", soundType)
		return
	}

	logger.Infof("🔊 准备缓存声音文件: %s -> %s", src.EmbeddedPath, src.FileName)
	// Ensure cached file exists
	cachedPath, err := ensureSoundCached(src.EmbeddedPath, src.FileName)
	if err != nil {
		logger.Errorf("Failed to prepare sound '%s': %v", soundType, err)
		return
	}

	logger.Infof("🔊 开始播放声音文件: %s", cachedPath)
	// Play with best available system player per OS
	if err := playFileCrossPlatform(cachedPath); err != nil {
		logger.Errorf("Failed to play sound '%s': %v", soundType, err)
	} else {
		logger.Infof("🔊 声音播放命令执行完成: %s", soundType)
	}
}

// PrewarmSoundCache writes embedded sounds to the cache directory in advance.
func (m *Model) PrewarmSoundCache() {
	m.soundMutex.Lock()
	defer m.soundMutex.Unlock()

	for _, src := range getSoundSources() {
		if _, err := ensureSoundCached(src.EmbeddedPath, src.FileName); err != nil {
			logger.Errorf("Prewarm sound cache failed for %s: %v", src.FileName, err)
		}
	}
}

// 声音资源映射，统一供 PlaySound 和预热逻辑使用
func getSoundSources() map[string]struct {
	EmbeddedPath string
	FileName     string
} {
	return map[string]struct {
		EmbeddedPath string
		FileName     string
	}{
		"didi":  {EmbeddedPath: "assets/didi.mp3", FileName: "didi.mp3"},
		"cough": {EmbeddedPath: "assets/cough.mp3", FileName: "cough.mp3"},
	}
}

// ensureSoundCached ensures a cached file exists by preferring embedded asset, falling back to download with timeout & retries.
func ensureSoundCached(embeddedPath, fileName string) (string, error) {
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		// Fallback to temp dir if cache dir unavailable
		cacheRoot = os.TempDir()
	}
	cacheDir := filepath.Join(cacheRoot, "nano-agent", "sounds")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	cachedPath := filepath.Join(cacheDir, fileName)

	// If already cached and non-empty, return immediately
	if fi, err := os.Stat(cachedPath); err == nil && fi.Size() > 0 {
		return cachedPath, nil
	}

	// Try embedded asset
	if embeddedPath != "" {
		if data, err := soundsFS.ReadFile(embeddedPath); err == nil && len(data) > 0 {
			// Write atomically to cachedPath
			tmp, err := os.CreateTemp(filepath.Dir(cachedPath), "embed-*.tmp")
			if err == nil {
				tmpPath := tmp.Name()
				_, writeErr := tmp.Write(data)
				closeErr := tmp.Close()
				if writeErr == nil && closeErr == nil {
					if err := os.Rename(tmpPath, cachedPath); err == nil {
						return cachedPath, nil
					}
				}
				// Cleanup temp if anything failed
				_ = os.Remove(tmpPath)
			}
		}
	}

	return "", fmt.Errorf("failed to cache sound file %s: embedded asset not found", fileName)
}

// playFileCrossPlatform attempts to play an audio file using best available tool per OS.
func playFileCrossPlatform(filePath string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("afplay", filePath).Run()
	case "linux":
		candidates := [][]string{
			{"mpg123", filePath},
			{"mpg321", filePath},
			{"mpv", "--really-quiet", "--no-video", filePath},
			{"mplayer", "-really-quiet", filePath},
			{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", filePath},
		}
		for _, c := range candidates {
			if _, err := exec.LookPath(c[0]); err == nil {
				return exec.Command(c[0], c[1:]...).Run()
			}
		}
		return fmt.Errorf("no audio player found (tried mpg123/mpg321/mpv/mplayer/ffplay)")
	case "windows":
		// Use PowerShell with Windows Media Player COM for MP3 playback
		if _, err := exec.LookPath("powershell"); err == nil {
			// Escape backslashes in path for PowerShell string
			psPath := strings.ReplaceAll(filePath, "\\", "\\\\")
			script := fmt.Sprintf(`$p = New-Object -ComObject WMPlayer.OCX.7; $p.URL = '%s'; $p.controls.play(); while ($p.playState -ne 1) { Start-Sleep -Milliseconds 200 }; $p.close()`, psPath)
			return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
		}
		// Fallback: try starting default app (non-blocking)
		return exec.Command("cmd", "/C", "start", "", filePath).Start()
	default:
		return fmt.Errorf("unsupported OS for playback: %s", runtime.GOOS)
	}
}

// Stop stops the TUI application
func (m *Model) Stop() {
	// Restore default SIGINT behavior if we ignored it during Run()
	if m.ignoredSigInt {
		signal.Reset(os.Interrupt)
		m.ignoredSigInt = false
	}

	// Stop StateManager animation
	if m.stateManager != nil {
		m.stateManager.Stop()
	}

	// 清理渲染节流器
	m.renderMutex.Lock()
	if m.renderThrottler != nil {
		m.renderThrottler.Stop()
		m.renderThrottler = nil
	}
	m.renderMutex.Unlock()

	m.appRunning = false
	m.app.Stop()
}

// isBusy checks if the agent is in a state where user input should be blocked.

// switchView switches to the next view
func (m *Model) switchView() {
	m.eventChan <- func() {
		// Only chat view available now
		m.showView("chat")
	}
}

// showView shows a specific view
func (m *Model) showView(view string) {
	m.eventChan <- func() {
		m.activeView = view
		// Move UI operations to UI goroutine
		m.runUI(func() {
			m.pages.SwitchToPage(view)
			switch view {
			case "chat":
				m.app.SetFocus(m.inputField)
			}
		})

		// Update status bar to reflect the view change
		m.updateStatusBar()
	}
}

// AddMessage adds a message to the chat
func (m *Model) AddMessage(role, content string) {
	m.eventChan <- func() {
		// Streaming merge: if the last message is an assistant message in streaming state, append content
		if role == "assistant" && len(m.messages) > 0 {
			if last, ok := m.messages[len(m.messages)-1].(*LLMResponseMessage); ok && last.Streaming {
				last.Content += content
				m.updateChatView()
				return
			}
		}

		var msg MessageInterface
		switch role {
		case "user":
			msg = NewUserMessage(fmt.Sprintf("user_%d", time.Now().UnixNano()), content)
		case "assistant":
			msg = NewLLMResponseMessage(fmt.Sprintf("llm_%d", time.Now().UnixNano()), content, true) // streaming=true
		case "system":
			msg = NewSystemMessage(fmt.Sprintf("system_%d", time.Now().UnixNano()), content)
		default:
			msg = NewLLMResponseMessage(fmt.Sprintf("msg_%d", time.Now().UnixNano()), content, false)
		}

		m.messages = append(m.messages, msg)
		m.updateChatView()
	}
}

// FinishStreaming marks the current streaming message as complete.
// Called when EventTypeDone is received, signaling the end of a model response.
func (m *Model) FinishStreaming() {
	m.eventChan <- func() {
		for i := len(m.messages) - 1; i >= 0; i-- {
			if llmMsg, ok := m.messages[i].(*LLMResponseMessage); ok && llmMsg.Streaming {
				llmMsg.Streaming = false
				width := 100
				if m.chatView != nil && m.chatView.GetPrimitive() != nil {
					_, _, w, _ := m.chatView.GetPrimitive().GetRect()
					if w > minMarkdownViewportWidth {
						width = w
					}
				}
				if rendered, err := uirender.Markdown(llmMsg.Content, uirender.MarkdownOptions{Width: width}); err == nil {
					llmMsg.Content = rendered
				} else {
					logger.Warnf("markdown render failed: %v", err)
				}
				break
			}
		}
		m.updateChatView()
	}
}

// AddThinking adds or updates the latest thinking block (collapsible reasoning).
func (m *Model) AddThinking(title, reasoning string, metadata map[string]interface{}) {
	m.eventChan <- func() {
		// Default title if empty
		if strings.TrimSpace(title) == "" {
			title = "🧠 正在思考..."
		}

		// Try to update the most recent thinking block
		var existing *ThinkingMessage
		for i := len(m.messages) - 1; i >= 0; i-- {
			if tm, ok := m.messages[i].(*ThinkingMessage); ok {
				existing = tm
				break
			}
		}

		isComplete := false
		if metadata != nil {
			if done, ok := metadata["is_complete"].(bool); ok && done {
				isComplete = true
			}
		}

		needsFullRedraw := false

		if existing != nil && !existing.Completed {
			// Incremental update: only update internal data
			if title != "" {
				existing.Title = title
			}
			if reasoning != "" {
				existing.Reasoning = reasoning
			}
			if isComplete {
				existing.Completed = true
				existing.Collapsed = true
				needsFullRedraw = true // Need redraw when complete to show collapsed state
			}
			existing.Timestamp = time.Now()
			// Streaming incremental: don't trigger redraw to avoid flicker
		} else {
			// New thinking block: need redraw
			thinkMsg := NewThinkingMessage(fmt.Sprintf("think_%d", time.Now().UnixNano()), title, reasoning)
			thinkMsg.Completed = isComplete
			if isComplete {
				thinkMsg.Collapsed = true
			} else if reasoning != "" {
				thinkMsg.Collapsed = false
			}
			m.messages = append(m.messages, thinkMsg)
			needsFullRedraw = true
		}

		if needsFullRedraw {
			m.updateChatView()
		}
		// Always update status bar (lightweight operation)
		m.updateStatusBar()
	}
}

// toggleLatestThinking collapses/expands the most recent thinking block.
func (m *Model) toggleLatestThinking() {
	m.eventChan <- func() {
		for i := len(m.messages) - 1; i >= 0; i-- {
			if tm, ok := m.messages[i].(*ThinkingMessage); ok {
				tm.Collapsed = !tm.Collapsed
				m.updateChatView()
				return
			}
		}
	}
}

// AddToolUse adds or updates a tool use message in the chat
func (m *Model) AddToolUse(toolUse *event.ToolUse) {
	m.eventChan <- func() {
		// Try to find an existing message for this tool call using the ID
		var existingMsg *ToolUseMessage
		for _, msg := range m.messages {
			if toolMsg, ok := msg.(*ToolUseMessage); ok && toolMsg.ToolCallID == toolUse.ID {
				existingMsg = toolMsg
				break
			}
		}

		if existingMsg != nil {
			// Update existing message
			existingMsg.Status = toolUse.Status
			existingMsg.Result = toolUse.Result
		} else {
			// Add new message
			msg := NewToolUseMessage(
				fmt.Sprintf("toolmsg_%d", time.Now().UnixNano()),
				toolUse.ID,
				toolUse.ToolName,
				toolUse.Parameters,
			)
			msg.Status = toolUse.Status
			msg.Result = toolUse.Result
			m.messages = append(m.messages, msg)
		}

		// Update status based on tool execution state
		switch toolUse.Status {
		case "executing":
			activity := fmt.Sprintf("执行工具: %s", toolUse.ToolName)
			// Update new state manager
			if m.stateManager != nil {
				m.stateManager.TransitionTo(AgentStateToolExecution, activity, map[string]interface{}{
					"tool_name": toolUse.ToolName,
					"tool_id":   toolUse.ID,
				})
			}
		case "success":
			// After a tool completes successfully, the agent typically continues thinking/synthesizing.
			// Do NOT mark idle here to avoid showing idle before the turn actually ends.
			activity := "工具执行完成，继续思考中"
			if m.stateManager != nil {
				m.stateManager.TransitionTo(AgentStateThinking, activity, map[string]interface{}{
					"tool_name": toolUse.ToolName,
					"tool_id":   toolUse.ID,
				})
			}
		case "error":
			activity := fmt.Sprintf("工具执行失败: %s", toolUse.ToolName)
			// Update new state manager
			if m.stateManager != nil {
				m.stateManager.TransitionTo(AgentStateError, activity, map[string]interface{}{
					"tool_name": toolUse.ToolName,
					"tool_id":   toolUse.ID,
				})
			}
		}

		m.updateChatView()
	}
}

// formatMessage formats a message for display in the TUI
func (m *Model) formatMessage(msg MessageInterface) string {
	timestamp := msg.GetTimestamp().Format("15:04:05")
	msgContent := GetDisplayContent(msg)
	if strings.TrimSpace(msgContent) == "" {
		return ""
	}

	var builder strings.Builder
	reset := m.styles.GetResetTag()
	switch msg.GetType() {
	case MessageTypeUser:
		fmt.Fprintf(&builder, "%s[User] %s%s\n%s\n\n", m.styles.GetColorTag("primary"), timestamp, reset, msgContent)
	case MessageTypeLLMResponse:
		fmt.Fprintf(&builder, "%s[Assistant] %s%s\n%s\n\n", m.styles.GetColorTag("success"), timestamp, reset, msgContent)
	case MessageTypeThinking:
		thinkMsg := msg.(*ThinkingMessage)
		title := thinkMsg.Title
		if strings.TrimSpace(title) == "" {
			title = "🧠 正在思考..."
		}
		state := "进行中"
		if thinkMsg.Completed {
			state = "完成"
		}
		header := fmt.Sprintf("%s[Thinking | %s]%s %s", m.styles.GetColorTag("secondary"), state, reset, title)
		if thinkMsg.Collapsed || strings.TrimSpace(thinkMsg.Reasoning) == "" {
			hint := fmt.Sprintf("%s(按 T 展开/折叠推理内容)%s", m.styles.GetColorTag("muted"), reset)
			fmt.Fprintf(&builder, "%s\n%s\n\n", header, hint)
		} else {
			reasoning := strings.TrimSpace(thinkMsg.Reasoning)
			reasoning = "  " + strings.ReplaceAll(reasoning, "\n", "\n  ")
			body := fmt.Sprintf("%s%s%s", m.styles.GetColorTag("white"), reasoning, reset)
			fmt.Fprintf(&builder, "%s\n%s\n\n", header, body)
		}
	case MessageTypeSystem:
		fmt.Fprintf(&builder, "%s[System] %s%s\n%s\n\n", m.styles.GetColorTag("secondary"), timestamp, reset, msgContent)
	case MessageTypeToolUse:
		toolMsg := msg.(*ToolUseMessage)
		fmt.Fprintf(&builder, "%s[Tool: %s] %s%s\n%s\n\n", m.styles.GetColorTag("warning"), toolMsg.ToolName, timestamp, reset, msgContent)
	case MessageTypeError:
		fmt.Fprintf(&builder, "%s[Error] %s%s\n%s\n\n", m.styles.GetColorTag("error"), timestamp, reset, msgContent)
	case MessageTypeConfirmation:
		fmt.Fprintf(&builder, "%s[Confirmation] %s%s\n%s\n\n", m.styles.GetColorTag("warning"), timestamp, reset, msgContent)
	}
	return builder.String()
}

// updateInputFieldState updates the input field placeholder and appearance based on agent status
func (m *Model) updateInputFieldState() {
	// Always schedule on UI goroutine for safety
	m.runUI(func() {
		m.updateInputFieldStateDirect()
	})
}

// updateInputFieldStateDirect performs the actual input field updates.
// This must be called from the UI goroutine only.
func (m *Model) updateInputFieldStateDirect() {
	m.inputStateMu.Lock()
	defer m.inputStateMu.Unlock()
	// 检查是否正在显示确认对话框，如果是则禁用输入框
	if m.showingConfirm || m.showingUserConfirm {
		m.inputField.SetPlaceholder("等待用户确认...")
		m.inputField.SetDisabled(true)
		return
	}

	// 根据Agent状态设置输入框状态
	if m.stateManager != nil {
		currentState := m.stateManager.GetCurrentState()
		switch currentState.AgentState {
		case AgentStateIdle:
			// 空闲状态下启用输入框
			m.inputField.SetPlaceholder("支持多行输入和粘贴，Enter换行，双击Enter发送消息")
			m.inputField.SetDisabled(false)
		case AgentStateWaitingApproval:
			// 等待批准状态下禁用输入框（需要用户通过确认对话框操作）
			m.inputField.SetPlaceholder("等待用户批准...")
			m.inputField.SetDisabled(true)
		case AgentStateProcessing, AgentStateThinking, AgentStateToolExecution:
			// 处理状态下启用输入框但提示排队（支持队列模式）
			queueSuffix := "，您的输入将在当前任务结束后处理 (双击Enter发送)"
			placeholder := "⏳ AI 正在处理中" + queueSuffix
			if currentState.AgentState == AgentStateToolExecution && currentState.ToolName != "" {
				placeholder = fmt.Sprintf("⏳ 正在执行工具 [%s]%s", currentState.ToolName, queueSuffix)
			}
			m.inputField.SetPlaceholder(placeholder)
			m.inputField.SetDisabled(false)
		default:
			// 其他状态启用输入框
			m.inputField.SetPlaceholder("支持多行输入和粘贴，Enter换行，双击Enter发送消息")
			m.inputField.SetDisabled(false)
		}
	} else {
		// 如果StateManager不可用，默认启用输入框
		m.inputField.SetPlaceholder("支持多行输入和粘贴，Enter换行，双击Enter发送消息")
		m.inputField.SetDisabled(false)
	}
}

// insertConfirmationIntoLayout dynamically inserts the confirmation component into current layout
func (m *Model) insertConfirmationIntoLayout() {
	m.runUI(func() {
		currentPage, _ := m.pages.GetFrontPage()
		if currentPage == "chat" {
			// Recreate chat layout with confirmation inserted between input and status
			chatLayoutWithConfirm := tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(m.chatView.GetPrimitive(), 0, 1, false).
				AddItem(m.inputField, 3, 0, false).
				AddItem(m.confirmLayout, 15, 0, true).
				AddItem(m.statusBar, 1, 0, false)
			m.pages.RemovePage("chat")
			m.pages.AddPage("chat", chatLayoutWithConfirm, true, true)
		}
		// For response and help views, confirmation shows as modal overlay
	})
}

// ShowUserConfirmation displays a simple user confirmation dialog with agree/reject options
func (m *Model) ShowUserConfirmation(message string, handler func(bool)) {
	m.eventChan <- func() {
		m.userConfirmHandler = handler
		m.showingUserConfirm = true
		// 统一通过 runUI 调度，避免直接 QueueUpdateDraw
		m.runUI(func() {
			m.userConfirmDialog.SetText(message)
			m.pages.AddPage("user_confirm", m.userConfirmDialog, true, true)
			m.app.SetFocus(m.userConfirmDialog)
		})
	}
}

// hideUserConfirmation hides the user confirmation dialog
func (m *Model) hideUserConfirmation() {
	if !m.showingUserConfirm {
		return
	}
	m.showingUserConfirm = false
	m.userConfirmHandler = nil
	m.runUI(func() {
		m.pages.RemovePage("user_confirm")
		m.app.SetFocus(m.inputField)
		// 用户确认对话框隐藏后更新输入框状态
		m.updateInputFieldStateDirect()
	})
}

// handleConfirmationResponse processes user's confirmation response
func (m *Model) handleConfirmationResponse(approved bool) {
	m.eventChan <- func() {
		if m.pendingConfirmation == nil {
			return
		}
		// Update confirmation message status
		if approved {
			m.pendingConfirmation.Status = "approved"
			if m.stateManager != nil {
				m.stateManager.TransitionTo(AgentStateToolExecution, "执行已批准的工具", nil)
			}
		} else {
			m.pendingConfirmation.Status = "rejected"
			if m.stateManager != nil {
				m.stateManager.SetIdle()
			}
		}
		// Store handler before clearing state
		handler := m.confirmationHandler
		// Clear state
		m.pendingConfirmation = nil
		m.confirmationHandler = nil
		m.showingConfirm = false
		// Hide confirmation UI in-place and restore chat layout
		m.runUI(func() {
			currentPage, _ := m.pages.GetFrontPage()
			if currentPage == "chat" {
				chatLayout := tview.NewFlex().SetDirection(tview.FlexRow).
					AddItem(m.chatView.GetPrimitive(), 0, 1, true).
					AddItem(m.inputField, 3, 0, true).
					AddItem(m.statusBar, 1, 0, false)
				m.pages.RemovePage("chat")
				m.pages.AddPage("chat", chatLayout, true, true)
				m.pages.SwitchToPage("chat")
			}
			m.app.SetFocus(m.inputField)
			// 确认对话框隐藏后更新输入框状态
			m.updateInputFieldStateDirect()
		})
		// Update UI
		m.updateChatView()
		m.updateStatusBarDirect()
		// Call the stored handler
		if handler != nil {
			go handler(approved)
		}
	}
}

// handleConfirmationResponseWithAllowlist approves the pending confirmation AND
// invokes the registered allowlistHandler so the caller can add a permanent
// rule for this tool to the session allowlist.
func (m *Model) handleConfirmationResponseWithAllowlist(toolInfo map[string]interface{}) {
	// Capture tool name and make a shallow copy of the params map BEFORE
	// calling handleConfirmationResponse, because tool execution starts
	// immediately after the callback and may concurrently mutate the map.
	// Parameter values are primitive JSON types (string/number/bool) in
	// practice, so a shallow copy is sufficient to avoid data races on the map
	// itself.
	toolName, _ := toolInfo["Name"].(string)
	origParams, _ := toolInfo["Parameters"].(map[string]interface{})
	var paramsCopy map[string]interface{}
	if origParams != nil {
		paramsCopy = make(map[string]interface{}, len(origParams))
		for k, v := range origParams {
			paramsCopy[k] = v
		}
	}
	ah := m.allowlistHandler
	// Approve normally (triggers tool execution).
	m.handleConfirmationResponse(true)
	// Fire the allowlist callback using the pre-copied data.
	if ah != nil {
		go ah(toolName, paramsCopy)
	}
}

// NewUserMessage creates a user message with the current timestamp.
func NewUserMessage(id, content string) *UserMessage {
	return &UserMessage{
		BaseMessage: BaseMessage{
			ID:        id,
			Timestamp: time.Now(),
			Type:      MessageTypeUser,
		},
		Content: content,
	}
}

// NewLLMResponseMessage creates an LLM response message with the current timestamp.
func NewLLMResponseMessage(id, content string, streaming bool) *LLMResponseMessage {
	return &LLMResponseMessage{
		BaseMessage: BaseMessage{
			ID:        id,
			Timestamp: time.Now(),
			Type:      MessageTypeLLMResponse,
		},
		Content:   content,
		Streaming: streaming,
	}
}

// NewSystemMessage creates a system message with the current timestamp.
func NewSystemMessage(id, content string) *SystemMessage {
	return &SystemMessage{
		BaseMessage: BaseMessage{
			ID:        id,
			Timestamp: time.Now(),
			Type:      MessageTypeSystem,
		},
		Content: content,
	}
}

// NewToolUseMessage creates a tool use message with the current timestamp.
func NewToolUseMessage(id, toolCallID, toolName string, parameters map[string]interface{}) *ToolUseMessage {
	return &ToolUseMessage{
		BaseMessage: BaseMessage{
			ID:        id,
			Timestamp: time.Now(),
			Type:      MessageTypeToolUse,
		},
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Parameters: parameters,
	}
}

// NewErrorMessage creates an error message with the current timestamp.
func NewErrorMessage(id, errorMsg, errorType string) *ErrorMessage {
	return &ErrorMessage{
		BaseMessage: BaseMessage{
			ID:        id,
			Timestamp: time.Now(),
			Type:      MessageTypeError,
		},
		Error:     errorMsg,
		ErrorType: errorType,
	}
}

// NewConfirmationMessage creates a confirmation message with pending status.
func NewConfirmationMessage(id, prompt string, toolInfo map[string]interface{}) *ConfirmationMessage {
	return &ConfirmationMessage{
		BaseMessage: BaseMessage{
			ID:        id,
			Timestamp: time.Now(),
			Type:      MessageTypeConfirmation,
		},
		Prompt:   prompt,
		ToolInfo: toolInfo,
		Status:   "pending",
	}
}

// GetToolUseMessage returns msg as a tool use message when possible.
func GetToolUseMessage(msg MessageInterface) *ToolUseMessage {
	if toolMsg, ok := msg.(*ToolUseMessage); ok {
		return toolMsg
	}
	return nil
}

// GetErrorMessage returns msg as an error message when possible.
func GetErrorMessage(msg MessageInterface) *ErrorMessage {
	if errorMsg, ok := msg.(*ErrorMessage); ok {
		return errorMsg
	}
	return nil
}

// GetLLMResponseMessage returns msg as an LLM response message when possible.
func GetLLMResponseMessage(msg MessageInterface) *LLMResponseMessage {
	if llmMsg, ok := msg.(*LLMResponseMessage); ok {
		return llmMsg
	}
	return nil
}

// GetUserMessage returns msg as a user message when possible.
func GetUserMessage(msg MessageInterface) *UserMessage {
	if userMsg, ok := msg.(*UserMessage); ok {
		return userMsg
	}
	return nil
}

// GetConfirmationMessage returns msg as a confirmation message when possible.
func GetConfirmationMessage(msg MessageInterface) *ConfirmationMessage {
	if confirmMsg, ok := msg.(*ConfirmationMessage); ok {
		return confirmMsg
	}
	return nil
}

// GetDisplayContent returns formatted content for display
func GetDisplayContent(msg MessageInterface) string {
	content := msg.GetContent()
	if content == "" {
		return ""
	}

	// Add streaming indicator for LLM responses
	if msg.GetType() == MessageTypeLLMResponse {
		if llmMsg, ok := msg.(*LLMResponseMessage); ok && llmMsg.Streaming {
			content += " ▋" // Add cursor for streaming
		}
	}

	return content
}

// GetStateManager returns the underlying StateManager for direct control
func (m *Model) GetStateManager() *StateManager {
	return m.stateManager
}

// SetAllowlistHandler registers a callback that is invoked when the user
// selects "同意并不再询问" in the confirmation dialog.
// The callback receives the tool name and parameters so the caller can build
// an appropriate allowlist rule.
func (m *Model) SetAllowlistHandler(h func(toolName string, params map[string]interface{})) {
	m.allowlistHandler = h
}

// setStatusBarText 统一的状态栏更新入口，纳入批量渲染与节流
func (m *Model) setStatusBarText(text string) {
	if m.statusBar == nil {
		// 组件尚未初始化，忽略这次更新（后续状态变化会再次触发）
		return
	}
	m.runUI(func() {
		m.statusBar.SetText(text)
	})
}

func (m *Model) updateChatView() {
	var content strings.Builder
	for _, msg := range m.messages {
		content.WriteString(m.formatMessage(msg))
	}
	m.runUI(func() {
		m.chatView.SetText(content.String())
		m.chatView.ScrollToEnd()
	})
}

func (m *Model) updateStatusBar() {
	var status strings.Builder
	// Keyboard shortcuts hint
	fmt.Fprintf(&status, "%sCtrl+R 新会话 | Ctrl+P 命令 | Ctrl+Z 取消 | Tab 切换 | q 退出%s | ",
		m.styles.GetColorTag("muted"), m.styles.GetResetTag())
	// Current view (omit default "chat")
	if m.activeView != "" && m.activeView != "chat" {
		fmt.Fprintf(&status, "%s%s%s | ", m.styles.GetColorTag("primary"), m.activeView, m.styles.GetResetTag())
	}
	// Use new StateManager for status formatting
	if m.stateManager != nil {
		status.WriteString(m.stateManager.FormatStatusText())
	}
	if m.connectionState != "" {
		fmt.Fprintf(&status, " | [green]● %s[-]", m.connectionState)
		if m.connectionDetail != "" {
			fmt.Fprintf(&status, " [gray]%s[-]", m.connectionDetail)
		}
	}
	m.setStatusBarText(status.String())
}

func (m *Model) updateStatusBarDirect() {
	if m.stateManager != nil {
		hint := m.styles.GetColorTag("muted") + "Ctrl+R 新会话 | Ctrl+P 命令 | Ctrl+Z 取消 | Tab 切换 | q 退出" + m.styles.GetResetTag()
		conn := ""
		if m.connectionState != "" {
			conn = fmt.Sprintf(" | [green]● %s[-]", m.connectionState)
			if m.connectionDetail != "" {
				conn += fmt.Sprintf(" [gray]%s[-]", m.connectionDetail)
			}
		}
		m.setStatusBarText(hint + " | " + m.stateManager.FormatStatusText() + conn)
	}
}

func (m *Model) updateStatusBarFromState(_ UIState) { //nolint:unused
	// We currently ignore the state param and ask StateManager for the formatted string
	// so formatting stays centralized.
	if m.stateManager != nil {
		m.setStatusBarText(m.stateManager.FormatStatusText())
	}
}
