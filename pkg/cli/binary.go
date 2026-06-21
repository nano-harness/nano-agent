// Package cli provides the command-line interface for the nano agent
package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/hookservice"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

var unsafeCacheKeyChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

const (
	binaryExitSuccess      = 0
	binaryExitRetry        = 10
	binaryExitAbandoned    = 20
	binaryExitTimeout      = 30
	binaryExitUnclassified = 1
)

// Sentinel errors for binary error classification.
// Wrap these with fmt.Errorf("...: %w", ErrTimeout) to enable errors.Is matching.
var (
	ErrTimeout       = errors.New("timeout")
	ErrRateLimit     = errors.New("rate limit")
	ErrSandboxDenied = errors.New("sandbox denied")
)

type binaryOptions struct {
	OutputDir      string
	Sandbox        string
	Goal           string
	GoalMaxTurns   int
	SessionID      string // NEW: explicit override
	HookService    *hookservice.Service
	PermissionMode string // --permission-mode flag value
	SkipPerms      bool   // --dangerously-skip-permissions flag value
	MCPConfigFile  string // --mcp-config: path to Claude Code-compatible .mcp.json
	AllowedTools   []string // --allowedTools: repeatable allow-pattern (first entry wins for a given prefix)
	DisallowedTools []string // --disallowedTools: repeatable deny-pattern (overrides allow)
	AddDirs        []string // --add-dir: additional directories to allow sandbox write access to (repeatable)
}

type binaryResultTokens struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

type binaryResultSummary struct {
	Status                string             `json:"status"`
	Reason                string             `json:"reason,omitempty"`
	TerminationCause      string             `json:"termination_cause,omitempty"`   // Structured enum: task_done, error_threshold, etc.
	BlockerFingerprint    string             `json:"blocker_fingerprint,omitempty"` // Stable blocker ID
	BlockedCommandsSample []string           `json:"blocked_commands_sample,omitempty"`
	ToolCalls             int                `json:"tool_calls"`
	DurationMS            int64              `json:"duration_ms"`
	Tokens                binaryResultTokens `json:"tokens"`
	GoalState             *agent.GoalState   `json:"goal_state,omitempty"`
	CacheKey              string             `json:"cache_key,omitempty"`
}

type binaryTurnTermination struct {
	Cause       string
	Fingerprint string
	Reason      string
}

type binaryExecution struct {
	Result                string
	Trajectory            []trajectoryEvent
	GoalState             *agent.GoalState
	SessionID             string
	Termination           binaryTurnTermination
	BlockedCommandsSample []string
}

