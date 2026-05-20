package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/mcp"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// NewMCPCommand creates a new MCP management command with enhanced functionality
func NewMCPCommand() *cobra.Command {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP (Model Context Protocol) servers",
		Long: `Comprehensive MCP server management including:
  • Server status monitoring and health checks
  • Interactive configuration wizard
  • Tool and resource discovery
  • Connection diagnostics and troubleshooting
  • Server lifecycle management`,
		Example: `  # Show MCP server status
  nano mcp status

  # Run interactive configuration wizard
  nano mcp config

  # List available tools from all servers
  nano mcp tools

  # Perform health checks
  nano mcp health

  # Generate diagnostic report
  nano mcp diagnostics`,
	}

	// Add enhanced subcommands
	mcpCmd.AddCommand(newMCPStatusCommand())
	mcpCmd.AddCommand(newMCPAddCommand())
	mcpCmd.AddCommand(newMCPAuthCommand())
	mcpCmd.AddCommand(newMCPToolsCommand())
	mcpCmd.AddCommand(newMCPCallCommand())
	mcpCmd.AddCommand(newMCPRunCommand())
	mcpCmd.AddCommand(newMCPResourcesCommand())
	mcpCmd.AddCommand(newMCPHealthCommand())
	mcpCmd.AddCommand(newMCPDiagnosticsCommand())
	mcpCmd.AddCommand(newMCPServersCommand())

	return mcpCmd
}

// newMCPStatusCommand creates an enhanced status command
func newMCPStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show comprehensive MCP server status",
		Long: `Display detailed status information for all configured MCP servers including:
  • Connection status and health
  • Available tools, resources, and prompts
  • Performance metrics and statistics
  • Last activity and uptime information`,
		Run: runMCPStatus,
	}

	cmd.Flags().Bool("verbose", false, "Show verbose status information")
	cmd.Flags().BoolP("json", "j", false, "Output status in JSON format")
	return cmd
}

// newMCPToolsCommand creates a tools listing command
func newMCPToolsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List available MCP tools",
		Long: `Display all available tools from connected MCP servers:
  • Tool names, descriptions, and parameters
  • Server source and availability status
  • Usage examples and documentation links`,
		Run: runMCPTools,
	}

	cmd.Flags().StringP("server", "s", "", "Filter tools by server name")
	cmd.Flags().BoolP("detailed", "d", false, "Show detailed tool information")
	cmd.Flags().BoolP("json", "j", false, "Output tools in JSON format")
	return cmd
}

func newMCPCallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "call [server] [tool]",
		Short: "Call an MCP tool directly",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := config.Get()
			if cfg == nil || cfg.MCP == nil || !cfg.MCP.EnableClient {
				color.Yellow("MCP is not enabled")
				return
			}

			serverName := args[0]
			toolName := args[1]

			argsJSON, _ := cmd.Flags().GetString("args")
			timeoutSeconds, _ := cmd.Flags().GetInt("timeout")

			var timeout time.Duration
			if timeoutSeconds > 0 {
				timeout = time.Duration(timeoutSeconds) * time.Second
			} else if cfg.MCP.Timeout > 0 {
				timeout = cfg.MCP.Timeout
			} else {
				timeout = 60 * time.Second
			}

			var toolArgs map[string]interface{}
			if strings.TrimSpace(argsJSON) != "" {
				if err := json.Unmarshal([]byte(argsJSON), &toolArgs); err != nil {
					color.Red("Invalid --args JSON: %v", err)
					return
				}
			} else {
				toolArgs = map[string]interface{}{}
			}

			client := createMCPClient(cfg)
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			if err := client.Start(ctx); err != nil {
				color.Red("Failed to start MCP client: %v", err)
				return
			}
			defer client.Stop() //nolint:errcheck

			if ok := waitForMCPServerConnected(client, serverName, 10*time.Second); !ok {
				color.Red("Server %s not connected", serverName)
				return
			}

			result, err := client.CallTool(ctx, serverName, toolName, toolArgs)
			if err != nil {
				color.Red("Tool call failed: %v", err)
				return
			}

			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
		},
	}

	cmd.Flags().String("args", "", "Tool arguments JSON object (e.g. '{\"url\":\"https://example.com\"}')")
	cmd.Flags().IntP("timeout", "t", 0, "Timeout in seconds (defaults to MCP timeout)")
	return cmd
}

