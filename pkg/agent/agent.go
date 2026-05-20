// Package agent provides the core agent logic and execution orchestration
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/cron"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/hookservice"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/mailbox"
	"github.com/nano-harness/nano-agent/pkg/mcp"
	"github.com/nano-harness/nano-agent/pkg/memory"
	"github.com/nano-harness/nano-agent/pkg/middleware"
	agentruntime "github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/nano-harness/nano-agent/pkg/skill"
	"github.com/nano-harness/nano-agent/pkg/tools"
	"github.com/nano-harness/nano-agent/pkg/tools/builtin"
)

const (
	// DefaultMaxConsecutiveToolCalls is the default maximum number of consecutive tool calls
	DefaultMaxConsecutiveToolCalls = 5
	// DefaultTimeWindow is the default time window for loop detection
	DefaultTimeWindow = 5 * time.Minute
)

// Agent struct encapsulates the core logic of the AI agent
type Agent struct {
	config            *config.Config
	runtime           *agentruntime.AgentRuntime
	toolbox           *tools.Toolbox
	llmClient         llm.LLMClient
	memoryManager     *memory.Manager
	toolScheduler     *ToolScheduler
	eventHandler      func(event.StreamEvent) //nolint:unused
	tokenStats        *llm.TokenStats         //nolint:unused
	loopDetector      *LoopDetector
	ctx               context.Context
	cancelFn          context.CancelFunc
	shutdownOnce      sync.Once
	isSubAgent        bool                   // 标识是否为subagent
	sessionManager    *SessionManager        // Manages isolated conversation sessions
	skillManager      *skill.Manager         // Manages Claude Code compatible skills
	permissionManager *permission.Manager    // Manages tool execution permissions
	hookEngine        *middleware.HookEngine // Manages lifecycle hook execution

	// stateStore provides persistent runtime state (scheduled tasks, active skills).
	stateStore *config.StateStore

	// cachedSystemPromptBuilder holds a pre-warmed SystemPromptBuilder whose user
	// info has been asynchronously detected during agent initialization.
	cachedSystemPromptBuilder *SystemPromptBuilder

	// tuiScheduler is set when the TUI is active and handles /loop commands.
	tuiScheduler *TUIScheduler

	// manageRoutineTool holds a reference to the registered manage_routine tool
	// so SetTUIScheduler can wire the live scheduler into it.
	manageRoutineTool *builtin.ManageRoutineTool

	progressiveDisclosure *ProgressiveDisclosure

	// approvalConfirmFnOverride optionally overrides the default auto-confirm logic.
	approvalConfirmFnOverride func(string) bool

	// activeSessionID is the session ID to use for ProcessStreamWithMultimodal
	// Default is "default" for backward compatibility
	activeSessionID string

	// id is the unique identifier for this agent (e.g., "main", "subagent-0-investigator")
	id string

	// mailbox is this agent's own inbox (nil if mailbox disabled)
	mailbox mailbox.Mailbox

	// parentMailbox is the parent agent's inbox for sending messages upward (nil if root agent)
	parentMailbox mailbox.Mailbox

	// stopHooks are callbacks triggered when a turn completes
	stopHooks []func(ctx context.Context, reason string)

	// currentTurnID tracks the current turn ID for idle notifications
	currentTurnID string
}

// LoopDetector helps detect and prevent infinite loops of tool calls
type LoopDetector struct {
	MaxConsecutiveToolCalls int
	TimeWindow              time.Duration
	Timestamps              []time.Time
	Mutex                   sync.Mutex
}

// NewLoopDetector creates a new LoopDetector with default settings
func NewLoopDetector() *LoopDetector {
	return &LoopDetector{
		MaxConsecutiveToolCalls: DefaultMaxConsecutiveToolCalls,
		TimeWindow:              DefaultTimeWindow,
		Timestamps:              make([]time.Time, 0),
	}
}

// Check checks for consecutive tool calls and returns an error if a loop is detected
func (ld *LoopDetector) Check() error {
	ld.Mutex.Lock()
	defer ld.Mutex.Unlock()

	now := time.Now()
	ld.Timestamps = append(ld.Timestamps, now)

	// Remove timestamps older than the time window
	var recentTimestamps []time.Time
	for _, ts := range ld.Timestamps {
		if now.Sub(ts) <= ld.TimeWindow {
			recentTimestamps = append(recentTimestamps, ts)
		}
	}
	ld.Timestamps = recentTimestamps

	if len(ld.Timestamps) > ld.MaxConsecutiveToolCalls {
		return fmt.Errorf("potential loop detected: %d consecutive tool calls within %v", len(ld.Timestamps), ld.TimeWindow)
	}

	return nil
}

// GenerateTitle generates a title for the session based on history
func (a *Agent) GenerateTitle(history []llm.Message) (string, error) {
	if len(history) == 0 {
		return "", fmt.Errorf("empty history")
	}

	var contextBuilder strings.Builder
	count := 0
	for _, msg := range history {
		if msg.Role == "system" {
			continue
		}

		content := msg.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}

		_, _ = fmt.Fprintf(&contextBuilder, "%s: %s\n", msg.Role, content)
		count++
		if count >= 4 {
			break
		}
	}

	if contextBuilder.Len() == 0 {
		return "", fmt.Errorf("no suitable content for title generation")
	}

	prompt := fmt.Sprintf(`请根据以下对话内容，生成一个简短的标题（10个字以内）。
要求：
1. 标题要能概括对话的主要话题
2. 不要使用引号、书名号等标点符号
3. 保持与对话相同的语言（如果是中文对话就用中文）
4. 直接返回标题文本，不要有其他解释

对话内容：
%s`, contextBuilder.String())

	return a.llmClient.GenerateContent(context.Background(), prompt)
}

