package tview

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/slash"
	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
	"github.com/google/uuid"
)

// ConversationMessage represents a message in the conversation history
type ConversationMessage struct {
	Role      string
	Content   string
	Timestamp time.Time
}

// ActiveTask represents a currently running task
type ActiveTask struct {
	ctx    context.Context
	cancel context.CancelFunc
	input  string
}

// Integration manages the TUI integration with the agent
type Integration struct {
	model        *Model
	conversation []*ConversationMessage
	activeTask   *ActiveTask
	inputQueue   []string

	// Handlers
	inputHandler  func(context.Context, string) error
	cancelHandler func() error
	outbound      func(eventsource.Outbound) error

	// Event channel
	eventChan chan func()

	// Context for the integration
	ctx    context.Context
	cancel context.CancelFunc

	// permissionManager is set after agent creation so slash commands can
	// inspect and modify the permission mode and session allowlist.
	permissionManager *permission.Manager

	// persistentAllowlist is the persistent allowlist store for /disallow cleanup.
	persistentAllowlist *permission.PersistentAllowlistStore
	// workdir is the current working directory for persistent allowlist.
	workdir string

	// engine is set after engine creation so slash commands can control
	// thinking mode and other engine-level settings.
	engine *engine.Engine

	// newSessionCallback is invoked when Ctrl+R / /clear is triggered.
	// It is wired by the cli layer to call agent.StartNewSession().
	newSessionCallback func() string
}

func (i *Integration) BindOutbound(send func(eventsource.Outbound) error) {
	i.outbound = send
	i.model.SetInputHandler(func(input string) {
		i.forwardOutboundInput(input)
	})
	i.model.SetCancelHandler(func() bool {
		_ = i.SendOutbound(eventsource.Outbound{Kind: "cancel"})
		return true
	})
	i.model.SetNewSessionHandler(func() {
		_ = i.SendOutbound(eventsource.Outbound{Kind: "control", Control: "/clear"})
	})
}

func (i *Integration) SendOutbound(o eventsource.Outbound) error {
	if i.outbound == nil {
		return nil
	}
	return i.outbound(o)
}

func (i *Integration) forwardOutboundInput(input string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return
	}
	if strings.HasPrefix(trimmed, "/") {
		switch strings.ToLower(trimmed) {
		case "/clear", "/reset", "/sessions", "/cancel":
			_ = i.SendOutbound(eventsource.Outbound{Kind: "control", Control: trimmed})
			return
		}
	}
	i.AddMessage("user", trimmed)
	i.model.GetStateManager().SetThinking("正在处理您的请求...")
	_ = i.SendOutbound(eventsource.Outbound{Kind: "submit", Text: trimmed})
}

