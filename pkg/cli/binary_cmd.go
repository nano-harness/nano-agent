// Package cli – binary subcommand.
//
// The `nano binary` command tree exposes one-shot, non-interactive operations
// that are useful in scripts, CI, and benchmark drivers (such as SWE-bench).
// Metadata subcommands support `--json` so callers can parse output without
// scraping human-readable text.
//
// The legacy global flag `--binary-mode` remains supported for backward
// compatibility but emits a deprecation warning that points users at
// `nano binary swebench`.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/skill"
	"github.com/nano-harness/nano-agent/pkg/slash"
	"github.com/nano-harness/nano-agent/pkg/tools"
	"github.com/spf13/cobra"
)

// NewBinaryCommand creates the `nano binary` command group.
func NewBinaryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "binary",
		Short: "Non-interactive (binary) operations for scripts and CI",
		Long: `One-shot operations that produce machine-readable output.

These commands are designed for use by external drivers (CI scripts,
SWE-bench harnesses, etc.) that need a deterministic, scriptable surface.
Metadata/list subcommands accept --json for stable, parseable output.`,
	}

	cmd.AddCommand(newBinaryQueryCommand())
	cmd.AddCommand(newBinaryListModelsCommand())
	cmd.AddCommand(newBinaryListToolsCommand())
	cmd.AddCommand(newBinaryListSlashCommand())
	cmd.AddCommand(newBinaryListSkillsCommand())
	cmd.AddCommand(newBinarySWEBenchCommand())

	return cmd
}

func newBinaryQueryCommand() *cobra.Command {
	var (
		jsonOut   bool
		outputDir string
	)
	c := &cobra.Command{
		Use:   "query [prompt...]",
		Short: "Run a one-shot agent query and exit",
		Long: `Execute a single prompt against the agent without entering the TUI
and exit. Equivalent to the legacy --binary-mode flag but discoverable as a
subcommand and forward-compatible with the planned binary surface.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.Join(args, " ")
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			if outputDir != "" {
				if err := os.MkdirAll(outputDir, 0o755); err != nil {
					return err
				}
			}
			result, trajectory, err := executeBinaryMode(prompt, wd, outputDir)
			if err != nil {
				return err
			}
			var trajectoryPath string
			if outputDir != "" {
				trajectoryPath = filepath.Join(outputDir, "trajectory.json")
				if err := saveTrajectory(trajectory, trajectoryPath); err != nil {
					return err
				}
			}
			if jsonOut {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"status":          "ok",
					"prompt":          prompt,
					"workdir":         wd,
					"output_dir":      outputDir,
					"trajectory_path": trajectoryPath,
					"result":          result,
				})
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), result)
			return err
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "emit a single JSON result document")
	c.Flags().StringVar(&outputDir, "output-dir", "", "directory to write trajectory + patch artifacts")
	return c
}

func newBinaryListModelsCommand() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "list-models",
		Short: "List provider/model presets",
		Run: func(_ *cobra.Command, _ []string) {
			presets := llm.KnownProviderPresets()
			if jsonOut {
				_ = json.NewEncoder(os.Stdout).Encode(presets)
				return
			}
			for _, p := range presets {
				fmt.Printf("- %s (%s)\n", p.DisplayName, p.BaseURL)
			}
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	return c
}

func newBinaryListToolsCommand() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "list-tools",
		Short: "List built-in tools available to the agent",
		Run: func(_ *cobra.Command, _ []string) {
			cwd, _ := os.Getwd()
			tb := tools.NewToolbox(cwd, &tools.ToolboxConfig{}, nil)
			defer tb.Close()
			descs := tb.Descriptors()
			sort.Slice(descs, func(i, j int) bool { return descs[i].Name < descs[j].Name })
			if jsonOut {
				_ = json.NewEncoder(os.Stdout).Encode(descs)
				return
			}
			for _, d := range descs {
				fmt.Printf("- %-32s  category=%-10s  mutates_fs=%v  needs_approval=%v\n",
					d.Name, d.Category, d.MutatesFS, d.RequiresConfirmation)
			}
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	return c
}

func newBinaryListSlashCommand() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "list-slash",
		Short: "List built-in slash commands",
		Run: func(_ *cobra.Command, _ []string) {
			cwd, _ := os.Getwd()
			cmds := slash.NewRegistry(cwd).All()
			if jsonOut {
				_ = json.NewEncoder(os.Stdout).Encode(cmds)
				return
			}
			for _, c := range cmds {
				fmt.Printf("- /%-22s [%s]  %s\n", c.Name, c.Category, c.Description)
			}
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	return c
}

func newBinaryListSkillsCommand() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "list-skills",
		Short: "List installed personal and project skills",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()
			home, _ := os.UserHomeDir()
			personalDir := filepath.Join(home, ".nano", "skills")
			projectDir := filepath.Join(cwd, ".nano", "skills")
			mgr := skill.NewManager(cwd, personalDir, projectDir, 0, 0, 0, false)
			if err := mgr.Discover(); err != nil {
				return fmt.Errorf("discover skills: %w", err)
			}
			meta := mgr.ListMetadata()
			if jsonOut {
				_ = json.NewEncoder(os.Stdout).Encode(meta)
				return nil
			}
			if len(meta) == 0 {
				fmt.Println("(no skills installed)")
				return nil
			}
			for _, m := range meta {
				fmt.Printf("- %-24s [%s]  %s\n", m.Name, m.Scope, m.Description)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	return c
}

func newBinarySWEBenchCommand() *cobra.Command {
	var outputDir string
	c := &cobra.Command{
		Use:   "swebench [prompt...]",
		Short: "SWE-bench-compatible one-shot evaluation",
		Long: `Run the agent in SWE-bench evaluation mode. Identical to invoking
the legacy --binary-mode flag, but discoverable as a subcommand. Writes the
generated patch and trajectory to --output-dir if provided.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runBinaryMode(args, outputDir)
		},
	}
	c.Flags().StringVar(&outputDir, "output-dir", "", "directory to write trajectory + patch artifacts")
	return c
}

// formatPresetsBrief is exported here so other CLI surfaces (e.g. /models)
// can call it without re-implementing the format.
func formatPresetsBrief(presets []llm.ProviderPreset) string {
	var b strings.Builder
	for _, p := range presets {
		fmt.Fprintf(&b, "- %s (%s)\n", p.DisplayName, p.BaseURL)
	}
	return strings.TrimRight(b.String(), "\n")
}