// matchesWildcard checks if a name matches a wildcard pattern
func matchesWildcard(name, pattern string) bool {
	matched, err := filepath.Match(pattern, name)
	return err == nil && matched
}

// Option defines a functional option for configuring the Agent
type Option func(*Agent)

// WithLLMClient allows overriding the default LLM client with a custom one (e.g., a mock)
func WithLLMClient(client llm.LLMClient) Option {
	return func(a *Agent) {
		a.llmClient = client
	}
}

// New creates a new AI agent instance
func New(cfg *config.Config, approvalHandler func(*ToolCallInfo) bool, opts ...Option) (*Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	bootstrap, err := buildAgentBootstrap(cfg, approvalHandler)
	if err != nil {
		return nil, err
	}

	agent := &Agent{
		ctx:             bootstrap.agentCtx,
		cancelFn:        bootstrap.agentCancel,
		config:          cfg,
		toolbox:         bootstrap.toolbox,
		llmClient:       bootstrap.llmClient,
		memoryManager:   bootstrap.memoryManager,
		toolScheduler:   bootstrap.toolScheduler,
		loopDetector:    NewLoopDetector(),
		isSubAgent:      cfg.IsSubAgent,
		sessionManager:  bootstrap.sessionManager,
		stateStore:      bootstrap.stateStore,
		activeSessionID: "default", // Default session ID for backward compatibility
		hookEngine:      bootstrap.hookEngine,
	}

	agent.permissionManager = bootstrap.permissionManager

	// Apply functional options
	for _, opt := range opts {
		opt(agent)
	}

	agent.runtime = newAgentRuntime(agent)
	agent.skillManager = bootstrap.skillManager
	if agent.hookEngine != nil {
		agent.hookEngine.SetAsyncRunner(hookservice.NewAsyncRunner(func(ctx context.Context, sessionID, reason string) error {
			if agent.mailbox == nil {
				return nil
			}
			if reason == "" {
				reason = "async ralph hook requested continuation"
			}
			return agent.mailbox.Send(ctx, mailbox.Message{
				ID:        fmt.Sprintf("ralph-%d", time.Now().UnixNano()),
				From:      "ralph-hook",
				To:        sessionID,
				Topic:     "ralph_async_rewake",
				Body:      map[string]interface{}{"content": reason, "session_id": sessionID},
				Timestamp: time.Now().UnixMilli(),
			})
		}))
	}

	// Register conversational configuration management tools.
	if !cfg.IsSubAgent {
		agent.registerBuiltinManagementTools(agent.toolbox, cfg, bootstrap.workingDir)
	}

	maybeStartAgentMCPClient(cfg, agent.toolbox)

	logger.Info("Agent initialized successfully")

	// Preload system info asynchronously to avoid delay on first conversation.
	// Only preload when auto-detection is enabled and no custom system prompt overrides it
	// (custom prompt callers often disable detection to avoid subprocess side effects in tests).
	agent.cachedSystemPromptBuilder = newPreloadedSystemPromptBuilder(cfg, bootstrap.workingDir, agent.toolbox, agent.memoryManager)

	return agent, nil
}

// GetTool returns a specific tool by name
func (a *Agent) GetTool(name string) (interfaces.Tool, bool) {
	return a.toolbox.Get(name)
}

// GetWorkingDirectory returns the current working directory
func (a *Agent) GetWorkingDirectory() string {
	workingDir, _ := os.Getwd()
	return workingDir
}

// GetLLMClient returns the LLM client for advanced usage
func (a *Agent) GetLLMClient() llm.LLMClient {
	return a.llmClient
}

// GetToolbox returns the toolbox for advanced usage
func (a *Agent) GetToolbox() *tools.Toolbox {
	return a.toolbox
}

// Runtime returns the behavior-preserving runtime boundary for core agent
// dependencies.
func (a *Agent) Runtime() *agentruntime.AgentRuntime {
	return a.runtime
}

// GetToolScheduler returns the tool scheduler for advanced usage
func (a *Agent) GetToolScheduler() *ToolScheduler {
	return a.toolScheduler
}

// GetEventHandler returns the current event handler (may be nil)
func (a *Agent) GetEventHandler() func(event.StreamEvent) {
	return a.eventHandler
}

// GetPermissionManager returns the permission manager for the agent.
func (a *Agent) GetPermissionManager() *permission.Manager {
	return a.permissionManager
}

// SetApprovalHandler sets the approval handler for the agent.
func (a *Agent) SetApprovalHandler(handler func(*ToolCallInfo) bool) {
	a.toolScheduler.SetApprovalHandler(handler)
}

// K2.3: SetApprovalHandlerV2 sets the V2 approval handler for the agent.
func (a *Agent) SetApprovalHandlerV2(handler func(*ToolCallInfo) ApprovalDecision) {
	a.toolScheduler.SetApprovalHandlerV2(handler)
}

// GetMemoryManager returns the memory manager for advanced usage
func (a *Agent) GetMemoryManager() *memory.Manager {
	return a.memoryManager
}