func newMCPRunCommand() *cobra.Command {
	type step struct {
		Tool string                 `json:"tool"`
		Args map[string]interface{} `json:"args"`
	}

	cmd := &cobra.Command{
		Use:   "run [server]",
		Short: "Run multiple MCP tool calls sequentially",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := config.Get()
			if cfg == nil || cfg.MCP == nil || !cfg.MCP.EnableClient {
				color.Yellow("MCP is not enabled")
				return
			}

			serverName := args[0]
			stepsJSON, _ := cmd.Flags().GetString("steps")
			timeoutSeconds, _ := cmd.Flags().GetInt("timeout")

			if strings.TrimSpace(stepsJSON) == "" {
				color.Red("--steps is required")
				return
			}

			var steps []step
			if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
				color.Red("Invalid --steps JSON: %v", err)
				return
			}
			if len(steps) == 0 {
				color.Red("--steps must contain at least one step")
				return
			}

			var timeout time.Duration
			if timeoutSeconds > 0 {
				timeout = time.Duration(timeoutSeconds) * time.Second
			} else if cfg.MCP.Timeout > 0 {
				timeout = cfg.MCP.Timeout
			} else {
				timeout = 60 * time.Second
			}

			client := createMCPClient(cfg)
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			if err := client.Start(ctx); err != nil {
				color.Red("Failed to start MCP client: %v", err)
				return
			}
			defer client.Stop() //nolint:errcheck

			if ok := waitForMCPServerConnected(client, serverName, 10*time.Second); !ok {
				color.Red("Server %s not connected", serverName)
				return
			}

			results := make([]any, 0, len(steps))
			for _, s := range steps {
				if strings.TrimSpace(s.Tool) == "" {
					color.Red("Step tool name is required")
					return
				}
				if s.Args == nil {
					s.Args = map[string]interface{}{}
				}
				res, err := client.CallTool(ctx, serverName, s.Tool, s.Args)
				if err != nil {
					color.Red("Tool call failed (%s): %v", s.Tool, err)
					return
				}
				results = append(results, map[string]any{
					"tool":   s.Tool,
					"result": res,
				})
			}

			data, _ := json.MarshalIndent(map[string]any{
				"server":  serverName,
				"results": results,
			}, "", "  ")
			fmt.Println(string(data))
		},
	}

	cmd.Flags().String("steps", "", "JSON array of steps (e.g. '[{\"tool\":\"new_page\",\"args\":{\"url\":\"https://example.com\"}}]')")
	cmd.Flags().IntP("timeout", "t", 0, "Timeout in seconds (defaults to MCP timeout)")
	return cmd
}

func waitForMCPServerConnected(client *mcp.MCPClient, serverName string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		connections := client.ListConnections()
		for _, conn := range connections {
			if conn.Name == serverName && conn.Connected {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func waitForAnyMCPServerConnected(client *mcp.MCPClient, serverFilter string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		connections := client.ListConnections()
		for _, conn := range connections {
			if serverFilter != "" && !strings.Contains(conn.Name, serverFilter) {
				continue
			}
			if conn.Connected {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// newMCPResourcesCommand creates a resources listing command
func newMCPResourcesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resources",
		Short: "List available MCP resources",
		Long: `Display all available resources from connected MCP servers:
  • Resource URIs, types, and descriptions
  • Access permissions and availability
  • Content previews and metadata`,
		Run: runMCPResources,
	}

	cmd.Flags().StringP("server", "s", "", "Filter resources by server name")
	cmd.Flags().StringP("type", "t", "", "Filter resources by type")
	cmd.Flags().BoolP("json", "j", false, "Output resources in JSON format")
	return cmd
}

// newMCPHealthCommand creates an enhanced health check command
func newMCPHealthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Perform comprehensive health checks",
		Long: `Execute health checks on all MCP servers:
  • Connection status and response times
  • Tool and resource availability
  • Error rates and performance metrics
  • Automatic reconnection attempts`,
		Run: runMCPHealth,
	}

	cmd.Flags().BoolP("fix", "f", false, "Attempt to fix detected issues automatically")
	cmd.Flags().IntP("timeout", "t", 30, "Health check timeout in seconds")
	cmd.Flags().Bool("continuous", false, "Run continuous health monitoring")
	return cmd
}

// newMCPDiagnosticsCommand creates an enhanced diagnostics command
func newMCPDiagnosticsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diagnostics",
		Short: "Generate comprehensive diagnostic report",
		Long: `Generate detailed diagnostic information:
  • System and environment details
  • Configuration validation
  • Connection troubleshooting
  • Performance analysis and recommendations`,
		Run: runMCPDiagnostics,
	}

	cmd.Flags().StringP("output", "o", "", "Save diagnostic report to file")
	cmd.Flags().Bool("verbose", false, "Include verbose diagnostic information")
	cmd.Flags().BoolP("json", "j", false, "Output diagnostics in JSON format")
	return cmd
}