// NewIntegration creates a new TUI integration
func NewIntegration() *Integration {
	ctx, cancel := context.WithCancel(context.Background())

	integration := &Integration{
		model:        NewModel(),
		conversation: make([]*ConversationMessage, 0),
		inputQueue:   make([]string, 0),
		eventChan:    make(chan func(), 256),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Start the event loop
	go integration.eventLoop()

	// Set up handlers
	integration.model.SetInputHandler(func(input string) {
		integration.processInput(input)
	})

	integration.model.SetCancelHandler(func() bool {
		integration.cancelCurrentTask()
		return true
	})

	integration.model.SetNewSessionHandler(func() {
		integration.StartNewSession()
	})

	return integration
}

// eventLoop processes events from the event channel
func (i *Integration) eventLoop() {
	for event := range i.eventChan {
		event()
	}
}

// Run starts the TUI
func (i *Integration) Run() error {
	return i.model.Run()
}

// Stop stops the TUI
func (i *Integration) Stop() {
	i.model.Stop()
}

// SetInputHandler sets the input handler
func (i *Integration) SetInputHandler(handler func(context.Context, string) error) {
	i.inputHandler = handler
}

// SetCancelHandler sets the cancel handler
func (i *Integration) SetCancelHandler(handler func() error) {
	i.cancelHandler = handler
}

// processInput processes user input with debouncing to prevent multiple rapid inputs
func (i *Integration) processInput(input string) {
	i.eventChan <- func() {
		// If there's an active task, queue the input
		if i.activeTask != nil {
			i.inputQueue = append(i.inputQueue, input)
			queueSize := len(i.inputQueue)
			i.model.AddMessage("system", fmt.Sprintf("Input queued: %s (Queue size: %d)", input, queueSize))
			return
		}

		// Trim whitespace and check if input is empty
		trimmedInput := strings.TrimSpace(input)
		if trimmedInput == "" {
			return
		}

		// Start new task
		i.startNewTask(trimmedInput)
	}
}

// startNewTask starts a new task with the given input
func (i *Integration) startNewTask(input string) {
	// Handle local slash commands (permission and engine controls) before forwarding to the agent.
	if handled := i.handleLocalSlashCommand(input); handled {
		return
	}

	// Add user message to conversation and UI
	userMsg := &ConversationMessage{
		Role:      "user",
		Content:   input,
		Timestamp: time.Now(),
	}
	i.conversation = append(i.conversation, userMsg)
	i.model.AddMessage("user", input)

	// Set model to thinking state
	i.model.GetStateManager().SetThinking("正在处理您的请求...")

	// Create task context
	ctx, cancel := context.WithCancel(i.ctx)
	i.activeTask = &ActiveTask{
		ctx:    ctx,
		cancel: cancel,
		input:  input,
	}

	// Start task in goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("Task panic recovered: %v", r)
				i.model.AddMessage("system", fmt.Sprintf("Task failed: %v", r))
			}

			// Process next task in the queue
			i.eventChan <- func() {
				i.activeTask = nil
				i.model.GetStateManager().SetIdle()
				if len(i.inputQueue) > 0 {
					nextInput := i.inputQueue[0]
					i.inputQueue = i.inputQueue[1:]
					i.startNewTask(nextInput)
				}
			}
		}()

		// Execute the task with the cancellable context
		if i.inputHandler != nil {
			if err := i.inputHandler(ctx, input); err != nil {
				// Check if the error is due to cancellation
				if err == ctx.Err() {
					i.model.AddMessage("system", "Task cancelled by user.")
				} else {
					i.model.AddMessage("system", fmt.Sprintf("Task failed: %v", err))
				}
			}
		}
	}()
}

// cancelCurrentTask cancels the currently running task
func (i *Integration) cancelCurrentTask() {
	i.eventChan <- func() {
		if i.activeTask == nil {
			i.model.AddMessage("system", "No active task to cancel.")
			return
		}

		// Cancel the task
		i.activeTask.cancel()
		i.activeTask = nil

		// Call the cancel handler if set
		if i.cancelHandler != nil {
			if err := i.cancelHandler(); err != nil {
				i.model.AddMessage("system", fmt.Sprintf("Cancel handler failed: %v", err))
			}
		}

		i.model.AddMessage("system", "Task cancelled.")
	}
}

// GetModel returns the underlying TUI model
func (i *Integration) GetModel() *Model {
	return i.model
}

// SetupCommandsProvider configures the commands provider for the TUI
func (i *Integration) SetupCommandsProvider() {
	i.eventChan <- func() {
		i.model.SetCommandsProvider(func() []slash.Command {
			cwd, _ := os.Getwd()
			return slash.NewRegistry(cwd).All()
		})
	}
}

// AddMessage adds a message to the conversation and UI
func (i *Integration) AddMessage(role, content string) {
	i.eventChan <- func() {
		// Add to conversation history if it's a user or assistant message
		if role == "user" || role == "assistant" {
			convMsg := &ConversationMessage{
				Role:      role,
				Content:   content,
				Timestamp: time.Now(),
			}
			i.conversation = append(i.conversation, convMsg)
		}
		i.model.AddMessage(role, content)
	}
}

// FinishStreaming marks the current streaming message as complete
func (i *Integration) FinishStreaming() {
	i.model.FinishStreaming()
}

// AddThinking adds/updates the latest thinking block (collapsible reasoning).
func (i *Integration) AddThinking(content, reasoning string, metadata map[string]interface{}) {
	i.eventChan <- func() {
		i.model.AddThinking(content, reasoning, metadata)
	}
}

// AddToolUse adds or updates a tool use message with deduplication
func (i *Integration) AddToolUse(toolUse *event.ToolUse) {
	normalizedToolUse := toolUse
	if toolUse != nil && strings.TrimSpace(toolUse.ID) == "" {
		toolUseCopy := *toolUse
		toolUseCopy.ID = fmt.Sprintf("tooluse-%s", uuid.New().String())
		normalizedToolUse = &toolUseCopy
	}
	i.eventChan <- func() {
		if normalizedToolUse != nil {
			i.model.GetStateManager().SetToolExecution(normalizedToolUse.ToolName, "")
		}
		i.model.AddToolUse(normalizedToolUse)
	}
}

