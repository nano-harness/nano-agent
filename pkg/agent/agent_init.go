package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/hookservice"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/memory"
	"github.com/nano-harness/nano-agent/pkg/middleware"
	agentruntime "github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/nano-harness/nano-agent/pkg/skill"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

func newAgentRuntime(agent *Agent) *agentruntime.AgentRuntime {
	return agentruntime.NewAgentRuntime(
		agent.llmClient,
		agent.toolbox,
		agent.sessionManager,
		agent.memoryManager,
		agentruntime.EventHandler(agent.eventHandler),
	)
}

type agentBootstrap struct {
	workingDir        string
	agentCtx          context.Context
	agentCancel       context.CancelFunc
	toolbox           *tools.Toolbox
	llmClient         llm.LLMClient
	memoryManager     *memory.Manager
	toolScheduler     *ToolScheduler
	sessionManager    *SessionManager
	stateStore        *config.StateStore
	permissionManager *permission.Manager
	skillManager      *skill.Manager
	hookEngine        *middleware.HookEngine
}

func buildAgentBootstrap(cfg *config.Config, approvalHandler func(*ToolCallInfo) bool) (*agentBootstrap, error) {
	workingDir, err := resolveAgentWorkingDir(cfg)
	if err != nil {
		return nil, err
	}

	toolbox := newAgentToolbox(cfg, workingDir)
	llmClient := newAgentLLMClient(cfg, toolbox)
	agentCtx, agentCancel := context.WithCancel(context.Background())
	startToolboxLLMUpdates(agentCtx, toolbox, llmClient)

	memoryManager := newAgentMemoryManager(cfg, workingDir)
	toolScheduler := newAgentToolScheduler(cfg, toolbox, approvalHandler)
	stateStore := newAgentStateStore(cfg)
	sessionManager := newAgentSessionManager(cfg, workingDir)
	permissionManager := newAgentPermissionManager(cfg, workingDir)
	toolScheduler.SetPermissionManager(permissionManager)
	skillManager := newAgentSkillManager(cfg, workingDir, stateStore)
	hookEngine := newAgentHookEngine(cfg, workingDir)

	// Wire hook engine into components
	toolScheduler.SetHookEngine(hookEngine)
	toolScheduler.SetWorkDir(workingDir)
	sessionManager.SetHookEngine(hookEngine)

	return &agentBootstrap{
		workingDir:        workingDir,
		agentCtx:          agentCtx,
		agentCancel:       agentCancel,
		toolbox:           toolbox,
		llmClient:         llmClient,
		memoryManager:     memoryManager,
		toolScheduler:     toolScheduler,
		sessionManager:    sessionManager,
		stateStore:        stateStore,
		permissionManager: permissionManager,
		skillManager:      skillManager,
		hookEngine:        hookEngine,
	}, nil
}

func resolveAgentWorkingDir(cfg *config.Config) (string, error) {
	if cfg.WorkingDir == "" {
		workingDir, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get working directory: %w", err)
		}
		return workingDir, nil
	}

	expanded := cfg.WorkingDir
	if strings.HasPrefix(expanded, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to resolve home directory: %w", err)
		}
		expanded = filepath.Join(home, expanded[2:])
	}
	absDir, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("failed to resolve working directory %q: %w", cfg.WorkingDir, err)
	}
	return absDir, nil
}

func newAgentToolbox(cfg *config.Config, workingDir string) *tools.Toolbox {
	return tools.NewToolbox(workingDir, newAgentToolboxConfig(cfg, workingDir), nil)
}

