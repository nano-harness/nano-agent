// Package cli provides the command-line interface for the nano agent
package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/patch"
)

const binaryResultSentinel = "<<<NANO_RESULT>>>"

var unsafeCacheKeyChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

const (
	binaryExitSuccess      = 0
	binaryExitRetry        = 10
	binaryExitAbandoned    = 20
	binaryExitTimeout      = 30
	binaryExitUnclassified = 1
)

type binaryOptions struct {
	OutputDir    string
	Sandbox      string
	OnExitCmd    string
	Goal         string
	GoalMaxTurns int
	SessionID    string // NEW: explicit override
}

type binaryResultTokens struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

type binaryResultSummary struct {
	Status              string             `json:"status"`
	Reason              string             `json:"reason,omitempty"`
	TerminationCause    string             `json:"termination_cause,omitempty"`   // Structured enum: task_done, error_threshold, etc.
	BlockerFingerprint  string             `json:"blocker_fingerprint,omitempty"` // Stable blocker ID
	ToolCalls           int                `json:"tool_calls"`
	DurationMS          int64              `json:"duration_ms"`
	Tokens              binaryResultTokens `json:"tokens"`
	GoalState           *agent.GoalState   `json:"goal_state,omitempty"`
	CacheKey            string             `json:"cache_key,omitempty"`
}

// runBinaryMode executes the agent in binary mode for SWE-bench evaluation
func runBinaryMode(args []string, outputDir string) error {
	return runBinaryModeWithOptions(args, binaryOptions{OutputDir: outputDir, Sandbox: "auto"})
}