func (i *Integration) SetConnectionStatus(state, detail string) {
	i.eventChan <- func() {
		i.model.SetConnectionStatus(state, detail)
	}
}

func (i *Integration) AddNotice(text string) {
	i.eventChan <- func() {
		i.model.AddMessage("system", text)
	}
}

func (i *Integration) UpdateSwarmRoster(roster string) {
	i.eventChan <- func() {
		i.model.UpdateSwarmRoster(roster)
	}
}

// GetConversationHistory returns the conversation history
func (i *Integration) GetConversationHistory() []*ConversationMessage {
	resultChan := make(chan []*ConversationMessage, 1)
	i.eventChan <- func() {
		// Return a copy to avoid race conditions
		history := make([]*ConversationMessage, len(i.conversation))
		copy(history, i.conversation)
		resultChan <- history
	}
	return <-resultChan
}

// IsTaskRunning returns whether a task is currently running
func (i *Integration) IsTaskRunning() bool {
	resultChan := make(chan bool, 1)
	i.eventChan <- func() {
		resultChan <- i.activeTask != nil
	}
	return <-resultChan
}

// GetQueuedInputCount returns the number of queued inputs
func (i *Integration) GetQueuedInputCount() int {
	resultChan := make(chan int, 1)
	i.eventChan <- func() {
		resultChan <- len(i.inputQueue)
	}
	return <-resultChan
}

// ClearConversation clears the conversation history
func (i *Integration) ClearConversation() {
	i.eventChan <- func() {
		i.conversation = make([]*ConversationMessage, 0)
		// Note: Model doesn't have ClearMessages method, so we'll just clear our history
	}
}

// ShowConfirmation shows a confirmation dialog
func (i *Integration) ShowConfirmation(message string, toolInfo map[string]interface{}, callback func(bool)) {
	i.model.ShowConfirmation(message, toolInfo, callback)
}

// SetAllowlistHandler registers the callback invoked when the user selects
// "同意并不再询问" in a confirmation dialog.
func (i *Integration) SetAllowlistHandler(h func(toolName string, params map[string]interface{})) {
	i.model.SetAllowlistHandler(h)
}

// SetPermissionManager wires a permission.Manager so that slash commands
// (/yolo, /permission, /allow, /disallow, /permissions) can change the
// permission mode and session allowlist at runtime.
func (i *Integration) SetPermissionManager(mgr *permission.Manager) {
	i.permissionManager = mgr
}

// SetPersistentAllowlist wires the persistent allowlist store and workdir
// so /disallow can remove rules from persistent storage.
func (i *Integration) SetPersistentAllowlist(store *permission.PersistentAllowlistStore, workdir string) {
	i.persistentAllowlist = store
	i.workdir = workdir
}

// SetEngine wires an Engine so that slash commands (/think) can control
// thinking mode and other engine-level settings at runtime.
func (i *Integration) SetEngine(eng *engine.Engine) {
	i.engine = eng
}

// SetNewSessionCallback wires the callback used by Ctrl+R / /clear to create
// a new agent session.
func (i *Integration) SetNewSessionCallback(cb func() string) {
	i.newSessionCallback = cb
}

// StartNewSession creates a new session, clearing conversation history and UI state.
func (i *Integration) StartNewSession() {
	i.eventChan <- func() {
		i.conversation = make([]*ConversationMessage, 0)
		i.inputQueue = nil
		if i.activeTask != nil {
			i.activeTask.cancel()
			i.activeTask = nil
		}
		// Clear messages in the model
		i.model.messages = make([]MessageInterface, 0)
		i.model.updateChatView()
		newID := ""
		if i.newSessionCallback != nil {
			newID = i.newSessionCallback()
		}
		msg := "已开启新会话"
		if newID != "" {
			msg = fmt.Sprintf("已开启新会话 (id: %s)", newID)
		}
		i.model.AddMessage("system", msg)
	}
}

// HandleResize handles terminal resize events
func (i *Integration) HandleResize() {
	// tview handles resize automatically
}

// GetCurrentView returns the current active view
func (i *Integration) GetCurrentView() string {
	return i.model.activeView
}

// SwitchToView switches to the specified view
func (i *Integration) SwitchToView(view string) {
	i.model.showView(view)
}

// Cleanup performs cleanup operations
func (i *Integration) Cleanup() {
	i.eventChan <- func() {
		// Cancel any active task
		if i.activeTask != nil {
			i.activeTask.cancel()
			i.activeTask = nil
		}

		// Clear queued inputs
		i.inputQueue = make([]string, 0)

		// Stop the application
		i.cancel()
	}
}

