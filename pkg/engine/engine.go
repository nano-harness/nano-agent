// Package engine provides a unified lifecycle manager for the agent, scheduler,
// and state store. All execution modes (TUI, BubbleTea, Daemon) should
// use Engine instead of wiring these components manually.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/cron"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/mailbox"
	nanoruntime "github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	agentTools "github.com/nano-harness/nano-agent/pkg/tools/agent"
)

// Engine encapsulates the lifecycle of an agent, its scheduler, and the shared
// state store. Use New to construct a ready-to-run Engine; call Start to
// activate the scheduler; call Shutdown to gracefully stop everything.
type Engine struct {
	// Agent is the core AI agent instance.
	Agent *agent.Agent
	// Scheduler is the cron-based recurring task scheduler.
	Scheduler *cron.Scheduler
	// StateStore is the shared persistent state store.
	StateStore *config.StateStore

	cronNotifier func(event.StreamEvent)
}

// EngineOption configures optional Engine behavior.
type EngineOption func(*engineOptions)

type engineOptions struct {
	enableScheduler bool
	agentOpts       []agent.Option
}

// WithScheduler enables cron task scheduling for this Engine instance.
// Use this for TUI, Daemon, and Lead modes. Omit for ACP and Teammate modes.
func WithScheduler() EngineOption {
	return func(o *engineOptions) {
		o.enableScheduler = true
	}
}

// WithAgentOpts passes agent.Option values through to agent.New().
func WithAgentOpts(opts ...agent.Option) EngineOption {
	return func(o *engineOptions) {
		o.agentOpts = append(o.agentOpts, opts...)
	}
}

func (e *Engine) SetCronNotifier(fn func(event.StreamEvent)) {
	e.cronNotifier = fn
}

func (e *Engine) notifyCronLifecycle(ev event.StreamEvent) {
	if e != nil && e.cronNotifier != nil {
		e.cronNotifier(ev)
	}
}

func cronTaskSessionID(taskID string) string {
	return fmt.Sprintf("cron-task-%s", taskID)
}

func newCronLifecycleEvent(eventType event.EventType, taskID, command, sessionID string, success bool, durationMs int64, err error) event.StreamEvent {
	ev := event.NewStreamEvent(eventType, "cron")
	ev.TaskID = taskID
	ev.SessionID = sessionID
	ev.Metadata = map[string]interface{}{
		"task_id":         taskID,
		"task_command":    command,
		"task_session_id": sessionID,
	}
	if eventType == event.EventTypeCronTaskFinished {
		ev.Metadata["success"] = success
		ev.Metadata["duration_ms"] = durationMs
		if err != nil {
			ev.Metadata["error"] = err.Error()
			ev.Error = err.Error()
		}
	}
	return ev
}

// buildCronApprovalHandler creates an approval handler based on the cron permission policy.
func buildCronApprovalHandler(policy string) func(*agent.ToolCallInfo) bool {
	switch policy {
	case "auto_approve":
		return func(*agent.ToolCallInfo) bool {
			return true // Auto-approve all tools
		}
	case "auto_reject":
		return func(info *agent.ToolCallInfo) bool {
			logger.Warnf("cron: auto-rejecting tool %s due to auto_reject policy", info.Name)
			return false // Reject all tools requiring confirmation
		}
	case "inherit":
		return nil // Use the global approval handler
	default:
		logger.Warnf("cron: unknown permission_policy %q, defaulting to auto_approve", policy)
		return func(*agent.ToolCallInfo) bool {
			return true
		}
	}
}

// New builds an Engine from the provided config and optional approval handler.
// The approvalHandler may be nil (all tool calls are auto-approved).
func New(cfg *config.Config, approvalHandler func(*agent.ToolCallInfo) bool, opts ...EngineOption) (*Engine, error) {
	parsed := &engineOptions{}
	for _, opt := range opts {
		opt(parsed)
	}

	agentInstance, err := agent.New(cfg, approvalHandler, parsed.agentOpts...)
	if err != nil {
		return nil, fmt.Errorf("engine: create agent: %w", err)
	}

	// Register standalone agent tools. Team-specific mailbox tools are registered
	// by NewLeadEngine/NewTeammateEngine once a team mailbox backend exists.
	agentTools.RegisterAgentTools(agentInstance.GetToolbox(), cfg, agentInstance)

	e := &Engine{
		Agent:      agentInstance,
		StateStore: agentInstance.GetStateStore(),
	}

	if parsed.enableScheduler {
		e.attachScheduler(cfg, agentInstance)
	}

	return e, nil
}

