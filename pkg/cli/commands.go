package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nano-harness/nano-agent/pkg/skill"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// NewCommandsCommand creates the commands management command
func NewCommandsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commands",
		Short: "Manage slash commands",
		Long:  "List and inspect project/user slash commands",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available commands",
		Run: func(_ *cobra.Command, _ []string) {
			cwd, _ := os.Getwd()
			m := skill.NewCommandManager(cwd)
			list := m.List()
			if len(list) == 0 {
				color.Yellow("No commands found. Create %s or %s.", filepath.Join(cwd, ".nano", "commands"), filepath.Join(os.Getenv("HOME"), ".nano", "commands"))
				return
			}
			for _, d := range list {
				fmt.Printf("/%s\t[%s]\t%s\n", d.Name, d.Source, d.Description)
			}
		},
	})

	return cmd
}