// emitResult writes JSON to <outputDir>/result.json (atomic, 0600) and to
// stdout with surrounding newlines for reliable line-boundary parsing.
// The file payload is bare JSON; the stdout payload is wrapped with "\n…\n".
// If patch is non-empty it is also written atomically to <outputDir>/solution.patch.
func emitResult(stdout io.Writer, outputDir string, summary binaryResultSummary, patch string) error {
	if err := validateSummary(&summary); err != nil {
		// Contract violation: rewrite as abandoned so orchestrators can still parse.
		summary = binaryResultSummary{
			Status:             "abandoned",
			Reason:             fmt.Sprintf("invalid result summary: %v", err),
			TerminationCause:   "contract_violation",
			BlockerFingerprint: "contract_violation",
		}
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	// A5: atomic write + 0600 so partial writes / crashes leave no truncated file.
	if err := writeFileAtomic(filepath.Join(outputDir, "result.json"), payload, 0o600); err != nil {
		return fmt.Errorf("write result.json: %w", err)
	}
	if patch != "" {
		if err := writeFileAtomic(filepath.Join(outputDir, "solution.patch"), []byte(patch), 0o600); err != nil {
			return fmt.Errorf("write solution.patch: %w", err)
		}
	}
	// A4: surround the JSON with newlines so the payload is unambiguously
	// line-delimited on stdout even if prior output did not end with a newline.
	// The file copy above remains bare JSON (no surrounding newlines).
	if _, err := stdout.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	if _, err := stdout.Write(payload); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	if _, err := stdout.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}

// emitPanicResult writes a valid abandoned JSON summary to stdout (and to
// <outputDir>/result.json when outputDir is non-empty). It is used by defer/recover
// in binary subcommands so an unhandled panic never leaves stdout without a
// parseable result line for orchestrators.
func emitPanicResult(stdout io.Writer, outputDir string, r any) {
	summary := binaryResultSummary{
		Status:             "abandoned",
		Reason:             fmt.Sprintf("panic: %v", r),
		TerminationCause:   "panic",
		BlockerFingerprint: "panic",
	}
	_ = emitResult(stdout, outputDir, summary, "")
}

// writeFileAtomic writes data to path atomically: it creates a sibling temp
// file, fsyncs it, then renames it over the target.  This ensures readers
// always observe a complete file, never a partial write.  perm is applied to
// the temp file before the rename so the final file has the requested mode.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Remove the temp file on any failure path; no-op after a successful rename.
	success := false
	defer func() {
		_ = tmp.Close()
		if !success {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("fsync temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	success = true
	return nil
}

func saveTrajectory(trajectory []trajectoryEvent, path string) error {
	data, err := json.MarshalIndent(trajectory, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trajectory: %w", err)
	}
	// A5: use atomic write so a partial trajectory is never visible to readers,
	// and 0600 because trajectory may contain sensitive tool outputs.
	return writeFileAtomic(path, data, 0o600)
}

// applyMCPOverrides applies MCP config file and tool permission flags to
// a Config copy. Used by both the streaming and non-streaming binary paths.
func applyMCPOverrides(cfg *config.Config, opts binaryOptions) error {
	if opts.MCPConfigFile != "" {
		servers, err := config.LoadMCPConfigFile(opts.MCPConfigFile)
		if err != nil {
			return fmt.Errorf("load MCP config file %s: %w", opts.MCPConfigFile, err)
		}
		cfg.MCP.Servers = append(cfg.MCP.Servers, servers...)
		if len(servers) > 0 {
			cfg.EnableMCP = true
		}
	}
	if len(opts.AllowedTools) > 0 {
		cfg.MCP.AllowedTools = opts.AllowedTools
	}
	if len(opts.DisallowedTools) > 0 {
		cfg.MCP.DisallowedTools = opts.DisallowedTools
	}
	return nil
}

func resolveBinarySessionID(opts binaryOptions) string {
	// 1. Explicit flag (highest priority)
	if strings.TrimSpace(opts.SessionID) != "" {
		return strings.TrimSpace(opts.SessionID)
	}
	// 2. NANO_SESSION_ID environment variable
	if env := strings.TrimSpace(os.Getenv("NANO_SESSION_ID")); env != "" {
		return env
	}
	// 3. SYMPHONY_ISSUE_ID environment variable (orchestrator-spawned fallback)
	if env := strings.TrimSpace(os.Getenv("SYMPHONY_ISSUE_ID")); env != "" {
		return env
	}
	// 4. Generate fresh session ID (same format as TUI mode)
	return agent.NewSession().ID
}

type binaryAgentContext struct {
	agentInstance *agent.Agent
	cfgCopy       config.Config
	sessionID     string
	goalCtx       *agent.GoalContext
	projectPath   string
	cleanup       func()
}

// prepareBinaryAgent performs the shared setup for both streaming and
// non-streaming binary execution paths: config copy, MCP/permission/sandbox
// resolution, directory change, agent creation, session/goal setup.
// Callers must invoke cleanup() when done.
func prepareBinaryAgent(prompt, projectPath string, opts binaryOptions, mode string) (*binaryAgentContext, error) {
	prompt, goalCondition, goalFromPrompt := prepareBinaryGoal(prompt, opts)
	cfg := config.Get()
	if cfg == nil {
		return nil, fmt.Errorf("configuration not initialized")
	}

	// Configure logger based on verbose setting from config
	logger.SetVerbose(cfg.Verbose)

	cfgCopy := *cfg
	// Orchestrator-spawned execution (e.g. from nano-symphony) needs MCP to call
	// back to the orchestrator. Standalone binary mode (SWE-bench style) should
	// not auto-connect to user-configured MCP servers like chrome-devtools that
	// they have in their global ~/.config/nano/config.yaml.
	//
	// Detection mirrors applyBinarySandboxMode below: SYMPHONY_WORKSPACE,
	// SYMPHONY_MCP_URL, or NANO_ORCHESTRATOR_PROFILE env vars indicate
	// orchestrator-spawned mode. These env var keys are part of the documented
	// CLI contract and must not change.
	if !isEmbeddedBinaryExecution() {
		cfgCopy.EnableMCP = false
	}
	if cfg.OSS != nil {
		ossCopy := *cfg.OSS
		ossCopy.Enabled = false
		cfgCopy.OSS = &ossCopy
	}
	if opts.GoalMaxTurns > 0 {
		if cfgCopy.Goal == nil {
			cfgCopy.Goal = &config.GoalConfig{}
		} else {
			goalCopy := *cfgCopy.Goal
			cfgCopy.Goal = &goalCopy
		}
		cfgCopy.Goal.MaxTurns = opts.GoalMaxTurns
	}

	// MCP + tool permissions from binary exec flags
	if err := applyMCPOverrides(&cfgCopy, opts); err != nil {
		return nil, err
	}

	// Resolve permission mode using unified resolver (includes env var resolution)
	res, warns := ResolvePermission(&cfgCopy, PermissionResolveOpts{
		FlagMode:       opts.PermissionMode,
		SkipPerms:      opts.SkipPerms,
		EnvHintEnabled: true,
	})
	LogPermissionResolution(mode, res, warns)

	// When auto mode has no escape hatch, gracefully degrade to default mode
	// rather than failing hard. This matches the documented behavior that
	// "ModeAuto behaves like ModeDefault when no classifier is wired."
	if res.Mode == permission.ModeAuto && !hasModeAutoEscape(&cfgCopy) {
		logger.Warnf("binary exec: permission_mode=auto has no classifier/escape configured; degrading to default mode")
		cfgCopy.PermissionMode = string(permission.ModeDefault)
	}

	applyBinarySandboxMode(&cfgCopy, opts.Sandbox, projectPath, opts.AddDirs)
	// S3: fail-closed when sandbox=on but no working backend is available.
	if err := validateSandboxRequirement(opts.Sandbox, cfgCopy.Sandbox, projectPath); err != nil {
		return nil, err
	}

	// Change to project directory
	oldDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %v", err)
	}
	if err := os.Chdir(projectPath); err != nil {
		_ = os.Chdir(oldDir)
		return nil, fmt.Errorf("failed to change to project directory: %v", err)
	}

	// Create agent instance
	agentInstance, err := agent.New(&cfgCopy, nil)
	if err != nil {
		_ = os.Chdir(oldDir)
		return nil, fmt.Errorf("failed to create agent: %v", err)
	}

	// Resolve and set session ID before accessing session
	sessionID := resolveBinarySessionID(opts)
	agentInstance.SetActiveSessionID(sessionID)

	if strings.TrimSpace(opts.OutputDir) != "" {
		agentInstance.GetSessionManager().SetStorage(agent.NewLocalSessionStorage(filepath.Join(opts.OutputDir, "sessions")))
	}
	session := agentInstance.GetSessionManager().GetOrCreateSession(sessionID)
	goalCtx := session.GoalContext()
	goalCtx.Configure(&cfgCopy)
	if goalCondition != "" {
		if err := goalCtx.SetGoal(goalCondition); err != nil {
			_ = os.Chdir(oldDir)
			return nil, err
		}
		if goalFromPrompt {
			logger.Infof("Binary mode enabled /goal from prompt: %s", goalCondition)
		} else {
			logger.Infof("Binary mode enabled /goal from flag: %s", goalCondition)
		}
	}

	return &binaryAgentContext{
		agentInstance: agentInstance,
		cfgCopy:       cfgCopy,
		sessionID:     sessionID,
		goalCtx:       goalCtx,
		projectPath:   projectPath,
		cleanup:       func() { _ = os.Chdir(oldDir) },
	}, nil
}

func executeBinaryModeWithOptions(prompt, projectPath string, opts binaryOptions) (binaryExecution, error) {
	execRes := binaryExecution{}

	ctxAgent, err := prepareBinaryAgent(prompt, projectPath, opts, "binary")
	if err != nil {
		return execRes, err
	}
	defer ctxAgent.cleanup()
	execRes.SessionID = ctxAgent.sessionID

	agentInstance := ctxAgent.agentInstance
	goalCtx := ctxAgent.goalCtx
	cfgCopy := ctxAgent.cfgCopy
	execProjectPath := ctxAgent.projectPath

	// Process the prompt using ProcessStream
	// A6: build a context with optional deadline and signal-aware cancellation.
	ctx, ctxCancel := buildBinaryExecContext()
	defer ctxCancel()
	var contentResult strings.Builder
	var streamResult strings.Builder
	var traj []trajectoryEvent
	optimizer := newTrajectoryOptimizer()
	var lastTokenStats *event.TokenStats
	var sawFinalContent bool

	// Get system prompt from agent and add it as the first trajectory entry
	systemPromptBuilder := agent.NewSystemPromptBuilder(agentInstance.GetWorkingDirectory(), agentInstance.GetToolbox().List(), agentInstance.GetMemoryManager(), &cfgCopy)
	systemPrompt := systemPromptBuilder.BuildEnhancedSystemPrompt(ctx, []string{})
	traj = append(traj, trajectoryEvent{
		Type:      "system_prompt",
		Content:   systemPrompt,
		Meta:      map[string]interface{}{"role": "system"},
		Timestamp: time.Now().Unix(),
	})

	// Seed initial user message into trajectory
	traj = append(traj, trajectoryEvent{
		Type:      string(event.EventTypeContent),
		Content:   prompt,
		Meta:      map[string]interface{}{"role": "user"},
		Timestamp: time.Now().Unix(),
	})

	// Create an event handler to capture output and build trajectory
	eventHandler := func(streamEvent event.StreamEvent) {
		// Accumulate assistant content from relevant events only
		if streamEvent.Type == event.EventTypeStreamContent && !sawFinalContent {
			if streamEvent.Content != "" {
				streamResult.WriteString(streamEvent.Content)
			}
		}
		if streamEvent.Type == event.EventTypeContent {
			if streamEvent.Content != "" {
				sawFinalContent = true
				contentResult.WriteString(streamEvent.Content)
			}
		}

		// 聚合 token_stats：只保留最后一次，用于最终输出
		if streamEvent.Type == event.EventTypeTokenStats {
			if streamEvent.TokenStats != nil {
				lastTokenStats = streamEvent.TokenStats
			}
			// 中间的 token_stats 不写入轨迹
			return
		}

		// 在完成事件到来时，追加一次最终 token_stats 到轨迹
		if streamEvent.Type == event.EventTypeDone && lastTokenStats != nil {
			ts := lastTokenStats
			e := trajectoryEvent{
				Type:      string(event.EventTypeTokenStats),
				Timestamp: streamEvent.Timestamp,
				TokenStats: map[string]interface{}{
					"input_tokens":           ts.InputTokens,
					"output_tokens":          ts.OutputTokens,
					"total_tokens":           ts.TotalTokens,
					"request_size_bytes":     ts.RequestSizeBytes,
					"response_size_bytes":    ts.ResponseSizeBytes,
					"start_time":             ts.StartTime,
					"end_time":               ts.EndTime,
					"duration_ms":            ts.Duration,
					"session_input_tokens":   ts.SessionInputTokens,
					"session_output_tokens":  ts.SessionOutputTokens,
					"session_total_tokens":   ts.SessionTotalTokens,
					"tokens_per_second":      ts.TokensPerSecond,
					"peak_tokens_per_second": ts.PeakTokensPerSecond,
					"is_streaming":           ts.IsStreaming,
					"update_count":           ts.UpdateCount,
					"reasoning_enabled":      ts.ReasoningEnabled,
					"reasoning_tokens":       ts.ReasoningTokens,
					"reasoning_effort":       ts.ReasoningEffort,
					"reasoning_fallback":     ts.ReasoningFallback,
					"reasoning_latency_ms":   ts.ReasoningLatency,
				},
			}
			traj = append(traj, e)
			// 追加一次后清空，避免多次写入
			lastTokenStats = nil
		}

		// 优化trajectory记录 - 只记录关键事件
		if !optimizer.shouldRecordEvent(streamEvent.Type) {
			return
		}

		// Record event into trajectory
		e := trajectoryEvent{
			Type:      string(streamEvent.Type),
			Content:   streamEvent.Content,
			Timestamp: streamEvent.Timestamp,
		}

		// 添加角色信息
		if streamEvent.Type == event.EventTypeContent {
			e.Meta = map[string]interface{}{"role": "assistant"}
		}

		// 记录压缩事件（完整元数据）
		if streamEvent.Type == event.EventTypeCompression {
			// 合并事件元数据，并附加来源
			meta := map[string]interface{}{}
			if streamEvent.Metadata != nil {
				for k, v := range streamEvent.Metadata {
					meta[k] = v
				}
			}
			meta["event_source"] = streamEvent.Source
			e.Meta = meta

			// content 仅展示压缩后的摘要内容（使用 summary_full）
			summaryFull, _ := streamEvent.Metadata["summary_full"].(string)
			if summaryFull != "" {
				e.Content = summaryFull
			} else {
				// 如果元数据缺失，回退为原始内容（通常为压缩统计）
				e.Content = streamEvent.Content
			}
		}

		// 记录上下文统计（来自 debug 事件）
		if streamEvent.Type == event.EventTypeDebug {
			// 直接写入上下文统计元数据
			meta := map[string]interface{}{}
			if streamEvent.Metadata != nil {
				for k, v := range streamEvent.Metadata {
					meta[k] = v
				}
			}
			meta["event_source"] = streamEvent.Source
			e.Meta = meta
		}

		// Tool calls
		if len(streamEvent.ToolCalls) > 0 {
			for _, tc := range streamEvent.ToolCalls {
				if tc == nil {
					continue
				}
				e.ToolCalls = append(e.ToolCalls, map[string]interface{}{
					"id":        tc.ID,
					"name":      tc.Name,
					"arguments": tc.Arguments,
				})
			}
		}
		// Tool result
		if streamEvent.ToolResult != nil {
			e.ToolResult = map[string]interface{}{
				"id":      streamEvent.ToolResult.ID,
				"content": streamEvent.ToolResult.Content,
				"error":   streamEvent.ToolResult.Error,
			}
		}
		if streamEvent.Error != "" {
			e.Error = streamEvent.Error
		}
		traj = append(traj, e)
	}

	// Fire SessionStart hook once per binary execution (before first LLM call).
	if opts.HookService != nil && len(opts.HookService.HooksForEvent(hookservice.EventSessionStart)) > 0 {
		_, err := opts.HookService.Dispatch(ctx, hookservice.EventSessionStart, "", hookservice.Input{
			Event:         hookservice.EventSessionStart,
			HookEventName: "SessionStart",
			SessionID:     ctxAgent.sessionID,
			Cwd:           execProjectPath,
			Source:        "startup",
		})
		if err != nil {
			logger.Warnf("SessionStart hook dispatch failed: %v", err)
		}
	}

	err = agentInstance.ProcessStream(ctx, prompt, eventHandler)
	goalState := goalSnapshotPtr(goalCtx)
	if err != nil {
		execRes.GoalState = goalState
		return execRes, fmt.Errorf("failed to process request: %v", err)
	}

	// 若未捕获到完成事件但仍有最终 token_stats，则在结束时补充一次
	if lastTokenStats != nil {
		ts := lastTokenStats
		e := trajectoryEvent{
			Type:      string(event.EventTypeTokenStats),
			Timestamp: time.Now().Unix(),
			TokenStats: map[string]interface{}{
				"input_tokens":           ts.InputTokens,
				"output_tokens":          ts.OutputTokens,
				"total_tokens":           ts.TotalTokens,
				"request_size_bytes":     ts.RequestSizeBytes,
				"response_size_bytes":    ts.ResponseSizeBytes,
				"start_time":             ts.StartTime,
				"end_time":               ts.EndTime,
				"duration_ms":            ts.Duration,
				"session_input_tokens":   ts.SessionInputTokens,
				"session_output_tokens":  ts.SessionOutputTokens,
				"session_total_tokens":   ts.SessionTotalTokens,
				"tokens_per_second":      ts.TokensPerSecond,
				"peak_tokens_per_second": ts.PeakTokensPerSecond,
				"is_streaming":           ts.IsStreaming,
				"update_count":           ts.UpdateCount,
				"reasoning_enabled":      ts.ReasoningEnabled,
				"reasoning_tokens":       ts.ReasoningTokens,
				"reasoning_effort":       ts.ReasoningEffort,
				"reasoning_fallback":     ts.ReasoningFallback,
				"reasoning_latency_ms":   ts.ReasoningLatency,
			},
		}
		traj = append(traj, e)
		lastTokenStats = nil
	}

	final := contentResult.String()
	if strings.TrimSpace(final) == "" {
		final = streamResult.String()
	}
	if goalState != nil {
		traj = append(traj, trajectoryEvent{
			Type:      "goal_state",
			Meta:      map[string]interface{}{"goal_state": goalState},
			Timestamp: time.Now().Unix(),
		})
	}

	execRes.Result = final
	execRes.Trajectory = traj
	execRes.GoalState = goalState
	if pm := agentInstance.GetPermissionManager(); pm != nil {
		execRes.BlockedCommandsSample = pm.DenialTrackerSample()
		if pm.DenialTrackerLockedOut() {
			execRes.Termination = binaryTurnTermination{
				Cause:       "classifier_lockout",
				Fingerprint: "classifier_lockout",
				Reason:      "consecutive deny limit reached",
			}
		}
	}
	return execRes, nil
}

func summarizeBinaryResult(traj []trajectoryEvent, start time.Time, result string, runErr error, goalState *agent.GoalState, term binaryTurnTermination, blockedSample []string) binaryResultSummary {
	summary := binaryResultSummary{
		Status:                "success",
		DurationMS:            time.Since(start).Milliseconds(),
		GoalState:             goalState,
		BlockedCommandsSample: blockedSample,
	}

	// Derive termination cause from available signals
	if strings.TrimSpace(result) == "" {
		summary.Reason = "empty result"
	}

	if runErr != nil {
		summary.Status = classifyBinaryError(runErr)
		summary.Reason = runErr.Error()
		summary.TerminationCause = "llm_failure"
		summary.BlockerFingerprint = "error:" + summary.Status
	}
	if runErr == nil && strings.TrimSpace(term.Cause) != "" {
		summary.TerminationCause = term.Cause
		summary.BlockerFingerprint = term.Fingerprint
		if strings.TrimSpace(term.Reason) != "" {
			summary.Reason = term.Reason
		}
		if term.Cause == "classifier_lockout" {
			summary.Status = "abandoned"
		}
	}

	if runErr == nil && goalState != nil {
		if goalState.AchievedAt != nil {
			summary.Status = "success"
			summary.TerminationCause = "natural_completion"
		} else if goalState.Condition != "" && !goalState.Active && goalState.MaxTurns > 0 && goalState.TurnsEvaluated >= goalState.MaxTurns {
			summary.Status = "needs_retry"
			summary.Reason = firstNonEmpty(goalState.LastReason, "goal max turns reached")
			summary.TerminationCause = "goal_max_turns"
			summary.BlockerFingerprint = fmt.Sprintf("goal_max_turns:%d", goalState.MaxTurns)
		} else if goalState.Condition != "" && goalState.AchievedAt == nil && !goalState.Active {
			// Goal stopped (e.g. evaluator_parse_failure)
			summary.Status = "needs_retry"
			summary.Reason = firstNonEmpty(goalState.LastReason, "goal stopped without achievement")
			summary.TerminationCause = "goal_stopped"
		} else if goalState.Condition != "" && goalState.AchievedAt == nil && goalState.Active {
			// Final fallback: goal active but agent exited without achievement
			summary.Status = "abandoned"
			summary.Reason = firstNonEmpty(goalState.LastReason, "goal active but agent exited without achievement")
			summary.TerminationCause = "goal_unresolved"
		}
	}

	for _, e := range traj {
		summary.ToolCalls += len(e.ToolCalls)
		if e.TokenStats != nil {
			if v, ok := intFromAny(e.TokenStats["input_tokens"]); ok {
				summary.Tokens.Input = v
			}
			if v, ok := intFromAny(e.TokenStats["output_tokens"]); ok {
				summary.Tokens.Output = v
			}
		}
	}

	return summary
}

func classifyBinaryError(err error) string {
	if err == nil {
		return "success"
	}
	// Prefer sentinel error matching via errors.Is
	switch {
	case errors.Is(err, ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, ErrRateLimit):
		return "needs_retry"
	case errors.Is(err, ErrSandboxDenied):
		return "abandoned"
	}
	// Fallback: string-based classification for legacy/unwrapped errors
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline exceeded"):
		return "timeout"
	case strings.Contains(msg, "retry"), strings.Contains(msg, "rate limit"), strings.Contains(msg, "temporar"):
		return "needs_retry"
	default:
		return "abandoned"
	}
}

func binaryExitCode(status string) int {
	switch status {
	case "success":
		return binaryExitSuccess
	case "needs_retry":
		return binaryExitRetry
	case "abandoned":
		return binaryExitAbandoned
	case "timeout":
		return binaryExitTimeout
	default:
		return binaryExitUnclassified
	}
}

// validStatuses are the contract-compliant values for binaryResultSummary.status.
var validStatuses = map[string]struct{}{
	"success":     {},
	"needs_retry": {},
	"abandoned":   {},
	"timeout":     {},
}

// validateSummary ensures a result summary conforms to the cross-project stdout
// contract before it is emitted. Invalid statuses are replaced with "abandoned"
// so orchestrators always receive a parseable payload.
func validateSummary(summary *binaryResultSummary) error {
	if summary == nil {
		return fmt.Errorf("summary is nil")
	}
	if _, ok := validStatuses[summary.Status]; !ok {
		return fmt.Errorf("invalid status %q", summary.Status)
	}
	return nil
}

// dispatchBinaryStopHook is removed — per-run result delivery now uses clean
// stdout JSON (binaryResultSummary). The hook layer no longer transports aggregated
// run summaries; this matches Claude Code's design.

func prepareBinaryGoal(prompt string, opts binaryOptions) (string, string, bool) {
	conditionFromPrompt, strippedPrompt, foundInPrompt := extractPromptGoal(prompt)
	if strings.TrimSpace(opts.Goal) != "" {
		if foundInPrompt {
			return strippedPrompt, strings.TrimSpace(opts.Goal), false
		}
		return prompt, strings.TrimSpace(opts.Goal), false
	}
	if foundInPrompt {
		return strippedPrompt, conditionFromPrompt, true
	}
	return prompt, "", false
}

func extractPromptGoal(prompt string) (condition, strippedPrompt string, found bool) {
	firstLine := prompt
	rest := ""
	if before, after, ok := strings.Cut(prompt, "\n"); ok {
		firstLine = before
		rest = after
	}
	trimmed := strings.TrimSpace(firstLine)
	if !strings.HasPrefix(trimmed, "/goal ") && !strings.HasPrefix(trimmed, "/goal\t") {
		return "", prompt, false
	}
	args := strings.TrimSpace(strings.TrimPrefix(trimmed, "/goal"))
	if args == "" {
		return "", prompt, false
	}
	return args, strings.TrimLeft(rest, "\r\n"), true
}

func goalSnapshotPtr(goalCtx *agent.GoalContext) *agent.GoalState {
	if goalCtx == nil {
		return nil
	}
	state := goalCtx.Snapshot()
	if state.Condition == "" && !state.Active && state.AchievedAt == nil {
		return nil
	}
	return &state
}

// applyBinarySandboxMode applies binary --sandbox=auto|on|off to a config copy.
// auto preserves standalone behavior but enables path/process isolation when an
// orchestrator-spawned environment is detected; on always enables it; off disables it.
func applyBinarySandboxMode(cfg *config.Config, mode, projectPath string, addDirs []string) {
	mode = strings.ToLower(strings.TrimSpace(firstNonEmpty(mode, "auto")))
	if mode == "off" {
		if cfg.Sandbox != nil {
			cfg.Sandbox.Enabled = false
		}
		return
	}
	if mode == "auto" && !isEmbeddedBinaryExecution() {
		return
	}
	if cfg.Sandbox == nil {
		cfg.Sandbox = &config.SandboxConfig{}
	}
	cfg.Sandbox.Enabled = true
	if cfg.Sandbox.Backend == "" {
		cfg.Sandbox.Backend = "native"
	}
	if len(cfg.Sandbox.AllowedPaths) == 0 {
		// Default allowed paths for embedded/binary mode
		allowedPaths := []string{projectPath}

		// Add OS temp directory
		if tmp := os.TempDir(); tmp != "" {
			allowedPaths = append(allowedPaths, tmp)
		}

		// Add user cache directory (platform-specific)
		if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
			allowedPaths = append(allowedPaths, cacheDir)
		}

		cfg.Sandbox.AllowedPaths = allowedPaths

		// Log effective sandbox configuration to stderr for visibility
		fmt.Fprintf(os.Stderr, "sandbox enabled; allowed_paths = %v; set NANO_SANDBOX_ALLOWED_PATHS to override\n", allowedPaths)
	}
	if !stringSliceContains(cfg.Sandbox.ExtraWritablePaths, projectPath) {
		cfg.Sandbox.ExtraWritablePaths = append(cfg.Sandbox.ExtraWritablePaths, projectPath)
	}
	for _, p := range binarySandboxWritableDefaults() {
		if p != "" && !stringSliceContains(cfg.Sandbox.ExtraWritablePaths, p) {
			cfg.Sandbox.ExtraWritablePaths = append(cfg.Sandbox.ExtraWritablePaths, p)
		}
	}
	for _, d := range addDirs {
		if d != "" && !stringSliceContains(cfg.Sandbox.ExtraWritablePaths, d) {
			cfg.Sandbox.ExtraWritablePaths = append(cfg.Sandbox.ExtraWritablePaths, d)
		}
	}
}