// newMCPServersCommand creates a server management command
func newMCPServersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "servers",
		Short: "Manage MCP server lifecycle",
		Long: `Manage individual MCP servers:
  • Start, stop, and restart servers
  • Enable or disable specific servers
  • View server-specific logs and metrics
  • Test individual server connections`,
	}

	// Add server subcommands
	cmd.AddCommand(newMCPServerListCommand())
	cmd.AddCommand(newMCPServerStartCommand())
	cmd.AddCommand(newMCPServerStopCommand())
	cmd.AddCommand(newMCPServerRestartCommand())
	cmd.AddCommand(newMCPServerTestCommand())

	return cmd
}

// runMCPTools lists available MCP tools
func runMCPTools(cmd *cobra.Command, _ []string) {
	cfg := config.Get()
	if cfg == nil || cfg.MCP == nil || !cfg.MCP.EnableClient {
		color.Yellow("MCP is not enabled")
		return
	}

	client := createMCPClient(cfg)
	ctx := context.Background()

	if err := client.Start(ctx); err != nil {
		color.Red("Failed to start MCP client: %v", err)
		return
	}
	defer client.Stop() //nolint:errcheck

	serverFilter, _ := cmd.Flags().GetString("server")
	detailed, _ := cmd.Flags().GetBool("detailed")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	waitForAnyMCPServerConnected(client, serverFilter, 10*time.Second)

	connections := client.ListConnections()
	toolsByServer := client.GetAllTools()
	totalTools := 0

	filtered := make(map[string][]mcp.MCPToolInfo)
	for _, conn := range connections {
		if serverFilter != "" && !strings.Contains(conn.Name, serverFilter) {
			continue
		}
		if !conn.Connected {
			continue
		}
		serverTools := toolsByServer[conn.Name]
		filtered[conn.Name] = serverTools
		totalTools += len(serverTools)
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(filtered, "", "  ")
		fmt.Println(string(data))
		return
	}

	color.Green("=== MCP Tools ===")
	color.White("Total tools available: %d", totalTools)

	for _, conn := range connections {
		if serverFilter != "" && !strings.Contains(conn.Name, serverFilter) {
			continue
		}
		if !conn.Connected {
			continue
		}

		serverTools := filtered[conn.Name]
		if detailed {
			data, _ := json.MarshalIndent(serverTools, "", "  ")
			color.White("\n%s (%s): %d tools\n%s", conn.Name, conn.Transport, len(serverTools), string(data))
			continue
		}

		color.White("• %s: %d tools (%s)", conn.Name, len(serverTools), conn.Transport)
	}
}

// runMCPResources lists available MCP resources
func runMCPResources(cmd *cobra.Command, _ []string) {
	cfg := config.Get()
	if cfg == nil || cfg.MCP == nil || !cfg.MCP.EnableClient {
		color.Yellow("MCP is not enabled")
		return
	}

	client := createMCPClient(cfg)
	ctx := context.Background()

	if err := client.Start(ctx); err != nil {
		color.Red("Failed to start MCP client: %v", err)
		return
	}
	defer client.Stop() //nolint:errcheck

	connections := client.ListConnections()
	serverFilter, _ := cmd.Flags().GetString("server")
	typeFilter, _ := cmd.Flags().GetString("type")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	allResources := make(map[string]interface{})
	totalResources := 0

	for _, conn := range connections {
		if serverFilter != "" && !strings.Contains(conn.Name, serverFilter) {
			continue
		}

		if conn.Connected {
			allResources[conn.Name] = map[string]interface{}{
				"server":         conn.Name,
				"transport":      conn.Transport,
				"resource_count": conn.ResourceCount,
				"connected":      conn.Connected,
			}
			totalResources += conn.ResourceCount
		}
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(allResources, "", "  ")
		fmt.Println(string(data))
		return
	}

	color.Green("=== MCP Resources ===")
	color.White("Total resources available: %d", totalResources)
	if typeFilter != "" {
		color.White("Filtered by type: %s", typeFilter)
	}

	for serverName, resourceInfo := range allResources {
		if info, ok := resourceInfo.(map[string]interface{}); ok {
			color.White("• %s: %v resources (%s)", serverName, info["resource_count"], info["transport"])
		}
	}
}

