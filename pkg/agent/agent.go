// Package agent provides the core agent logic and execution orchestration
package agent

import (
	"context"
	"encoding/json"
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
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/mcp"
	"github.com/nano-harness/nano-agent/pkg/memory"
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
	toolbox           *tools.Toolbox
	llmClient         llm.LLMClient
	memoryManager     *memory.Manager
	toolScheduler     *ToolScheduler
	eventHandler      func(event.StreamEvent) //nolint:unused
	tokenStats        *llm.TokenStats         //nolint:unused
	loopDetector      *LoopDetector
	cancelFn          context.CancelFunc  //nolint:unused
	shutdownOnce      sync.Once           //nolint:unused
	isSubAgent        bool                // 标识是否为subagent
	isForkChild       bool                // marks this agent as a forked child
	agentType         AgentType           // built-in agent type for fork children
	sessionManager    *SessionManager     // Manages isolated conversation sessions
	skillManager      *skill.Manager      // Manages Claude Code compatible skills
	permissionManager *permission.Manager // Manages tool execution permissions

	// stateStore provides persistent runtime state (scheduled tasks, active skills).
	stateStore *config.StateStore

	// cachedSystemPromptBuilder holds a pre-warmed SystemPromptBuilder whose user
	// info has been asynchronously detected during agent initialization.
	cachedSystemPromptBuilder *SystemPromptBuilder

	// tuiScheduler is set when the TUI is active and handles /loop commands.
	tuiScheduler *TUIScheduler

	// manageScheduleTool holds a reference to the registered manage_schedule tool
	// so SetTUIScheduler can wire the live scheduler into it.
	manageScheduleTool *builtin.ManageScheduleTool

	// approvalConfirmFnOverride optionally overrides the default auto-confirm logic.
	approvalConfirmFnOverride func(string) bool

	// activeSessionID is the session ID to use for ProcessStreamWithMultimodal
	// Default is "default" for backward compatibility
	activeSessionID string
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

	// Get current working directory; prefer an explicit override from config.
	var workingDir string
	if cfg.WorkingDir != "" {
		expanded := cfg.WorkingDir
		if strings.HasPrefix(expanded, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to resolve home directory: %w", err)
			}
			expanded = filepath.Join(home, expanded[2:])
		}
		absDir, err := filepath.Abs(expanded)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve working directory %q: %w", cfg.WorkingDir, err)
		}
		workingDir = absDir
	} else {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	// Create toolbox configuration with consolidated MCP config
	webAPIKeys := make(map[string]string)
	if cfg.WebSearchAPIKeys.Serper != "" {
		webAPIKeys["serper"] = cfg.WebSearchAPIKeys.Serper
	}
	if cfg.WebSearchAPIKeys.Tavily != "" {
		webAPIKeys["tavily"] = cfg.WebSearchAPIKeys.Tavily
	}

	toolboxConfig := &tools.ToolboxConfig{
		WorkingDirectory: workingDir,
		Timeout:          cfg.ResponseTimeout,
		MaxFileSize:      cfg.MaxFileSize,
		MaxResponseSize:  cfg.MaxFileSize, // Use same limit for now
		UserAgent:        "nano/1.0",
		AllowedCommands:  cfg.AllowedCommands, // propagate from global config
		BlockedCommands:  cfg.BlockedCommands, // propagate from global config
		WebSearchAPIKeys: webAPIKeys,
		EnableMCP:        cfg.EnableMCP,
		MCPConfig:        convertMCPConfig(cfg.MCP), // Convert consolidated config

		// Tool-specific configurations
		ReadFileMaxLines:    cfg.ReadFileMaxLines,
		SearchMaxResults:    cfg.SearchMaxResults,
		WebRequestTimeout:   cfg.WebRequestTimeout,
		WebSearchTimeout:    cfg.WebSearchTimeout,
		WebMaxContentSize:   cfg.WebMaxContentSize,
		WebSearchMaxResults: cfg.WebSearchMaxResults,
		FileDiffMaxLines:    cfg.FileDiffMaxLines,
		GitMaxLogEntries:    cfg.GitMaxLogEntries,

		// NEW: propagate env filtering and strict mode from global config
		AllowedEnvVars: cfg.AllowedEnvVars,
		BlockedEnvVars: cfg.BlockedEnvVars,
		Strict:         cfg.Strict,

		// Sandbox configuration
		Sandbox: cfg.Sandbox,
	}

	// Wire up OpenSpec configuration
	if cfg.OpenSpec != nil && cfg.OpenSpec.Enabled {
		toolboxConfig.EnableOpenSpec = true
		toolboxConfig.OpenSpecRootDir = cfg.OpenSpec.RootDir
		toolboxConfig.OpenSpecDefaultSchema = cfg.OpenSpec.DefaultSchema
		toolboxConfig.OpenSpecMaxArtifact = cfg.OpenSpec.MaxArtifactSize
	}

	// Create toolbox with new architecture
	toolbox := tools.NewToolbox(workingDir, toolboxConfig, nil)

	// Create LLM client with streaming and function calling support
	llmClient := llm.NewClient(
		cfg.APIKey,
		cfg.BaseURL,
		cfg.Model,
		toolbox.List(),
	)

	// Start goroutine to listen for tools update events
	go func() {
		for event := range toolbox.GetToolsUpdateChannel() {
			logger.Debugf("Received tools update event: %s", event.Type)
			llmClient.UpdateTools(event.Tools)
			logger.Infof("Updated LLM client with %d tools after MCP registration", len(event.Tools))
		}
	}()

	// Create memory manager (local SQLite, no remote service required).
	// Memory is enabled whenever a [memory] section appears in config.
	memoryManager := memory.NewManager(workingDir, "", cfg.Memory != nil)

	// Memory tools are not registered by default for main agent
	// They will be registered only for sub-agents that have memory enabled

	// Create tool scheduler
	defaultEventHandler := func(event event.StreamEvent) {
		// Default event handler - can be overridden later
		logger.Debugf("Tool scheduler event: %s", event.Type)
	}

	recovery := NewToolRecoveryStrategy(defaultEventHandler)
	if cfg.ToolRecovery != nil {
		maxRetries := cfg.ToolRecovery.Default.MaxRetries
		if maxRetries <= 0 {
			maxRetries = 3
		}
		retryDelay := cfg.ToolRecovery.Default.RetryDelay
		if retryDelay <= 0 {
			retryDelay = time.Second
		}
		backoffMultiplier := cfg.ToolRecovery.Default.BackoffMultiplier
		if backoffMultiplier <= 0 {
			backoffMultiplier = 2.0
		}
		recovery.UpdateStrategy(maxRetries, retryDelay, backoffMultiplier)

		maxDelay := cfg.ToolRecovery.Default.MaxDelay
		if maxDelay <= 0 {
			maxDelay = 30 * time.Second
		}
		jitterRatio := cfg.ToolRecovery.Default.JitterRatio
		if jitterRatio < 0 {
			jitterRatio = 0
		}
		recovery.UpdateBackoffOptions(maxDelay, jitterRatio)

		for toolName, p := range cfg.ToolRecovery.PerTool {
			recovery.SetToolPolicy(toolName, ToolRetryPolicy{
				MaxRetries:        p.MaxRetries,
				RetryDelay:        p.RetryDelay,
				BackoffMultiplier: p.BackoffMultiplier,
				MaxDelay:          p.MaxDelay,
				JitterRatio:       p.JitterRatio,
			})
		}
	}

	toolScheduler := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          toolbox,
		EventHandler:     defaultEventHandler,
		RecoveryStrategy: recovery,
		ApprovalHandler:  approvalHandler,
	})

	// Initialize session manager options
	smOpts := []SessionManagerOption{
		WithSessionTTL(30 * time.Minute),
	}

	// Initialize StateStore for persistent runtime state (skills, scheduled tasks).
	// Honour cfg.Scheduler.Enabled and cfg.Scheduler.StateFile when available.
	var stateStore *config.StateStore
	schedulerEnabled := true
	stateFilePath := ""
	if cfg.Scheduler != nil {
		schedulerEnabled = cfg.Scheduler.Enabled
		stateFilePath = cfg.Scheduler.StateFile
	}
	if schedulerEnabled {
		if stateFilePath == "" {
			if defaultPath, err := config.DefaultStateStorePath(); err == nil {
				stateFilePath = defaultPath
			}
		}
		if stateFilePath != "" {
			stateStore = config.NewStateStore(stateFilePath)
			if err := stateStore.Load(); err != nil {
				logger.Warnf("Failed to load state store: %v", err)
			}
		}
	}

	var sessionStorage SessionStorage

	// Initialize OSS session storage if enabled
	if cfg.OSS != nil && cfg.OSS.Enabled {
		storage, err := NewOSSSessionStorage(cfg.OSS)
		if err != nil {
			logger.Errorf("Failed to initialize OSS session storage: %v", err)
		} else {
			sessionStorage = storage
			logger.Info("OSS session storage initialized")
		}
	}

	if sessionStorage == nil {
		// Use ProjectSessionStorage for TUI mode (non-daemon, has working dir)
		if workingDir != "" && !cfg.IsDaemon {
			projectStorage, err := NewProjectSessionStorage(workingDir)
			if err != nil {
				logger.Warnf("Failed to initialize project session storage: %v, falling back to local storage", err)
				sessionStorage = NewLocalSessionStorage("")
				logger.Info("Local session storage initialized (fallback)")
			} else {
				sessionStorage = projectStorage
				logger.Info("Project session storage initialized")
			}
		} else {
			sessionStorage = NewLocalSessionStorage("")
			logger.Info("Local session storage initialized")
		}
	}
	smOpts = append(smOpts, WithSessionStorage(sessionStorage))

	agent := &Agent{
		config:          cfg,
		toolbox:         toolbox,
		llmClient:       llmClient,
		memoryManager:   memoryManager,
		toolScheduler:   toolScheduler,
		loopDetector:    NewLoopDetector(),
		isSubAgent:      cfg.IsSubAgent,
		sessionManager:  NewSessionManager(smOpts...),
		stateStore:      stateStore,
		activeSessionID: "default", // Default session ID for backward compatibility
	}

	// Initialise PermissionManager from config.
	{
		mode := permission.ModeDefault
		switch permission.PermissionMode(cfg.PermissionMode) {
		case permission.ModeAcceptEdits, permission.ModeYOLO:
			mode = permission.PermissionMode(cfg.PermissionMode)
		}
		var rules []permission.PermissionRule
		for _, raw := range cfg.AllowedRules {
			rules = append(rules, permission.ParseRule(raw))
		}
		agent.permissionManager = permission.NewManager(mode, rules)
		toolScheduler.SetPermissionManager(agent.permissionManager)
		if stateStore != nil {
			stateStore.ClearAllPendingApprovals()
			toolScheduler.SetStateStore(stateStore)
		}
	}

	// Apply functional options
	for _, opt := range opts {
		opt(agent)
	}

	// Initialize SkillManager if skills are enabled
	if cfg.Skills != nil && cfg.Skills.Enabled {
		sm := skill.NewManager(
			workingDir,
			cfg.Skills.PersonalDir,
			cfg.Skills.ProjectDir,
			cfg.Skills.MaxSkillSize,
			cfg.Skills.MaxSkills,
			cfg.Skills.MaxActiveSkills,
			cfg.Skills.AutoInvoke,
		)
		if stateStore != nil {
			sm.SetStateStore(stateStore)
		}
		if err := sm.Discover(); err != nil {
			logger.Warnf("Failed to discover skills: %v", err)
		} else {
			agent.skillManager = sm
			logger.Infof("Skills support initialized: %d skills discovered", sm.Count())
			// Restore active skills persisted from previous sessions.
			if stateStore != nil {
				for _, skillName := range stateStore.GetActiveSkills() {
					if err := sm.ActivateSkill(skillName); err != nil {
						logger.Warnf("Failed to restore active skill %q: %v", skillName, err)
					}
				}
			}
		}
	}

	// Register ForkTool on top-level (non-sub-agent) agents to enable fork-based orchestration.
	if !cfg.IsSubAgent {
		forkTool := NewForkTool(NewForkManager(agent))
		if err := toolbox.Register(forkTool); err != nil {
			logger.Warnf("Failed to register fork tool: %v", err)
		} else {
			logger.Info("ForkTool registered successfully")
		}

		// Register conversational configuration management tools.
		agent.registerBuiltinManagementTools(toolbox, cfg, workingDir)
	}

	// Start MCP client asynchronously if enabled
	if cfg.EnableMCP && toolbox.IsMCPEnabled() {
		go func() {
			// Create context with timeout for MCP initialization
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			logger.Info("Starting MCP client asynchronously with 60s timeout...")
			err := toolbox.StartMCP(ctx)
			if err != nil {
				logger.Errorf("Failed to start MCP client: %v", err)
				logger.Warn("Continuing without MCP functionality")
			} else {
				logger.Info("MCP client started successfully")
			}
		}()
		logger.Info("MCP client initialization started in background")
	}

	logger.Info("Agent initialized successfully")

	// Preload system info asynchronously to avoid delay on first conversation.
	// Only preload when auto-detection is enabled and no custom system prompt overrides it
	// (custom prompt callers often disable detection to avoid subprocess side effects in tests).
	preloadSPB := NewSystemPromptBuilder(workingDir, toolbox.List(), memoryManager, cfg)
	if cfg.UserInfo != nil && cfg.UserInfo.AutoDetectUserInfo && cfg.CustomSystemPrompt == "" {
		preloadSPB.PreloadUserInfo()
	}
	agent.cachedSystemPromptBuilder = preloadSPB

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

