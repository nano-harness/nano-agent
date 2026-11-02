package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/cron"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newCronRunCommand() *cobra.Command {
	var (
		timeout    time.Duration
		saveEvents bool
	)

	cmd := &cobra.Command{
		Use:   "run <command>",
		Short: "手动触发任务执行",
		Long:  "立即执行一个 AI 代理命令（不添加到定时任务列表）。用于测试或一次性执行。",
		Example: `  nano cron run "check build status"
  nano cron run --timeout 5m "generate report"
  nano cron run --save-events "analyze logs"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command := args[0]
			if len(args) > 1 {
				// Join all args if multiple were provided
				command = ""
				for i, arg := range args {
					if i > 0 {
						command += " "
					}
					command += arg
				}
			}

			// Determine config path
			configFile, _ := cmd.Root().PersistentFlags().GetString("config")
			if configFile == "" {
				home, err := os.UserHomeDir()
				if err == nil {
					configFile = filepath.Join(home, ".nano", "config.yaml")
				}
			}

			// Load config
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Create agent instance
			agentInstance, err := agent.New(cfg, nil)
			if err != nil {
				return fmt.Errorf("create agent: %w", err)
			}
			defer func() { _ = agentInstance.Shutdown() }()

			// Generate session ID
			sessionID := fmt.Sprintf("cron-manual-%s", uuid.New().String()[:8])

			// Setup event logging if requested
			var eventsFile *os.File
			var eventsPath string
			if saveEvents {
				home, err := os.UserHomeDir()
				if err == nil {
					eventsDir := filepath.Join(home, ".nano", "cron-events", "manual")
					if err := os.MkdirAll(eventsDir, 0o755); err == nil {
						eventsPath = filepath.Join(eventsDir, fmt.Sprintf("%d.jsonl", time.Now().Unix()))
						eventsFile, _ = os.OpenFile(eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
						if eventsFile != nil {
							defer func() { _ = eventsFile.Close() }()
						}
					}
				}
			}

			// Track metrics
			var toolCallCount int
			var tokenUsage int64
			started := time.Now()

			callback := func(se event.StreamEvent) {
				// Write event to audit file
				if eventsFile != nil {
					data, _ := json.Marshal(se)
					_, _ = eventsFile.Write(append(data, '\n'))
				}

				// Track metrics
				if se.Type == event.EventTypeToolCall {
					toolCallCount++
				}
				if se.Type == event.EventTypeTokenStats && se.TokenStats != nil {
					tokenUsage += int64(se.TokenStats.TotalTokens)
				}

				// Print content to console
				if se.Content != "" {
					fmt.Print(se.Content)
				}
			}

			// Execute with timeout
			if timeout == 0 {
				timeout = 10 * time.Minute
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			fmt.Printf("🚀 执行任务: %s\n", command)
			fmt.Printf("Session ID: %s\n", sessionID)
			if eventsPath != "" {
				fmt.Printf("Events: %s\n", eventsPath)
			}
			fmt.Println("----------------------------------------")

			err = agentInstance.ProcessStream(ctx, command, callback)
			finished := time.Now()

			fmt.Println()
			fmt.Println("----------------------------------------")
			if err != nil {
				fmt.Printf("❌ 任务失败: %v\n", err)
			} else {
				fmt.Printf("✅ 任务成功完成\n")
			}
			fmt.Printf("执行时间: %s\n", finished.Sub(started).Round(time.Millisecond))
			fmt.Printf("工具调用: %d\n", toolCallCount)
			if tokenUsage > 0 {
				fmt.Printf("Token 使用: %d\n", tokenUsage)
			}

			// Log to task_log if we have a TaskLog configured
			taskLogPath, logErr := cron.DefaultTaskLogPath()
			if logErr == nil && taskLogPath != "" {
				taskLog := cron.NewTaskLog(taskLogPath)
				entry := cron.TaskLogEntry{
					TaskID:        "manual",
					Command:       command,
					StartedAt:     started,
					FinishedAt:    finished,
					Success:       err == nil,
					Source:        "cli-manual",
					SessionID:     sessionID,
					EventsPath:    eventsPath,
					DurationMs:    finished.Sub(started).Milliseconds(),
					ToolCallCount: toolCallCount,
					TokenUsage:    tokenUsage,
					SchemaVersion: 2,
				}
				if err != nil {
					entry.Error = err.Error()
				}
				_ = taskLog.Append(entry)
			}

			if err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "任务执行超时时间")
	cmd.Flags().BoolVar(&saveEvents, "save-events", false, "保存详细的事件日志文件")

	return cmd
}