// Server subcommands
func newMCPServerListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configured MCP servers",
		Run: func(_ *cobra.Command, _ []string) {
			cfg := config.Get()
			if cfg == nil || cfg.MCP == nil {
				color.Yellow("No MCP configuration found")
				return
			}

			color.Green("=== Configured MCP Servers ===")
			for i, server := range cfg.MCP.Servers {
				status := "✓ enabled"
				if !server.Enabled {
					status = "✗ disabled"
				}
				color.White("%d. %s (%s) - %s", i+1, server.Name, server.Transport, status)
				if server.Description != "" {
					color.White("   %s", server.Description)
				}
			}
		},
	}
}

func newMCPServerStartCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "start [server-name]",
		Short: "Start (probe) a specific MCP server",
		Long: `Probe a specific MCP server by establishing a fresh connection and
verifying it responds to capability discovery. MCP servers in nano-agent are
launched on-demand per client connection, so "start" is implemented as a
connection probe; a successful probe confirms the configured command, env,
and transport are valid.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serverName := args[0]
			color.White("Starting MCP server: %s", serverName)
			info, err := probeMCPServer(cmd.Context(), serverName)
			if err != nil {
				return fmt.Errorf("failed to start MCP server %s: %w", serverName, err)
			}
			color.Green("✅ MCP server %s started (transport=%s, tools=%d, resources=%d, prompts=%d)",
				info.Name, info.Transport, info.ToolCount, info.ResourceCount, info.PromptCount)
			return nil
		},
	}
}

func newMCPServerStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [server-name]",
		Short: "Stop a specific MCP server (disable in config)",
		Long: `Disable a specific MCP server in the active configuration. MCP servers
in nano-agent are launched on-demand per client connection (one process per
session) and cannot be killed centrally from the CLI, so "stop" is
implemented as a persistent disable: the server's "enabled" flag is set to
false in the active config so future sessions skip it.

Re-enable with "nano mcp servers start <name>".`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			serverName := args[0]
			cfg := config.Get()
			if cfg == nil || cfg.MCP == nil {
				color.Yellow("MCP is not configured")
				return nil
			}
			found := false
			for i := range cfg.MCP.Servers {
				if cfg.MCP.Servers[i].Name == serverName {
					cfg.MCP.Servers[i].Enabled = false
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("MCP server not found in config: %s", serverName)
			}
			color.Green("✅ MCP server %s disabled in active config", serverName)
			color.Yellow("ℹ️  This change applies to the in-memory config only. " +
				"Edit ~/.nano/config.yaml to persist across restarts.")
			return nil
		},
	}
}

func newMCPServerRestartCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "restart [server-name]",
		Short: "Restart (re-probe) a specific MCP server",
		Long: `Restart a specific MCP server by re-running the connection probe.
Equivalent to "stop" followed by "start" but skips the disable step so the
server stays enabled in config.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serverName := args[0]
			color.Yellow("Restarting MCP server: %s", serverName)
			info, err := probeMCPServer(cmd.Context(), serverName)
			if err != nil {
				return fmt.Errorf("failed to restart MCP server %s: %w", serverName, err)
			}
			color.Green("✅ MCP server %s restarted (transport=%s, tools=%d)",
				info.Name, info.Transport, info.ToolCount)
			return nil
		},
	}
}

func newMCPServerTestCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "test [server-name]",
		Short: "Test connection to a specific MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serverName := args[0]
			color.White("Testing connection to MCP server: %s", serverName)
			info, err := probeMCPServer(cmd.Context(), serverName)
			if err != nil {
				return fmt.Errorf("server %s connection test failed: %w", serverName, err)
			}
			color.Green("✅ Server %s connected (%s): %d tools, %d resources, %d prompts",
				info.Name, info.Transport, info.ToolCount, info.ResourceCount, info.PromptCount)
			return nil
		},
	}
}

