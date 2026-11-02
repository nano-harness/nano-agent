package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/daemon"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// ClientFactory is a function that returns a daemon client
type ClientFactory func() *daemon.Client

// NewClientCommand creates the client command for daemon communication
func NewClientCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "client",
		Short: "Communicate with nano daemon",
		Long:  `Send commands and requests to a running nano daemon process.`,
	}

	// Add client subcommands
	cmd.AddCommand(newClientExecCommand(nil))
	cmd.AddCommand(newClientStatusCommand(nil))
	cmd.AddCommand(newClientSessionsCommand(nil))
	cmd.AddCommand(newClientMCPCommand(nil))
	cmd.AddCommand(newClientMemoryCommand(nil))

	return cmd
}

// newClientExecCommand creates the exec subcommand
func newClientExecCommand(factory ClientFactory) *cobra.Command {
	if factory == nil {
		factory = createDaemonClient
	}
	cmd := &cobra.Command{
		Use:   "exec [command]",
		Short: "Execute command on daemon",
		Long:  `Execute a command on the running nano daemon and return the result.`,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command := strings.Join(args, " ")
			timeout, _ := cmd.Flags().GetInt("timeout")
			includeSteps, _ := cmd.Flags().GetBool("include-steps")
			sessionID, _ := cmd.Flags().GetString("session-id")
			background, _ := cmd.Flags().GetBool("background")

			client := factory()

			color.Blue("🚀 Executing command: %s", command)
			if background {
				response, err := client.ExecuteInSession(command, sessionID, timeout, includeSteps, true)
				if err != nil {
					return fmt.Errorf("failed to execute command: %w", err)
				}
				if response.Success {
					color.Green("✅ Session execution started in background")
					if response.SessionID != "" {
						logger.Infof("Session ID: %s", response.SessionID)
						fmt.Printf("Session started: %s\n", response.SessionID)
						fmt.Println("Use 'nano client sessions list' to check status")
					}
					return nil
				}
				if response.Error != "" {
					return fmt.Errorf("command failed: %s", response.Error)
				}
				return fmt.Errorf("command failed")
			}
			response, err := client.ExecuteInSession(command, sessionID, timeout, includeSteps, false)
			if err != nil {
				return fmt.Errorf("failed to execute command: %w", err)
			}

			if response.Success {
				color.Green("✅ Command executed successfully")
				if response.SessionID != "" {
					logger.Infof("Session ID: %s", response.SessionID)
				}
				if includeSteps && len(response.Steps) > 0 {
					fmt.Println("\n--- Steps ---")
					// Pretty print steps JSON minimally
					b, _ := json.MarshalIndent(response.Steps, "", "  ")
					fmt.Println(string(b))
				}
				// Print token usage if present
				if response.TokenStats != nil {
					ts := response.TokenStats
					fmt.Println("\n--- Token Usage ---")
					logger.Infof("Input: %d  Output: %d  Total: %d", ts.InputTokens, ts.OutputTokens, ts.TotalTokens)
					if ts.SessionTotalTokens > 0 {
						logger.Infof("Session Total: %d (In: %d, Out: %d)", ts.SessionTotalTokens, ts.SessionInputTokens, ts.SessionOutputTokens)
					}
				}
				if response.Result != "" {
					fmt.Println("\n--- Result ---")
					fmt.Println(response.Result)
				}
				return nil
			} else {
				if response.Error != "" {
					return fmt.Errorf("command failed: %s", response.Error)
				}
				return fmt.Errorf("command failed")
			}
		},
	}

	cmd.Flags().IntP("timeout", "t", daemon.DefaultTaskTimeoutSeconds, "Command timeout in seconds")
	cmd.Flags().Bool("include-steps", false, "Include streamed steps (tool calls/results) in the HTTP response")
	cmd.Flags().String("session-id", "", "Execute within an existing session ID")
	cmd.Flags().BoolP("background", "b", false, "Execute command in background")

	return cmd
}

func newClientSessionsCommand(factory ClientFactory) *cobra.Command {
	if factory == nil {
		factory = createDaemonClient
	}
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Session management",
		Long:  `List, inspect, cancel, delete, and reset sessions on the daemon.`,
	}

	cmd.AddCommand(newClientSessionsListCommand(factory))
	cmd.AddCommand(newClientSessionsGetCommand(factory))
	cmd.AddCommand(newClientSessionsCancelCommand(factory))
	cmd.AddCommand(newClientSessionsDeleteCommand(factory))
	cmd.AddCommand(newClientSessionsResetCommand(factory))

	return cmd
}

