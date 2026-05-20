package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/acp"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/spf13/cobra"
)

// NewACPCommand creates the `nano acp` command
func NewACPCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "acp",
		Short: "ACP (Agent Client Protocol) server for editor integration",
		Long: `Start an ACP-compatible server for integration with editors like Zed, JetBrains, and Neovim.

The ACP server communicates via stdio using JSON-RPC 2.0 protocol.`,
	}

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start ACP server in stdio mode",
		Long: `Start the ACP server that communicates via stdin/stdout using JSON-RPC 2.0.

This command is typically invoked by ACP-compatible editors (Zed, JetBrains, Neovim)
as a subprocess for agent integration.`,
		RunE: runACPServe,
	}

	// Flags
	serveCmd.Flags().String("workdir", "", "Working directory for the agent (default: current directory)")
	serveCmd.Flags().String("config", "", "Path to configuration file")
	serveCmd.Flags().String("log-file", "", "Path to log file (required, stdout/stderr reserved for JSON-RPC)")
	serveCmd.Flags().String("log-level", "info", "Log level (trace, debug, info, warn, error)")
	serveCmd.Flags().String("fs-mode", "auto", "Filesystem mode: acp|local|auto")
	serveCmd.Flags().Bool("enable-swarm", false, "Enable swarm/multi-agent support")

	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure nano-agent API keys and settings interactively",
		Long: `Interactive setup wizard for nano-agent ACP authentication.
Configures NANO_API_KEY, NANO_BASE_URL, and NANO_MODEL in ~/.nano/.env file.`,
		RunE: runACPSetup,
	}

	cmd.AddCommand(serveCmd)
	cmd.AddCommand(setupCmd)
	return cmd
}

func runACPServe(cmd *cobra.Command, args []string) error {
	// Get flags
	workdir, _ := cmd.Flags().GetString("workdir")
	configPath, _ := cmd.Flags().GetString("config")
	logFile, _ := cmd.Flags().GetString("log-file")
	logLevel, _ := cmd.Flags().GetString("log-level")
	fsModeStr, _ := cmd.Flags().GetString("fs-mode")
	enableSwarm, _ := cmd.Flags().GetBool("enable-swarm")

	// Configure logging - MUST use file, not stdout/stderr
	if logFile == "" {
		// Default log file
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		logFile = homeDir + "/.nano/acp-server.log"
	}

	// Ensure log directory exists
	if err := os.MkdirAll(logFile[:len(logFile)-len("/acp-server.log")], 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	// Initialize logger with file output
	if err := logger.InitFileLogger(logFile, logLevel); err != nil {
		return fmt.Errorf("initialize logger: %w", err)
	}

	logger.Infof("ACP: Starting server (log: %s, level: %s)", logFile, logLevel)

	// Parse fs-mode
	var fsMode acp.FSMode
	switch fsModeStr {
	case "acp":
		fsMode = acp.FSModeACP
	case "local":
		fsMode = acp.FSModeLocal
	case "auto":
		fsMode = acp.FSModeAuto
	default:
		return fmt.Errorf("invalid fs-mode: %s (must be acp, local, or auto)", fsModeStr)
	}

	// Load config
	var cfg *config.Config
	var err error
	if configPath != "" {
		cfg, err = config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	} else {
		cfg, err = config.LoadConfig("")
		if err != nil {
			logger.Warnf("Failed to load config, using defaults: %v", err)
			cfg = config.DefaultConfig()
		}
	}

	// Create and start ACP server
	server, err := acp.NewServer(acp.ServerOptions{
		Config:      cfg,
		FSMode:      fsMode,
		EnableSwarm: enableSwarm,
		WorkDir:     workdir,
	})
	if err != nil {
		return fmt.Errorf("create ACP server: %w", err)
	}

	// Handle graceful shutdown
	defer func() {
		if err := server.Shutdown(); err != nil {
			logger.Errorf("ACP: Shutdown error: %v", err)
		}
	}()

	// Start serving
	logger.Info("ACP: Server ready, listening on stdin")
	if err := server.Serve(); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

func runACPSetup(cmd *cobra.Command, args []string) error {
	fmt.Println("=== Nano Agent ACP Setup ===")
	fmt.Println()
	fmt.Println("This wizard will configure your nano-agent API keys and settings.")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// Get home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	nanoDir := filepath.Join(homeDir, ".nano")
	envFile := filepath.Join(nanoDir, ".env")

	// Create .nano directory if it doesn't exist
	if err := os.MkdirAll(nanoDir, 0755); err != nil {
		return fmt.Errorf("create .nano directory: %w", err)
	}

	// Load existing .env file if it exists
	existingEnv := make(map[string]string)
	if data, err := os.ReadFile(envFile); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				existingEnv[parts[0]] = parts[1]
			}
		}
	}

	// Helper function to prompt with default
	promptWithDefault := func(prompt, key, defaultValue string) (string, error) {
		if defaultValue == "" {
			if existing, ok := existingEnv[key]; ok {
				defaultValue = existing
			}
		}

		if defaultValue != "" {
			fmt.Printf("%s [%s]: ", prompt, defaultValue)
		} else {
			fmt.Printf("%s: ", prompt)
		}

		input, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}

		input = strings.TrimSpace(input)
		if input == "" {
			return defaultValue, nil
		}
		return input, nil
	}

	// Prompt for API key
	apiKey, err := promptWithDefault("Enter your API key (NANO_API_KEY)", "NANO_API_KEY", "")
	if err != nil {
		return fmt.Errorf("read API key: %w", err)
	}
	if apiKey == "" {
		return fmt.Errorf("API key is required")
	}

	// Prompt for base URL
	baseURL, err := promptWithDefault("Enter API base URL (NANO_BASE_URL)", "NANO_BASE_URL", "https://api.deepseek.com/v1")
	if err != nil {
		return fmt.Errorf("read base URL: %w", err)
	}

	// Prompt for model
	model, err := promptWithDefault("Enter model name (NANO_MODEL)", "NANO_MODEL", "deepseek-chat")
	if err != nil {
		return fmt.Errorf("read model: %w", err)
	}

	// Update environment variables
	existingEnv["NANO_API_KEY"] = apiKey
	existingEnv["NANO_BASE_URL"] = baseURL
	existingEnv["NANO_MODEL"] = model

	// Write to .env file
	var envContent strings.Builder
	envContent.WriteString("# Nano Agent Configuration\n")
	envContent.WriteString("# Generated by 'nano acp setup'\n\n")

	// Write the three main variables first
	envContent.WriteString(fmt.Sprintf("NANO_API_KEY=%s\n", apiKey))
	envContent.WriteString(fmt.Sprintf("NANO_BASE_URL=%s\n", baseURL))
	envContent.WriteString(fmt.Sprintf("NANO_MODEL=%s\n", model))

	// Write other existing variables
	for key, value := range existingEnv {
		if key != "NANO_API_KEY" && key != "NANO_BASE_URL" && key != "NANO_MODEL" {
			envContent.WriteString(fmt.Sprintf("%s=%s\n", key, value))
		}
	}

	if err := os.WriteFile(envFile, []byte(envContent.String()), 0600); err != nil {
		return fmt.Errorf("write .env file: %w", err)
	}

	fmt.Println()
	fmt.Printf("✓ Configuration saved to %s\n", envFile)
	fmt.Println()
	fmt.Println("Setup complete. You can now restart the ACP client.")
	fmt.Println()

	return nil
}