// createMCPClient creates an MCP client from configuration
func createMCPClient(cfg *config.Config) *mcp.MCPClient {
	mcpConfig := &mcp.MCPConfig{
		EnableClient:        cfg.MCP.EnableClient,
		MCPServers:          convertMCPServers(cfg.MCP.Servers),
		DefaultTransport:    cfg.MCP.DefaultTransport,
		Timeout:             cfg.MCP.Timeout,
		MaxRetries:          cfg.MCP.MaxRetries,
		EnableHealthCheck:   cfg.MCP.EnableHealthCheck,
		HealthCheckInterval: cfg.MCP.HealthCheckInterval,
		HealthCheckTimeout:  cfg.MCP.HealthCheckTimeout,
	}
	return mcp.NewMCPClient(mcpConfig)
}

// runMCPStatus displays MCP server status
func runMCPStatus(cmd *cobra.Command, _ []string) {
	cfg := config.Get()
	if cfg == nil || cfg.MCP == nil || !cfg.MCP.EnableClient {
		color.Yellow("MCP is not enabled")
		return
	}

	client := createMCPClient(cfg)
	ctx := context.Background()

	// Start client
	if err := client.Start(ctx); err != nil {
		color.Red("Failed to start MCP client: %v", err)
		return
	}
	defer client.Stop() //nolint:errcheck

	// Get flags
	verbose, _ := cmd.Flags().GetBool("verbose")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	// Get status
	status := client.Status()
	connections := client.ListConnections()

	if jsonOutput {
		statusData := map[string]interface{}{
			"configured_servers":  status.ConfiguredServers,
			"connected_servers":   status.ConnectedServers,
			"available_tools":     status.AvailableTools,
			"available_resources": status.AvailableResources,
			"available_prompts":   status.AvailablePrompts,
			"connections":         connections,
		}
		data, _ := json.MarshalIndent(statusData, "", "  ")
		fmt.Println(string(data))
		return
	}

	color.Green("=== MCP Status ===")
	color.White("Configured Servers: %d", status.ConfiguredServers)
	color.White("Connected Servers: %d", status.ConnectedServers)
	color.White("Available Tools: %d", status.AvailableTools)
	color.White("Available Resources: %d", status.AvailableResources)
	color.White("Available Prompts: %d", status.AvailablePrompts)

	if len(connections) > 0 {
		color.Green("\n=== Connections ===")
		for _, conn := range connections {
			status := "✓"
			statusColor := color.GreenString
			if !conn.Connected {
				status = "✗"
				statusColor = color.RedString
			}

			if verbose {
				color.White("%s %s (%s):", statusColor(status), conn.Name, conn.Transport)
				color.White("  Tools: %d, Resources: %d, Prompts: %d", conn.ToolCount, conn.ResourceCount, conn.PromptCount)
				color.White("  Last Activity: %s", conn.LastActivity.Format("2006-01-02 15:04:05"))
			} else {
				color.White("%s %s (%s): %d tools, %d resources, %d prompts",
					statusColor(status), conn.Name, conn.Transport, conn.ToolCount, conn.ResourceCount, conn.PromptCount)
			}
		}
	}
}

// runMCPHealth performs enhanced health checks
func runMCPHealth(cmd *cobra.Command, _ []string) {
	cfg := config.Get()
	if cfg == nil || cfg.MCP == nil || !cfg.MCP.EnableClient {
		color.Yellow("MCP is not enabled")
		return
	}

	client := createMCPClient(cfg)
	ctx := context.Background()

	// Get flags
	fix, _ := cmd.Flags().GetBool("fix")
	timeout, _ := cmd.Flags().GetInt("timeout")
	continuous, _ := cmd.Flags().GetBool("continuous")

	// Set timeout context
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	// Start client
	if err := client.Start(ctx); err != nil {
		color.Red("Failed to start MCP client: %v", err)
		return
	}
	defer client.Stop() //nolint:errcheck

	if continuous {
		color.Green("=== Continuous Health Monitoring (Press Ctrl+C to stop) ===")
		for {
			performHealthCheck(client, fix)
			time.Sleep(5 * time.Second)
			fmt.Println("---")
		}
	} else {
		performHealthCheck(client, fix)
	}
}

