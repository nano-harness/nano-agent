package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
	"github.com/spf13/cobra"
)

// NewSandboxCommand creates the sandbox management command.
func NewSandboxCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Sandbox debugging utilities",
	}

	printCmd := &cobra.Command{
		Use:   "print",
		Short: "Render the resolved SBPL sandbox profile to stdout",
		Long: `Loads .nano/nano.yaml from --workdir (defaults to CWD), constructs the
sandbox profile exactly as the spawn path would, and prints the SBPL
string to stdout. No sandbox is invoked; nothing is written to disk.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runtime.GOOS != "darwin" {
				return fmt.Errorf("sandbox print is only supported on macOS (darwin)")
			}

			workdir, _ := cmd.Flags().GetString("workdir")
			if workdir == "" {
				var err error
				workdir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("cannot determine working directory: %w", err)
				}
			}
			workdir, _ = filepath.Abs(workdir)

			// Load config from the workdir
			origDir, _ := os.Getwd()
			if err := os.Chdir(workdir); err != nil {
				return fmt.Errorf("cannot chdir to %q: %w", workdir, err)
			}
			defer func() { _ = os.Chdir(origDir) }()

			cfg, err := config.LoadConfig("")
			if err != nil {
				// Use default config if no config file found
				cfg = config.DefaultConfig()
			}
			if cfg.Sandbox == nil {
				cfg.Sandbox = &config.SandboxConfig{}
			}

			sb := sandbox.NewSandboxExecSandbox(cfg.Sandbox, workdir)
			fmt.Fprint(cmd.OutOrStdout(), sb.BuildProfileForInspection())
			return nil
		},
	}
	printCmd.Flags().String("workdir", "", "Working directory (defaults to CWD)")

	cmd.AddCommand(printCmd)
	return cmd
}
