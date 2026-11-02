package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/cron"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/spf13/cobra"
)

func newCronLogsCommand() *cobra.Command {
	var (
		taskID     string
		failed     bool
		sinceStr   string
		jsonOutput bool
		limit      int
		follow     bool
	)

	cmd := &cobra.Command{
		Use:   "logs [task-id]",
		Short: "查看定时任务执行日志",
		Long:  "显示定时任务的执行历史记录，支持过滤和实时跟踪。",
		Example: `  nano cron logs                    # 显示最近 50 条日志
  nano cron logs abc-123            # 显示指定任务的日志
  nano cron logs --failed           # 仅显示失败的执行
  nano cron logs --since 1h         # 显示最近 1 小时的日志
  nano cron logs -f                 # 实时跟踪新日志
  nano cron logs --json             # JSON 格式输出
  nano cron logs -n 100             # 显示最近 100 条`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				taskID = args[0]
			}

			// Get task log path
			logPath, err := cron.DefaultTaskLogPath()
			if err != nil {
				return fmt.Errorf("get task log path: %w", err)
			}

			tl := cron.NewTaskLog(logPath)

			// Parse since duration
			var since time.Time
			if sinceStr != "" {
				duration, err := parseDuration(sinceStr)
				if err != nil {
					return fmt.Errorf("invalid --since value %q: %w", sinceStr, err)
				}
				since = time.Now().Add(-duration)
			}

			if follow {
				return followLogs(tl, taskID, failed, since)
			}

			return displayLogs(tl, taskID, failed, since, limit, jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&failed, "failed", false, "仅显示失败的执行")
	cmd.Flags().StringVar(&sinceStr, "since", "", "时间过滤（例如: 1h, 24h, 7d）")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON 格式输出")
	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "显示条数限制")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "实时跟踪新日志")

	return cmd
}

func displayLogs(tl *cron.TaskLog, taskID string, failedOnly bool, since time.Time, limit int, jsonOutput bool) error {
	entries, err := tl.Query()
	if err != nil {
		return fmt.Errorf("query logs: %w", err)
	}

	// Filter entries
	var filtered []cron.TaskLogEntry
	for _, e := range entries {
		// Filter by task ID
		if taskID != "" && e.TaskID != taskID {
			continue
		}
		// Filter by failed status
		if failedOnly && e.Success {
			continue
		}
		// Filter by time
		if !since.IsZero() && e.StartedAt.Before(since) {
			continue
		}
		filtered = append(filtered, e)
	}

	// Apply limit (from the end)
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}

	if len(filtered) == 0 {
		fmt.Println("暂无日志记录。")
		return nil
	}

	if jsonOutput {
		return displayLogsJSON(filtered)
	}

	return displayLogsTable(filtered)
}

func displayLogsJSON(entries []cron.TaskLogEntry) error {
	for _, e := range entries {
		data, err := e.MarshalJSON()
		if err != nil {
			logger.Warnf("marshal entry: %v", err)
			continue
		}
		fmt.Println(string(data))
	}
	return nil
}

func displayLogsTable(entries []cron.TaskLogEntry) error {
	// Print header
	fmt.Printf("%-36s %-8s %-19s %-8s %-s\n",
		"TASK_ID", "STATUS", "STARTED_AT", "DURATION", "COMMAND")
	fmt.Println(strings.Repeat("-", 120))

	// Print entries
	for _, e := range entries {
		status := "✓ OK"
		if !e.Success {
			status = "✗ FAIL"
			if e.FailureStage != "" {
				status = fmt.Sprintf("✗ %s", e.FailureStage)
			}
		}

		duration := e.FinishedAt.Sub(e.StartedAt)
		durationStr := formatDuration(duration)

		// Truncate command if too long
		command := e.Command
		if len(command) > 50 {
			command = command[:47] + "..."
		}

		fmt.Printf("%-36s %-8s %-19s %-8s %-s\n",
			e.TaskID[:36],
			status,
			e.StartedAt.Format("2006-01-02 15:04:05"),
			durationStr,
			command)

		// Show error if failed
		if !e.Success && e.Error != "" {
			errorMsg := e.Error
			if len(errorMsg) > 100 {
				errorMsg = errorMsg[:97] + "..."
			}
			fmt.Printf("  Error: %s\n", errorMsg)
		}
	}

	fmt.Printf("\nTotal: %d entries\n", len(entries))
	return nil
}

func followLogs(tl *cron.TaskLog, taskID string, failedOnly bool, since time.Time) error {
	fmt.Println("Following logs... (Ctrl+C to stop)")

	lastSeen := time.Now()
	if !since.IsZero() {
		lastSeen = since
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		entries, err := tl.Query()
		if err != nil {
			logger.Warnf("query logs: %v", err)
			continue
		}

		// Find new entries
		var newEntries []cron.TaskLogEntry
		for _, e := range entries {
			if e.StartedAt.After(lastSeen) {
				// Apply filters
				if taskID != "" && e.TaskID != taskID {
					continue
				}
				if failedOnly && e.Success {
					continue
				}
				newEntries = append(newEntries, e)
			}
		}

		// Display new entries
		for _, e := range newEntries {
			status := "✓"
			if !e.Success {
				status = "✗"
			}
			fmt.Printf("[%s] %s %s: %s\n",
				e.StartedAt.Format("15:04:05"),
				status,
				e.TaskID[:8],
				e.Command)
			if !e.Success && e.Error != "" {
				fmt.Printf("  Error: %s\n", e.Error)
			}
			lastSeen = e.StartedAt
		}
	}

	return nil
}

func parseDuration(s string) (time.Duration, error) {
	// Try standard duration format first
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Try shorthand formats: 1h, 24h, 7d
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration format")
	}

	valueStr := s[:len(s)-1]
	unit := s[len(s)-1:]

	var value int
	_, err := fmt.Sscanf(valueStr, "%d", &value)
	if err != nil {
		return 0, err
	}

	switch unit {
	case "s":
		return time.Duration(value) * time.Second, nil
	case "m":
		return time.Duration(value) * time.Minute, nil
	case "h":
		return time.Duration(value) * time.Hour, nil
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown unit: %s", unit)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}