func isEmbeddedBinaryExecution() bool {
	for _, env := range []string{"SYMPHONY_WORKSPACE", "SYMPHONY_MCP_URL", "NANO_ORCHESTRATOR_PROFILE"} {
		if strings.TrimSpace(os.Getenv(env)) != "" {
			return true
		}
	}
	return false
}

func binarySandboxWritableDefaults() []string {
	var out []string
	if tmp := os.TempDir(); tmp != "" {
		out = append(out, tmp)
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".nano", "cache"))
	}
	return out
}

// validateSandboxRequirement checks whether the sandbox mode requested by the
// caller can be satisfied on the current platform.  When mode is "on" (explicit)
// and the platform or required tooling cannot provide process isolation, this
// function returns an error so the caller fails-closed rather than silently
// running without any sandbox.  For mode "auto" or "off" no error is returned.
func validateSandboxRequirement(mode string, sandboxCfg *config.SandboxConfig, workingDir string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "on" {
		return nil
	}
	if _, err := sandbox.NewOrError(sandboxCfg, workingDir); err != nil {
		return fmt.Errorf("--sandbox=on: %w", err)
	}
	return nil
}

// recordBinaryPromptCacheKey stores non-secret metadata for NANO_CACHE_KEY or
// SYMPHONY_ISSUE_ID under ~/.cache/nano/<key>/prompt-cache.json. It returns the
// sanitized key used on disk so result summaries can correlate retries without
// writing prompt contents or secrets to cache metadata.
func recordBinaryPromptCacheKey(prompt string) (string, error) {
	// Prefer the explicit resume identity from the orchestrator, then legacy cache
	// key env vars. The key is now stable and no longer depends on the prompt
	// content, so prompt tweaks don't break retry correlation.
	key := firstNonEmpty(os.Getenv("SYMPHONY_RESUME_IDENTITY"), os.Getenv("NANO_CACHE_KEY"), os.Getenv("SYMPHONY_ISSUE_ID"))
	if strings.TrimSpace(key) == "" {
		return "", nil
	}
	safeKey := sanitizeCacheKey(key)
	home, err := os.UserHomeDir()
	if err != nil {
		return safeKey, err
	}
	dir := filepath.Join(home, ".cache", "nano", safeKey)
	// Directory 0700 keeps per-user cache metadata private while preserving the
	// owner execute bit required for directory traversal; the file itself is 0600.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return safeKey, err
	}
	sum := sha256.Sum256([]byte(prompt))
	meta := map[string]interface{}{
		"cache_key":     key,
		"prompt_sha256": hex.EncodeToString(sum[:]),
		"updated_at":    time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return safeKey, err
	}
	return safeKey, os.WriteFile(filepath.Join(dir, "prompt-cache.json"), data, 0o600)
}

