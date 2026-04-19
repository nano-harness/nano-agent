package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/watcher"
	"github.com/spf13/cobra"
)

// NewWatcherCommand creates the "nano watcher" command group for managing
// event-monitoring rules via the daemon API or local state store.
func NewWatcherCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watcher",
		Short: "管理事件监听规则",
		Long:  "nano watcher 命令组用于管理事件监听规则（轮询 Aone MR、shell 脚本等）。",
	}

	cmd.AddCommand(newWatcherAddCommand())
	cmd.AddCommand(newWatcherListCommand())
	cmd.AddCommand(newWatcherRemoveCommand())
	cmd.AddCommand(newWatcherStatusCommand())

	return cmd
}

func newWatcherAddCommand() *cobra.Command {
	var (
		source   string
		event    string
		filter   string
		command  string
		interval string
		timeout  string
		shellCmd string
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "添加事件监听规则",
		Example: `  nano watcher add --source aone --event new_mr --filter "repo:aone/a1 state:opened" --command "评审 MR {{.MR_URL}}" --interval 5m
  nano watcher add --source shell --shell-command "my-script.sh" --command "处理结果: {{.OUTPUT}}" --interval 10m`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if source == "" {
				return fmt.Errorf("--source is required (aone or shell)")
			}
			if command == "" {
				return fmt.Errorf("--command is required")
			}

			cfg := config.Get()
			if cfg != nil && cfg.Watcher != nil && cfg.Watcher.Enabled {
				// Watcher is active; add rule via state store.
				return addWatcherRuleLocal(source, event, filter, command, shellCmd, interval, timeout)
			}

			// Config-only mode: add to the .nano.yaml watcher.rules section.
			fmt.Println("⚠️  Watcher 未启用。请在 .nano.yaml 中设置 watcher.enabled: true")
			fmt.Printf("或者添加以下配置到 watcher.rules:\n")
			fmt.Printf("  - source: %s\n    event: %s\n    filter: %s\n    command: %s\n    interval: %s\n",
				source, event, filter, command, interval)
			return nil
		},
	}

	cmd.Flags().StringVar(&source, "source", "", "事件源: aone 或 shell")
	cmd.Flags().StringVar(&event, "event", "custom", "事件类型: new_mr, ci_failure, push, custom")
	cmd.Flags().StringVar(&filter, "filter", "", "过滤条件")
	cmd.Flags().StringVar(&command, "command", "", "触发命令模板")
	cmd.Flags().StringVar(&interval, "interval", "5m", "轮询间隔")
	cmd.Flags().StringVar(&timeout, "timeout", "30m", "任务超时")
	cmd.Flags().StringVar(&shellCmd, "shell-command", "", "shell 源的命令")

	return cmd
}

func newWatcherListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出所有事件监听规则",
		RunE: func(cmd *cobra.Command, args []string) error {
			ss, err := openStateStore()
			if err != nil {
				return err
			}
			rules := ss.GetWatcherRules()
			if len(rules) == 0 {
				fmt.Println("暂无事件监听规则。")
				return nil
			}
			data, _ := json.MarshalIndent(rules, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}
}

func newWatcherRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <rule-id>",
		Short: "删除事件监听规则",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			ss, err := openStateStore()
			if err != nil {
				return err
			}
			ss.RemoveWatcherRule(id)
			if err := ss.Save(); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
			fmt.Printf("✅ 规则 %s 已删除\n", id)
			return nil
		},
	}
}

func newWatcherStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "查看事件监听规则状态",
		RunE: func(cmd *cobra.Command, args []string) error {
			ss, err := openStateStore()
			if err != nil {
				return err
			}
			rules := ss.GetWatcherRules()
			if len(rules) == 0 {
				fmt.Println("暂无活跃的事件监听规则。")
				return nil
			}
			fmt.Printf("活跃规则数: %d\n\n", len(rules))
			for _, r := range rules {
				fmt.Printf("ID:       %s\n", shortID(r.ID))
				fmt.Printf("源:       %s\n", r.Source)
				fmt.Printf("事件:     %s\n", r.Event)
				fmt.Printf("间隔:     %s\n", r.Interval)
				if r.Filter != "" {
					fmt.Printf("过滤:     %s\n", r.Filter)
				}
				fmt.Printf("命令:     %s\n", r.Command)
				fmt.Printf("创建时间: %s\n\n", r.CreatedAt)
			}
			return nil
		},
	}
}

// addWatcherRuleLocal persists a new watcher rule to the local state store.
func addWatcherRuleLocal(source, event, filter, command, shellCmd, interval, timeout string) error {
	ss, err := openStateStore()
	if err != nil {
		return err
	}

	w := watcher.New(nil, ss)
	rule := watcher.Rule{
		Source:       source,
		Event:        event,
		Filter:       filter,
		Command:      command,
		ShellCommand: shellCmd,
	}
	if iv := strings.TrimSpace(interval); iv != "" {
		if d, pErr := parseDurationArg(iv); pErr == nil {
			rule.Interval = d
		}
	}
	if tv := strings.TrimSpace(timeout); tv != "" {
		if d, pErr := parseDurationArg(tv); pErr == nil {
			rule.Timeout = d
		}
	}

	rule = w.AddRule(rule)
	fmt.Printf("✅ 规则已创建\n  ID: %s\n  source: %s\n  event: %s\n  interval: %s\n",
		rule.ID, rule.Source, rule.Event, rule.Interval)
	return nil
}

// parseDurationArg parses a duration string like "5m", "1h", "30s".
func parseDurationArg(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// shortID returns a short prefix of an ID safe for display.
// If the ID is shorter than 8 characters, the full ID is returned.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
