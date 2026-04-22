package cli

import (
	"fmt"

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

	return cmd
}

func newSessionPruneCommand() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "清理过期会话",
		Long: `清理过期或超出数量限制的会话数据。

默认策略:
- 删除超过 30 天的会话
- 每个项目最多保留 50 个最近的会话

这个命令会扫描所有项目并应用清理策略。`,
		Example: `  nano session prune
  nano session prune --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRun {
				fmt.Println("🔍 执行试运行模式（不会删除任何数据）...")
				// TODO: Implement dry-run mode that reports what would be deleted
				fmt.Println("❗ 试运行模式尚未实现，请稍后再试。")
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

	return cmd
}
