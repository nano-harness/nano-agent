package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/daemon"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// NewDaemonCommand creates the daemon command
func NewDaemonCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage nano daemon process",
		Long:  `Start, stop, and manage the nano agent as a background daemon process.`,
	}

	// Add daemon subcommands
	cmd.AddCommand(newDaemonStartCommand())
	cmd.AddCommand(newDaemonStopCommand())
	cmd.AddCommand(newDaemonRestartCommand())
	cmd.AddCommand(newDaemonStatusCommand())
	cmd.AddCommand(newDaemonLogsCommand())
	cmd.AddCommand(newDaemonConfigCommand())
	cmd.AddCommand(newDaemonCleanupLegacyCommand())

	// Add special foreground command (used internally)
	cmd.AddCommand(newDaemonForegroundCommand())

	return cmd
}

// newDaemonStartCommand creates daemon start command
func newDaemonStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start nano daemon in background",
		Long:  `Start the nano agent as a background daemon process.`,
		Run: func(_ *cobra.Command, _ []string) {
			manager := daemon.NewManager()

			// Load configuration
			if err := manager.LoadConfig(); err != nil {
				logger.Warnf("Failed to load daemon config, using defaults: %v", err)
			}

			// Start daemon
			if err := manager.Start(true); err != nil {
				color.Red("❌ Failed to start daemon: %v", err)
				os.Exit(1)
			}

			config := manager.GetConfig()
			color.Green("✅ Nano daemon started successfully")
			color.Blue("   Listen address: %s:%d", config.Host, config.Port)
			color.Blue("   PID file: %s", config.PidFile)
			if config.LogFile != "" {
				color.Blue("   Log file: %s", config.LogFile)
			}
		},
	}

	return cmd
}

// newDaemonStopCommand creates daemon stop command
func newDaemonStopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop nano daemon",
		Long:  `Stop the running nano daemon process.`,
		Run: func(_ *cobra.Command, _ []string) {
			manager := daemon.NewManager()

			// Load configuration to respect custom pid_file/log_file
			if err := manager.LoadConfig(); err != nil {
				logger.Warnf("Failed to load daemon config, using defaults: %v", err)
			}

			if !manager.IsRunning() {
				color.Yellow("⚠️  Daemon is not running")
				return
			}

			if err := manager.Stop(); err != nil {
				color.Red("❌ Failed to stop daemon: %v", err)
				os.Exit(1)
			}

			color.Green("✅ Nano daemon stopped successfully")
		},
	}

	return cmd
}

// newDaemonRestartCommand creates daemon restart command
func newDaemonRestartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart nano daemon",
		Long:  `Restart the nano daemon process.`,
		Run: func(_ *cobra.Command, _ []string) {
			manager := daemon.NewManager()

			// Load configuration
			if err := manager.LoadConfig(); err != nil {
				logger.Warnf("Failed to load daemon config, using defaults: %v", err)
			}

			if err := manager.Restart(); err != nil {
				color.Red("❌ Failed to restart daemon: %v", err)
				os.Exit(1)
			}

			config := manager.GetConfig()
			color.Green("✅ Nano daemon restarted successfully")
			color.Blue("   Listen address: %s:%d", config.Host, config.Port)
		},
	}

	return cmd
}

