package cli

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/daemon"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/nano-harness/nano-agent/pkg/slash"
	"github.com/nano-harness/nano-agent/pkg/ui"
	"github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	"github.com/nano-harness/nano-agent/pkg/ui/bubbletea/banner"
	"github.com/nano-harness/nano-agent/pkg/ui/tview"
	"github.com/nano-harness/nano-agent/pkg/version"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// truncatePreview safely truncates preview text to a reasonable length
func truncatePreview(v interface{}) string {
	if v == nil {
		return ""
	}
	s := fmt.Sprintf("%v", v)
	// Limit to ~300 chars for readability
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}

// setNestedKey sets a value in a map using a dot-delimited key path.
// For example, setNestedKey(m, "advanced.fork.max_depth", "5") creates
// nested maps as needed and sets the leaf value.
func setNestedKey(m map[string]interface{}, key string, value string) {
	parts := strings.Split(key, ".")
	current := m
	for i, part := range parts {
		if i == len(parts)-1 {
			// Leaf – try to coerce the value to a native type for clean YAML output.
			current[part] = coerceYAMLValue(value)
			return
		}
		next, ok := current[part]
		if !ok {
			next = make(map[string]interface{})
			current[part] = next
		}
		if nextMap, ok := next.(map[string]interface{}); ok {
			current = nextMap
		} else {
			// Intermediate key exists but isn't a map – overwrite it.
			nextMap := make(map[string]interface{})
			current[part] = nextMap
			current = nextMap
		}
	}
}

// coerceYAMLValue attempts to interpret a string value as a native Go type
// (int, float, bool) so that yaml.Marshal produces clean, unquoted output.
func coerceYAMLValue(s string) interface{} {
	// Boolean
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	}
	// Integer (must match the entire string)
	var n int
	if cnt, err := fmt.Sscanf(s, "%d", &n); cnt == 1 && err == nil && fmt.Sprintf("%d", n) == s {
		return n
	}
	// Float (must match the entire string)
	var f float64
	if cnt, err := fmt.Sscanf(s, "%g", &f); cnt == 1 && err == nil {
		return f
	}
	return s
}

// float64FromAny converts interface{} to float64 safely
func float64FromAny(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	case string:
		var f float64
		_, _ = fmt.Sscanf(x, "%f", &f)
		return f
	default:
		return 0.0
	}
}

// NewRootCmd creates and returns a new root command.
// This factory function allows for better testability by avoiding global state.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "nano [prompt]",
		Short: "A nano AI-powered code generation and modification agent",
		Long: `nano is a lightweight AI agent designed for code generation,
modification, and development tasks. It provides intelligent assistance for developers
with safety measures and validation.

Usage:
  nano "fix the bug in main.go"
  nano "add error handling to the function"
  nano  # Interactive mode`,
		Run:  runAgent,
		Args: cobra.ArbitraryArgs, // Allow arbitrary arguments
	}

	// Add subcommands
	cmd.AddCommand(NewMemoryCommand())
	cmd.AddCommand(NewDaemonCommand())
	cmd.AddCommand(NewClientCommand())
	cmd.AddCommand(NewMCPCommand())
	cmd.AddCommand(NewConfigCommand())
	cmd.AddCommand(NewCommandsCommand())
	cmd.AddCommand(NewModelCommand())
	cmd.AddCommand(NewModelsCommand())
	cmd.AddCommand(NewThinkCommand())
	cmd.AddCommand(NewEventsCommand())
	cmd.AddCommand(NewAuditCommand())
	cmd.AddCommand(NewDoctorCommand())
	cmd.AddCommand(NewUpdateCommand())
	cmd.AddCommand(NewRoutinesCommand())
	cmd.AddCommand(NewSessionCommand())
	cmd.AddCommand(NewCompletionCommand(cmd))
	cmd.AddCommand(NewACPCommand())
	// Swarm commands
	cmd.AddCommand(NewChatCommand())
	cmd.AddCommand(NewLeadChatCommand())
	cmd.AddCommand(NewTeammateCommand())
	cmd.AddCommand(NewBinaryCommand())

	// Flags for mode selection
	cmd.PersistentFlags().BoolP("version", "v", false, "version for nano")
	cmd.Flags().BoolP("tui", "t", false, "force TUI mode even if daemon is running")
	cmd.Flags().BoolP("daemon", "d", false, "force daemon client mode")
	cmd.Flags().Int("timeout", daemon.DefaultTaskTimeoutSeconds, "command timeout in seconds for daemon mode")
	cmd.Flags().String("session-id", "", "session id for daemon mode execution")
	cmd.PersistentFlags().StringP("config", "c", "", "config file path (optional, overrides auto-discovery)")
	cmd.PersistentFlags().String("ui", string(ui.ModeBubbleTea), "TUI backend: bubbletea or tview")

	// Experimental: Bubble Tea non-alt-screen TUI
	cmd.Flags().Bool("tea", false, "use experimental Bubble Tea TUI (non alt-screen)")
	cmd.Flags().Bool("no-banner", false, "disable startup ASCII banner animation in TUI mode")

	// TUI session management flags
	cmd.Flags().Bool("continue", false, "resume the most recent session in the current project (TUI mode)")
	cmd.Flags().String("session", "", "use a specific session id (TUI mode); creates if not exists")
	cmd.Flags().String("team", "", "start TUI in team-lead mode with mailbox support (TUI mode)")

	// SWE-bench compatibility flags
	cmd.Flags().Bool("binary-mode", false, "Enable binary mode for SWE-bench evaluation")
	cmd.Flags().String("output-dir", "", "Output directory for generated files (binary mode)")

	// Permission mode flags
	cmd.Flags().String("permission-mode", "", "permission mode: default, acceptEdits, yolo")
	cmd.Flags().Bool("dangerously-skip-permissions", false, "skip all permission checks (equivalent to --permission-mode=yolo)")

	return cmd
}

// rootCmd maintains backward compatibility for existing code that references the global variable.
// New code should use NewRootCmd() instead.
var rootCmd = NewRootCmd()

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return NewRootCmd().Execute()
}