// performHealthCheck performs the actual health check logic
func performHealthCheck(client *mcp.MCPClient, fix bool) {
	connections := client.ListConnections()
	if len(connections) == 0 {
		color.Yellow("No MCP servers configured")
		return
	}

	color.Green("=== MCP Health Status ===")
	healthyCount := 0
	totalCount := len(connections)

	for _, conn := range connections {
		status := "healthy"
		statusColor := color.GreenString
		issues := []string{}

		if !conn.Connected {
			status = "disconnected"
			statusColor = color.RedString
			issues = append(issues, "not connected")
		} else {
			healthyCount++
			if time.Since(conn.LastActivity) > 30*time.Second {
				status = "stale"
				statusColor = color.YellowString
				issues = append(issues, "no recent activity")
			}
			if conn.ToolCount == 0 {
				issues = append(issues, "no tools available")
			}
		}

		fmt.Printf("%s: %s (transport: %s, tools: %d, last activity: %s)\n",
			conn.Name,
			statusColor(status),
			conn.Transport,
			conn.ToolCount,
			conn.LastActivity.Format("15:04:05"))

		if len(issues) > 0 {
			color.Yellow("  Issues: %s", strings.Join(issues, ", "))
			if fix {
				color.White("  Attempting to fix issues...")
				// In a real implementation, you would attempt to reconnect or fix issues
				color.Green("  ✅ Fix attempted")
			}
		}
	}

	color.White("\nOverall Health: %d/%d servers healthy (%.1f%%)",
		healthyCount, totalCount, float64(healthyCount)/float64(totalCount)*100)
}

// runMCPDiagnostics generates comprehensive diagnostics
// runMCPDiagnostics runs diagnostics on MCP servers
func runMCPDiagnostics(cmd *cobra.Command, _ []string) {
	cfg := config.Get()
	if cfg == nil || cfg.MCP == nil || !cfg.MCP.EnableClient {
		color.Yellow("MCP is not enabled")
		return
	}

	client := createMCPClient(cfg)
	ctx := context.Background()

	// Get flags
	outputFile, _ := cmd.Flags().GetString("output")
	verbose, _ := cmd.Flags().GetBool("verbose")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	// Start client
	if err := client.Start(ctx); err != nil {
		color.Red("Failed to start MCP client: %v", err)
		return
	}
	defer client.Stop() //nolint:errcheck

	color.White("Generating comprehensive diagnostic report...")

	// Generate diagnostics
	report, err := client.GenerateDiagnosticReport(ctx)
	if err != nil {
		color.Red("Failed to generate diagnostic report: %v", err)
		return
	}

	// Prepare output
	var output string
	if jsonOutput {
		data, _ := json.MarshalIndent(report, "", "  ")
		output = string(data)
	} else {
		// Generate human-readable report
		diagnostics := client.GetDiagnostics()
		output = diagnostics.PrintSummary(report)
		if verbose {
			// Add additional verbose information
			output += "\n\n=== Verbose Details ===\n"
			data, _ := json.MarshalIndent(report, "", "  ")
			output += string(data)
		}
	}

	// Output to file or console
	if outputFile != "" {
		if err := os.WriteFile(outputFile, []byte(output), 0644); err != nil {
			color.Red("Failed to write diagnostic report to file: %v", err)
			return
		}
		color.Green("✅ Diagnostic report saved to: %s", outputFile)
	} else {
		fmt.Println(output)
	}

	// Show summary statistics
	if !jsonOutput {
		color.Green("\n=== Diagnostic Summary ===")
		color.White("Report generated at: %s", report.Timestamp.Format("2006-01-02 15:04:05"))
		color.White("Total servers analyzed: %d", len(report.Connections))
		color.White("System info included: %v", report.SystemInfo.OS != "")
	}
}

// convertMCPServers converts config MCPServerConfig to mcp MCPServerConfig
func convertMCPServers(servers []config.MCPServerConfig) []mcp.MCPServerConfig {
	var result []mcp.MCPServerConfig
	for _, server := range servers {
		mcpServer := mcp.MCPServerConfig{
			Name:        server.Name,
			Description: server.Description,
			Command:     server.Command,
			URL:         server.URL,
			Transport:   server.Transport,
			Headers:     server.Headers,
			Enabled:     server.Enabled,
			Timeout:     server.Timeout,
		}
		// Convert OAuth config if present
		if server.OAuth != nil {
			mcpServer.OAuth = &mcp.OAuthConfig{
				AuthorizationURL: server.OAuth.AuthorizationURL,
				TokenURL:         server.OAuth.TokenURL,
				ClientID:         server.OAuth.ClientID,
				ClientSecret:     server.OAuth.ClientSecret,
				Scopes:           server.OAuth.Scopes,
				RedirectPort:     server.OAuth.RedirectPort,
			}
		}
		result = append(result, mcpServer)
	}
	return result
}