// newDaemonStatusCommand creates daemon status command
func newDaemonStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		Long:  `Display status information about the nano daemon process.`,
		Run: func(_ *cobra.Command, _ []string) {
			manager := daemon.NewManager()

			// Load configuration to ensure correct pid_file is used
			if err := manager.LoadConfig(); err != nil {
				logger.Warnf("Failed to load daemon config, using defaults: %v", err)
			}

			status, err := manager.Status()
			if err != nil {
				color.Red("❌ Failed to get daemon status: %v", err)
				os.Exit(1)
			}

			fmt.Println("=== Nano Daemon Status ===")

			if status.Running {
				color.Green("Status: ✅ Running")
				color.Blue("PID: %d", status.PID)
			} else {
				color.Yellow("Status: ⚠️  Stopped")
			}

			logger.Infof("Listen Address: %s:%d", status.Config.Host, status.Config.Port)
			logger.Infof("PID File: %s", status.PidFile)

			if status.Config.LogFile != "" {
				logger.Infof("Log File: %s", status.Config.LogFile)
			}

			if status.Config.APIKey != "" {
				logger.Infof("API Key: %s", "*** (configured)")
			} else {
				logger.Infof("API Key: %s", "(not configured)")
			}

			logger.Infof("CORS Enabled: %v", status.Config.EnableCORS)

			if status.Config.TLSCertFile != "" {
				logger.Infof("TLS: Enabled (%s)", status.Config.TLSCertFile)
			} else {
				logger.Info("TLS: Disabled")
			}

			logger.Infof("Last Check: %s", status.Timestamp.Format("2006-01-02 15:04:05"))
		},
	}

	// Add JSON output flag
	cmd.Flags().BoolP("json", "j", false, "Output status in JSON format")

	return cmd
}

// newDaemonLogsCommand creates daemon logs command
func newDaemonLogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show daemon logs",
		Long:  `Display recent logs from the nano daemon process.`,
		Run: func(cmd *cobra.Command, _ []string) {
			manager := daemon.NewManager()

			// Load configuration to read from configured log_file
			if err := manager.LoadConfig(); err != nil {
				logger.Warnf("Failed to load daemon config, using defaults: %v", err)
			}

			lines, _ := cmd.Flags().GetInt("lines")
			follow, _ := cmd.Flags().GetBool("follow")

			if follow {
				color.Yellow("⚠️  Follow mode not implemented yet, showing recent logs")
			}

			logs, err := manager.Logs(lines)
			if err != nil {
				color.Red("❌ Failed to get daemon logs: %v", err)
				os.Exit(1)
			}

			if len(logs) == 0 {
				color.Yellow("📝 No logs available")
				return
			}

			logger.Infof("=== Recent Daemon Logs (last %d lines) ===", len(logs))
			for _, line := range logs {
				logger.Info(strings.TrimSuffix(line, "\n"))
			}
		},
	}

	cmd.Flags().IntP("lines", "n", 50, "Number of lines to show")
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")

	return cmd
}

// newDaemonConfigCommand creates daemon config command
func newDaemonConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage daemon configuration",
		Long:  `View and modify nano daemon configuration.`,
	}

	// Add config subcommands
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current daemon configuration",
		Run: func(_ *cobra.Command, _ []string) {
			manager := daemon.NewManager()
			if err := manager.LoadConfig(); err != nil {
				color.Red("❌ Failed to load daemon config: %v", err)
				os.Exit(1)
			}

			config := manager.GetConfig()
			data, _ := json.MarshalIndent(config, "", "  ")
			fmt.Println(string(data))
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set daemon configuration value",
		Args:  cobra.ExactArgs(2),
		Run: func(_ *cobra.Command, args []string) {
			manager := daemon.NewManager()
			if err := manager.LoadConfig(); err != nil {
				color.Red("❌ Failed to load daemon config: %v", err)
				os.Exit(1)
			}

			key := args[0]
			value := args[1]
			config := manager.GetConfig()

			// Update configuration based on key
			switch key {
			case "port":
				if port, err := strconv.Atoi(value); err == nil {
					config.Port = port
				} else {
					color.Red("❌ Invalid port number: %s", value)
					os.Exit(1)
				}
			case "host":
				config.Host = value
			case "api_key":
				config.APIKey = value
			case "enable_cors":
				config.EnableCORS = value == "true"
			case "log_file":
				config.LogFile = value
			case "tls_cert_file":
				config.TLSCertFile = value
			case "tls_key_file":
				config.TLSKeyFile = value
			case "pid_file":
				config.PidFile = value
			default:
				color.Red("❌ Unknown configuration key: %s", key)
				os.Exit(1)
			}

			if err := manager.UpdateConfig(config); err != nil {
				color.Red("❌ Failed to save daemon config: %v", err)
				os.Exit(1)
			}

			color.Green("✅ Configuration updated: %s = %s", key, value)
		},
	})

	return cmd
}