func newClientSessionsListCommand(factory ClientFactory) *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List sessions and tasks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := factory()
			limit, _ := cmd.Flags().GetInt("limit")

			resp, err := client.ListSessions(limit)
			if err != nil {
				color.Red("❌ Failed to list sessions: %v", err)
				return fmt.Errorf("failed to list sessions: %w", err)
			}

			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}

	listCmd.Flags().Int("limit", 0, "Limit the number of returned items")
	return listCmd
}

func newClientSessionsGetCommand(factory ClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "get [id]",
		Short: "Get a session detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			client := factory()
			resp, err := client.GetSession(args[0])
			if err != nil {
				color.Red("❌ Failed to get session: %v", err)
				return fmt.Errorf("failed to get session: %w", err)
			}
			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}
}

func newClientSessionsCancelCommand(factory ClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel [id]",
		Short: "Cancel a running session by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			client := factory()
			resp, err := client.CancelSession(args[0])
			if err != nil {
				color.Red("❌ Failed to cancel: %v", err)
				return fmt.Errorf("failed to cancel: %w", err)
			}
			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}
}

func newClientSessionsDeleteCommand(factory ClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete a session or task by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			client := factory()
			resp, err := client.DeleteSession(args[0])
			if err != nil {
				color.Red("❌ Failed to delete: %v", err)
				return fmt.Errorf("failed to delete: %w", err)
			}
			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}
}

