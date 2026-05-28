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
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/hookservice"
	"github.com/nano-harness/nano-agent/pkg/logger"
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

// emitResult writes byte-identical JSON to both stdout and <outputDir>/result.json.
// If patch is non-empty it is also written to <outputDir>/solution.patch.
func emitResult(stdout io.Writer, outputDir string, summary binaryResultSummary, patch string) error {
	payload, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "result.json"), payload, 0o644); err != nil {
		return fmt.Errorf("write result.json: %w", err)
	}
	if patch != "" {
		if err := os.WriteFile(filepath.Join(outputDir, "solution.patch"), []byte(patch), 0o644); err != nil {
			return fmt.Errorf("write solution.patch: %w", err)
		}
	}
	if _, err := stdout.Write(payload); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}

func saveTrajectory(trajectory []trajectoryEvent, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(trajectory)
}

// trajectoryEvent captures a simplified event for trajectory logging
type trajectoryEvent struct {
	Type       string                   `json:"type"`
	Content    string                   `json:"content,omitempty"`
	ToolCalls  []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolResult map[string]interface{}   `json:"tool_result,omitempty"`
	TokenStats map[string]interface{}   `json:"token_stats,omitempty"`
	Error      string                   `json:"error,omitempty"`
	Meta       map[string]interface{}   `json:"meta,omitempty"`
	Timestamp  int64                    `json:"timestamp,omitempty"`
}

// trajectoryOptimizer handles trajectory compression and optimization
type trajectoryOptimizer struct {
	compressionEnabled bool
}

// newTrajectoryOptimizer creates a new trajectory optimizer
func newTrajectoryOptimizer() *trajectoryOptimizer {
	return &trajectoryOptimizer{
		compressionEnabled: true,
	}
}

// shouldRecordEvent determines if an event should be recorded in trajectory
func (to *trajectoryOptimizer) shouldRecordEvent(eventType event.EventType) bool {
	if !to.compressionEnabled {
		return true
	}

	// 只记录关键事件，移除流内容相关事件
	switch eventType {
	case event.EventTypeStreamContent:
		return false // 完全移除流内容记录
	case event.EventTypeContent:
		return true // 保留最终内容
	case event.EventTypeToolCall, event.EventTypeToolResult:
		return true // 保留工具调用和结果
	case event.EventTypeError:
		return true // 保留错误信息
	case event.EventTypeCompression:
		return true // 保留压缩信息
	case event.EventTypeDebug:
		return true // 保留上下文统计信息
	case event.EventTypeTokenStats:
		return false // 去除中间过程的令牌统计事件，最终一次在完成时收敛输出
	case event.EventTypeFinalSummary:
		return true // 保留最终摘要
	default:
		return false // 其他事件类型默认不记录
	}
}

// resolveBinarySessionID picks a stable session id for binary exec hooks/lifecycle.
// Priority: --session-id flag → NANO_SESSION_ID env → SYMPHONY_ISSUE_ID env → fresh session_<hex>
func resolveBinarySessionID(opts binaryOptions) string {
	// 1. Explicit flag (highest priority)
	if strings.TrimSpace(opts.SessionID) != "" {
		return strings.TrimSpace(opts.SessionID)
	}
	// 2. NANO_SESSION_ID environment variable
	if env := strings.TrimSpace(os.Getenv("NANO_SESSION_ID")); env != "" {
		return env
	}
	// 3. SYMPHONY_ISSUE_ID environment variable (fallback for symphony integration)
	if env := strings.TrimSpace(os.Getenv("SYMPHONY_ISSUE_ID")); env != "" {
		return env
	}
	// 4. Generate fresh session ID (same format as TUI mode)
	return agent.NewSession().ID
}