// SetMemoryManager replaces the agent's memory manager
func (a *Agent) SetMemoryManager(m *memory.Manager) {
	a.memoryManager = m
}

// SetIsSubAgent marks whether this agent is a sub-agent
func (a *Agent) SetIsSubAgent(v bool) {
	a.isSubAgent = v
}

// validateMessageSequence checks if a message sequence is valid
func (a *Agent) validateMessageSequence(history []llm.Message) error { //nolint:unused
	for i, msg := range history {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			hasFollowingToolMessage := false
			for j := i + 1; j < len(history); j++ {
				nextMsg := history[j]
				if nextMsg.Role == "tool" {
					hasFollowingToolMessage = true
					break
				}
				if nextMsg.Role == "assistant" {
					break
				}
			}
			if !hasFollowingToolMessage {
				return fmt.Errorf("assistant message at index %d has tool_calls but no following tool messages", i)
			}
		}
	}
	return nil
}

// cleanupMessageSequence removes incomplete tool call sequences
func (a *Agent) cleanupMessageSequence(history []llm.Message) []llm.Message {
	if len(history) == 0 {
		return history
	}
	var cleanedHistory []llm.Message
	for i, msg := range history {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			hasFollowingToolMessage := false
			for j := i + 1; j < len(history); j++ {
				nextMsg := history[j]
				if nextMsg.Role == "tool" {
					hasFollowingToolMessage = true
					break
				}
				if nextMsg.Role == "assistant" {
					break
				}
			}
			if hasFollowingToolMessage {
				cleanedHistory = append(cleanedHistory, msg)
			} else {
				cleanedMsg := msg
				cleanedMsg.ToolCalls = nil
				if cleanedMsg.Content == "" {
					cleanedMsg.Content = "(Tool call interrupted)"
				}
				cleanedHistory = append(cleanedHistory, cleanedMsg)
				logger.Warnf("Removed incomplete tool calls at index %d", i)
			}
		} else {
			cleanedHistory = append(cleanedHistory, msg)
		}
	}
	return cleanedHistory
}

// DeleteSession deletes a session.
func (a *Agent) DeleteSession(sessionID string) (bool, error) {
	return a.sessionManager.DeleteSession(sessionID)
}

// ProcessStreamWithMultimodalAndSession processes a request with multimodal content and a specific session ID.
func (a *Agent) ProcessStreamWithMultimodalAndSession(ctx context.Context, sessionID string, userInput string, images []llm.MultimodalImage, onEvent func(event.StreamEvent)) error {
	// Get or create session
	session := a.sessionManager.GetOrCreateSession(sessionID)

	// Send session info event
	onEvent(event.StreamEvent{
		Type:      event.EventTypeSessionInfo,
		SessionID: session.ID,
		Content:   fmt.Sprintf("Using session: %s", session.ID),
		Timestamp: time.Now().Unix(),
	})

	// Process with session's conversation history
	return a.processStreamWithSessionInternal(ctx, session, userInput, images, onEvent)
}

