package cli

import (
	"fmt"
	"os"

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

	cmd.AddCommand(serveCmd)
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