func init() {
	cobra.OnInitialize(initConfig)
}

func getUIMode(cmd *cobra.Command) ui.Mode {
	raw := string(ui.ModeBubbleTea)
	if cmd != nil {
		if v, err := cmd.Flags().GetString("ui"); err == nil && strings.TrimSpace(v) != "" {
			raw = v
		} else if v, err := cmd.Root().PersistentFlags().GetString("ui"); err == nil && strings.TrimSpace(v) != "" {
			raw = v
		}
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ui.ModeTView):
		return ui.ModeTView
	case string(ui.ModeBubbleTea), "":
		return ui.ModeBubbleTea
	default:
		logger.Warnf("unknown --ui value %q; falling back to bubbletea", raw)
		return ui.ModeBubbleTea
	}
}

// initConfig reads configuration from multiple sources in priority order
func initConfig() {
	// Get config file from flag if provided
	configFile, _ := rootCmd.PersistentFlags().GetString("config")
	if strings.TrimSpace(configFile) != "" {
		_ = os.Setenv("NANO_CONFIG_FILE", configFile)
	}

	// Initialize with config file parameter (empty string uses multi-level config system)
	if _, err := config.LoadConfig(configFile); err != nil {
		logger.Errorf("Error initializing config: %v", err)
		_, _ = fmt.Fprintf(os.Stderr, "Error initializing config: %v\n", err)
		// Note: os.Exit here is acceptable because initConfig is called via cobra.OnInitialize,
		// which doesn't support error returns. Config initialization failure prevents all commands
		// from functioning properly, so exiting here is the appropriate behavior.
		os.Exit(1)
	}
}

// NewConfigCommand creates the config command
func NewConfigCommand() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management commands",
		Long:  `Manage nano configuration files and settings`,
	}

	// Add subcommands
	configCmd.AddCommand(&cobra.Command{
		Use:   "paths",
		Short: "Show configuration file paths and loading order",
		Long:  `Display all configuration file paths in loading order and their existence status`,
		Run: func(cmd *cobra.Command, _ []string) {
			configFile, _ := cmd.Root().PersistentFlags().GetString("config")
			paths := config.GetConfigLocations(configFile)

			fmt.Println("Configuration file loading order (highest to lowest priority):")
			fmt.Println()

			for i, path := range paths {
				status := "❌ Not found"
				if path.Exists {
					status = "✅ Found"
				}

				priority := len(paths) - i
				logger.Infof("%d. [%s] %s", priority, path.Type, path.Path)
				logger.Infof("   %s", status)
				logger.Info("")
			}

			fmt.Println("Note: Files with higher priority override settings from lower priority files.")
			fmt.Println("Environment variables have the highest priority and override file settings.")
		},
	})

	// nano config init
	configCmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Initialize a new .nano.yaml config file",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Print("Enter your API key: ")
			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("failed to read input: %w", err)
			}
			apiKey := strings.TrimSpace(scanner.Text())
			if apiKey == "" {
				return fmt.Errorf("api_key cannot be empty")
			}
			// Marshal via yaml so special characters are properly quoted.
			initCfg := map[string]string{"api_key": apiKey, "model": "deepseek-chat"}
			out, err := yaml.Marshal(initCfg)
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}
			if err := os.WriteFile(".nano.yaml", out, 0o600); err != nil {
				return fmt.Errorf("failed to write .nano.yaml: %w", err)
			}
			fmt.Println("Created .nano.yaml")
			return nil
		},
	})

	// nano config show
	configCmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := config.Get()
			if cfg == nil {
				fmt.Println("No configuration loaded")
				return
			}
			maskedKey := cfg.APIKey
			if len(maskedKey) > 4 {
				maskedKey = "sk-****" + maskedKey[len(maskedKey)-4:]
			} else if maskedKey != "" {
				maskedKey = "sk-****"
			}
			fmt.Printf("api_key: %s\nmodel: %s\nbase_url: %s\n", maskedKey, cfg.Model, cfg.BaseURL)
		},
	})

	// nano config set <key> <value>
	configCmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration key (supports dot-path keys like advanced.fork.max_depth)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			key, value := args[0], args[1]

			// Read existing config into a map (or start with an empty map)
			data, err := os.ReadFile(".nano.yaml")
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to read .nano.yaml: %w", err)
			}
			cfgMap := make(map[string]interface{})
			if len(data) > 0 {
				if err := yaml.Unmarshal(data, &cfgMap); err != nil {
					return fmt.Errorf("failed to parse .nano.yaml: %w", err)
				}
			}

			// Set the value at the (possibly nested) key path
			setNestedKey(cfgMap, key, value)

			out, err := yaml.Marshal(cfgMap)
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}
			if err := os.WriteFile(".nano.yaml", out, 0o600); err != nil {
				return fmt.Errorf("failed to write .nano.yaml: %w", err)
			}
			fmt.Printf("Set %s = %s\n", key, value)
			return nil
		},
	})

	// nano config validate
	configCmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate that required configuration fields are set",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := config.Get()
			if cfg == nil {
				fmt.Println("❌ No configuration loaded")
				return
			}
			valid := true
			if cfg.APIKey == "" {
				fmt.Println("❌ api_key is not set")
				valid = false
			} else {
				fmt.Println("✅ api_key is set")
			}
			if cfg.Model == "" {
				fmt.Println("❌ model is not set")
				valid = false
			} else {
				fmt.Printf("✅ model: %s\n", cfg.Model)
			}
			if valid {
				fmt.Println("✅ Configuration is valid")
			}
		},
	})

	return configCmd
}