// RegisterTool registers an additional tool into the agent's toolbox and
// updates the LLM client's tool list. This is a convenience wrapper around
// toolbox.Register used by the Engine to wire in dynamically created tools.
func (a *Agent) RegisterTool(t interfaces.Tool) error {
	if err := a.toolbox.Register(t); err != nil {
		return err
	}
	// Keep the LLM client in sync with the updated tool list.
	a.llmClient.UpdateTools(a.toolbox.List())
	return nil
}

// GetToolScheduler returns the tool scheduler for advanced usage
func (a *Agent) GetToolScheduler() *ToolScheduler {
	return a.toolScheduler
}

// GetPermissionManager returns the permission manager for the agent.
func (a *Agent) GetPermissionManager() *permission.Manager {
	return a.permissionManager
}

// SetPermissionMode changes the active permission mode at runtime.
func (a *Agent) SetPermissionMode(mode permission.PermissionMode) {
	if a.permissionManager != nil {
		a.permissionManager.SetMode(mode)
	}
}

// SetApprovalHandler sets the approval handler for the agent.
func (a *Agent) SetApprovalHandler(handler func(*ToolCallInfo) bool) {
	a.toolScheduler.SetApprovalHandler(handler)
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
	}

	// Create enhanced turn that uses StreamRenderer
	turn := NewTurnWithMultimodal(userInput, images, turnConfig)

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
		clean := validator.SanitizeEvent(ev)
		onEvent(clean)
	}

	// Set the event handler for both the turn and the tool scheduler
	turn.eventHandler = sanitizedOnEvent
	if a.toolScheduler != nil {
		a.toolScheduler.SetEventHandler(sanitizedOnEvent)
	}

	// Execute the turn (this will use the plan manager and enhanced features)
	err := turn.Execute(ctx)

	// Update session's conversation history after turn execution
	filteredMessages := make([]llm.Message, 0, len(turn.Messages))
	for _, msg := range turn.Messages {
		if msg.Role == "system" {
			continue
		}
		filteredMessages = append(filteredMessages, msg)
	}
	session.SetConversationHistory(filteredMessages)

	var totalTokens int
	if turn.TokenStats != nil {
		if turn.TokenStats.SessionTotalTokens > 0 {
			totalTokens = turn.TokenStats.SessionTotalTokens
		} else {
			totalTokens = turn.TokenStats.TotalTokens
		}
	}
	session.UpdateStats(totalTokens, time.Since(turn.StartTime).Seconds())

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
			go func(s *Session, h []llm.Message) {
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
					if err := a.sessionManager.SaveSession(s.ID); err != nil {
						logger.Warnf("Failed to save session %s to storage after setting short title: %v", s.ID, err)
					}
					logger.Infof("Used short user message as title for session %s: %s", s.ID, title)
					return
				}

				// Otherwise generate with LLM
				generatedTitle, err := a.GenerateTitle(h)
				if err == nil && generatedTitle != "" {
					s.SetMetadata("title", generatedTitle)

					if err := a.sessionManager.SaveSession(s.ID); err != nil {
						logger.Warnf("Failed to save session %s to storage after title generation: %v", s.ID, err)
					}

					logger.Infof("Generated title for session %s: %s", s.ID, generatedTitle)

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
					logger.Warnf("Failed to generate title for session %s: %v", s.ID, err)
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
		if err == context.Canceled {
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

// GetSessionManager returns the session manager for external access.
func (a *Agent) GetSessionManager() *SessionManager {
	return a.sessionManager
}

// ProcessResult represents the result of processing a request
type ProcessResult struct {
	Success  bool     `json:"success"`
	Response string   `json:"response"`
	Actions  []string `json:"actions"`
	Error    string   `json:"error,omitempty"`
}

// Shutdown gracefully shuts down the agent and its resources
func (a *Agent) Shutdown() error {
	logger.Info("Shutting down agent")

	// Stop session manager if running
	if a.sessionManager != nil {
		a.sessionManager.Shutdown()
	}

	// Stop MCP client if running
	if a.toolbox.IsMCPEnabled() {
		err := a.toolbox.StopMCP()
		if err != nil {
			logger.Errorf("Failed to stop MCP client: %v", err)
		} else {
			logger.Info("MCP client stopped successfully")
		}
	}

	// Close toolbox and its channels
	a.toolbox.Close()

	logger.Info("Agent shutdown completed")
	return nil
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

	// manage_schedule: scheduler is nil until TUI starts; it is wired in
	// SetTUIScheduler via ManageScheduleTool.SetScheduler.
	manageScheduleTool := builtin.NewManageScheduleTool(nil, a.stateStore, a.approvalConfirmFn)
	if err := tb.Register(manageScheduleTool); err != nil {
		logger.Debugf("manage_schedule already registered: %v", err)
	}
	// Store a reference so SetTUIScheduler can wire the live scheduler later.
	a.manageScheduleTool = manageScheduleTool

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

// SetScheduler wires a live cron.Scheduler directly into the manage_schedule
// tool so tasks created via conversation are scheduled immediately.
// This is the preferred method when using the Engine; SetTUIScheduler is kept
// for backward compatibility with the classic TUI path.
func (a *Agent) SetScheduler(s *cron.Scheduler) {
	if s != nil && a.manageScheduleTool != nil {
		a.manageScheduleTool.SetScheduler(s)
	}
}

// SetTUIScheduler wires a TUIScheduler so that /loop commands work in turns.
// It also updates the manage_schedule tool with the live cron scheduler so
// tasks created via conversation are scheduled immediately.
func (a *Agent) SetTUIScheduler(ts *TUIScheduler) {
	a.tuiScheduler = ts
	if ts != nil && a.manageScheduleTool != nil {
		a.manageScheduleTool.SetScheduler(ts.Scheduler())
	}
}
