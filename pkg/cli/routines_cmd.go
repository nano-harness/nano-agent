package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/cron"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/tools/builtin"
	cronlib "github.com/robfig/cron/v3"
	"github.com/spf13/cobra"
)

// NewRoutinesCommand creates the "nano routines" command group for managing
// recurring scheduled tasks.
func NewRoutinesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "routines",
		Short: "管理定时任务（routines）",
		Long:  "nano routines 命令族用于管理周期性定时任务（cron / 自然语言间隔触发）。",
	}

	cmd.AddCommand(newRoutinesAddCommand())
	cmd.AddCommand(newRoutinesListCommand())
	cmd.AddCommand(newRoutinesRemoveCommand())
	cmd.AddCommand(newRoutinesLogsCommand())
	cmd.AddCommand(newRoutinesStatsCommand())
	cmd.AddCommand(newRoutinesRunCommand())

	return cmd
}

func newRoutinesAddCommand() *cobra.Command {
	var (
		every   string
		cronExp string
	)

	cmd := &cobra.Command{
		Use:   "add <command>",
		Short: "添加定时任务",
		Long:  "创建一个新的定时任务，在指定时间间隔或 cron 表达式触发时执行命令。",
		Example: `  nano routines add --every 5m "check build status"
  nano routines add --cron "0 9 * * 1-5" "generate daily standup report"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command := strings.Join(args, " ")

			if every == "" && cronExp == "" {
				return fmt.Errorf("specify --every or --cron")
			}
			if every != "" && cronExp != "" {
				return fmt.Errorf("specify either --every or --cron, not both")
			}

			var finalCronExpr string
			if cronExp != "" {
				finalCronExpr = cronExp
			} else {
				// Parse natural language schedule using ParseNaturalSchedule
				parsed, err := builtin.ParseNaturalSchedule(every)
				if err != nil {
					return fmt.Errorf("invalid interval %q: %w", every, err)
				}
				finalCronExpr = parsed
			}

			// Try to persist via state store.
			ss, err := openStateStore()
			if err != nil {
				return err
			}
			ss.AddTask(config.PersistedTask{
				CronExpr: finalCronExpr,
				Command:  command,
				Source:   "cli",
			})
			if err := ss.Save(); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
			fmt.Printf("✅ 任务已创建\n  cron: %s\n  command: %s\n", finalCronExpr, command)
			return nil
		},
	}

	cmd.Flags().StringVar(&every, "every", "", "轮询间隔，例如 5m, 1h")
	cmd.Flags().StringVar(&cronExp, "cron", "", "cron 表达式，例如 '0 9 * * 1-5'")

	return cmd
}

func newRoutinesListCommand() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出所有定时任务",
		RunE: func(cmd *cobra.Command, args []string) error {
			ss, err := openStateStore()
			if err != nil {
				return err
			}
			tasks := ss.GetTasks()
			if len(tasks) == 0 {
				fmt.Println("暂无定时任务。")
				return nil
			}

			if jsonOutput {
				data, _ := json.MarshalIndent(tasks, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			return displayTasksTable(tasks)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "以 JSON 格式输出")

	return cmd
}

func newRoutinesRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <task-id>",
		Short: "删除定时任务",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			ss, err := openStateStore()
			if err != nil {
				return err
			}
			ss.RemoveTask(id)
			if err := ss.Save(); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
			fmt.Printf("✅ 任务 %s 已删除\n", id)
			return nil
		},
	}
}

func newRoutinesLogsCommand() *cobra.Command {
	return newCronLogsCommand() // Reuse existing implementation
}

func newRoutinesStatsCommand() *cobra.Command {
	return newCronStatsCommand() // Reuse existing implementation
}

func newRoutinesRunCommand() *cobra.Command {
	return newCronRunCommand() // Reuse existing implementation
}

// openStateStore opens the default local state store used by routines CLI commands.
func openStateStore() (*config.StateStore, error) {
	p, err := config.DefaultStateStorePath()
	if err != nil {
		return nil, fmt.Errorf("get state store path: %w", err)
	}
	ss := config.NewStateStore(p)
	if err := ss.Load(); err != nil {
		logger.Warnf("routines: could not load state: %v", err)
	}
	return ss, nil
}

// displayTasksTable displays tasks in a formatted table with next run time and last execution status.
func displayTasksTable(tasks []config.PersistedTask) error {
	// Get task log path for last execution status
	taskLogPath, err := cron.DefaultTaskLogPath()
	if err != nil {
		logger.Warnf("could not determine task log path: %v", err)
	}

	var taskLog *cron.TaskLog
	var logEntries []cron.TaskLogEntry
	if taskLogPath != "" {
		taskLog = cron.NewTaskLog(taskLogPath)
		logEntries, _ = taskLog.Query()
	}

	// Build a map of task ID to last execution status
	lastExec := make(map[string]*cron.TaskLogEntry)
	for i := range logEntries {
		entry := &logEntries[i]
		if existing, ok := lastExec[entry.TaskID]; !ok || entry.StartedAt.After(existing.StartedAt) {
			lastExec[entry.TaskID] = entry
		}
	}

	// Print header
	fmt.Printf("%-36s %-20s %-15s %-25s %-10s\n", "TASK ID", "CRON EXPRESSION", "SOURCE", "NEXT RUN", "LAST STATUS")
	fmt.Println(strings.Repeat("-", 120))

	// Create cron parser for calculating next run times
	parser := cronlib.NewParser(cronlib.Second | cronlib.Minute | cronlib.Hour | cronlib.Dom | cronlib.Month | cronlib.Dow | cronlib.Descriptor)

	for _, task := range tasks {
		// Calculate next run time
		nextRun := "N/A"
		if schedule, err := parser.Parse(task.CronExpr); err == nil {
			next := schedule.Next(time.Now())
			nextRun = formatNextRun(next)
		}

		// Get last execution status
		lastStatus := "Never run"
		if entry, ok := lastExec[task.ID]; ok {
			if entry.Success {
				lastStatus = "✅ Success"
			} else {
				lastStatus = "❌ Failed"
			}
		}

		// Truncate command for display
		command := task.Command
		if len(command) > 40 {
			command = command[:37] + "..."
		}

		source := task.Source
		if source == "" {
			source = "unknown"
		}

		fmt.Printf("%-36s %-20s %-15s %-25s %-10s\n",
			task.ID,
			task.CronExpr,
			source,
			nextRun,
			lastStatus,
		)
		fmt.Printf("  └─ %s\n", command)
	}

	return nil
}

// formatNextRun formats the next run time in a human-readable format.
func formatNextRun(t time.Time) string {
	now := time.Now()
	duration := t.Sub(now)

	if duration < time.Minute {
		return "< 1 minute"
	} else if duration < time.Hour {
		mins := int(duration.Minutes())
		return fmt.Sprintf("in %d min", mins)
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())
		return fmt.Sprintf("in %d hours", hours)
	} else if duration < 7*24*time.Hour {
		days := int(duration.Hours() / 24)
		return fmt.Sprintf("in %d days", days)
	}

	return t.Format("2006-01-02 15:04")
}