func newAgentToolboxConfig(cfg *config.Config, workingDir string) *tools.ToolboxConfig {
	webAPIKeys := make(map[string]string)
	if cfg.WebSearchAPIKeys.Serper != "" {
		webAPIKeys["serper"] = cfg.WebSearchAPIKeys.Serper
	}
	if cfg.WebSearchAPIKeys.Tavily != "" {
		webAPIKeys["tavily"] = cfg.WebSearchAPIKeys.Tavily
	}

	toolboxConfig := &tools.ToolboxConfig{
		WorkingDirectory:      workingDir,
		Timeout:               cfg.ResponseTimeout,
		MaxFileSize:           cfg.MaxFileSize,
		MaxResponseSize:       cfg.MaxFileSize,
		UserAgent:             "nano/1.0",
		AllowedCommands:       cfg.AllowedCommands,
		BlockedCommands:       cfg.BlockedCommands,
		SensitiveReadPaths:    cfg.SensitiveReadPaths,
		ArbitraryExecCommands: cfg.ArbitraryExecCommands,
		WebSearchAPIKeys:      webAPIKeys,
		EnableMCP:             cfg.EnableMCP,
		MCPConfig:             convertMCPConfig(cfg.MCP),

		ReadFileMaxLines:    cfg.ReadFileMaxLines,
		SearchMaxResults:    cfg.SearchMaxResults,
		WebRequestTimeout:   cfg.WebRequestTimeout,
		WebSearchTimeout:    cfg.WebSearchTimeout,
		WebMaxContentSize:   cfg.WebMaxContentSize,
		WebSearchMaxResults: cfg.WebSearchMaxResults,
		FileDiffMaxLines:    cfg.FileDiffMaxLines,
		GitMaxLogEntries:    cfg.GitMaxLogEntries,

		AllowedEnvVars: cfg.AllowedEnvVars,
		BlockedEnvVars: cfg.BlockedEnvVars,
		Strict:         cfg.Strict,

		Sandbox: cfg.Sandbox,
	}

	if cfg.OpenSpec != nil && cfg.OpenSpec.Enabled {
		toolboxConfig.EnableOpenSpec = true
		toolboxConfig.OpenSpecRootDir = cfg.OpenSpec.RootDir
		toolboxConfig.OpenSpecDefaultSchema = cfg.OpenSpec.DefaultSchema
		toolboxConfig.OpenSpecMaxArtifact = cfg.OpenSpec.MaxArtifactSize
	}

	return toolboxConfig
}

func newAgentLLMClient(cfg *config.Config, toolbox *tools.Toolbox) llm.LLMClient {
	primary, fallbacks, err := llm.ResolveRoutes(cfg)
	if err != nil {
		logger.Warnf("failed to resolve model routes; falling back to legacy client: %v", err)
		return llm.NewClient(
			cfg.APIKey,
			cfg.BaseURL,
			cfg.Model,
			toolbox.List(),
		)
	}
	if len(fallbacks) == 0 {
		return llm.NewClientForRoute(primary, toolbox.List(), cfg)
	}
	routes := append([]llm.ResolvedRoute{primary}, fallbacks...)
	return llm.NewMultiRouteClient(routes, toolbox.List(), cfg)
}

func newAgentMemoryManager(cfg *config.Config, workingDir string) *memory.Manager {
	return memory.NewManager(workingDir, "", cfg.Memory != nil)
}

func newAgentToolScheduler(cfg *config.Config, toolbox *tools.Toolbox, approvalHandler func(*ToolCallInfo) bool) *ToolScheduler {
	defaultEventHandler := func(event event.StreamEvent) {
		logger.Debugf("Tool scheduler event: %s", event.Type)
	}
	recovery := newAgentToolRecoveryStrategy(cfg, defaultEventHandler)

	toolScheduler := NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:          toolbox,
		EventHandler:     defaultEventHandler,
		RecoveryStrategy: recovery,
		ApprovalHandler:  approvalHandler,
	})
	toolScheduler.SetAgentConfig(cfg)
	return toolScheduler
}

func startToolboxLLMUpdates(ctx context.Context, toolbox *tools.Toolbox, client llm.LLMClient) {
	go func() {
		ch := toolbox.GetToolsUpdateChannel()
		for {
			select {
			case event, ok := <-ch:
				if !ok {
					return
				}
				logger.Debugf("Received tools update event: %s", event.Type)
				client.UpdateTools(event.Tools)
				logger.Infof("Updated LLM client with %d tools after MCP registration", len(event.Tools))
			case <-ctx.Done():
				return
			}
		}
	}()
}

func newAgentToolRecoveryStrategy(cfg *config.Config, eventHandler func(event.StreamEvent)) *ToolRecoveryStrategy {
	recovery := NewToolRecoveryStrategy(eventHandler)
	if cfg.ToolRecovery == nil {
		return recovery
	}

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

	return recovery
}