func sanitizeCacheKey(key string) string {
	sanitized := strings.Trim(unsafeCacheKeyChars.ReplaceAllString(key, "_"), "_")
	if sanitized == "" {
		return "default"
	}
	return sanitized
}

// runBinaryExecStreaming implements streaming NDJSON output for binary exec
func runBinaryExecStreaming(cmd interface {
	OutOrStdout() io.Writer
	ErrOrStderr() io.Writer
}, prompt, projectPath string, opts binaryOptions, format string, quiet bool) error {
	ctxAgent, err := prepareBinaryAgent(prompt, projectPath, opts, "binary-stream")
	if err != nil {
		return err
	}
	defer ctxAgent.cleanup()

	agentInstance := ctxAgent.agentInstance

	// Create encoder for streaming output
	enc := json.NewEncoder(cmd.OutOrStdout())
	// A6: use a cancellable context with optional timeout, same as the non-stream path.
	ctx, ctxCancel := buildBinaryExecContext()
	defer ctxCancel()

	// Stream events as NDJSON
	eventHandler := func(streamEvent event.StreamEvent) {
		// Build event output
		eventOut := map[string]interface{}{
			"type": string(streamEvent.Type),
			"ts":   time.Now().UTC().Format(time.RFC3339),
		}

		if streamEvent.Content != "" {
			eventOut["content"] = streamEvent.Content
		}

		if streamEvent.Type == event.EventTypeThinking && streamEvent.Reasoning != "" {
			eventOut["reasoning"] = streamEvent.Reasoning
		}

		if streamEvent.Type == event.EventTypeToolUse && streamEvent.ToolUse != nil {
			eventOut["tool_name"] = streamEvent.ToolUse.ToolName
			eventOut["tool_params"] = streamEvent.ToolUse.Parameters
		}

		if streamEvent.Type == event.EventTypeToolResult && streamEvent.ToolResult != nil {
			eventOut["tool_result"] = streamEvent.ToolResult.Content
		}

		if streamEvent.Type == event.EventTypeTokenStats && streamEvent.TokenStats != nil {
			ts := streamEvent.TokenStats
			eventOut["token_stats"] = map[string]interface{}{
				"input":  ts.InputTokens,
				"output": ts.OutputTokens,
				"total":  ts.TotalTokens,
			}
		}

		if streamEvent.Error != "" {
			eventOut["error"] = streamEvent.Error
		}

		// Write event as NDJSON line
		_ = enc.Encode(eventOut)
	}

	err = agentInstance.ProcessStream(ctx, prompt, eventHandler)

	// Send done event
	_ = enc.Encode(map[string]interface{}{
		"type": "done",
		"ts":   time.Now().UTC().Format(time.RFC3339),
	})

	return err
}

func intFromAny(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// hasModeAutoEscape reports whether cfg contains at least one configuration
// escape hatch that allows --permission-mode=auto to make meaningful progress
// (i.e. some tool calls will be approved rather than blocked unconditionally).
func hasModeAutoEscape(cfg *config.Config) bool {
	return cfg.PermissionAuto != nil ||
		len(cfg.AllowedRules) > 0 ||
		(cfg.Daemon != nil && cfg.Daemon.ConfirmPolicy == config.ConfirmPolicyAllow) ||
		(cfg.Daemon != nil && len(cfg.Daemon.AllowlistedTools) > 0)
}

// buildBinaryExecContext creates the execution context for a binary exec run.
// It registers SIGTERM/SIGINT handlers so the agent can shut down gracefully
// and still emit a partial result. Timeout is managed by the spawner layer.
func buildBinaryExecContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	// Register SIGTERM / SIGINT so an operator kill translates into an orderly
	// context cancellation, letting ProcessStream flush what it has.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case sig := <-sigCh:
			logger.Infof("binary exec: received signal %s; cancelling execution context", sig)
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(sigCh)
	}()

	return ctx, cancel
}