func newDaemonCleanupLegacyCommand() *cobra.Command {
	var apply bool
	var local bool
	var includeOSS bool

	cmd := &cobra.Command{
		Use:   "cleanup-legacy",
		Short: "Clean legacy task data",
		Long:  "Clean legacy local/OSS task artifacts left from older task-based implementations. Defaults to dry-run.",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := config.Get()

			if local {
				paths := legacyTaskPaths()
				existing := make([]string, 0, len(paths))
				for _, p := range paths {
					if p == "" {
						continue
					}
					if st, err := os.Stat(p); err == nil && st.IsDir() {
						existing = append(existing, p)
					}
				}

				if len(existing) == 0 {
					color.Blue("Local: no legacy task directories found")
				} else {
					color.Yellow("Local legacy task directories:")
					for _, p := range existing {
						fmt.Println(" - " + p)
					}
					if apply {
						for _, p := range existing {
							if err := os.RemoveAll(p); err != nil {
								color.Red("❌ Failed to delete %s: %v", p, err)
								os.Exit(1)
							}
						}
						color.Green("✅ Local legacy task directories deleted")
					} else {
						color.Blue("Local: dry-run (use --apply to delete)")
					}
				}
			}

			if includeOSS {
				if cfg == nil || cfg.OSS == nil || !cfg.OSS.Enabled {
					color.Blue("OSS: not enabled, skipping")
					return
				}
				ossCfg := cfg.OSS
				if strings.TrimSpace(ossCfg.DefaultBucket) == "" {
					color.Blue("OSS: default bucket not configured, skipping")
					return
				}

				client, err := oss.New(ossCfg.NormalizedEndpoint(), ossCfg.AccessKeyID, ossCfg.AccessKeySecret)
				if err != nil {
					color.Red("❌ OSS: failed to create client: %v", err)
					os.Exit(1)
				}
				bucket, err := client.Bucket(ossCfg.DefaultBucket)
				if err != nil {
					color.Red("❌ OSS: failed to get bucket: %v", err)
					os.Exit(1)
				}

				prefix := "tasks/"
				var keys []string
				marker := ""
				for {
					lor, err := bucket.ListObjects(oss.Prefix(prefix), oss.Marker(marker), oss.MaxKeys(1000))
					if err != nil {
						color.Red("❌ OSS: list objects failed: %v", err)
						os.Exit(1)
					}
					for _, obj := range lor.Objects {
						if obj.Key != "" {
							keys = append(keys, obj.Key)
						}
					}
					if !lor.IsTruncated {
						break
					}
					marker = lor.NextMarker
				}

				if len(keys) == 0 {
					color.Blue("OSS: no legacy task objects found")
					return
				}

				color.Yellow("OSS legacy task objects: %d", len(keys))
				preview := 20
				if len(keys) < preview {
					preview = len(keys)
				}
				for i := 0; i < preview; i++ {
					fmt.Println(" - " + keys[i])
				}
				if len(keys) > preview {
					fmt.Printf(" ... (%d more)\n", len(keys)-preview)
				}

				if !apply {
					color.Blue("OSS: dry-run (use --apply to delete)")
					return
				}

				deleted := 0
				for _, k := range keys {
					if err := bucket.DeleteObject(k); err != nil {
						color.Red("❌ OSS: failed to delete %s: %v", k, err)
						os.Exit(1)
					}
					deleted++
				}
				color.Green("✅ OSS legacy task objects deleted: %d", deleted)
			}
		},
	}

	cmd.Flags().BoolVar(&apply, "apply", false, "Actually delete legacy data (default: dry-run)")
	cmd.Flags().BoolVar(&local, "local", true, "Clean local legacy task directories")
	cmd.Flags().BoolVar(&includeOSS, "oss", true, "Clean legacy task objects in OSS (prefix tasks/)")

	return cmd
}