// handleLocalSlashCommand intercepts locally-handled slash commands and
// returns true if the input was handled (so the agent is NOT called).
//
// Supported commands:
//
//	/yolo                       – switch to YOLO mode (all tools auto-approved)
//	/permission <mode>          – set mode: default | acceptEdits | yolo
//	/allow <rule>               – add a session allowlist rule (e.g. /allow Bash(git *))
//	/disallow <rule>            – remove a session allowlist rule
//	/permissions                – show current mode and allowlist rules
//	/think [on|off|status]      – control thinking mode (reasoning)
func (i *Integration) handleLocalSlashCommand(input string) bool {
	pm := i.permissionManager
	lower := strings.ToLower(strings.TrimSpace(input))

	switch {
	case lower == "/yolo":
		if pm != nil {
			pm.SetMode(permission.ModeYOLO)
		}
		i.model.AddMessage("system", "⚡ YOLO 模式已激活：所有工具将自动执行，无需确认。")
		return true

	case lower == "/permissions":
		if pm == nil {
			i.model.AddMessage("system", "权限管理器未初始化。")
			return true
		}
		mode := pm.GetMode()
		rules := pm.GetSessionAllowlist().ListRules()
		var sb strings.Builder
		fmt.Fprintf(&sb, "🔒 当前权限模式：%s\n", mode)
		if len(rules) == 0 {
			sb.WriteString("📋 Session 白名单：（空）\n")
		} else {
			sb.WriteString("📋 Session 白名单规则：\n")
			for _, r := range rules {
				fmt.Fprintf(&sb, "  • %s\n", r.RawPattern)
			}
		}
		i.model.AddMessage("system", strings.TrimRight(sb.String(), "\n"))
		return true

	case strings.HasPrefix(lower, "/permission "):
		arg := strings.TrimSpace(input[len("/permission "):])
		if pm == nil {
			i.model.AddMessage("system", "权限管理器未初始化。")
			return true
		}
		mode := permission.PermissionMode(arg)
		switch mode {
		case permission.ModeDefault, permission.ModeAcceptEdits, permission.ModeYOLO:
			pm.SetMode(mode)
			i.model.AddMessage("system", fmt.Sprintf("✅ 权限模式已切换为：%s", arg))
		default:
			i.model.AddMessage("system", fmt.Sprintf("❌ 未知模式：%s（可选：default / acceptEdits / yolo）", arg))
		}
		return true

	case strings.HasPrefix(lower, "/allow "):
		raw := strings.TrimSpace(input[len("/allow "):])
		if raw == "" {
			i.model.AddMessage("system", "❌ 规则不能为空。示例：/allow Bash(git *) 或 /allow write_file(*.go)")
			return true
		}
		if pm == nil {
			i.model.AddMessage("system", "权限管理器未初始化。")
			return true
		}
		rule := permission.ParseRule(raw)
		if rule.ToolName == "" {
			i.model.AddMessage("system", fmt.Sprintf("❌ 无效规则：%q", raw))
			return true
		}
		pm.GetSessionAllowlist().AddRule(rule)
		i.model.AddMessage("system", fmt.Sprintf("✅ 已添加白名单规则：%s", rule.RawPattern))
		return true

	case strings.HasPrefix(lower, "/disallow "):
		raw := strings.TrimSpace(input[len("/disallow "):])
		if pm == nil {
			i.model.AddMessage("system", "权限管理器未初始化。")
			return true
		}
		pm.GetSessionAllowlist().RemoveRule(raw)
		// Also remove from persistent storage
		if i.persistentAllowlist != nil && i.workdir != "" {
			if _, err := i.persistentAllowlist.RemoveRuleForWorkdir(i.workdir, raw); err != nil {
				logger.Warnf("Failed to remove persistent allowlist rule %q: %v", raw, err)
			}
		}
		i.model.AddMessage("system", fmt.Sprintf("🗑️ 已移除白名单规则：%s", raw))
		return true

	case strings.HasPrefix(lower, "/think"):
		// Handle /think command via Engine
		if i.engine == nil {
			i.model.AddMessage("system", "❌ Engine 未初始化。")
			return true
		}
		// Extract args after /think
		args := strings.TrimSpace(input[len("/think"):])
		result := i.engine.HandleThinkCommand(args)
		i.model.AddMessage("system", result)
		return true

	case lower == "/clear", lower == "/new":
		// /clear or /new: Start a new session (clear context) - equivalent to Ctrl+R
		i.StartNewSession()
		return true
	}

	return false
}