func executeBinaryModeWithOptions(prompt, projectPath string, opts binaryOptions) (binaryExecution, error) {
	execRes := binaryExecution{}
	prompt, goalCondition, goalFromPrompt := prepareBinaryGoal(prompt, opts)
	cfg := config.Get()
	if cfg == nil {
		return execRes, fmt.Errorf("configuration not initialized")
	}

	// Configure logger based on verbose setting from config
	logger.SetVerbose(cfg.Verbose)

	cfgCopy := *cfg
	// Embedded execution (under symphony or other orchestrators) needs MCP to
	// call back to the orchestrator. Standalone binary mode (SWE-bench style)
	// should not auto-connect to user-configured MCP servers like
	// chrome-devtools that they have in their global ~/.config/nano/config.yaml.
	//
	// Detection mirrors applyBinarySandboxMode below: SYMPHONY_* /
	// NANO_ORCHESTRATOR_PROFILE env vars indicate embedded mode.
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

	// Resolve permission mode using unified resolver (includes env var resolution)
	res, warns := ResolvePermission(&cfgCopy, PermissionResolveOpts{
		FlagMode:       opts.PermissionMode,
		SkipPerms:      opts.SkipPerms,
		EnvHintEnabled: true,
	})
	LogPermissionResolution("binary", res, warns)

	applyBinarySandboxMode(&cfgCopy, opts.Sandbox, projectPath)

	// Change to project directory
	oldDir, err := os.Getwd()
	if err != nil {
		return execRes, fmt.Errorf("failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(oldDir) }()

	if err := os.Chdir(projectPath); err != nil {
		return execRes, fmt.Errorf("failed to change to project directory: %v", err)
	}

	// Create agent instance
	agentInstance, err := agent.New(&cfgCopy, nil)
	if err != nil {
		return execRes, fmt.Errorf("failed to create agent: %v", err)
	}

	// Resolve and set session ID before accessing session
	sessionID := resolveBinarySessionID(opts)
	execRes.SessionID = sessionID
	agentInstance.SetActiveSessionID(sessionID)

	if strings.TrimSpace(opts.OutputDir) != "" {
		agentInstance.GetSessionManager().SetStorage(agent.NewLocalSessionStorage(filepath.Join(opts.OutputDir, "sessions")))
	}
	session := agentInstance.GetSessionManager().GetOrCreateSession(sessionID)
	goalCtx := session.GoalContext()
	goalCtx.Configure(&cfgCopy)
	if goalCondition != "" {
		if err := goalCtx.SetGoal(goalCondition); err != nil {
			return execRes, err
		}
		if goalFromPrompt {
			logger.Infof("Binary mode enabled /goal from prompt: %s", goalCondition)
		} else {
			logger.Infof("Binary mode enabled /goal from flag: %s", goalCondition)
		}
	}

	// Process the prompt using ProcessStream
	ctx := context.Background()
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
			SessionID:     sessionID,
			Cwd:           projectPath,
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

// dispatchBinaryStopHook is removed — per-run result delivery now uses clean
// stdout JSON (AgentResultSummary). The hook layer no longer transports aggregated
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
// embedding orchestrator environment is detected; on always enables it; off disables it.
func applyBinarySandboxMode(cfg *config.Config, mode, projectPath string) {
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

// recordBinaryPromptCacheKey stores non-secret metadata for NANO_CACHE_KEY or
// SYMPHONY_ISSUE_ID under ~/.cache/nano/<key>/prompt-cache.json. It returns the
// sanitized key used on disk so result summaries can correlate retries without
// writing prompt contents or secrets to cache metadata.
func recordBinaryPromptCacheKey(prompt string) (string, error) {
	key := firstNonEmpty(os.Getenv("NANO_CACHE_KEY"), os.Getenv("SYMPHONY_ISSUE_ID"))
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
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not initialized")
	}

	cfgCopy := *cfg
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
	applyBinarySandboxMode(&cfgCopy, opts.Sandbox, projectPath)

	// Change to project directory
	oldDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(oldDir) }()

	if err := os.Chdir(projectPath); err != nil {
		return fmt.Errorf("failed to change to project directory: %v", err)
	}

	// Create agent instance
	agentInstance, err := agent.New(&cfgCopy, nil)
	if err != nil {
		return fmt.Errorf("failed to create agent: %v", err)
	}

	// Resolve and set session ID before accessing session
	sessionID := resolveBinarySessionID(opts)
	agentInstance.SetActiveSessionID(sessionID)
	agentInstance.GetSessionManager().GetOrCreateSession(sessionID)

	// Create encoder for streaming output
	enc := json.NewEncoder(cmd.OutOrStdout())
	ctx := context.Background()

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