func newClientSessionsResetCommand(factory ClientFactory) *cobra.Command {
	resetCmd := &cobra.Command{
		Use:   "reset [session_id]",
		Short: "Reset a session history",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(_ *cobra.Command, args []string) error {
			client := factory()
			sessionID := ""
			if len(args) == 1 {
				sessionID = args[0]
			}

			resp, err := client.ResetSession(sessionID)
			if err != nil {
				color.Red("❌ Failed to reset session: %v", err)
				return fmt.Errorf("failed to reset session: %w", err)
			}
			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}

	return resetCmd
}

// newClientStatusCommand creates the status subcommand
func newClientStatusCommand(factory ClientFactory) *cobra.Command {
	if factory == nil {
		factory = createDaemonClient
	}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get daemon status",
		Long:  `Get status information from the running nano daemon.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			client := factory()

			// Get health
			health, err := client.Health()
			if err != nil {
				color.Red("❌ Failed to get daemon health: %v", err)
				return fmt.Errorf("failed to get daemon health: %w", err)
			}

			// Get status
			status, err := client.Status()
			if err != nil {
				color.Red("❌ Failed to get daemon status: %v", err)
				return fmt.Errorf("failed to get daemon status: %w", err)
			}

			fmt.Println("=== Nano Daemon Status ===")
			color.Green("Health: %s", health.Status)
			logger.Infof("Version: %s", health.Version)
			logger.Infof("Uptime: %.2f seconds", health.Uptime)
			logger.Infof("Agent Status: %s", status.AgentStatus)
			logger.Infof("MCP Enabled: %v", status.MCPEnabled)
			logger.Infof("Memory Size: %d", status.MemorySize)
			logger.Infof("Active Tools: %d", status.ActiveTools)
			return nil
		},
	}

	return cmd
}

// newClientMCPCommand creates the MCP subcommand
func newClientMCPCommand(factory ClientFactory) *cobra.Command {
	if factory == nil {
		factory = createDaemonClient
	}
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP related commands",
		Long:  `Interact with MCP functionality through the daemon.`,
	}

	// MCP status
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Get MCP status",
		RunE: func(_ *cobra.Command, _ []string) error {
			client := factory()

			response, err := client.MCPStatus()
			if err != nil {
				color.Red("❌ Failed to get MCP status: %v", err)
				return fmt.Errorf("failed to get MCP status: %w", err)
			}

			fmt.Println("=== MCP Status ===")
			logger.Infof("Enabled: %v", response.Enabled)
			logger.Infof("Servers: %d", response.Servers)
			logger.Infof("Tools: %d", response.Tools)
			logger.Infof("Connections: %d", len(response.Connections))
			return nil
		},
	})

	// MCP tools
	cmd.AddCommand(&cobra.Command{
		Use:   "tools",
		Short: "List MCP tools",
		RunE: func(_ *cobra.Command, _ []string) error {
			client := factory()

			response, err := client.MCPTools()
			if err != nil {
				color.Red("❌ Failed to get MCP tools: %v", err)
				return fmt.Errorf("failed to get MCP tools: %w", err)
			}

			fmt.Println("=== MCP Tools ===")
			if len(response.Tools) == 0 {
				color.Yellow("No MCP tools available")
			} else {
				data, _ := json.MarshalIndent(response.Tools, "", "  ")
				fmt.Println(string(data))
			}
			return nil
		},
	})

	// MCP diagnostics
	cmd.AddCommand(&cobra.Command{
		Use:   "diagnostics",
		Short: "Get MCP diagnostics",
		RunE: func(_ *cobra.Command, _ []string) error {
			client := factory()

			response, err := client.MCPDiagnostics()
			if err != nil {
				color.Red("❌ Failed to get MCP diagnostics: %v", err)
				return fmt.Errorf("failed to get MCP diagnostics: %w", err)
			}

			fmt.Println("=== MCP Diagnostics ===")
			data, _ := json.MarshalIndent(response, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	})

	return cmd
}

// newClientMemoryCommand creates the memory subcommand
func newClientMemoryCommand(factory ClientFactory) *cobra.Command {
	if factory == nil {
		factory = createDaemonClient
	}
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Memory related commands",
		Long:  `Interact with memory functionality through the daemon.`,
	}

	// List memory
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List sessions",
		RunE: func(_ *cobra.Command, _ []string) error {
			client := factory()

			response, err := client.ListMemory()
			if err != nil {
				color.Red("❌ Failed to list memory: %v", err)
				return fmt.Errorf("failed to list memory: %w", err)
			}

			fmt.Println("=== Memory Entries ===")
			logger.Infof("Total: %d entries", response.Count)

			if len(response.Entries) == 0 {
				color.Yellow("No memory entries found")
			} else {
				data, _ := json.MarshalIndent(response.Entries, "", "  ")
				fmt.Println(string(data))
			}
			return nil
		},
	})

	// Save memory
	cmd.AddCommand(&cobra.Command{
		Use:   "save [key] [content]",
		Short: "Save memory entry",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			client := factory()

			key := args[0]
			content := args[1]
			tags := []string{} // Could add flag for this

			response, err := client.SaveMemory(key, content, tags)
			if err != nil {
				color.Red("❌ Failed to save memory: %v", err)
				return fmt.Errorf("failed to save memory: %w", err)
			}

			if response.Success {
				color.Green("✅ Memory saved: %s", response.Key)
			} else {
				color.Red("❌ Failed to save memory entry")
			}
			return nil
		},
	})

	// Get memory
	cmd.AddCommand(&cobra.Command{
		Use:   "get [key]",
		Short: "Get memory entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			client := factory()

			key := args[0]
			response, err := client.GetMemory(key)
			if err != nil {
				color.Red("❌ Failed to get memory: %v", err)
				return fmt.Errorf("failed to get memory: %w", err)
			}

			if response.Found {
				logger.Infof("Key: %s", response.Key)
				logger.Infof("Content: %s", response.Content)
			} else {
				color.Yellow("Memory entry not found: %s", key)
			}
			return nil
		},
	})

	// Delete memory
	cmd.AddCommand(&cobra.Command{
		Use:   "delete [key]",
		Short: "Delete memory entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			client := factory()

			key := args[0]
			response, err := client.DeleteMemory(key)
			if err != nil {
				color.Red("❌ Failed to delete memory: %v", err)
				return fmt.Errorf("failed to delete memory: %w", err)
			}

			if response.Success {
				color.Green("✅ Memory deleted: %s", response.Key)
			} else {
				color.Red("❌ Failed to delete memory entry")
			}
			return nil
		},
	})

	return cmd
}

// createDaemonClient creates a daemon client using default configuration
func createDaemonClient() *daemon.Client {
	// First try to get daemon config from main config
	mainConfig := config.Get()
	if mainConfig != nil && mainConfig.Daemon != nil {
		// Use daemon config from main config file
		return daemon.NewClientFromConfig(mainConfig.Daemon)
	}

	// Fallback to daemon manager for backward compatibility
	manager := daemon.NewManager()
	if err := manager.LoadConfig(); err != nil {
		// Use default config if loading fails
		color.Yellow("⚠️  Using default daemon configuration")
	}

	daemonConfig := manager.GetConfig()
	return daemon.NewClientFromConfig(daemonConfig)
}