// buildCronExecutor creates the rich task executor closure used by the scheduler.
func (e *Engine) buildCronExecutor(agentInstance *agent.Agent, cronCfg *config.CronConfig) func(command, taskID string) (cron.TaskExecutionMetadata, error) {
	return func(command, taskID string) (cron.TaskExecutionMetadata, error) {
		meta := cron.TaskExecutionMetadata{}

		meta.SessionID = cronTaskSessionID(taskID)

		// Create events directory if configured
		eventsDir := cronCfg.EventsDir
		if eventsDir == "" {
			home, err := os.UserHomeDir()
			if err == nil {
				eventsDir = filepath.Join(home, ".nano", "cron-events")
			}
		}

		var eventsFile *os.File
		if eventsDir != "" {
			taskEventsDir := filepath.Join(eventsDir, taskID)
			if err := os.MkdirAll(taskEventsDir, 0o755); err == nil {
				eventsPath := filepath.Join(taskEventsDir, fmt.Sprintf("%s.jsonl", meta.SessionID))
				meta.EventsPath = eventsPath
				eventsFile, _ = os.OpenFile(eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
				if eventsFile != nil {
					defer func() { _ = eventsFile.Close() }()
				}
			}
		}

		// Track metrics from events
		var toolCallCount int
		var tokenUsage int64
		var lastToolName string

		callback := func(se event.StreamEvent) {
			// Write event to audit file
			if eventsFile != nil {
				data, _ := json.Marshal(se)
				_, _ = eventsFile.Write(append(data, '\n'))
			}

			// Track metrics
			if se.Type == event.EventTypeToolCall {
				toolCallCount++
				if len(se.ToolCalls) > 0 && se.ToolCalls[0] != nil {
					lastToolName = se.ToolCalls[0].Name
				}
			}
			if se.Type == event.EventTypeTokenStats && se.TokenStats != nil {
				tokenUsage += int64(se.TokenStats.TotalTokens)
			}

			// Log events
			logger.Debugf("engine: cron task %s event [%s]: %s", taskID, se.Type, se.Content)
		}

		// Apply turn timeout
		timeout := cronCfg.TurnTimeout
		if timeout == 0 {
			timeout = 10 * time.Minute
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cronHandler := buildCronApprovalHandler(cronCfg.PermissionPolicy)
		if cronHandler != nil {
			ctx = agent.WithApprovalHandler(ctx, cronHandler)
		}

		started := time.Now()
		e.notifyCronLifecycle(newCronLifecycleEvent(event.EventTypeCronTaskStarted, taskID, command, meta.SessionID, true, 0, nil))
		err := agentInstance.ProcessStreamWithMultimodalAndSession(ctx, meta.SessionID, command, nil, callback)
		e.notifyCronLifecycle(newCronLifecycleEvent(event.EventTypeCronTaskFinished, taskID, command, meta.SessionID, err == nil, time.Since(started).Milliseconds(), err))

		meta.ToolCallCount = toolCallCount
		meta.TokenUsage = tokenUsage

		// Infer failure stage if there was an error
		if err != nil {
			meta.FailedTool = lastToolName
			if errors.Is(err, context.DeadlineExceeded) {
				meta.FailureStage = "timeout"
			} else if strings.Contains(err.Error(), "tool execution failed") {
				meta.FailureStage = "tool_exec"
			} else if strings.Contains(err.Error(), "llm") || strings.Contains(err.Error(), "LLM") {
				meta.FailureStage = "llm_call"
			} else if errors.Is(err, context.Canceled) {
				meta.FailureStage = "context_cancel"
			} else {
				meta.FailureStage = "unknown"
			}
		}

		return meta, err
	}
}

// attachScheduler creates the cron scheduler, wires the executor, and binds it to the agent.
func (e *Engine) attachScheduler(cfg *config.Config, agentInstance *agent.Agent) {
	cronCfg := cfg.Cron
	if cronCfg == nil {
		cronCfg = &config.CronConfig{
			PermissionPolicy:   "auto_approve",
			TurnTimeout:        10 * time.Minute,
			LogRetentionDays:   30,
			LogCleanupInterval: 24 * time.Hour,
		}
	}

	// Build the rich executor that the scheduler uses to run commands.
	executeTaskWithMeta := e.buildCronExecutor(agentInstance, cronCfg)

	// Provide legacy wrapper for backward compatibility
	executeTask := func(command string) error {
		_, err := executeTaskWithMeta(command, "legacy")
		return err
	}

	// Create scheduler and wire into the manage_schedule tool.
	e.Scheduler = cron.New(executeTask)
	if e.StateStore != nil {
		e.Scheduler.SetStateStore(e.StateStore)
	}

	// Initialize TaskLog
	logPath := cronCfg.LogPath
	if logPath == "" {
		var err error
		logPath, err = cron.DefaultTaskLogPath()
		if err != nil {
			logger.Warnf("engine: could not determine default task log path: %v", err)
		}
	}
	if logPath != "" {
		taskLog := cron.NewTaskLog(logPath)
		e.Scheduler.SetTaskLog(taskLog)
		e.Scheduler.SetLogRetention(cronCfg.LogRetentionDays, cronCfg.LogCleanupInterval)
	}

	// Set the rich executor for cron tasks
	e.Scheduler.SetExecuteTaskRich(executeTaskWithMeta)

	agentInstance.SetScheduler(e.Scheduler)
}

// NewLeadEngine creates an Engine for a team-lead agent with swarm capabilities.
// The lead agent can spawn teammates and coordinate multi-agent tasks.
func NewLeadEngine(cfg *config.Config, approvalHandler func(*agent.ToolCallInfo) bool, teamName string, opts ...agent.Option) (*Engine, error) {
	if teamName == "" {
		teamName = "default"
	}

	// Create agent
	agentInstance, err := agent.New(cfg, approvalHandler, opts...)
	if err != nil {
		return nil, fmt.Errorf("engine: create lead agent: %w", err)
	}

	// Initialize mailbox backend for the team
	mailboxBackend, err := mailbox.NewFileBackend(nanoruntime.MailboxDir(teamName), mailbox.Options{
		MaxPerAgent: 1000,
		MaxBodyKB:   1024, // 1MB
	})
	if err != nil {
		logger.Warnf("engine: failed to initialize mailbox backend: %v", err)
		mailboxBackend = nil // Continue without mailbox
	}

	agentTools.RegisterAgentTools(agentInstance.GetToolbox(), cfg, agentInstance)

	// Register swarm tools (lead mode)
	agentTools.RegisterSwarmTools(agentInstance.GetToolbox(), cfg, mailboxBackend, nil)

	if mailboxBackend != nil {
		if mb, err := mailboxBackend.Open("team-lead@" + teamName); err == nil {
			agentInstance.SetMailbox(mb)
		}
	}

	e := &Engine{
		Agent:      agentInstance,
		StateStore: agentInstance.GetStateStore(),
	}

	e.attachScheduler(cfg, agentInstance)

	logger.Infof("Created team-lead engine for team '%s'", teamName)
	return e, nil
}

// NewTeammateEngine creates an Engine for a teammate agent spawned by a team-lead.
// Teammates have restricted capabilities and automatically send idle notifications.
func NewTeammateEngine(cfg *config.Config, approvalHandler func(*agent.ToolCallInfo) bool, identity *swarm.TeammateIdentity) (*Engine, error) {
	if identity == nil {
		return nil, fmt.Errorf("engine: teammate identity is required")
	}

	// Create agent (context will be used internally when needed)
	agentInstance, err := agent.New(cfg, approvalHandler)
	if err != nil {
		return nil, fmt.Errorf("engine: create teammate agent: %w", err)
	}

	// Initialize mailbox backend for the team
	mailboxBackend, err := mailbox.NewFileBackend(nanoruntime.MailboxDir(identity.TeamName), mailbox.Options{
		MaxPerAgent: 1000,
		MaxBodyKB:   1024, // 1MB
	})
	if err != nil {
		logger.Warnf("engine: failed to initialize mailbox backend: %v", err)
		mailboxBackend = nil // Continue without mailbox
	}

	agentTools.RegisterAgentTools(agentInstance.GetToolbox(), cfg, agentInstance)

	// Register swarm tools (teammate mode)
	agentTools.RegisterSwarmTools(agentInstance.GetToolbox(), cfg, mailboxBackend, identity)

	if mailboxBackend != nil {
		if myMb, err := mailboxBackend.Open(identity.AgentID); err == nil {
			agentInstance.SetMailbox(myMb)
		}
		if leadMb, err := mailboxBackend.Open("team-lead@" + identity.TeamName); err == nil {
			agentInstance.SetParentMailbox(leadMb)
			swarm.RegisterIdleHook(agentInstance, identity, leadMb)
		}
	}

	e := &Engine{
		Agent:      agentInstance,
		StateStore: agentInstance.GetStateStore(),
	}

	// Teammates don't need scheduler functionality
	e.Scheduler = nil

	logger.Infof("Created teammate engine for %s in team '%s'", identity.AgentID, identity.TeamName)
	return e, nil
}

// Start activates optional background services (scheduler, log cleanup).
// Safe to call even when no Scheduler is configured — becomes a no-op.
func (e *Engine) Start() error {
	if e.Scheduler == nil {
		logger.Debug("engine: Start() called without scheduler, skipping")
		return nil
	}

	e.Scheduler.Start()
	if err := e.Scheduler.LoadPersistedTasks(); err != nil {
		logger.Warnf("engine: failed to reload persisted tasks: %v", err)
	}

	return nil
}

// Shutdown stops the scheduler and agent gracefully.
func (e *Engine) Shutdown() error {
	if e.Scheduler != nil {
		e.Scheduler.Stop()
	}

	if e.Agent != nil {
		return e.Agent.Shutdown()
	}

	return nil
}

// ThinkingStatus represents the current thinking mode status
type ThinkingStatus struct {
	Enabled bool
	Effort  string
	Source  string // "config" | "runtime" | "default"
}

// String returns a formatted string representation of thinking status
func (ts ThinkingStatus) String() string {
	if !ts.Enabled {
		return "已关闭"
	}
	effortStr := ""
	if ts.Effort != "" {
		effortStr = fmt.Sprintf(", effort: %s", ts.Effort)
	}
	return fmt.Sprintf("已开启 (来源: %s%s)", ts.Source, effortStr)
}

// SetThinkingEnabled enables or disables thinking mode at runtime
func (e *Engine) SetThinkingEnabled(enabled bool) {
	cfg := e.Agent.GetConfig()
	if cfg.Reasoning == nil {
		cfg.Reasoning = &config.ReasoningConfig{}
	}
	cfg.Reasoning.SetRuntimeEnabled(enabled)
	if enabled {
		logger.Infof("engine: thinking mode enabled")
	} else {
		logger.Infof("engine: thinking mode disabled")
	}
}

// IsThinkingEnabled returns whether thinking mode is currently active
func (e *Engine) IsThinkingEnabled() bool {
	cfg := e.Agent.GetConfig()
	if cfg.Reasoning == nil {
		return false
	}
	return cfg.Reasoning.IsEffectivelyEnabled()
}

// GetThinkingStatus returns detailed information about the current thinking mode
func (e *Engine) GetThinkingStatus() ThinkingStatus {
	cfg := e.Agent.GetConfig()
	if cfg.Reasoning == nil {
		return ThinkingStatus{
			Enabled: false,
			Source:  "default",
		}
	}

	return ThinkingStatus{
		Enabled: cfg.Reasoning.IsEffectivelyEnabled(),
		Effort:  cfg.Reasoning.Effort,
		Source:  cfg.Reasoning.GetRuntimeSource(),
	}
}

// HandleThinkCommand handles the /think slash command
func (e *Engine) HandleThinkCommand(args string) string {
	trimmedArgs := strings.TrimSpace(args)

	switch trimmedArgs {
	case "on":
		e.SetThinkingEnabled(true)
		status := e.GetThinkingStatus()
		if status.Effort != "" {
			return fmt.Sprintf("🧠 思考模式已开启 (effort: %s)", status.Effort)
		}
		return "🧠 思考模式已开启"

	case "off":
		e.SetThinkingEnabled(false)
		return "💤 思考模式已关闭"

	case "status", "":
		status := e.GetThinkingStatus()
		return fmt.Sprintf("🧠 思考模式: %s", status.String())

	default:
		return "❌ 无效参数。用法: /think [on|off|status]"
	}
}