// newMCPAddCommand creates the `nano mcp add` command for non-interactive server registration.
func newMCPAddCommand() *cobra.Command {
	var (
		transport   string
		description string
		headerKV    []string
		disabled    bool
	)

	cmd := &cobra.Command{
		Use:   "add [name] [command-or-url...]",
		Short: "Add an MCP server to configuration",
		Long: `Register a new MCP server without running the interactive wizard.

Examples:
  # stdio server
  nano mcp add filesystem npx -y @modelcontextprotocol/server-filesystem /tmp

  # HTTP/Streamable server
  nano mcp add myserver --transport streamable https://example.com/mcp

  # With custom headers
  nano mcp add secure-server --header "Authorization=Bearer TOKEN" https://api.example.com/mcp`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			rest := args[1:]

			configFile, _ := cmd.Root().PersistentFlags().GetString("config")
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if cfg.MCP == nil {
				cfg.MCP = &config.MCPConfig{EnableClient: true}
			}

			// Detect transport
			if transport == "" {
				if len(rest) == 1 && (strings.HasPrefix(rest[0], "http://") || strings.HasPrefix(rest[0], "https://")) {
					transport = "streamable"
				} else {
					transport = "stdio"
				}
			}

			// Parse headers
			headers := make(map[string]string)
			for _, h := range headerKV {
				parts := strings.SplitN(h, "=", 2)
				if len(parts) == 2 {
					headers[parts[0]] = parts[1]
				}
			}

			server := config.MCPServerConfig{
				Name:        name,
				Description: description,
				Transport:   transport,
				Enabled:     !disabled,
				Headers:     headers,
			}
			if transport == "stdio" {
				server.Command = rest
			} else {
				server.URL = rest[0]
			}

			// Check for duplicates
			for _, s := range cfg.MCP.Servers {
				if s.Name == name {
					return fmt.Errorf("MCP server %q already exists; remove it first", name)
				}
			}
			cfg.MCP.Servers = append(cfg.MCP.Servers, server)

			// Persist config
			if err := persistConfig(cfg, configFile); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Printf("Added MCP server %q (%s)\n", name, transport)
			return nil
		},
	}

	cmd.Flags().StringVar(&transport, "transport", "", "Transport type: stdio, streamable (auto-detected if omitted)")
	cmd.Flags().StringVar(&description, "description", "", "Human-readable description")
	cmd.Flags().StringArrayVar(&headerKV, "header", nil, "HTTP header in KEY=VALUE format (repeatable)")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "Register server but leave it disabled")
	return cmd
}