// runAgent is the main entry point for the agent
func runAgent(cmd *cobra.Command, args []string) {
	if v, _ := cmd.Flags().GetBool("version"); v {
		fmt.Printf("nano version %s (commit %s, built %s)\n", version.Version, version.CommitHash, version.BuildTime)
		return
	}

	// Check for binary mode flag
	binaryMode, _ := cmd.Flags().GetBool("binary-mode")
	outputDir, _ := cmd.Flags().GetString("output-dir")

	if binaryMode {
		fmt.Fprintln(os.Stderr,
			"⚠️  --binary-mode is deprecated; use `nano binary swebench [prompt...]` (or `nano binary query`) instead. "+
				"This flag will be removed in a future release.")
		if err := runBinaryMode(args, outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Check for force TUI mode flag
	forceTUI, _ := cmd.Flags().GetBool("tui")

	// Check for force daemon client mode flag
	useDaemon, _ := cmd.Flags().GetBool("daemon")

	// Decide which TUI to use early to enable TUI mode logging
	useTea, _ := cmd.Flags().GetBool("tea")

	// Determine execution mode
	if useDaemon || (!forceTUI && isDaemonRunning()) {
		// Use daemon mode if explicitly requested or daemon is running
		if err := runDaemonClientMode(cmd, args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Enable TUI mode logging BEFORE any other initialization if we're going to use TUI
	if useTea || forceTUI {
		logger.SetTUIMode(true)
		defer logger.SetTUIMode(false)
	}

	cfg := config.Get()
	if cfg == nil {
		logger.Error("configuration not initialized")
		return
	}

	// Apply permission-mode CLI overrides (flags take priority over config file).
	skipPerms, _ := cmd.Flags().GetBool("dangerously-skip-permissions")
	permMode, _ := cmd.Flags().GetString("permission-mode")
	if skipPerms {
		cfg.PermissionMode = string(permission.ModeYOLO)
	} else if permMode != "" {
		cfg.PermissionMode = permMode
	}
	if permission.PermissionMode(cfg.PermissionMode) == permission.ModeYOLO {
		if cfg.Sandbox == nil {
			cfg.Sandbox = &config.SandboxConfig{}
		}
		if cfg.Sandbox.Backend == "" {
			cfg.Sandbox.Enabled = true
			cfg.Sandbox.Backend = "docker"
			logger.Infof("YOLO permission mode selected; defaulting sandbox backend to docker")
		}
	}

	// CLI overrides removed - loop detection configuration is now handled entirely through config files

	// Configure global logger based on verbose setting
	logger.Infof("Initializing agent with verbose=%v", cfg.Verbose)
	logger.SetVerbose(cfg.Verbose)

	if useTea {
		if err := runBubbleTeaMode(cmd, args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Use classic tview-based TUI (default or forced)
		if err := runTUIMode(cmd, args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

// isDaemonRunning checks if daemon is currently running
func isDaemonRunning() bool {
	manager := daemon.NewManager()
	_ = manager.LoadConfig() // ensure we respect configured pid_file
	return manager.IsRunning()
}

// runDaemonClientMode executes commands via daemon client
func runDaemonClientMode(cmd *cobra.Command, args []string) error {
	// Default timeout if cmd is nil or flag is not set
	timeout := daemon.DefaultTaskTimeoutSeconds
	sessionID := ""
	if cmd != nil {
		var err error
		timeout, err = cmd.Flags().GetInt("timeout")
		if err != nil {
			// Fallback to default if flag is not found (e.g., called from TUI fallback)
			timeout = daemon.DefaultTaskTimeoutSeconds
		}
		sessionID, _ = cmd.Flags().GetString("session-id")
	}
	if len(args) == 0 {
		color.Yellow("🔗 Daemon is running. Use 'nano client exec \"your command\"' for interactive execution.")
		color.Blue("💡 Or use 'nano --tui' to force TUI mode.")
		return nil
	}

	// Join all args as a single command
	command := strings.Join(args, " ")
	color.Blue("🔗 Executing via daemon: %s", command)

	// Create daemon client
	client := createDaemonClient()

	// Execute command
	response, err := client.ExecuteInSession(command, sessionID, timeout, false, false)
	if err != nil {
		color.Red("❌ Failed to execute command via daemon: %v", err)
		color.Yellow("💡 Try 'nano --tui \"%s\"' to run in TUI mode instead.", command)
		return fmt.Errorf("failed to execute command via daemon: %w", err)
	}

	if response.Success {
		// Print token usage if present
		if response.TokenStats != nil {
			ts := response.TokenStats
			fmt.Println("\n--- Token Usage ---")
			logger.Infof("Input: %d  Output: %d  Total: %d", ts.InputTokens, ts.OutputTokens, ts.TotalTokens)
			if ts.SessionTotalTokens > 0 {
				logger.Infof("Session Total: %d (In: %d, Out: %d)", ts.SessionTotalTokens, ts.SessionInputTokens, ts.SessionOutputTokens)
			}
		}
		fmt.Println()
	} else {
		color.Red("❌ Command failed: %s", response.Error)
		return fmt.Errorf("command failed: %s", response.Error)
	}
	return nil
}

// resolveTUISessionID determines which session ID to use in TUI mode based on flags:
//   - --session <id>  → use specified id (highest priority)
//   - --continue      → use latest session id from ProjectSessionStorage; if none, generate new
//   - default         → generate a new unique session id per launch
func resolveTUISessionID(cmd *cobra.Command, ag *agent.Agent) string {
	if cmd != nil {
		if explicit, _ := cmd.Flags().GetString("session"); strings.TrimSpace(explicit) != "" {
			return strings.TrimSpace(explicit)
		}
		if cont, _ := cmd.Flags().GetBool("continue"); cont {
			if sm := ag.GetSessionManager(); sm != nil {
				if ps, ok := sm.GetStorage().(*agent.ProjectSessionStorage); ok {
					if latest, err := ps.GetLatestSessionID(); err == nil && latest != "" {
						return latest
					}
					logger.Warnf("--continue specified but no previous session found; creating new")
				}
			}
		}
	}
	return agent.NewSession().ID // generates a fresh session_<hex>
}

// runTUIMode starts the TUI dashboard mode
func runTUIMode(cmd *cobra.Command, args []string) error {
	// Acquire TUI mode lock to prevent simultaneous TUI/Daemon execution
	lock, lockErr := runtime.NewLockFile(runtime.ModeTUI)
	if lockErr != nil {
		color.Red("❌ Failed to create TUI lock: %v", lockErr)
		return fmt.Errorf("failed to create TUI lock: %w", lockErr)
	}
	if lockErr = lock.Acquire(); lockErr != nil {
		color.Red("❌ %v", lockErr)
		return fmt.Errorf("failed to acquire TUI lock: %w", lockErr)
	}
	defer func() {
		if lockErr := lock.Release(); lockErr != nil {
			logger.Warnf("Failed to release TUI lock: %v", lockErr)
		}
	}()

	// Check TTY compatibility first
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		color.Yellow("⚠️  TTY environment not supported for TUI mode")
		color.Blue("💡 Falling back to daemon mode or direct execution")

		// If we have args, execute directly
		if len(args) > 0 {
			// This is a fallback and we don't have access to the original cobra command here.
			// We pass 'nil' and the runDaemonClientMode should handle it gracefully.
			return runDaemonClientMode(nil, args)
		}

		// Otherwise suggest daemon mode
		color.Yellow("🔗 Consider using daemon mode: 'nano daemon start' then 'nano client exec \"your command\"'")
		return fmt.Errorf("TTY environment not supported for TUI mode")
	}

	// Enable TUI mode in logger FIRST to prevent any startup logs from appearing in TUI
	// Note: This is already set in runAgent, but we keep it here for safety
	logger.SetTUIMode(true)
	defer logger.SetTUIMode(false) // Restore normal logging on exit

	cfg := config.Get()
	// Start local-only pprof server for TUI mode using top-level config only
	var pprofServer *http.Server
	if cfg != nil {
		pprofEnabled := cfg.EnablePprof
		pprofPort := cfg.PprofPort
		if pprofEnabled {
			if pprofPort == 0 {
				pprofPort = 6060
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
			addr := fmt.Sprintf("%s:%d", "127.0.0.1", pprofPort)
			pprofServer = &http.Server{Addr: addr, Handler: mux}
			go func() {
				logger.Infof("Starting pprof server on %s (local-only)", addr)
				if err := pprofServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Warnf("pprof server error: %v", err)
				}
			}()
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = pprofServer.Shutdown(ctx)
			}()
		}
	}

	// Create TUI integration first
	integration := tview.NewIntegration()
	integration.SetupCommandsProvider()

	// Predeclare agentInstance and err so they can be referenced in closures
	var agentInstance *agent.Agent
	var err error

	// Create approval handler that connects TUI to tool scheduler
	approvalHandler := func(toolInfo *agent.ToolCallInfo) bool {
		// Use simple confirmation UI without parameter editing
		msg := fmt.Sprintf("确认执行工具：%s (ID: %s)？", toolInfo.Name, toolInfo.ID)
		toolMap := map[string]interface{}{
			"ID":         toolInfo.ID,
			"Name":       toolInfo.Name,
			"Parameters": toolInfo.Parameters,
		}

		integration.ShowConfirmation(msg, toolMap, func(approved bool) {
			// Return decision to tool scheduler
			_ = agentInstance.GetToolScheduler().HandleConfirmationResponse(toolInfo.ID, approved)
		})
		// Return false to keep the call in awaiting_approval; the actual decision will come via HandleConfirmationResponse
		return false
	}

	// Load persistent allowlist for current workdir and merge into cfg.
	allowlistPath, _ := permission.DefaultPersistentAllowlistPath()
	allowlistStore := permission.NewPersistentAllowlistStore(allowlistPath)
	if err := allowlistStore.Load(); err != nil {
		logger.Warnf("Failed to load persistent allowlist: %v", err)
	}
	var cwd string
	cwd, _ = os.Getwd()
	for _, raw := range allowlistStore.RulesForWorkdir(cwd) {
		cfg.AllowedRules = append(cfg.AllowedRules, raw)
	}

	// Build the engine (agent + scheduler + watcher) using the approval handler
	// Check if team-lead mode is requested
	var eng *engine.Engine
	teamName, _ := cmd.Flags().GetString("team")
	if teamName != "" {
		logger.Infof("Starting TUI in team-lead mode for team '%s'", teamName)
		eng, err = engine.NewLeadEngine(cfg, approvalHandler, teamName)
	} else {
		eng, err = engine.New(cfg, approvalHandler)
	}
	if err != nil {
		color.Red("Error initializing engine: %v", err)
		return fmt.Errorf("error initializing engine: %w", err)
	}
	agentInstance = eng.Agent
	defer func() {
		if err := eng.Shutdown(); err != nil {
			logger.Errorf("Engine shutdown error: %v", err)
		}
	}()

	// Set up session ID for TUI mode
	sessionID := resolveTUISessionID(cmd, agentInstance)
	agentInstance.SetActiveSessionID(sessionID)
	// Touch session in manager so storage / index get initialised
	agentInstance.GetSessionManager().GetOrCreateSession(sessionID)
	logger.Infof("TUI session id: %s", sessionID)

	// Wrap the engine's shared scheduler as a TUIScheduler
	tuiScheduler := agent.NewTUISchedulerFromScheduler(eng.Scheduler, eng.StateStore)
	agentInstance.SetTUIScheduler(tuiScheduler)

	cronTracker := ui.NewCronStatusTracker()
	integration.SetCronTracker(cronTracker)
	integration.SetRoutinesLister(tuiScheduler.FormatTasks)
	integration.SetRunningStatusLister(cronTracker.FormatDetails)
	eng.SetCronNotifier(cronTracker.Handle)

	// Start scheduler + watcher through the engine
	if startErr := eng.Start(); startErr != nil {
		logger.Warnf("Engine start failed: %v", startErr)
	}

	// Register allowlist handler so "同意并不再询问" adds a rule to the session allowlist.
	integration.SetAllowlistHandler(func(toolName string, params map[string]interface{}) {
		if pm := agentInstance.GetPermissionManager(); pm != nil {
			rules := permission.BuildAllowlistRules(toolName, params)
			for _, rule := range rules {
				pm.GetSessionAllowlist().AddRule(rule)
				// Persist the rule to disk
				if _, err := allowlistStore.AddRuleForWorkdir(cwd, rule.RawPattern); err != nil {
					logger.Warnf("Failed to persist allowlist rule %q: %v", rule.RawPattern, err)
				}
			}
		}
	})
	// Wire permission manager so slash commands work.
	integration.SetPermissionManager(agentInstance.GetPermissionManager())
	// Wire persistent allowlist store for /disallow cleanup
	integration.SetPersistentAllowlist(allowlistStore, cwd)
	// Wire engine so /think command works.
	integration.SetEngine(eng)
	// Wire new session callback so Ctrl+R / /clear work.
	integration.SetNewSessionCallback(func() string {
		return agentInstance.StartNewSession()
	})

	// If we have a direct command to execute in TUI mode,
	// we should pass it to the TUI instead of executing directly
	// to avoid stdout interference with TUI display
	var initialCommand string
	if len(args) > 0 {
		initialCommand = strings.Join(args, " ")
	}

	// Redirect stdout to prevent third-party library logs from interfering with TUI
	originalStdout := os.Stdout
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err == nil {
		os.Stdout = devNull
		defer func() {
			_ = devNull.Close()
			os.Stdout = originalStdout
		}()
	}

	// Set up handlers for agent integration
	integration.SetInputHandler(func(ctx context.Context, input string) error {
		return agentInstance.ProcessStream(ctx, input, func(streamEvent event.StreamEvent) {
			// Handle different event types for TUI output
			switch streamEvent.Type {
			case event.EventTypeStreamContent:
				integration.AddMessage("assistant", streamEvent.Content)
			case event.EventTypeContent:
				if streamEvent.Source != "llm_client" {
					integration.AddMessage("assistant", streamEvent.Content)
				}
			case event.EventTypeError:
				integration.AddMessage("system", fmt.Sprintf("Error: %s", streamEvent.Error))
			case event.EventTypeDone:
				integration.GetModel().GetStateManager().SetIdle()
			case event.EventTypeToolUse:
				if streamEvent.ToolUse != nil {
					integration.GetModel().GetStateManager().SetToolExecution(streamEvent.ToolUse.ToolName, "")
				}
				integration.GetModel().AddToolUse(streamEvent.ToolUse)
			case event.EventTypeTokenStats:
				integration.GetModel().GetStateManager().UpdateTokenStats(streamEvent.TokenStats)
			case event.EventTypeThinking:
				activity := streamEvent.Content
				if activity == "" {
					activity = "思考中"
				}
				integration.AddThinking(streamEvent.Content, streamEvent.Reasoning, streamEvent.Metadata)
				integration.GetModel().GetStateManager().SetThinking(activity)
			case event.EventTypeCompression:
				// Show compression marker with key stats and truncated full summary
				orig := streamEvent.Metadata["original_tokens"]
				cmp := streamEvent.Metadata["compressed_tokens"]
				ratio := streamEvent.Metadata["compression_ratio"]
				before := streamEvent.Metadata["messages_before"]
				after := streamEvent.Metadata["messages_after"]
				trigger := streamEvent.Metadata["triggered_by"]
				summary := streamEvent.Metadata["summary_full"]
				msg := fmt.Sprintf("🗜️ 上下文压缩: %v → %v tokens (减少 %.2f%%)\n消息数: %v → %v，触发: %v\n摘要: %v",
					orig, cmp, (1.0-float64FromAny(ratio))*100, before, after, trigger, truncatePreview(summary))
				integration.AddMessage("system", msg)
			case event.EventTypeTaskStart:
				// Mark thinking/processing when a task starts
				activity := streamEvent.Content
				if activity == "" {
					activity = "开始任务"
				}
				integration.GetModel().GetStateManager().SetThinking(activity)
				// Add a chat marker for task start
				integration.AddMessage("system", fmt.Sprintf("🟡 任务开始: %s", activity))
			case event.EventTypeTaskProgress:
				// Show progress in current activity
				pct := int(streamEvent.Progress * 100)
				activity := fmt.Sprintf("任务进度: %d%%", pct)
				if streamEvent.Content != "" {
					activity = fmt.Sprintf("%s（%d%%）", streamEvent.Content, pct)
				}
				integration.GetModel().GetStateManager().SetProcessing(activity)
			case event.EventTypeTaskCompletion:
				activity := streamEvent.Content
				if activity == "" {
					activity = "任务完成"
				}
				integration.GetModel().GetStateManager().SetCompleted(activity)
				integration.GetModel().PlaySound("cough")
				// Add a chat marker for task completion (initial command)
				integration.AddMessage("system", fmt.Sprintf("✅ 任务完成: %s", activity))
			case event.EventTypeSatisfactionEval:
				integration.GetModel().PlaySound("cough")
			case event.EventTypeTerminationSignal:
				integration.GetModel().PlaySound("cough")
			}
		})
	})

	integration.SetCancelHandler(func() error {
		// Handle task cancellation if needed
		return nil
	})

	// If we have an initial command, execute it automatically in TUI
	if initialCommand != "" {
		// Add the initial command as a message and process it
		integration.AddMessage("user", initialCommand)
		go func() {
			// Small delay to ensure TUI is fully initialized
			time.Sleep(100 * time.Millisecond)

			ctx := context.Background()
			err := agentInstance.ProcessStream(ctx, initialCommand, func(streamEvent event.StreamEvent) {
				// Handle different event types for TUI output
				switch streamEvent.Type {
				case event.EventTypeStreamContent:
					integration.AddMessage("assistant", streamEvent.Content)
				case event.EventTypeContent:
					if streamEvent.Source != "llm_client" {
						integration.AddMessage("assistant", streamEvent.Content)
					}
				case event.EventTypeError:
					integration.AddMessage("system", fmt.Sprintf("Error: %s", streamEvent.Error))
				case event.EventTypeDone:
					integration.GetModel().GetStateManager().SetIdle()
				case event.EventTypeToolUse:
					if streamEvent.ToolUse != nil {
						integration.GetModel().GetStateManager().SetToolExecution(streamEvent.ToolUse.ToolName, "")
					}
					integration.GetModel().AddToolUse(streamEvent.ToolUse)
				case event.EventTypeTokenStats:
					integration.GetModel().GetStateManager().UpdateTokenStats(streamEvent.TokenStats)
				case event.EventTypeThinking:
					activity := streamEvent.Content
					if activity == "" {
						activity = "思考中"
					}
					integration.AddThinking(streamEvent.Content, streamEvent.Reasoning, streamEvent.Metadata)
					integration.GetModel().GetStateManager().SetThinking(activity)
				case event.EventTypeCompression:
					orig := streamEvent.Metadata["original_tokens"]
					cmp := streamEvent.Metadata["compressed_tokens"]
					ratio := streamEvent.Metadata["compression_ratio"]
					before := streamEvent.Metadata["messages_before"]
					after := streamEvent.Metadata["messages_after"]
					trigger := streamEvent.Metadata["triggered_by"]
					summary := streamEvent.Metadata["summary_full"]
					msg := fmt.Sprintf("🗜️ 上下文压缩: %v → %v tokens (减少 %.2f%%)\n消息数: %v → %v，触发: %v\n摘要: %v",
						orig, cmp, (1.0-float64FromAny(ratio))*100, before, after, trigger, truncatePreview(summary))
					integration.AddMessage("system", msg)
				case event.EventTypeTaskStart:
					activity := streamEvent.Content
					if activity == "" {
						activity = "开始任务"
					}
					integration.GetModel().GetStateManager().SetThinking(activity)
					// Add a chat marker for task start (initial command)
					integration.AddMessage("system", fmt.Sprintf("🟡 任务开始: %s", activity))
				case event.EventTypeTaskProgress:
					pct := int(streamEvent.Progress * 100)
					activity := fmt.Sprintf("任务进度: %d%%", pct)
					if streamEvent.Content != "" {
						activity = fmt.Sprintf("%s（%d%%）", streamEvent.Content, pct)
					}
					integration.GetModel().GetStateManager().SetProcessing(activity)
				case event.EventTypeTaskCompletion:
					activity := streamEvent.Content
					if activity == "" {
						activity = "任务完成"
					}
					integration.GetModel().GetStateManager().SetCompleted(activity)
					integration.GetModel().PlaySound("cough")
					// Add a chat marker for task completion (initial command)
					integration.AddMessage("system", fmt.Sprintf("✅ 任务完成: %s", activity))
				case event.EventTypeSatisfactionEval:
					integration.GetModel().PlaySound("cough")
				case event.EventTypeTerminationSignal:
					integration.GetModel().PlaySound("cough")
				}
			})
			if err != nil {
				integration.AddMessage("system", fmt.Sprintf("Initial command failed: %v", err))
			}
		}()
	}

	// Start TUI (this blocks until quit)
	err = integration.Run()
	if err != nil {
		color.Red("TUI Error: %v", err)
		logger.Errorf("TUI failed with error: %v\n", err)
		return fmt.Errorf("TUI failed: %w", err)
	}

	color.Green("👋 TUI session ended")
	return nil
}

// runBubbleTeaMode starts the Bubble Tea TUI in non-alt-screen mode
func runBubbleTeaMode(cmd *cobra.Command, args []string) error {
	// Acquire TUI mode lock to prevent simultaneous TUI/Daemon execution
	lock, lockErr := runtime.NewLockFile(runtime.ModeTUI)
	if lockErr != nil {
		color.Red("❌ Failed to create TUI lock: %v", lockErr)
		return fmt.Errorf("failed to create TUI lock: %w", lockErr)
	}
	if lockErr = lock.Acquire(); lockErr != nil {
		color.Red("❌ %v", lockErr)
		return fmt.Errorf("failed to acquire TUI lock: %w", lockErr)
	}
	defer func() {
		if lockErr := lock.Release(); lockErr != nil {
			logger.Warnf("Failed to release TUI lock: %v", lockErr)
		}
	}()

	// Ensure we are in a terminal
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		color.Yellow("⚠️  TTY environment not supported for Bubble Tea TUI")
		if len(args) > 0 {
			return runDaemonClientMode(nil, args)
		}
		// Escape inner quotes to avoid syntax issues
		color.Yellow("🔗 Consider using daemon mode: 'nano daemon start' then 'nano client exec \"your command\"'")
		return fmt.Errorf("TTY environment not supported for Bubble Tea TUI")
	}

	// Enable TUI mode in logger FIRST to avoid any startup logs appearing in TUI
	// Note: This is already set in runAgent, but we keep it here for safety
	logger.SetTUIMode(true)
	defer logger.SetTUIMode(false)

	cfg := config.Get()
	if cfg == nil {
		logger.Error("configuration not initialized")
		return fmt.Errorf("configuration not initialized")
	}

	// Start local-only pprof server for Bubble Tea mode using top-level config only
	var pprofServer *http.Server
	{
		pprofEnabled := cfg.EnablePprof
		pprofPort := cfg.PprofPort
		if pprofEnabled {
			if pprofPort == 0 {
				pprofPort = 6060
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
			addr := fmt.Sprintf("%s:%d", "127.0.0.1", pprofPort)
			pprofServer = &http.Server{Addr: addr, Handler: mux}
			go func() {
				logger.Infof("Starting pprof server on %s (local-only)", addr)
				if err := pprofServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Warnf("pprof server error: %v", err)
				}
			}()
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = pprofServer.Shutdown(ctx)
			}()
		}
	}

	// Create channels for communication
	submitCh := make(chan string)
	cancelCh := make(chan struct{})

	// Get CWD and API Base URL
	var cwd string
	cwd, _ = os.Getwd()
	apiBaseURL := cfg.BaseURL

	// Create Bubble Tea model
	m := bubbletea.New(submitCh, cancelCh, apiBaseURL, cwd)

	// Prepare program without Alt Screen for natural terminal scrolling
	p := tea.NewProgram(m)

	// Predeclare agentInstance for use in approval handler
	var agentInstance *agent.Agent
	var err error

	// Create approval handler that connects Bubble Tea TUI to tool scheduler
	approvalHandler := func(toolInfo *agent.ToolCallInfo) bool {
		// Use Bubble Tea confirmation UI
		msg := fmt.Sprintf("确认执行工具：%s (ID: %s)？", toolInfo.Name, toolInfo.ID)
		toolMap := map[string]interface{}{
			"ID":         toolInfo.ID,
			"Name":       toolInfo.Name,
			"Parameters": toolInfo.Parameters,
		}

		// Show confirmation dialog in Bubble Tea model via p.Send() to
		// ensure thread-safe delivery through the Bubble Tea event loop.
		p.Send(bubbletea.ShowConfirmationMsg{
			Message:  msg,
			ToolInfo: toolMap,
			Callback: func(approved bool) {
				// Return decision to tool scheduler
				_ = agentInstance.GetToolScheduler().HandleConfirmationResponse(toolInfo.ID, approved)
			},
		})
		// Return false to keep the call in awaiting_approval; the actual decision will come via HandleConfirmationResponse
		return false
	}

	// Play banner animation BEFORE redirecting stdout (so banner is visible to user)
	noBanner, _ := cmd.Flags().GetBool("no-banner")
	if !noBanner {
		_ = banner.Play(os.Stdout, banner.Options{
			Theme:    banner.DefaultTheme,
			Colorize: banner.IsInteractiveTTY(),
		})
	}

	// Redirect stdout to prevent third-party library logs from interfering with BubbleTea TUI
	originalStdout := os.Stdout
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err == nil {
		os.Stdout = devNull
		defer func() {
			_ = devNull.Close()
			os.Stdout = originalStdout
		}()
	}

	// Load persistent allowlist for current workdir and merge into cfg.
	allowlistPath, _ := permission.DefaultPersistentAllowlistPath()
	allowlistStore := permission.NewPersistentAllowlistStore(allowlistPath)
	if err := allowlistStore.Load(); err != nil {
		logger.Warnf("Failed to load persistent allowlist: %v", err)
	}
	cwd, _ = os.Getwd()
	for _, raw := range allowlistStore.RulesForWorkdir(cwd) {
		cfg.AllowedRules = append(cfg.AllowedRules, raw)
	}

	// Build the engine (agent + scheduler + watcher) using the approval handler
	// defined above. agentInstance is assigned here so the closure can resolve it.
	// Check if team-lead mode is requested
	var eng *engine.Engine
	teamName, _ := cmd.Flags().GetString("team")
	if teamName != "" {
		logger.Infof("Starting Bubble Tea TUI in team-lead mode for team '%s'", teamName)
		eng, err = engine.NewLeadEngine(cfg, approvalHandler, teamName)
	} else {
		eng, err = engine.New(cfg, approvalHandler)
	}
	if err != nil {
		color.Red("Error initializing engine: %v", err)
		return fmt.Errorf("error initializing engine: %w", err)
	}
	agentInstance = eng.Agent
	defer func() {
		if err := eng.Shutdown(); err != nil {
			logger.Errorf("Engine shutdown error: %v", err)
		}
	}()

	// Set up session ID for Bubble Tea TUI mode
	sessionID := resolveTUISessionID(cmd, agentInstance)
	agentInstance.SetActiveSessionID(sessionID)
	// Touch session in manager so storage / index get initialised
	agentInstance.GetSessionManager().GetOrCreateSession(sessionID)
	logger.Infof("TUI session id: %s", sessionID)

	// Wrap the engine's shared scheduler as a TUIScheduler so /loop and
	// /schedule slash commands keep working in BubbleTea mode.
	btScheduler := agent.NewTUISchedulerFromScheduler(eng.Scheduler, eng.StateStore)
	agentInstance.SetTUIScheduler(btScheduler)

	eventStore := daemon.NewTaskEventStore(5000)
	cronTracker := ui.NewCronStatusTracker()
	cronTracker.SetOnChange(func() {
		p.Send(bubbletea.CronStatusMsg{Indicator: cronTracker.FormatIndicator()})
	})
	m.SetModelLister(slash.BuildModelLister(cfg))
	m.SetModelStatusGetter(slash.BuildModelStatusGetter(cfg))
	m.SetModelSwitcher(slash.BuildModelSwitcher(filepath.Join(cwd, ".nano.yaml")))
	m.SetModelFallbackHandler(slash.BuildModelFallbackHandler(cfg))
	m.SetModelDoctor(slash.BuildModelDoctor(cfg))
	m.SetContextStatusGetter(slash.BuildContextStatusGetter(agentInstance))
	m.SetDoctorReporter(slash.BuildDoctorReporter(cfg))
	m.SetEventsQuerier(slash.BuildEventsQuerier(eventStore))
	m.SetAuditQuerier(slash.BuildAuditQuerier(eventStore))
	if sm := agentInstance.GetSkillManager(); sm != nil {
		m.SetSkillLister(sm.ListSkillNames)
	}
	m.SetRoutinesLister(btScheduler.FormatTasks)
	m.SetRunningStatusLister(cronTracker.FormatDetails)
	m.SetRoutinesAdder(func(description string) string {
		id, err := btScheduler.AddRoutineFromDescription(description)
		if err != nil {
			return fmt.Sprintf("❌ 添加 routine 失败：%v", err)
		}
		return fmt.Sprintf("✅ 已添加 routine %s", id)
	})
	m.SetRoutinesRemover(func(taskID string) string {
		if err := btScheduler.RemoveTask(strings.TrimSpace(taskID)); err != nil {
			return fmt.Sprintf("❌ 删除 routine 失败：%v", err)
		}
		return fmt.Sprintf("✅ 已删除 routine %s", strings.TrimSpace(taskID))
	})
	m.SetRoutinesPauser(func(taskID string) string {
		if err := btScheduler.PauseTask(strings.TrimSpace(taskID)); err != nil {
			return fmt.Sprintf("❌ 暂停 routine 失败：%v", err)
		}
		return fmt.Sprintf("✅ 已暂停 routine %s", strings.TrimSpace(taskID))
	})
	m.SetRoutinesResumer(func(taskID string) string {
		if err := btScheduler.ResumeTask(strings.TrimSpace(taskID)); err != nil {
			return fmt.Sprintf("❌ 恢复 routine 失败：%v", err)
		}
		return fmt.Sprintf("✅ 已恢复 routine %s", strings.TrimSpace(taskID))
	})
	eng.SetCronNotifier(func(ev event.StreamEvent) {
		eventStore.Add(ev)
		cronTracker.Handle(ev)
	})

	// Start scheduler + watcher through the engine.
	if startErr := eng.Start(); startErr != nil {
		logger.Warnf("Engine start failed: %v", startErr)
	}

	// Register allowlist handler so "始终允许" adds a rule to the session allowlist.
	m.SetAllowlistHandler(func(toolName string, params map[string]interface{}) {
		if pm := agentInstance.GetPermissionManager(); pm != nil {
			rules := permission.BuildAllowlistRules(toolName, params)
			for _, rule := range rules {
				pm.GetSessionAllowlist().AddRule(rule)
				// Persist the rule to disk
				if _, err := allowlistStore.AddRuleForWorkdir(cwd, rule.RawPattern); err != nil {
					logger.Warnf("Failed to persist allowlist rule %q: %v", rule.RawPattern, err)
				}
			}
		}
	})

	// Wire permission manager and tool names into the Bubble Tea model
	m.SetPermissionManager(agentInstance.GetPermissionManager())
	// Wire persistent allowlist store for /disallow cleanup
	m.SetPersistentAllowlist(allowlistStore, cwd)
	// Wire engine so /think command works
	m.SetEngine(eng)
	// Wire new session handler so Ctrl+R / /clear work
	m.SetNewSessionHandler(func() string {
		return agentInstance.StartNewSession()
	})
	allTools := agentInstance.GetToolbox().List()
	toolNames := make([]string, 0, len(allTools))
	for _, t := range allTools {
		toolNames = append(toolNames, t.Name())
	}
	m.SetAvailableToolNames(toolNames)

	// Stream bridge: forward agent events to Bubble Tea
	var cancelFn context.CancelFunc
	go func() {
		for {
			select {
			case txt := <-submitCh:
				// Start processing
				ctx := context.Background()
				ctx, cancelFn = context.WithCancel(ctx)
				go func(in string) {
					_ = agentInstance.ProcessStream(ctx, in, func(se event.StreamEvent) {
						eventStore.Add(se)
						// Forward each event into Bubble Tea synchronously to preserve
						// ordering. This is safe because we are NOT inside Update()
						// (the deadlock is prevented by building async tea.Cmd for
						// confirmation callbacks instead).
						switch se.Type {
						case event.EventTypeCronTaskStarted, event.EventTypeCronTaskFinished, event.EventTypeCronTaskProgress:
							return
						case event.EventTypeStreamContent:
							p.Send(bubbletea.Message{Role: "assistant_stream", Content: se.Content})
						case event.EventTypeContent:
							if se.Source != "llm_client" {
								p.Send(bubbletea.Message{Role: "assistant_stream", Content: se.Content})
							}
						case event.EventTypeError:
							p.Send(bubbletea.Message{Role: "error", Content: se.Error})
						case event.EventTypeThinking:
							p.Send(bubbletea.ThinkingMsg{
								Title:          se.Content,
								Reasoning:      se.Reasoning,
								ReasoningDelta: se.ReasoningDelta,
								Metadata:       se.Metadata,
							})
						case event.EventTypeToolUse:
							toolName := se.ToolUse.ToolName
							params := se.ToolUse.Parameters
							result := se.ToolUse.Result

							var content string
							if result == "" {
								content = fmt.Sprintf("正在调用工具: %s\n参数: %v", toolName, params)
							} else {
								// Truncate result for display, handling multi-byte characters
								runes := []rune(result)
								if len(runes) > 500 {
									result = string(runes[:500]) + "..."
								}
								content = fmt.Sprintf("工具: %s 调用完成\n结果: %s", toolName, result)
							}

							p.Send(bubbletea.Message{Role: "tool", Content: content})
						case event.EventTypeTokenStats:
							if se.TokenStats != nil {
								p.Send(buildTokenStatsUpdate(se.TokenStats))
							}
						case event.EventTypeDone:
							p.Send(bubbletea.StatusUpdate("完成"))
						case event.EventTypeCompression:
							orig := se.Metadata["original_tokens"]
							cmp := se.Metadata["compressed_tokens"]
							ratio := se.Metadata["compression_ratio"]
							before := se.Metadata["messages_before"]
							after := se.Metadata["messages_after"]
							trigger := se.Metadata["triggered_by"]
							summary := se.Metadata["summary_full"]
							content := fmt.Sprintf("🗜️ 上下文压缩: %v → %v tokens (减少 %.2f%%)\n消息数: %v → %v，触发: %v\n摘要: %v",
								orig, cmp, (1.0-float64FromAny(ratio))*100, before, after, trigger, truncatePreview(summary))
							p.Send(bubbletea.Message{Role: "system", Content: content})
						}
					})
				}(txt)
			case <-cancelCh:
				if cancelFn != nil {
					cancelFn()
				}
			}
		}
	}()

	// Auto-run initial command if provided
	if len(args) > 0 {
		initial := strings.Join(args, " ")
		go func() { submitCh <- initial }()
	}

	// Run program (blocks)
	if _, err := p.Run(); err != nil {
		color.Red("Bubble Tea TUI error: %v", err)
		return fmt.Errorf("Bubble Tea TUI error: %w", err)
	}

	color.Green("👋 Bubble Tea TUI session ended")
	return nil
}

func buildTokenStatsUpdate(stats *event.TokenStats) bubbletea.TokenStatsUpdate {
	if stats == nil {
		return bubbletea.TokenStatsUpdate{}
	}
	return bubbletea.TokenStatsUpdate{
		InputTokens:       stats.InputTokens,
		OutputTokens:      stats.OutputTokens,
		TotalTokens:       stats.TotalTokens,
		Peak:              stats.PeakTokensPerSecond,
		ContextWindowMax:  stats.ContextWindowMax,
		ContextUsedTokens: stats.ContextUsedTokens,
	}
}
