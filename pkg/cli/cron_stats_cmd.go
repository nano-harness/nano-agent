package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/cron"
	"github.com/spf13/cobra"
)

func newCronStatsCommand() *cobra.Command {
	var (
		taskID     string
		sinceStr   string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "查看定时任务执行统计",
		Long:  "显示定时任务的执行统计信息，包括成功率、失败率、平均执行时间、工具调用次数、Token 使用量等。",
		Example: `  nano cron stats
  nano cron stats --task-id abc123
  nano cron stats --since 7d
  nano cron stats --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			taskLogPath, err := cron.DefaultTaskLogPath()
			if err != nil {
				return fmt.Errorf("get task log path: %w", err)
			}

			taskLog := cron.NewTaskLog(taskLogPath)
			entries, err := taskLog.Query()
			if err != nil {
				return fmt.Errorf("query task log: %w", err)
			}

			// Filter by time range
			if sinceStr != "" {
				duration, err := parseDuration(sinceStr)
				if err != nil {
					return fmt.Errorf("invalid --since value: %w", err)
				}
				cutoff := time.Now().Add(-duration)
				var filtered []cron.TaskLogEntry
				for _, e := range entries {
					if e.StartedAt.After(cutoff) {
						filtered = append(filtered, e)
					}
				}
				entries = filtered
			}

			// Filter by task ID
			if taskID != "" {
				var filtered []cron.TaskLogEntry
				for _, e := range entries {
					if e.TaskID == taskID {
						filtered = append(filtered, e)
					}
				}
				entries = filtered
			}

			if len(entries) == 0 {
				fmt.Println("无执行记录。")
				return nil
			}

			stats := computeStats(entries)

			if jsonOutput {
				data, _ := json.MarshalIndent(stats, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			displayStatsTable(stats)
			return nil
		},
	}

	cmd.Flags().StringVar(&taskID, "task-id", "", "仅显示指定任务的统计")
	cmd.Flags().StringVar(&sinceStr, "since", "", "仅统计指定时间范围内的记录，例如 1h, 24h, 7d")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "以 JSON 格式输出")

	return cmd
}

// TaskStats holds aggregated statistics for task executions.
type TaskStats struct {
	TotalRuns        int                 `json:"total_runs"`
	SuccessfulRuns   int                 `json:"successful_runs"`
	FailedRuns       int                 `json:"failed_runs"`
	SuccessRate      float64             `json:"success_rate"`
	AvgDurationMs    int64               `json:"avg_duration_ms"`
	TotalToolCalls   int                 `json:"total_tool_calls"`
	TotalTokenUsage  int64               `json:"total_token_usage"`
	FailuresByStage  map[string]int      `json:"failures_by_stage"`
	FailuresByTool   map[string]int      `json:"failures_by_tool"`
	ExecutionsByTask map[string]TaskStat `json:"executions_by_task"`
}

// TaskStat holds per-task statistics.
type TaskStat struct {
	TaskID         string  `json:"task_id"`
	Command        string  `json:"command"`
	TotalRuns      int     `json:"total_runs"`
	SuccessfulRuns int     `json:"successful_runs"`
	FailedRuns     int     `json:"failed_runs"`
	SuccessRate    float64 `json:"success_rate"`
	AvgDurationMs  int64   `json:"avg_duration_ms"`
	TotalToolCalls int     `json:"total_tool_calls"`
	TotalTokens    int64   `json:"total_tokens"`
}

// computeStats aggregates statistics from log entries.
func computeStats(entries []cron.TaskLogEntry) TaskStats {
	stats := TaskStats{
		FailuresByStage:  make(map[string]int),
		FailuresByTool:   make(map[string]int),
		ExecutionsByTask: make(map[string]TaskStat),
	}

	taskData := make(map[string]*taskAggregator)

	for _, e := range entries {
		stats.TotalRuns++
		if e.Success {
			stats.SuccessfulRuns++
		} else {
			stats.FailedRuns++
			if e.FailureStage != "" {
				stats.FailuresByStage[e.FailureStage]++
			}
			if e.FailedToolName != "" {
				stats.FailuresByTool[e.FailedToolName]++
			}
		}

		stats.TotalToolCalls += e.ToolCallCount
		stats.TotalTokenUsage += e.TokenUsage

		// Per-task aggregation
		if _, ok := taskData[e.TaskID]; !ok {
			taskData[e.TaskID] = &taskAggregator{
				TaskID:  e.TaskID,
				Command: e.Command,
			}
		}
		agg := taskData[e.TaskID]
		agg.TotalRuns++
		if e.Success {
			agg.SuccessfulRuns++
		} else {
			agg.FailedRuns++
		}
		agg.TotalDurationMs += e.DurationMs
		agg.TotalToolCalls += e.ToolCallCount
		agg.TotalTokens += e.TokenUsage
	}

	if stats.TotalRuns > 0 {
		stats.SuccessRate = float64(stats.SuccessfulRuns) / float64(stats.TotalRuns) * 100

		var totalDuration int64
		for _, e := range entries {
			totalDuration += e.DurationMs
		}
		stats.AvgDurationMs = totalDuration / int64(stats.TotalRuns)
	}

	// Convert per-task aggregators to TaskStat
	for id, agg := range taskData {
		ts := TaskStat{
			TaskID:         agg.TaskID,
			Command:        agg.Command,
			TotalRuns:      agg.TotalRuns,
			SuccessfulRuns: agg.SuccessfulRuns,
			FailedRuns:     agg.FailedRuns,
			TotalToolCalls: agg.TotalToolCalls,
			TotalTokens:    agg.TotalTokens,
		}
		if ts.TotalRuns > 0 {
			ts.SuccessRate = float64(ts.SuccessfulRuns) / float64(ts.TotalRuns) * 100
			ts.AvgDurationMs = agg.TotalDurationMs / int64(ts.TotalRuns)
		}
		stats.ExecutionsByTask[id] = ts
	}

	return stats
}

type taskAggregator struct {
	TaskID          string
	Command         string
	TotalRuns       int
	SuccessfulRuns  int
	FailedRuns      int
	TotalDurationMs int64
	TotalToolCalls  int
	TotalTokens     int64
}

// displayStatsTable displays statistics in a formatted table.
func displayStatsTable(stats TaskStats) {
	fmt.Println("=== 总体统计 ===")
	fmt.Printf("总运行次数:   %d\n", stats.TotalRuns)
	fmt.Printf("成功运行:     %d\n", stats.SuccessfulRuns)
	fmt.Printf("失败运行:     %d\n", stats.FailedRuns)
	fmt.Printf("成功率:       %.1f%%\n", stats.SuccessRate)
	fmt.Printf("平均执行时间: %s\n", formatDuration(time.Duration(stats.AvgDurationMs)*time.Millisecond))
	fmt.Printf("工具调用总数: %d\n", stats.TotalToolCalls)
	fmt.Printf("Token 使用量: %d\n", stats.TotalTokenUsage)
	fmt.Println()

	// Failure breakdown by stage
	if len(stats.FailuresByStage) > 0 {
		fmt.Println("=== 失败阶段分布 ===")
		type stageCount struct {
			stage string
			count int
		}
		var stages []stageCount
		for s, c := range stats.FailuresByStage {
			stages = append(stages, stageCount{s, c})
		}
		sort.Slice(stages, func(i, j int) bool {
			return stages[i].count > stages[j].count
		})
		for _, sc := range stages {
			pct := float64(sc.count) / float64(stats.FailedRuns) * 100
			fmt.Printf("  %-20s: %3d (%.1f%%)\n", sc.stage, sc.count, pct)
		}
		fmt.Println()
	}

	// Failure breakdown by tool
	if len(stats.FailuresByTool) > 0 {
		fmt.Println("=== 失败工具分布 ===")
		type toolCount struct {
			tool  string
			count int
		}
		var tools []toolCount
		for t, c := range stats.FailuresByTool {
			tools = append(tools, toolCount{t, c})
		}
		sort.Slice(tools, func(i, j int) bool {
			return tools[i].count > tools[j].count
		})
		for _, tc := range tools {
			pct := float64(tc.count) / float64(stats.FailedRuns) * 100
			fmt.Printf("  %-20s: %3d (%.1f%%)\n", tc.tool, tc.count, pct)
		}
		fmt.Println()
	}

	// Per-task breakdown
	if len(stats.ExecutionsByTask) > 1 {
		fmt.Println("=== 各任务统计 ===")
		fmt.Printf("%-36s %-10s %-10s %-12s %-15s\n", "TASK ID", "RUNS", "SUCCESS", "SUCCESS RATE", "AVG DURATION")
		fmt.Println(strings.Repeat("-", 100))

		// Sort by total runs descending
		var taskStats []TaskStat
		for _, ts := range stats.ExecutionsByTask {
			taskStats = append(taskStats, ts)
		}
		sort.Slice(taskStats, func(i, j int) bool {
			return taskStats[i].TotalRuns > taskStats[j].TotalRuns
		})

		for _, ts := range taskStats {
			avgDur := formatDuration(time.Duration(ts.AvgDurationMs) * time.Millisecond)
			fmt.Printf("%-36s %-10d %-10d %-12s %-15s\n",
				ts.TaskID,
				ts.TotalRuns,
				ts.SuccessfulRuns,
				fmt.Sprintf("%.1f%%", ts.SuccessRate),
				avgDur,
			)

			// Truncate command for display
			command := ts.Command
			if len(command) > 80 {
				command = command[:77] + "..."
			}
			fmt.Printf("  └─ %s\n", command)
		}
	}
}