// processStreamWithSessionInternal processes a request using a specific session's conversation history.
func (a *Agent) processStreamWithSessionInternal(ctx context.Context, session *Session, userInput string, images []llm.MultimodalImage, onEvent func(event.StreamEvent)) error {
	logger.Infof("Processing streaming request with session %s: %s", session.ID, userInput)
	_ = a.sessionManager.TransitionSessionState(session.ID, SessionStateActive, "turn_started")
	defer func() {
		if session.GetState() == SessionStateActive {
			_ = a.sessionManager.TransitionSessionState(session.ID, SessionStateIdle, "turn_finished")
		}
	}()

	var turnAllowedTools []interfaces.Tool
	{
		wd := a.toolbox.GetWorkingDirectory()
		if def, processed, args, ok := skill.ParseSlashCommand(wd, userInput); ok { //nolint:ineffassign,staticcheck
			// Extract and optionally execute prelude shell lines
			preludes, rest := skill.ExtractCommandPreludes(def.Body)
			preludeOutput := ""
			// Check if shell tool is allowed (exact or wildcard)
			allowShell := false
			if len(def.AllowedTools) == 0 {
				allowShell = false
			} else {
				for _, pat := range def.AllowedTools {
					if pat == "run_shell_command" || (strings.ContainsAny(pat, "*?") && matchesWildcard("run_shell_command", pat)) {
						allowShell = true
						break
					}
				}
			}
			if len(preludes) > 0 {
				if allowShell {
					if tool, ok := a.toolbox.Get("run_shell_command"); ok {
						preludeFailed := false
						for _, cmd := range preludes {
							params := map[string]interface{}{
								"command":         cmd,
								"capture_output":  true,
								"timeout_seconds": float64(def.PreludeTimeoutSeconds),
								"directory":       wd,
							}
							res, _ := tool.Execute(context.Background(), params)
							if res != nil {
								// format control
								format := def.PreludeOutput
								if !res.Success {
									preludeFailed = true
								}
								switch format {
								case "full":
									preludeOutput += res.UserContent + "\n"
								case "summary":
									status := "SUCCESS"
									if !res.Success {
										status = "FAILED"
									}
									preludeOutput += fmt.Sprintf("[prelude] %s → %s\n", cmd, status)
								}
							}
						}
						if preludeFailed && strings.ToLower(def.PreludeOnError) == "abort" {
							return fmt.Errorf("prelude command failed and policy=abort")
						}
					}
				} else {
					preludeOutput += "[⚠️ Prelude shell commands skipped: run_shell_command not allowed by allowed-tools]\n"
				}
			}
			if def.PermissionProfile != "" && a.permissionManager != nil {
				mode := permission.PermissionMode(def.PermissionProfile)
				if permission.IsValidMode(mode) {
					previousMode := a.permissionManager.GetMode()
					a.permissionManager.SetMode(mode)
					defer a.permissionManager.SetMode(previousMode)
				} else {
					logger.Warnf("Ignoring invalid permission-profile %q for slash command %q", def.PermissionProfile, def.Name)
				}
			}
			// Re-render prompt without prelude lines
			processed = skill.RenderCommandBody(rest, args)
			if preludeOutput != "" {
				userInput = preludeOutput + "\n" + processed
			} else {
				userInput = processed
			}
			if len(def.AllowedTools) > 0 && a.toolScheduler != nil {
				// Limit exposed tools to LLM and enforce at scheduler level
				allowedExact := make(map[string]struct{}, len(def.AllowedTools))
				var allowedPatterns []string
				for _, n := range def.AllowedTools {
					if n == "" {
						continue
					}
					if strings.ContainsAny(n, "*?") {
						allowedPatterns = append(allowedPatterns, n)
					} else {
						allowedExact[n] = struct{}{}
					}
				}
				// Build filtered tools list for this turn with wildcard support
				filtered := make([]interfaces.Tool, 0)
				for _, t := range a.toolbox.List() {
					name := t.Name()
					if _, ok := allowedExact[name]; ok {
						filtered = append(filtered, t)
						continue
					}
					for _, pat := range allowedPatterns {
						if matchesWildcard(name, pat) {
							filtered = append(filtered, t)
							break
						}
					}
				}
				// Temporarily set whitelist on scheduler
				a.toolScheduler.SetAllowedTools(def.AllowedTools)
				defer a.toolScheduler.ClearAllowedTools()
				// Override tools for this turn via context (we'll pass filtered later)
				// Store filtered tools in context via closure variable
				turnAllowedTools = filtered
			}
		}
	}
	logger.Infof("Starting agent request processing with session %s: %s", session.ID, userInput)
	if len(images) > 0 {
		logger.Infof("Processing with %d multimodal images", len(images))
	}

	// Get session's conversation history
	sessionHistory := session.GetConversationHistory()

	sessionHistory = a.cleanupMessageSequence(sessionHistory)
	ralphCtx := session.RalphContext()
	ralphCtx.Configure(a.config)
	goalCtx := session.GoalContext()
	goalCtx.Configure(a.config)

	// Sanitize all outgoing events centrally
	validator := event.NewEventValidator()
	// Apply config-driven redaction rules (daemon-level overrides take precedence)
	if a.config != nil {
		var sr *config.SecretRedactionConfig
		if a.config.Daemon != nil && a.config.Daemon.SecretRedaction != nil {
			sr = a.config.Daemon.SecretRedaction
		} else {
			sr = a.config.SecretRedaction
		}
		if sr != nil && sr.Enabled {
			if !sr.IncludeDefaults {
				validator.ClearRedactionPatterns()
				validator.SetSensitiveKeys(nil)
			}
			if len(sr.SensitiveKeys) > 0 {
				validator.MergeSensitiveKeys(sr.SensitiveKeys)
			}
			for _, p := range sr.Additional {
				if err := validator.AddRedactionPatternString(p.Regex, p.Replacement); err != nil {
					logger.Warnf("Invalid redaction pattern '%s': %v", p.Name, err)
				}
			}
		}
	}
	sanitizedOnEvent := func(ev event.StreamEvent) {
		// Add session ID to all events
		ev.SessionID = session.ID
		if ev.Type == event.EventTypeWaitingForUser {
			_ = a.sessionManager.TransitionSessionState(session.ID, SessionStateAwaitingInput, "waiting_for_user")
		}
		clean := validator.SanitizeEvent(ev)
		onEvent(clean)
	}

	// Create turn configuration with session's conversation history
	turnConfig := &TurnConfig{
		WorkingDir:    a.toolbox.GetWorkingDirectory(),
		Toolbox:       a.toolbox,
		LLMClient:     a.llmClient,
		MemoryManager: a.memoryManager,
		Tools: func() []interfaces.Tool {
			if turnAllowedTools != nil {
				return turnAllowedTools
			}
			return a.toolbox.List()
		}(),
		ToolScheduler:             a.toolScheduler,
		TUIScheduler:              a.tuiScheduler,
		InitialMessages:           sessionHistory,
		IsSubAgent:                a.isSubAgent,
		AgentConfig:               a.config,
		SkillManager:              a.skillManager,
		CachedSystemPromptBuilder: a.cachedSystemPromptBuilder,
		SessionID:                 session.ID, // Pass session ID for background task isolation (Phase 2)
		Agent:                     a,          // Pass agent reference for mailbox injection
		HookEngine:                a.hookEngine,
		RalphContext:              ralphCtx,
		GoalContext:               goalCtx,
		GoalEvaluator:             NewGoalEvaluator(a.llmClient),
		Transcript:                session.Transcript(),
	}

	if a.toolScheduler != nil {
		a.toolScheduler.SetEventHandler(sanitizedOnEvent)
	}

	a.eventHandler = sanitizedOnEvent // Store in agent for GetEventHandler()
	nextInput := userInput
	nextImages := images
	var turn *Turn
	var err error
	for {
		turnConfig.InitialMessages = sessionHistory
		turn = NewTurnWithMultimodal(nextInput, nextImages, turnConfig)
		turn.eventHandler = sanitizedOnEvent
		err = turn.Execute(ctx)

		filteredMessages := make([]llm.Message, 0, len(turn.Messages))
		for _, msg := range turn.Messages {
			if msg.Role == "system" {
				continue
			}
			filteredMessages = append(filteredMessages, msg)
		}
		session.SetConversationHistory(filteredMessages)
		if info := turn.GetCompressionInfo(); info != nil {
			session.SetMetadata("last_compression", info)
			a.writeCompactionCheckpoint(session, info)
		}

		var totalTokens int
		if turn.TokenStats != nil {
			if turn.TokenStats.SessionTotalTokens > 0 {
				totalTokens = turn.TokenStats.SessionTotalTokens
			} else {
				totalTokens = turn.TokenStats.TotalTokens
			}
		}
		session.UpdateStats(totalTokens, time.Since(turn.StartTime).Seconds())

		if errors.Is(err, ErrContinueRequested) && goalCtx.IsActive() {
			sanitizedOnEvent(event.NewStreamEvent(event.EventTypeWarning, "agent_turn").
				WithContent(fmt.Sprintf("[Goal continuation %d/%d] %s", goalCtx.Snapshot().TurnsEvaluated, goalCtx.MaxTurns(), turn.ContinuationReason())))
			nextInput = turn.ContinuationReason()
			nextImages = nil
			sessionHistory = filteredMessages
			continue
		}

		if errors.Is(err, ErrContinueRequested) && ralphCtx.IsEnabled() {
			iter, exceeded := ralphCtx.NextIteration()
			if exceeded {
				logger.Warnf("Ralph-loop max iteration (%d) reached, forcing stop", ralphCtx.Max())
				sanitizedOnEvent(event.NewStreamEvent(event.EventTypeRalphStopped, "agent_turn").
					WithContent("ralph-loop max iteration reached").
					WithMetadata("iteration", iter).
					WithMetadata("max_iterations", ralphCtx.Max()))
				err = nil
				break
			}
			sanitizedOnEvent(event.NewStreamEvent(event.EventTypeRalphIteration, "agent_turn").
				WithContent(fmt.Sprintf("[Ralph iteration %d/%d]", iter, ralphCtx.Max())).
				WithMetadata("iteration", iter).
				WithMetadata("max_iterations", ralphCtx.Max()))
			nextInput = turn.ContinuationReason()
			nextImages = nil
			sessionHistory = filteredMessages
			ralphCtx.SetActive(true)
			continue
		}
		break
	}
	ralphCtx.Reset()
	if errors.Is(err, ErrContinueRequested) {
		err = nil
	}

	history := session.GetConversationHistory()
	if len(history) >= 1 {
		shouldGenerate := false

		title, hasTitle := session.GetMetadata("title")
		titleStr, _ := title.(string)
		if !hasTitle || titleStr == "" || titleStr == "New Chat" || titleStr == "新会话" || strings.HasPrefix(titleStr, "Chat ") {
			shouldGenerate = true
		}

		if shouldGenerate && !a.isSubAgent {
			historyCopy := append([]llm.Message(nil), history...)
			ctx, cancel := context.WithCancel(context.Background())
			cancelIdx := a.sessionManager.RegisterBackgroundCancel(session.ID, cancel)

			go func(s *Session, h []llm.Message) {
				defer cancel()
				defer a.sessionManager.UnregisterBackgroundCancel(s.ID, cancelIdx)

				// Try to use first user message as title if short enough
				var firstUserMsg string
				for _, msg := range h {
					if msg.Role == "user" {
						firstUserMsg = strings.TrimSpace(msg.Content)
						break
					}
				}

				// If short message (<= 20 runes), use it directly
				if firstUserMsg != "" && len([]rune(firstUserMsg)) <= 20 {
					// Clean up newlines for title
					title := strings.ReplaceAll(firstUserMsg, "\n", " ")
					s.SetMetadata("title", title)
					if err := a.sessionManager.SaveSessionIfActive(ctx, s.ID); err != nil {
						logger.Debugf("Could not save session %s after setting short title: %v", s.ID, err)
					} else {
						logger.Infof("Used short user message as title for session %s: %s", s.ID, title)
					}
					return
				}

				// Otherwise generate with LLM
				generatedTitle, err := a.GenerateTitle(h)
				if err == nil && generatedTitle != "" {
					s.SetMetadata("title", generatedTitle)

					if err := a.sessionManager.SaveSessionIfActive(ctx, s.ID); err != nil {
						logger.Debugf("Could not save session %s after title generation: %v", s.ID, err)
					} else {
						logger.Infof("Generated title for session %s: %s", s.ID, generatedTitle)
					}

					// Send session info event to notify listeners (e.g. CLI, UI)
					if onEvent != nil {
						onEvent(event.StreamEvent{
							Type:      event.EventTypeSessionInfo,
							SessionID: s.ID,
							Title:     generatedTitle,
							Timestamp: time.Now().Unix(),
							Metadata:  map[string]interface{}{"title": generatedTitle},
						})
					}
				} else if err != nil {
					logger.Debugf("Failed to generate title for session %s: %v", s.ID, err)
				}
			}(session, historyCopy)
		}
	}

	// Auto-save to storage if enabled
	if err := a.sessionManager.SaveSession(session.ID); err != nil {
		logger.Warnf("Failed to save session %s to storage: %v", session.ID, err)
	}

	if err != nil {
		// Don't log error if task was cancelled by user
		if errors.Is(err, context.Canceled) {
			logger.Debugf("Task cancelled by user")
			return err
		}
		errMsg := fmt.Sprintf("turn execution failed: %v, \n stack trace: %s", err, debug.Stack())
		logger.Errorf("%s", errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	logger.Infof("Agent request processing with session %s completed successfully", session.ID)
	return nil
}

// GetContextStatus returns context budget and compression status for a session.
func (a *Agent) GetContextStatus(sessionID string) (ContextStatus, bool) {
	if a == nil || a.sessionManager == nil {
		return ContextStatus{}, false
	}
	session, ok := a.sessionManager.GetSession(sessionID)
	if !ok || session == nil {
		return ContextStatus{}, false
	}
	strategy := NewCompressionStrategy()
	var last *CompressionInfo
	if raw, ok := session.GetMetadata("last_compression"); ok {
		if info, ok := raw.(*CompressionInfo); ok {
			last = info
		}
	}
	status := strategy.Status(session.GetConversationHistory(), "", last)
	if a.config != nil {
		status.CompressionEnabled = a.config.ContextConfig.EnableCompression
	}
	return status, true
}

// ProcessStream processes a user request with streaming output using Turn-based approach
func (a *Agent) ProcessStream(ctx context.Context, userInput string, onEvent func(event.StreamEvent)) error {
	return a.ProcessStreamWithMultimodal(ctx, userInput, nil, onEvent)
}

// ProcessStreamWithMultimodal processes a user request with multimodal content and streaming output.
// It uses the active session ID set via SetActiveSessionID.
func (a *Agent) ProcessStreamWithMultimodal(ctx context.Context, userInput string, images []llm.MultimodalImage, onEvent func(event.StreamEvent)) error {
	return a.ProcessStreamWithMultimodalAndSession(ctx, a.activeSessionID, userInput, images, onEvent)
}

// SetActiveSessionID sets the active session ID for ProcessStreamWithMultimodal
func (a *Agent) SetActiveSessionID(id string) {
	if id == "" {
		id = "default"
	}
	a.activeSessionID = id
}

// GetActiveSessionID returns the current active session ID
func (a *Agent) GetActiveSessionID() string {
	return a.activeSessionID
}

// StartNewSession creates a brand-new session with a fresh ID, sets it as the
// active session, and returns the new session ID. The previous session remains
// in the SessionManager (subject to TTL-based cleanup) so it can still be
// resumed via --session <old_id>.
func (a *Agent) StartNewSession() string {
	newSession := a.sessionManager.GetOrCreateSession("") // empty -> auto generate
	a.SetActiveSessionID(newSession.ID)
	return newSession.ID
}

// GetSessionManager returns the session manager for external access.
func (a *Agent) GetSessionManager() *SessionManager {
	return a.sessionManager
}

func (a *Agent) HandleGoalCommand(sessionID, args string) string {
	if a == nil || a.sessionManager == nil {
		return "❌ Agent 未初始化。"
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = a.GetActiveSessionID()
	}
	session := a.sessionManager.GetOrCreateSession(sessionID)
	ctx := session.GoalContext()
	ctx.Configure(a.config)
	args = strings.TrimSpace(args)
	lower := strings.ToLower(args)
	switch {
	case args == "":
		return ctx.Status()
	case lower == "clear" || lower == "stop" || lower == "off" || lower == "reset" || lower == "none" || lower == "cancel":
		ctx.Clear()
		if err := a.sessionManager.SaveSession(session.ID); err != nil {
			logger.Warnf("Failed to save cleared goal for session %s: %v", session.ID, err)
		}
		return "✅ /goal 已清除。"
	default:
		if err := ctx.SetGoal(args); err != nil {
			return fmt.Sprintf("❌ 设置 /goal 失败：%v", err)
		}
		if err := a.sessionManager.SaveSession(session.ID); err != nil {
			logger.Warnf("Failed to save goal for session %s: %v", session.ID, err)
		}
		return "✅ /goal 已设置：" + args
	}
}

// GetSkillManager returns the loaded skill manager for TUI command wiring.
func (a *Agent) GetSkillManager() *skill.Manager {
	return a.skillManager
}

// Shutdown gracefully shuts down the agent and its resources
func (a *Agent) Shutdown() error {
	var err error
	a.shutdownOnce.Do(func() {
		logger.Info("Shutting down agent")

		// Cancel agent context to stop all goroutines
		if a.cancelFn != nil {
			a.cancelFn()
		}

		// Stop session manager if running
		if a.sessionManager != nil {
			a.sessionManager.Shutdown()
		}

		// Stop MCP client if running
		if a.toolbox.IsMCPEnabled() {
			if e := a.toolbox.StopMCP(); e != nil {
				logger.Errorf("Failed to stop MCP client: %v", e)
				err = e
			} else {
				logger.Info("MCP client stopped successfully")
			}
		}

		// Close toolbox and its channels
		a.toolbox.Close()
		if a.hookEngine != nil {
			_ = a.hookEngine.Close()
		}

		logger.Info("Agent shutdown completed")
	})
	return err
}

func (a *Agent) writeCompactionCheckpoint(session *Session, info *CompressionInfo) {
	if a == nil || a.sessionManager == nil || session == nil || info == nil {
		return
	}
	storage, ok := a.sessionManager.GetStorage().(IncrementalSessionStorage)
	if !ok {
		return
	}
	hash := sha256.Sum256([]byte(info.Summary))
	marker := CompactionMarker{
		OriginalMessageCount:   info.MessagesBefore,
		CompressedMessageCount: info.MessagesAfter,
		OriginalTokens:         info.OriginalTokens,
		CompressedTokens:       info.CompressedTokens,
		SummaryHash:            fmt.Sprintf("%x", hash[:]),
		LastSeqBeforeCompact:   session.LastPersistedSeq,
	}
	if err := storage.WriteCheckpoint(session.ID, marker); err != nil {
		logger.Warnf("Failed to write compaction checkpoint for session %s: %v", session.ID, err)
		return
	}
	session.LastCompactionSeq = marker.LastSeqBeforeCompact + 1
	a.sessionManager.emitLifecycle(context.Background(), session.ID, SessionLifecycleCheckpoint, map[string]interface{}{"reason": "autocompact"})
}

// convertMCPConfig converts consolidated config.MCPConfig to mcp.MCPConfig
func convertMCPConfig(cfg *config.MCPConfig) *mcp.MCPConfig {
	if cfg == nil {
		return nil
	}

	// Convert server configs
	servers := make([]mcp.MCPServerConfig, len(cfg.Servers))
	for i, server := range cfg.Servers {
		servers[i] = mcp.MCPServerConfig{
			Name:        server.Name,
			Description: server.Description,
			Command:     server.Command,
			URL:         server.URL,
			Transport:   server.Transport,
			Headers:     server.Headers,
			Enabled:     server.Enabled,
			Timeout:     server.Timeout,
		}
	}

	// Convert TLS config
	var tlsConfig *mcp.TLSConfig
	if cfg.TLS != nil {
		tlsConfig = &mcp.TLSConfig{
			Enabled:    cfg.TLS.Enabled,
			CertFile:   cfg.TLS.CertFile,
			KeyFile:    cfg.TLS.KeyFile,
			CAFile:     cfg.TLS.CAFile,
			SkipVerify: cfg.TLS.SkipVerify,
		}
	}

	return &mcp.MCPConfig{
		EnableClient:        cfg.EnableClient,
		MCPServers:          servers,
		DefaultTransport:    cfg.DefaultTransport,
		Timeout:             cfg.Timeout,
		MaxRetries:          cfg.MaxRetries,
		EnableAuth:          cfg.EnableAuth,
		AuthTokens:          cfg.AuthTokens,
		TLSConfig:           tlsConfig,
		EnableHealthCheck:   cfg.EnableHealthCheck,
		HealthCheckInterval: cfg.HealthCheckInterval,
		HealthCheckTimeout:  cfg.HealthCheckTimeout,
	}
}

// registerBuiltinManagementTools registers the conversational configuration
// management tools (manage_skill, manage_mcp_server, manage_schedule,
// discover_tools, discover_skills) into the toolbox.
func (a *Agent) registerBuiltinManagementTools(tb *tools.Toolbox, cfg *config.Config, workingDir string) {
	// Resolve the project-level config path for MCP mutations.
	configPath := filepath.Join(workingDir, ".nano.yaml")

	// manage_skill: delegates to the agent's skill manager (may be nil)
	manageSkillTool := builtin.NewManageSkillTool(a.skillManager, a.approvalConfirmFn)
	if err := tb.Register(manageSkillTool); err != nil {
		logger.Debugf("manage_skill already registered: %v", err)
	}

	// manage_mcp_server: writes to the project-level config file
	manageMCPTool := builtin.NewManageMCPTool(cfg, configPath, a.approvalConfirmFn)
	if err := tb.Register(manageMCPTool); err != nil {
		logger.Debugf("manage_mcp_server already registered: %v", err)
	}

	manageExtensionTool := builtin.NewManageExtensionTool(a.skillManager, cfg, configPath, tb, a.approvalConfirmFn)
	if err := tb.Register(manageExtensionTool); err != nil {
		logger.Debugf("manage_extension already registered: %v", err)
	}

	// manage_routine: scheduler is nil until TUI starts; it is wired in
	// SetTUIScheduler via ManageRoutineTool.SetScheduler.
	manageRoutineTool := builtin.NewManageRoutineTool(nil, a.stateStore, a.approvalConfirmFn)
	if err := tb.Register(manageRoutineTool); err != nil {
		logger.Debugf("manage_routine already registered: %v", err)
	}
	// Store a reference so SetTUIScheduler can wire the live scheduler later.
	a.manageRoutineTool = manageRoutineTool

	// discover_tools: search + full-schema lookup (returns full JSON schema)
	discoverToolsTool := builtin.NewDiscoverToolsTool(func(toolName string) (string, bool) {
		t, ok := tb.Get(toolName)
		if !ok {
			return "", false
		}
		schema := t.Schema()
		if schema == nil {
			return "", false
		}
		data, err := json.Marshal(schema)
		if err != nil {
			return schema.Description, true
		}
		return string(data), true
	})
	// Index all currently registered tools so search/list works.
	for _, tool := range tb.List() {
		category := ""
		if schema := tool.Schema(); schema != nil {
			category = string(tool.Category())
		}
		discoverToolsTool.IndexTool(tool.Name(), tool.Description(), category, "")
	}
	if err := tb.Register(discoverToolsTool); err != nil {
		logger.Debugf("discover_tools already registered: %v", err)
	}

	// discover_skills: skill search + full instructions lookup
	discoverSkillsTool := builtin.NewDiscoverSkillsTool(a.skillManager)
	if err := tb.Register(discoverSkillsTool); err != nil {
		logger.Debugf("discover_skills already registered: %v", err)
	}

	a.progressiveDisclosure = NewProgressiveDisclosure(20, 5)
	discoverToolsTool.SetOnExpand(a.progressiveDisclosure.MarkExpanded)
	a.progressiveDisclosure.IndexTools(tb.List())
	if setter, ok := a.llmClient.(interface{ SetToolGate(interfaces.ToolGate) }); ok {
		setter.SetToolGate(a.progressiveDisclosure)
	}
	// Connect ProgressiveDisclosure to ToolScheduler for schema auto-injection
	a.toolScheduler.SetProgressiveDisclosure(a.progressiveDisclosure)

	logger.Info("Conversational management tools registered")
}

// approvalConfirmFn is a stub that auto-confirms. In TUI mode, the caller
// replaces this with a proper user-facing confirmation dialog.
// This default ensures tests and CLI mode work without interactive input.
func (a *Agent) approvalConfirmFn(summary string) bool {
	if a.approvalConfirmFnOverride != nil {
		return a.approvalConfirmFnOverride(summary)
	}
	return true
}

// SetApprovalConfirmFn allows replacing the default auto-confirm function with
// an interactive confirmation dialog (e.g. from the TUI).
func (a *Agent) SetApprovalConfirmFn(fn func(string) bool) {
	a.approvalConfirmFnOverride = fn
}

// GetApprovalConfirmFn returns the agent's confirmation function so external
// components (e.g. Engine) can pass it to tools that need user confirmation.
func (a *Agent) GetApprovalConfirmFn() func(string) bool {
	return a.approvalConfirmFn
}

// GetStateStore returns the agent's persistent state store.
func (a *Agent) GetStateStore() *config.StateStore {
	return a.stateStore
}

// GetConfig returns the agent's configuration
func (a *Agent) GetConfig() *config.Config {
	return a.config
}

// SetScheduler wires a live cron.Scheduler directly into the manage_routine
// tool so tasks created via conversation are scheduled immediately.
// This is the preferred method when using the Engine; SetTUIScheduler is kept
// for backward compatibility with the classic TUI path.
func (a *Agent) SetScheduler(s *cron.Scheduler) {
	if s != nil && a.manageRoutineTool != nil {
		a.manageRoutineTool.SetScheduler(s)
	}
}

// SetTUIScheduler wires a TUIScheduler so that /routines commands work in turns.
// It also updates the manage_routine tool with the live cron scheduler so
// tasks created via conversation are scheduled immediately.
func (a *Agent) SetTUIScheduler(ts *TUIScheduler) {
	a.tuiScheduler = ts
	if ts != nil && a.manageRoutineTool != nil {
		a.manageRoutineTool.SetScheduler(ts.Scheduler())
	}
}

// ID returns the unique identifier for this agent
func (a *Agent) ID() string {
	return a.id
}

// SetID sets the unique identifier for this agent
func (a *Agent) SetID(id string) {
	a.id = id
}

// Mailbox returns this agent's own inbox
func (a *Agent) Mailbox() mailbox.Mailbox {
	return a.mailbox
}

// SetMailbox sets this agent's own inbox
func (a *Agent) SetMailbox(mb mailbox.Mailbox) {
	a.mailbox = mb
}

// ParentMailbox returns the parent agent's inbox for sending messages upward
func (a *Agent) ParentMailbox() mailbox.Mailbox {
	return a.parentMailbox
}

// SetParentMailbox sets the parent agent's inbox
func (a *Agent) SetParentMailbox(mb mailbox.Mailbox) {
	a.parentMailbox = mb
}

// RegisterStopHook registers a callback to be called when a turn completes
func (a *Agent) RegisterStopHook(hook func(ctx context.Context, reason string)) {
	a.stopHooks = append(a.stopHooks, hook)
}

// StopHooks returns all registered stop hooks
func (a *Agent) StopHooks() []func(ctx context.Context, reason string) {
	return a.stopHooks
}

// CurrentTurnID returns the current turn ID
func (a *Agent) CurrentTurnID() string {
	return a.currentTurnID
}

// SetCurrentTurnID sets the current turn ID
func (a *Agent) SetCurrentTurnID(id string) {
	a.currentTurnID = id
}

// IsSubAgent returns true if this agent was configured as a teammate/sub-agent.
func (a *Agent) IsSubAgent() bool {
	return a.isSubAgent
}

// EmitEvent emits an event through the agent's current active event handler.
// This allows tools (like send_message) to emit events without directly accessing the turn.
// If no active turn/handler is available, logs a debug message (non-fatal).
func (a *Agent) EmitEvent(ev event.StreamEvent) {
	if a.eventHandler != nil {
		a.eventHandler(ev)
	} else {
		logger.Debugf("EmitEvent called but no active event handler (agent %s)", a.id)
	}
}
