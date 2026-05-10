package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewCompletionCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for nano.

To load completions in your current shell:

  # bash
  source <(nano completion bash)

  # zsh
  source <(nano completion zsh)

  # fish
  nano completion fish | source

  # powershell
  nano completion powershell | Out-String | Invoke-Expression`,
		Args:              cobra.ExactArgs(1),
		ValidArgs:         []string{"bash", "zsh", "fish", "powershell"},
		ValidArgsFunction: cobra.FixedCompletions([]string{"bash", "zsh", "fish", "powershell"}, cobra.ShellCompDirectiveNoFileComp),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(cmd.OutOrStdout(), true)
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q; valid shells: bash, zsh, fish, powershell", args[0])
			}
		},
	}
	return cmd
}
