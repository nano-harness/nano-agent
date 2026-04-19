// Package engine provides a unified lifecycle manager for the agent, scheduler,
// watcher, and state store. All execution modes (TUI, BubbleTea, Daemon) should
// use Engine instead of wiring these components manually.
package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/cron"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/tools/builtin"
	"github.com/nano-harness/nano-agent/pkg/watcher"
)

// Engine encapsulates the lifecycle of an agent, its scheduler, optional
// watcher, and the shared state store. Use New to construct a ready-to-run
// Engine; call Start to activate the scheduler and watcher; call Shutdown to
// gracefully stop everything.
type Engine struct {
	// Agent is the core AI agent instance.
	Agent *agent.Agent
	// Scheduler is the cron-based recurring task scheduler.
	Scheduler *cron.Scheduler
	// Watcher is the event-monitoring component (may be nil if not configured).
	Watcher *watcher.Watcher
	// StateStore is the shared persistent state store.
	StateStore *config.StateStore

	manageWatcherTool *builtin.ManageWatcherTool
}

// New builds an Engine from the provided config and optional approval handler.
// The approvalHandler may be nil (all tool calls are auto-approved).
func New(cfg *config.Config, approvalHandler func(*agent.ToolCallInfo) bool) (*Engine, error) {
	if cfg == nil {
		return nil, fmt.Errorf("engine: config is required")
	}

	agentInstance, err := agent.New(cfg, approvalHandler)
	if err != nil {
		return nil, fmt.Errorf("engine: create agent: %w", err)
	}

	e := &Engine{
		Agent:      agentInstance,
		StateStore: agentInstance.GetStateStore(),
	}

	// Build the executor that the scheduler and watcher use to run commands.
	executeTask := func(command string) error {
		ctx := context.Background()
		return agentInstance.ProcessStream(ctx, command, func(se event.StreamEvent) {
			logger.Debugf("engine: task event [%s]: %s", se.Type, se.Content)
		})
	}

	// Create scheduler and wire into the manage_schedule tool.
	e.Scheduler = cron.New(executeTask)
	if e.StateStore != nil {
		e.Scheduler.SetStateStore(e.StateStore)
	}
	agentInstance.SetScheduler(e.Scheduler)

	// Create watcher if enabled in config.
	if cfg.Watcher != nil && cfg.Watcher.Enabled {
		e.Watcher = watcher.New(executeTask, e.StateStore)

		// Load static rules from config using the non-persisting path so that
		// config-sourced rules do not accumulate in the state store on every restart.
		for _, r := range cfg.Watcher.Rules {
			e.Watcher.AddRuleNoStore(watcher.Rule{
				ID:           r.ID,
				Source:       r.Source,
				Event:        r.Event,
				Filter:       r.Filter,
				Command:      r.Command,
				Interval:     r.Interval,
				Timeout:      r.Timeout,
				ShellCommand: r.ShellCommand,
			})
		}
	}

	// Register manage_watcher tool and wire the live watcher into it.
	mwt := builtin.NewManageWatcherTool(e.Watcher, agentInstance.GetApprovalConfirmFn())
	e.manageWatcherTool = mwt
	if err := agentInstance.RegisterTool(mwt); err != nil {
		logger.Warnf("engine: failed to register manage_watcher tool: %v", err)
	}

	return e, nil
}

// Start activates the scheduler (loading any persisted tasks) and the watcher.
// Call this after setting up any UI-specific event bridges.
func (e *Engine) Start() error {
	if e.Scheduler != nil {
		e.Scheduler.Start()
		if err := e.Scheduler.LoadPersistedTasks(); err != nil {
			logger.Warnf("engine: failed to reload persisted tasks: %v", err)
		}
	}

	if e.Watcher != nil {
		e.Watcher.Start()
	}

	return nil
}

// Shutdown stops the watcher, scheduler, and agent gracefully.
func (e *Engine) Shutdown() error {
	if e.Watcher != nil {
		e.Watcher.Stop()
	}

	if e.Scheduler != nil {
		e.Scheduler.Stop()
	}

	if e.Agent != nil {
		return e.Agent.Shutdown()
	}

	return nil
}

// SetWatcher replaces the watcher used by the engine and updates the
// manage_watcher tool reference. Useful when the watcher is created after
// initial Engine construction (e.g. in tests).
func (e *Engine) SetWatcher(w *watcher.Watcher) {
	e.Watcher = w
	if e.manageWatcherTool != nil {
		e.manageWatcherTool.SetWatcher(w)
	}
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