func runBinaryModeWithOptions(args []string, opts binaryOptions) error {
	if len(args) == 0 {
		return fmt.Errorf("prompt required in binary mode")
	}

	prompt := strings.Join(args, " ")
	start := time.Now()
	summary := binaryResultSummary{Status: "abandoned", Reason: "not started"}
	exitHook := firstNonEmpty(opts.OnExitCmd, os.Getenv("NANO_ON_EXIT"))
	defer func() {
		runBinaryExitHook(exitHook, summary)
	}()

	// Create output directory if specified
	if opts.OutputDir != "" {
		if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
			return fmt.Errorf("error creating output directory: %w", err)
		}
	}

	// Get current working directory as project path
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("error getting current directory: %w", err)
	}

	cacheKey, cacheErr := recordBinaryPromptCacheKey(prompt)
	if cacheErr != nil {
		logger.Warnf("binary prompt cache metadata: %v", cacheErr)
	}

	// Execute agent in binary mode
	// Optional: start local-only pprof server using top-level config only
	cfg := config.Get()
	var pprofServer *http.Server
	if cfg != nil {
		pprofEnabled := cfg.EnablePprof
		pprofPort := cfg.PprofPort
		if pprofEnabled {
			if pprofPort == 0 {
				pprofPort = 6060
			}
			pprofMux := http.NewServeMux()
			pprofMux.HandleFunc("/debug/pprof/", pprof.Index)
			pprofMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			pprofMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			pprofMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			pprofMux.HandleFunc("/debug/pprof/trace", pprof.Trace)

			addr := fmt.Sprintf("%s:%d", "127.0.0.1", pprofPort)
			pprofServer = &http.Server{Addr: addr, Handler: pprofMux}
			go func() {
				logger.Infof("Starting pprof server on %s (local-only)", addr)
				if err := pprofServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Warnf("pprof server error: %v", err)
				}
			}()
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = pprofServer.Shutdown(ctx)
			}()
		}
	}

	result, trajectory, goalState, err := executeBinaryModeWithOptions(prompt, projectPath, opts)
	summary = summarizeBinaryResult(trajectory, start, result, err, goalState)
	summary.CacheKey = cacheKey
	if err != nil {
		_ = writeBinaryResult(os.Stdout, summary)
		return fmt.Errorf("error executing agent: %w", err)
	}

	// Generate patch using PatchGenerator
	baseCommit := os.Getenv("NANO_BASE_COMMIT")
	patchGen := patch.NewGenerator(projectPath, baseCommit)
	patchContent, err := patchGen.GenerateGitDiff()
	if err != nil {
		return fmt.Errorf("error generating patch: %w", err)
	}

	// Save patch file or output to stdout
	if opts.OutputDir != "" {
		patchPath := filepath.Join(opts.OutputDir, "solution.patch")
		if err := patchGen.SavePatch(patchContent, patchPath); err != nil {
			return fmt.Errorf("error writing patch file: %w", err)
		}
		logger.Infof("Patch saved to: %s", patchPath)

		// Save trajectory file alongside patch
		if trajectory != nil {
			trajPath := filepath.Join(opts.OutputDir, "trajectory.json")
			f, err := os.Create(trajPath)
			if err != nil {
				return fmt.Errorf("error creating trajectory file: %w", err)
			}
			defer func() { _ = f.Close() }()
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			if err := enc.Encode(trajectory); err != nil {
				return fmt.Errorf("error writing trajectory file: %w", err)
			}
			logger.Infof("Trajectory saved to: %s", trajPath)
		}
	} else {
		fmt.Print(patchContent)
	}
	// Write summary to stderr so stdout only contains the patch/result
	if err := writeBinaryResult(os.Stderr, summary); err != nil {
		return err
	}

	// Output result to stderr for logging
	if result != "" {
		_, _ = fmt.Fprintf(os.Stderr, "Agent execution completed\n")
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

// executeBinaryMode runs the agent with the given prompt
func executeBinaryMode(prompt, projectPath, outputDir string) (string, []trajectoryEvent, error) {
	result, trajectory, _, err := executeBinaryModeWithOptions(prompt, projectPath, binaryOptions{OutputDir: outputDir, Sandbox: "auto"})
	return result, trajectory, err
}

// resolveBinarySessionID picks a stable session id for binary-mode hooks/lifecycle.
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

func executeBinaryModeWithOptions(prompt, projectPath string, opts binaryOptions) (string, []trajectoryEvent, *agent.GoalState, error) {
	prompt, goalCondition, goalFromPrompt := prepareBinaryGoal(prompt, opts)
	cfg := config.Get()
	if cfg == nil {
		return "", nil, nil, fmt.Errorf("configuration not initialized")
	}

	// Configure logger based on verbose setting from config
	logger.SetVerbose(cfg.Verbose)

	cfgCopy := *cfg
	// Embedded execution (under symphony or other orchestrators) needs MCP to
	// call back to the orchestrator. Standalone binary mode (SWE-bench style)
	// should not auto-connect to user-configured MCP servers like
	// chrome-devtools that they have in their global ~/.nano.yaml.
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
	applyBinarySandboxMode(&cfgCopy, opts.Sandbox, projectPath)

	// Change to project directory
	oldDir, err := os.Getwd()
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(oldDir) }()

	if err := os.Chdir(projectPath); err != nil {
		return "", nil, nil, fmt.Errorf("failed to change to project directory: %v", err)
	}

	// Create agent instance
	agentInstance, err := agent.New(&cfgCopy, nil)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to create agent: %v", err)
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
			return "", nil, nil, err
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

	err = agentInstance.ProcessStream(ctx, prompt, eventHandler)
	goalState := goalSnapshotPtr(goalCtx)
	if err != nil {
		return "", nil, goalState, fmt.Errorf("failed to process request: %v", err)
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
	return final, traj, goalState, nil
}

func summarizeBinaryResult(traj []trajectoryEvent, start time.Time, result string, runErr error, goalState *agent.GoalState) binaryResultSummary {
	summary := binaryResultSummary{
		Status:     "success",
		DurationMS: time.Since(start).Milliseconds(),
		GoalState:  goalState,
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

	if runErr == nil && goalState != nil {
		if goalState.AchievedAt != nil {
			summary.Status = "success"
			summary.TerminationCause = "natural_completion"
		} else if goalState.Condition != "" && !goalState.Active && goalState.MaxTurns > 0 && goalState.TurnsEvaluated >= goalState.MaxTurns {
			summary.Status = "needs_retry"
			summary.Reason = firstNonEmpty(goalState.LastReason, "goal max turns reached")
			summary.TerminationCause = "goal_max_turns"
			summary.BlockerFingerprint = fmt.Sprintf("goal_max_turns:%d", goalState.MaxTurns)
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

func writeBinaryResult(out *os.File, summary binaryResultSummary) error {
	return writeBinaryResultTo(out, summary)
}

func writeBinaryResultTo(out interface{ Write([]byte) (int, error) }, summary binaryResultSummary) error {
	switch strings.ToLower(firstNonEmpty(os.Getenv("NANO_BINARY_RESULT_FORMAT"), "both")) {
	case "plain", "none", "off":
		return nil
	case "json":
		return json.NewEncoder(out).Encode(summary)
	default:
		data, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "\n%s%s\n", binaryResultSentinel, data)
		return err
	}
}

// runBinaryExitHook executes an optional shell hook after binary execution.
// The hook receives NANO_RESULT_JSON, NANO_RESULT_STATUS, and NANO_RESULT_EXIT_CODE
// so orchestrators can report completion even when they did not parse stdout.
func runBinaryExitHook(command string, summary binaryResultSummary) {
	if strings.TrimSpace(command) == "" {
		return
	}
	payload, _ := json.Marshal(summary)
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		logger.Warnf("binary exit hook skipped: sh not found in PATH: %v", err)
		return
	}
	cmd := exec.Command(shellPath, "-c", command)
	cmd.Env = append(os.Environ(),
		"NANO_RESULT_JSON="+string(payload),
		"NANO_RESULT_STATUS="+summary.Status,
		"NANO_RESULT_EXIT_CODE="+fmt.Sprintf("%d", binaryExitCode(summary.Status)),
	)
	if err := cmd.Run(); err != nil {
		logger.Warnf("binary exit hook failed: %v", err)
	}
}

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
