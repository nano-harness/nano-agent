package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/tools/builtin"
	"github.com/spf13/cobra"
)

// NewCronCommand creates the "nano cron" command group for managing system
// crontab-style recurring tasks.
func NewCronCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "管理定时任务",
		Long:  "nano cron 命令组用于管理通过 nano scheduler 执行的定时任务。",
	}

	cmd.AddCommand(newCronAddCommand())
	cmd.AddCommand(newCronListCommand())
	cmd.AddCommand(newCronRemoveCommand())

	return cmd
}

func newCronAddCommand() *cobra.Command {
	var (
		every   string
		cronExp string
	)

	cmd := &cobra.Command{
		Use:   "add <command>",
		Short: "添加定时任务",
		Long:  "创建一个新的定时任务，在指定时间间隔或 cron 表达式触发时执行命令。",
		Example: `  nano cron add --every 5m "check build status"
  nano cron add --cron "0 9 * * 1-5" "generate daily standup report"`,
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
				// Use the loop parser to convert the interval to a cron expression.
				lc, err := builtin.ParseLoopCommand("/loop " + every + " " + command)
				if err != nil {
					return fmt.Errorf("invalid interval %q: %w", every, err)
				}
				finalCronExpr = lc.CronExpr
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

func newCronListCommand() *cobra.Command {
	return &cobra.Command{
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
			data, _ := json.MarshalIndent(tasks, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}
}

func newCronRemoveCommand() *cobra.Command {
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

// openStateStore opens the default local state store used by cron/watcher CLI commands.
func openStateStore() (*config.StateStore, error) {
	p, err := config.DefaultStateStorePath()
	if err != nil {
		return nil, fmt.Errorf("get state store path: %w", err)
	}
	ss := config.NewStateStore(p)
	if err := ss.Load(); err != nil {
		logger.Warnf("cron: could not load state: %v", err)
	}
	return ss, nil
}