func newAgentStateStore(cfg *config.Config) *config.StateStore {
	schedulerEnabled := true
	stateFilePath := ""
	if cfg.Scheduler != nil {
		schedulerEnabled = cfg.Scheduler.Enabled
		stateFilePath = cfg.Scheduler.StateFile
	}
	if !schedulerEnabled {
		return nil
	}

	if stateFilePath == "" {
		if defaultPath, err := config.DefaultStateStorePath(); err == nil {
			stateFilePath = defaultPath
		}
	}
	if stateFilePath == "" {
		return nil
	}

	stateStore := config.NewStateStore(stateFilePath)
	if err := stateStore.Load(); err != nil {
		logger.Warnf("Failed to load state store: %v", err)
	}
	return stateStore
}

func newAgentSessionManager(cfg *config.Config, workingDir string) *SessionManager {
	return NewSessionManager(
		WithSessionTTL(30*time.Minute),
		WithSessionStorage(newAgentSessionStorage(cfg, workingDir)),
	)
}

func newAgentSessionStorage(cfg *config.Config, workingDir string) SessionStorage {
	if cfg.OSS != nil && cfg.OSS.Enabled {
		storage, err := NewOSSSessionStorage(cfg.OSS)
		if err != nil {
			logger.Errorf("Failed to initialize OSS session storage: %v", err)
		} else {
			logger.Info("OSS session storage initialized")
			return storage
		}
	}

	if workingDir != "" && !cfg.IsDaemon {
		projectStorage, err := NewProjectSessionStorage(workingDir)
		if err != nil {
			logger.Warnf("Failed to initialize project session storage: %v, falling back to local storage", err)
			logger.Info("Local session storage initialized (fallback)")
			return NewLocalSessionStorage("")
		}
		logger.Info("Project session storage initialized")
		return projectStorage
	}

	logger.Info("Local session storage initialized")
	return NewLocalSessionStorage("")
}

func newAgentPermissionManager(cfg *config.Config, workingDir string) *permission.Manager {
	mode := permission.ModeDefault
	switch permission.PermissionMode(cfg.PermissionMode) {
	case permission.ModeAcceptEdits, permission.ModeYOLO:
		mode = permission.PermissionMode(cfg.PermissionMode)
	}
	var rules []permission.PermissionRule
	for _, raw := range cfg.AllowedRules {
		rules = append(rules, permission.ParseRule(raw))
	}
	return permission.NewManagerWithWorkdir(mode, rules, workingDir)
}

func newAgentSkillManager(cfg *config.Config, workingDir string, stateStore *config.StateStore) *skill.Manager {
	if cfg.Skills == nil || !cfg.Skills.Enabled {
		return nil
	}

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
	sm.EnableBuiltinSkills(cfg.Skills.AutoActivate)
	if err := sm.Discover(); err != nil {
		logger.Warnf("Failed to discover skills: %v", err)
		return nil
	}

	logger.Infof("Skills support initialized: %d skills discovered", sm.Count())
	if stateStore != nil {
		for _, skillName := range stateStore.GetActiveSkills() {
			if err := sm.ActivateSkill(skillName); err != nil {
				logger.Warnf("Failed to restore active skill %q: %v", skillName, err)
			}
		}
	}
	for _, skillName := range cfg.Skills.AutoActivate {
		if err := sm.ActivateSkill(skillName); err != nil {
			logger.Warnf("Failed to auto-activate skill %q: %v", skillName, err)
		}
	}

	return sm
}

