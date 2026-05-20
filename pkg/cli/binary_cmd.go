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
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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

	cmd.AddCommand(newBinaryExecCommand())
	cmd.AddCommand(newBinaryListModelsCommand())
	cmd.AddCommand(newBinaryListToolsCommand())
	cmd.AddCommand(newBinaryListSlashCommand())
	cmd.AddCommand(newBinaryListSkillsCommand())
	cmd.AddCommand(newBinarySWEBenchCommand())

	return cmd
}

func newBinaryExecCommand() *cobra.Command {
	var (
		jsonOut      bool
		outputDir    string
		sandboxMode  string
		onExitCmd    string
		goal         string
		goalMaxTurns int
		format       string
		quiet        bool
		stream       bool
		sessionID    string
	)
	c := &cobra.Command{
		Use:   "exec [prompt...]",
		Short: "Run a one-shot agent task and exit",
		Long: `Execute a single prompt against the full agent without entering the TUI
and exit. This command may use tools and mutate the workspace, so it is intended
for orchestrators, scripts, CI, and other non-interactive drivers.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt, err := promptFromArgsOrStdin(args, cmd.InOrStdin())
			if err != nil {
				return err
			}
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			if outputDir != "" {
				if err := os.MkdirAll(outputDir, 0o755); err != nil {
					return err
				}
			}

			// Handle format flag - if --json is used, default to json format
			if jsonOut && format == "" {
				format = "json"
			}
			if format == "" {
				format = "plain"
			}

			start := time.Now()
			opts := binaryOptions{
				OutputDir:    outputDir,
				Sandbox:      sandboxMode,
				OnExitCmd:    onExitCmd,
				Goal:         goal,
				GoalMaxTurns: goalMaxTurns,
				SessionID:    sessionID,
			}

			// Handle streaming output
			if stream {
				return runBinaryExecStreaming(cmd, prompt, wd, opts, format, quiet)
			}

			result, trajectory, goalState, err := executeBinaryModeWithOptions(prompt, wd, opts)
			summary := summarizeBinaryResult(trajectory, start, result, err, goalState)
			summary.CacheKey, _ = recordBinaryPromptCacheKey(prompt)
			defer runBinaryExitHook(firstNonEmpty(onExitCmd, os.Getenv("NANO_ON_EXIT")), summary)
			if err != nil {
				if !quiet {
					_ = writeBinaryResultTo(cmd.ErrOrStderr(), summary)
				}
				return withExitCode(err, binaryExitCode(summary.Status))
			}
			var trajectoryPath string
			if outputDir != "" {
				trajectoryPath = filepath.Join(outputDir, "trajectory.json")
				if err := saveTrajectory(trajectory, trajectoryPath); err != nil {
					return err
				}
			}

			// Output based on format
			switch format {
			case "json":
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"status":          "ok",
					"prompt":          prompt,
					"workdir":         wd,
					"output_dir":      outputDir,
					"trajectory_path": trajectoryPath,
					"result":          result,
					"summary":         summary,
				})
			case "jsonl", "ndjson":
				// For non-streaming mode, output as single JSON line
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"type":   "result",
					"result": result,
				}); err != nil {
					return err
				}
				if !quiet {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
						"type":    "summary",
						"summary": summary,
					})
				}
			default: // plain
				if _, err := fmt.Fprint(cmd.OutOrStdout(), result); err != nil {
					return err
				}
				if !quiet {
					return writeBinaryResultTo(cmd.ErrOrStderr(), summary)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "emit a single JSON result document (deprecated: use --format json)")
	c.Flags().StringVar(&format, "format", "", "output format: plain, json, or jsonl (ndjson)")
	c.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress metadata output, only show result")
	c.Flags().BoolVar(&stream, "stream", false, "enable streaming NDJSON output")
	c.Flags().StringVar(&outputDir, "output-dir", "", "directory to write trajectory + patch artifacts")
	c.Flags().StringVar(&sandboxMode, "sandbox", "auto", "sandbox mode for embedded execution: auto, on, or off")
	c.Flags().StringVar(&onExitCmd, "on-exit-cmd", "", "shell command to run with NANO_RESULT_JSON when the command exits")
	c.Flags().StringVar(&goal, "goal", "", "goal condition to evaluate across turns")
	c.Flags().IntVar(&goalMaxTurns, "goal-max-turns", 0, "maximum goal evaluation turns (overrides config when > 0)")
	c.Flags().StringVar(&sessionID, "session-id", "", "explicit session ID for hook routing (overrides NANO_SESSION_ID and SYMPHONY_ISSUE_ID)")
	return c
}

func promptFromArgsOrStdin(args []string, stdin io.Reader) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	if file, ok := stdin.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeCharDevice != 0 {
			return "", fmt.Errorf("prompt required: provide as arguments or pipe/redirect stdin")
		}
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("read prompt from stdin: %w", err)
	}
	prompt := string(data)
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt required: provide as arguments or pipe/redirect stdin")
	}
	return prompt, nil
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
	var outputDir, sandboxMode, onExitCmd string
	c := &cobra.Command{
		Use:   "swebench [prompt...]",
		Short: "SWE-bench-compatible one-shot evaluation",
		Long: `Run the agent in SWE-bench evaluation mode. Identical to invoking
the legacy --binary-mode flag, but discoverable as a subcommand. Writes the
generated patch and trajectory to --output-dir if provided.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			err := runBinaryModeWithOptions(args, binaryOptions{OutputDir: outputDir, Sandbox: sandboxMode, OnExitCmd: onExitCmd})
			if err != nil {
				status := classifyBinaryError(err)
				return withExitCode(err, binaryExitCode(status))
			}
			return nil
		},
	}
	c.Flags().StringVar(&outputDir, "output-dir", "", "directory to write trajectory + patch artifacts")
	c.Flags().StringVar(&sandboxMode, "sandbox", "auto", "sandbox mode for embedded execution: auto, on, or off")
	c.Flags().StringVar(&onExitCmd, "on-exit-cmd", "", "shell command to run with NANO_RESULT_JSON when the command exits")
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
