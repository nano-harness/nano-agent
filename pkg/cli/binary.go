// Package cli provides the command-line interface for the nano agent
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/patch"
)

// runBinaryMode executes the agent in binary mode for SWE-bench evaluation
func runBinaryMode(args []string, outputDir string) error {
	if len(args) == 0 {
		return fmt.Errorf("prompt required in binary mode")
	}

	prompt := strings.Join(args, " ")

	// Create output directory if specified
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return fmt.Errorf("error creating output directory: %w", err)
		}
	}

	// Get current working directory as project path
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("error getting current directory: %w", err)
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

	result, trajectory, err := executeBinaryMode(prompt, projectPath, outputDir)
	if err != nil {
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
	if outputDir != "" {
		patchPath := filepath.Join(outputDir, "solution.patch")
		if err := patchGen.SavePatch(patchContent, patchPath); err != nil {
			return fmt.Errorf("error writing patch file: %w", err)
		}
		logger.Infof("Patch saved to: %s", patchPath)

		// Save trajectory file alongside patch
		if trajectory != nil {
			trajPath := filepath.Join(outputDir, "trajectory.json")
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
	cfg := config.Get()
	if cfg == nil {
		return "", nil, fmt.Errorf("configuration not initialized")
	}

	// Configure logger based on verbose setting from config
	logger.SetVerbose(cfg.Verbose)

	cfgCopy := *cfg
	cfgCopy.EnableMCP = false
	if cfg.OSS != nil {
		ossCopy := *cfg.OSS
		ossCopy.Enabled = false
		cfgCopy.OSS = &ossCopy
	}

	// Change to project directory
	oldDir, err := os.Getwd()
	if err != nil {
		return "", nil, fmt.Errorf("failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(oldDir) }()

	if err := os.Chdir(projectPath); err != nil {
		return "", nil, fmt.Errorf("failed to change to project directory: %v", err)
	}

	// Create agent instance
	agentInstance, err := agent.New(&cfgCopy, nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create agent: %v", err)
	}
	if strings.TrimSpace(outputDir) != "" {
		agentInstance.GetSessionManager().SetStorage(agent.NewLocalSessionStorage(filepath.Join(outputDir, "sessions")))
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
	if err != nil {
		return "", nil, fmt.Errorf("failed to process request: %v", err)
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
	return final, traj, nil
}