func legacyTaskPaths() []string {
	paths := []string{}
	if runtime.GOOS == "darwin" {
		paths = append(paths, "/tmp/nano-agent/tasks")
	} else {
		paths = append(paths, "/tmp/nano-agent/tasks")
	}

	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		paths = append(paths, filepath.Join(home, ".nano", "tasks"))
	}
	return paths
}

// newDaemonForegroundCommand creates the internal foreground command
func newDaemonForegroundCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "foreground",
		Hidden: true, // Hide from help, used internally
		Short:  "Run daemon in foreground (internal use)",
		Run: func(_ *cobra.Command, _ []string) {
			runDaemonForeground()
		},
	}

	return cmd
}

// runDaemonForeground runs the daemon server in foreground mode
func runDaemonForeground() {
	// Initialize config
	cfg := config.Get()
	if cfg == nil {
		logger.Errorf("Configuration not initialized")
		os.Exit(1)
	}

	// Create daemon manager to get config (includes daemon log_file)
	manager := daemon.NewManager()
	if err := manager.LoadConfig(); err != nil {
		logger.Warnf("Failed to load daemon config, using defaults: %v", err)
	}

	// Configure unified logging for daemon mode before creating Agent/Server
	daemonCfg := cfg.Daemon
	if daemonCfg == nil {
		daemonCfg = manager.GetConfig()
	}

	if daemonCfg != nil {
		home, _ := os.UserHomeDir()
		configDir := filepath.Join(home, ".nano")
		if strings.TrimSpace(daemonCfg.PidFile) == "" {
			daemonCfg.PidFile = filepath.Join(configDir, "daemon.pid")
		}
		if strings.TrimSpace(daemonCfg.LogFile) == "" {
			daemonCfg.LogFile = filepath.Join(configDir, "daemon.log")
		}
		if runtime.GOOS == "darwin" {
			if strings.Contains(daemonCfg.PidFile, "/home/ubuntu") {
				daemonCfg.PidFile = filepath.Join(configDir, filepath.Base(daemonCfg.PidFile))
			}
			if strings.Contains(daemonCfg.LogFile, "/home/ubuntu") {
				daemonCfg.LogFile = filepath.Join(configDir, filepath.Base(daemonCfg.LogFile))
			}
		}
	}

	logPath := ""
	if daemonCfg != nil {
		logPath = daemonCfg.LogFile
	}
	// If running under systemd, journald already captures stderr. Still keep console duplication
	// unless explicitly disabled via env NANO_DAEMON_CONSOLE_LOG=false
	alsoConsole := true
	if v := os.Getenv("NANO_DAEMON_CONSOLE_LOG"); v == "false" || v == "0" { // allow override
		alsoConsole = false
	}
	logger.ConfigureForDaemon(logPath, alsoConsole)

	// Apply secure defaults for daemon mode (hardened environment)
	// Only set values when not explicitly configured by the user where possible
	// 1) Env filtering: in daemon mode, do NOT enforce strict allowlist; only block sensitive env vars
	// (Previously we set a minimal allowed env list and Strict=true; this is intentionally disabled per policy.)
	cfg.Strict = false
	// 2) Block common sensitive environment variables from leaking to tools
	defaultBlockedEnv := []string{
		"API_KEY", "NANO_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "AZURE_OPENAI_API_KEY",
		"HUGGINGFACEHUB_API_TOKEN", "SERPER_API_KEY", "TAVILY_API_KEY",
		"GITHUB_TOKEN", "GH_TOKEN", "NPM_TOKEN", "NODE_AUTH_TOKEN",
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"TWILIO_AUTH_TOKEN", "STRIPE_API_KEY", "SLACK_BOT_TOKEN", "DISCORD_TOKEN",
		"DATABASE_URL", "POSTGRES_PASSWORD", "MYSQL_PWD", "REDIS_PASSWORD", "KAFKA_SASL_PASSWORD",
	}
	// merge without duplicates
	blockedSet := make(map[string]struct{}, len(cfg.BlockedEnvVars))
	for _, k := range cfg.BlockedEnvVars {
		blockedSet[strings.ToUpper(k)] = struct{}{}
	}
	for _, k := range defaultBlockedEnv {
		uk := strings.ToUpper(k)
		if _, ok := blockedSet[uk]; !ok {
			cfg.BlockedEnvVars = append(cfg.BlockedEnvVars, k)
			blockedSet[uk] = struct{}{}
		}
	}

	// 3) Add recommended blocked shell commands by default (no allowlist in daemon mode)
	// If user already configured BlockedCommands, we will merge to ensure a secure baseline.
	defaultBlockedCommands := []string{
		// High-risk destructive and privilege commands
		"rm", "rmdir", "del", "deltree",
		"format", "fdisk", "dd",
		"sudo", "su", "chmod", "chown",
		"passwd", "useradd", "userdel",
		"shutdown", "reboot", "halt",
		// Network and remote transfer tools
		"curl", "wget", "nc", "netcat",
		"ssh", "scp", "rsync",
		// Containers and services
		"docker", "podman",
		"systemctl", "service",
		// Filesystem mounts
		"mount", "umount",
		// Scheduled tasks
		"crontab",
	}
	blockedCmdSet := make(map[string]struct{}, len(cfg.BlockedCommands))
	for _, c := range cfg.BlockedCommands {
		blockedCmdSet[c] = struct{}{}
	}
	for _, c := range defaultBlockedCommands {
		if _, ok := blockedCmdSet[c]; !ok {
			cfg.BlockedCommands = append(cfg.BlockedCommands, c)
			blockedCmdSet[c] = struct{}{}
		}
	}

	// 4) Minimal tool access by default in daemon: disable high-risk tools
	// defaultDisabled := []string{"run_shell_command", "web_fetch", "web_search", "git_manager", "oss_manager", "engineering_tools"}
	// disabledSet := make(map[string]struct{}, len(cfg.DisabledTools))
	// for _, t := range cfg.DisabledTools {
	//     disabledSet[t] = struct{}{}
	// }
	// for _, t := range defaultDisabled {
	//     if _, ok := disabledSet[t]; !ok {
	//         cfg.DisabledTools = append(cfg.DisabledTools, t)
	//         disabledSet[t] = struct{}{}
	//     }
	// }

	// Mark as daemon mode to use LocalSessionStorage instead of ProjectSessionStorage
	cfg.IsDaemon = true

	// Load persistent allowlist for current workdir and merge into cfg.
	allowlistPath, _ := permission.DefaultPersistentAllowlistPath()
	allowlistStore := permission.NewPersistentAllowlistStore(allowlistPath)
	if err := allowlistStore.Load(); err != nil {
		logger.Warnf("Failed to load persistent allowlist: %v", err)
	}
	cwd, _ := os.Getwd()
	for _, raw := range allowlistStore.RulesForWorkdir(cwd) {
		cfg.AllowedRules = append(cfg.AllowedRules, raw)
	}

	// Create engine instance
	eng, err := engine.New(cfg, nil)
	if err != nil {
		logger.Errorf("Failed to create engine: %v", err)
		os.Exit(1)
	}

	defer func() {
		if err := eng.Shutdown(); err != nil {
			logger.Errorf("Engine shutdown error: %v", err)
		}
	}()

	// Create and start daemon server
	server := daemon.NewServerWithEngine(eng, daemonCfg)

	logger.Info("Starting nano daemon in foreground mode...")

	// Start the engine (scheduler + watcher)
	if startErr := eng.Start(); startErr != nil {
		logger.Warnf("Engine start failed: %v", startErr)
	}

	if err := server.Start(); err != nil {
		logger.Errorf("Daemon server error: %v", err)
		os.Exit(1)
	}
}