func maybeStartAgentMCPClient(cfg *config.Config, toolbox *tools.Toolbox) {
	if !cfg.EnableMCP || !toolbox.IsMCPEnabled() {
		return
	}

	go func() {
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

func newPreloadedSystemPromptBuilder(cfg *config.Config, workingDir string, toolbox *tools.Toolbox, memoryManager *memory.Manager) *SystemPromptBuilder {
	preloadSPB := NewSystemPromptBuilder(workingDir, toolbox.List(), memoryManager, cfg)
	if cfg.UserInfo != nil && cfg.UserInfo.AutoDetectUserInfo && cfg.CustomSystemPrompt == "" {
		preloadSPB.PreloadUserInfo()
	}
	return preloadSPB
}

func newAgentHookEngine(cfg *config.Config, workingDir string) *middleware.HookEngine {
	var hooks []middleware.Hook
	if cfg.Security != nil {
		for _, hookCfg := range cfg.Security.Hooks {
			if !hookCfg.Enabled {
				continue
			}

			// Default to command type if not specified
			hookType := hookservice.HookType(hookCfg.Type)
			if hookType == "" {
				hookType = hookservice.HookTypeCommand
			}

			h := middleware.Hook{
				Name:          hookCfg.Name,
				Event:         hookservice.Event(hookCfg.Event),
				Pattern:       hookCfg.Pattern,
				Type:          hookType,
				Command:       hookCfg.Command,
				Enabled:       hookCfg.Enabled,
				FailurePolicy: hookservice.FailurePolicy(hookCfg.FailurePolicy),
				EnvWhitelist:  hookCfg.EnvWhitelist,
				Async:         hookCfg.Async,
				AsyncRewake:   hookCfg.AsyncRewake,
				Once:          hookCfg.Once,
				StatusMessage: hookCfg.StatusMessage,
			}

			// Translate HTTP sub-config
			if hookCfg.HTTP != nil {
				h.HTTPConfig = &hookservice.HTTPHookConfig{
					URL:            hookCfg.HTTP.URL,
					Method:         hookCfg.HTTP.Method,
					Headers:        hookCfg.HTTP.Headers,
					URLAllowlist:   hookCfg.HTTP.URLAllowlist,
					AllowedEnvVars: hookCfg.HTTP.AllowedEnvVars,
					TimeoutSeconds: hookCfg.HTTP.TimeoutSeconds,
					MaxResponseKB:  hookCfg.HTTP.MaxResponseKB,
				}
			}

			// Translate Prompt sub-config
			if hookCfg.Prompt != nil {
				h.PromptConfig = &hookservice.PromptHookConfig{
					Prompt:    hookCfg.Prompt.Prompt,
					Model:     hookCfg.Prompt.Model,
					MaxTokens: hookCfg.Prompt.MaxTokens,
				}
			}

			// Translate Agent sub-config
			if hookCfg.Agent != nil {
				h.AgentConfig = &hookservice.AgentHookConfig{
					Agent: hookCfg.Agent.Agent,
					Task:  hookCfg.Agent.Task,
				}
			}

			hooks = append(hooks, h)
		}
	}

	firewallEnabled := cfg.Firewall == nil || cfg.Firewall.Enabled
	if len(hooks) == 0 && !firewallEnabled {
		return nil
	}

	hookEngine := middleware.NewHookEngine(hooks)
	if firewallEnabled {
		hookEngine.RegisterProgrammaticHook(permission.NewFirewallHook(newAgentFirewallConfig(cfg.Firewall)))
	}
	return hookEngine
}

func newAgentFirewallConfig(cfg *config.FirewallConfig) permission.FirewallConfig {
	firewallConfig := permission.DefaultFirewallConfig()
	if cfg == nil {
		return firewallConfig
	}
	if cfg.SeverityThreshold != "" {
		firewallConfig.SeverityThreshold = permission.Severity(cfg.SeverityThreshold)
	}
	if cfg.FailurePolicy != "" {
		firewallConfig.FailurePolicy = cfg.FailurePolicy
	}
	firewallConfig.CustomPatterns = convertFirewallPatterns(cfg.CustomPatterns)
	firewallConfig.Overrides = append([]string(nil), cfg.Overrides...)
	return firewallConfig
}

func convertFirewallPatterns(patterns []config.DangerousCommandPattern) []permission.DangerousCommandRule {
	var rules []permission.DangerousCommandRule
	for _, p := range patterns {
		rules = append(rules, permission.DangerousCommandRule{
			Pattern:  p.Pattern,
			Reason:   p.Reason,
			Severity: permission.Severity(p.Severity),
			Category: p.Category,
		})
	}
	return rules
}
