package cli

import (
	"fmt"
	"os"
	"sort"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/spf13/cobra"
)

// NewSessionCommand creates the "nano session" command group for managing sessions.
func NewSessionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "管理会话",
		Long:  "nano session 命令组用于管理存储的会话数据。",
	}

	cmd.AddCommand(newSessionPruneCommand())
	cmd.AddCommand(newSessionStatsCommand())

	return cmd
}

func newSessionPruneCommand() *cobra.Command {
	var dryRun bool
	var reason string

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "清理过期会话",
		Long: `清理过期或超出数量限制的会话数据。

默认策略:
- 删除超过 30 天的会话
- 每个项目最多保留 50 个最近的会话

这个命令会扫描所有项目并应用清理策略。`,
		Example: `  nano session prune
  nano session prune --dry-run
  nano session prune --reason idle_ttl`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRun {
				fmt.Println("🔍 执行试运行模式（不会删除任何数据）...")
				cands, err := agent.CleanupAllProjectsPreview()
				if err != nil {
					return fmt.Errorf("预览清理候选失败: %w", err)
				}
				if reason != "" {
					filtered := cands[:0]
					for _, c := range cands {
						if c.Reason == reason {
							filtered = append(filtered, c)
						}
					}
					cands = filtered
				}
				if len(cands) == 0 {
					fmt.Println("✅ 暂无符合条件的会话需要清理。")
					return nil
				}
				fmt.Printf("将清理 %d 个会话：\n", len(cands))
				for _, c := range cands {
					fmt.Printf("  - %s  reason=%-16s  updated=%s  project=%s\n",
						c.SessionID, c.Reason, c.UpdatedAt, c.ProjectDir)
				}
				fmt.Println("\n提示：移除 --dry-run 以执行清理。")
				return nil
			}

			if reason != "" {
				fmt.Printf("🔎 使用清理原因过滤: %s\n", reason)
				fmt.Println("🧹 开始按原因清理会话...")
				if err := agent.CleanupAllProjectsByReason(reason); err != nil {
					return fmt.Errorf("按原因清理会话失败: %w", err)
				}
				fmt.Println("✅ 会话清理完成")
				return nil
			}

			fmt.Println("🧹 开始清理过期会话...")
			if err := agent.CleanupAllProjects(); err != nil {
				return fmt.Errorf("清理会话失败: %w", err)
			}
			fmt.Println("✅ 会话清理完成")
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "仅显示将要删除的会话，不实际删除")
	cmd.Flags().StringVar(&reason, "reason", "", "按清理原因过滤（idle_ttl、max_per_project）")

	return cmd
}

func newSessionStatsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "查看会话状态统计",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			storage, err := agent.NewProjectSessionStorage(wd)
			if err != nil {
				return err
			}
			infos, err := storage.ListSessionInfos()
			if err != nil {
				return err
			}
			counts := map[string]int{
				string(agent.SessionStateActive):        0,
				string(agent.SessionStateIdle):          0,
				string(agent.SessionStateAwaitingInput): 0,
				string(agent.SessionStateSuspended):     0,
				string(agent.SessionStateTerminated):    0,
			}
			for _, info := range infos {
				state := info.State
				if state == "" {
					state = string(agent.SessionStateIdle)
				}
				counts[state]++
			}
			keys := make([]string, 0, len(counts))
			for key := range counts {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			fmt.Printf("total_loaded: %d\n", len(infos))
			for _, key := range keys {
				fmt.Printf("%s: %d\n", key, counts[key])
			}
			return nil
		},
	}
}