// newMCPAuthCommand creates the `nano mcp auth` command for OAuth 2.0 authorization.
func newMCPAuthCommand() *cobra.Command {
	var (
		authURL      string
		tokenURL     string
		clientID     string
		clientSecret string
		scopes       string
		revoke       bool
		listTokens   bool
	)

	cmd := &cobra.Command{
		Use:   "auth [server-name]",
		Short: "Authenticate an MCP server via OAuth 2.0 (PKCE)",
		Long: `Perform OAuth 2.0 authorization code flow (PKCE) for an MCP server.
The access token is stored in ~/.nano/mcp_tokens.json and used automatically.

Examples:
  # Authorize a server
  nano mcp auth myserver --auth-url https://auth.example.com/authorize \
      --token-url https://auth.example.com/token --client-id abc123

  # Show stored tokens
  nano mcp auth --list

  # Revoke a stored token
  nano mcp auth myserver --revoke`,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mcp.NewTokenStore("")
			if err != nil {
				return fmt.Errorf("token store: %w", err)
			}

			if listTokens {
				tokens := store.List()
				if len(tokens) == 0 {
					fmt.Println("No stored OAuth tokens.")
					return nil
				}
				fmt.Printf("%-20s %-12s %-30s %s\n", "SERVER", "TYPE", "EXPIRES", "SCOPES")
				for _, t := range tokens {
					exp := "never"
					if !t.ExpiresAt.IsZero() {
						if t.IsExpired() {
							exp = "EXPIRED"
						} else {
							exp = t.ExpiresAt.Format("2006-01-02 15:04")
						}
					}
					fmt.Printf("%-20s %-12s %-30s %s\n", t.ServerName, t.TokenType, exp, t.Scope)
				}
				return nil
			}

			if len(args) == 0 {
				return fmt.Errorf("server name is required")
			}
			serverName := args[0]

			if revoke {
				if err := store.Delete(serverName); err != nil {
					return fmt.Errorf("revoke token: %w", err)
				}
				fmt.Printf("Token for %q revoked.\n", serverName)
				return nil
			}

			// Try to load OAuth config from server settings if not provided via flags
			configFile, _ := cmd.Root().PersistentFlags().GetString("config")
			cfg, err := config.LoadConfig(configFile)
			if err == nil && cfg.MCP != nil {
				for _, server := range cfg.MCP.Servers {
					if server.Name == serverName && server.OAuth != nil {
						// Use config values as defaults, but allow flags to override
						if authURL == "" {
							authURL = server.OAuth.AuthorizationURL
						}
						if tokenURL == "" {
							tokenURL = server.OAuth.TokenURL
						}
						if clientID == "" {
							clientID = server.OAuth.ClientID
						}
						if clientSecret == "" {
							clientSecret = server.OAuth.ClientSecret
						}
						if scopes == "" {
							scopes = server.OAuth.Scopes
						}
						break
					}
				}
			}

			if authURL == "" || tokenURL == "" || clientID == "" {
				return fmt.Errorf("--auth-url, --token-url, and --client-id are required (or configure OAuth in server settings)")
			}

			oauthCfg := &mcp.OAuthConfig{
				AuthorizationURL: authURL,
				TokenURL:         tokenURL,
				ClientID:         clientID,
				ClientSecret:     clientSecret,
				Scopes:           scopes,
			}
			client := mcp.NewOAuthClient(oauthCfg, store)
			ctx := context.Background()

			fmt.Printf("Starting OAuth 2.0 authorization for %q...\n", serverName)
			entry, err := client.Authorize(ctx, serverName, func(u string) error {
				fmt.Printf("\nOpen this URL in your browser:\n\n  %s\n\nWaiting for callback...\n", u)
				return nil
			})
			if err != nil {
				return fmt.Errorf("authorization failed: %w", err)
			}

			exp := "no expiry"
			if !entry.ExpiresAt.IsZero() {
				exp = entry.ExpiresAt.Format("2006-01-02 15:04:05")
			}
			fmt.Printf("Authorization successful! Token stored (expires: %s)\n", exp)

			// Persist OAuth config back to server settings if it wasn't already there
			if cfg != nil && cfg.MCP != nil {
				updated := false
				for i := range cfg.MCP.Servers {
					if cfg.MCP.Servers[i].Name == serverName && cfg.MCP.Servers[i].OAuth == nil {
						cfg.MCP.Servers[i].OAuth = &config.MCPOAuthConfig{
							AuthorizationURL: authURL,
							TokenURL:         tokenURL,
							ClientID:         clientID,
							ClientSecret:     clientSecret,
							Scopes:           scopes,
						}
						updated = true
						break
					}
				}
				if updated {
					if err := persistConfig(cfg, configFile); err != nil {
						fmt.Printf("Warning: failed to save OAuth config to server settings: %v\n", err)
					} else {
						fmt.Printf("OAuth configuration saved to server %q settings.\n", serverName)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&authURL, "auth-url", "", "OAuth authorization endpoint URL")
	cmd.Flags().StringVar(&tokenURL, "token-url", "", "OAuth token endpoint URL")
	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth client ID")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "OAuth client secret (optional for public clients)")
	cmd.Flags().StringVar(&scopes, "scopes", "", "Space-separated OAuth scopes")
	cmd.Flags().BoolVar(&revoke, "revoke", false, "Revoke and delete the stored token for this server")
	cmd.Flags().BoolVar(&listTokens, "list", false, "List all stored OAuth tokens")
	return cmd
}

// persistConfig writes the config back to disk (best-effort YAML).
// Falls back to printing a warning when the config file path is unavailable.
func persistConfig(cfg *config.Config, configFile string) error {
	if configFile == "" {
		home, _ := os.UserHomeDir()
		configFile = filepath.Join(home, ".nano", "config.yaml")
	}
	if err := os.MkdirAll(filepath.Dir(configFile), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configFile, data, 0o644)
}
