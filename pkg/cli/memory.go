package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/memory"
	"github.com/spf13/cobra"
)

// NewMemoryCommand creates the memory management command
func NewMemoryCommand() *cobra.Command {
	memoryCmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage local memory system",
		Long:  `Manage the local SQLite-backed memory system for storing and retrieving contextual information.`,
	}

	// Add subcommands
	memoryCmd.AddCommand(newMemorySearchCommand())
	memoryCmd.AddCommand(newMemorySaveCommand())
	memoryCmd.AddCommand(newMemoryStatsCommand())

	return memoryCmd
}

// newLocalManager builds a memory.Manager from CLI flags / config.
// workingDir defaults to $PWD when empty.
func newLocalManager(configFile string) (*memory.Manager, error) {
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}
	_ = cfg // may be used for future per-project dataDir

	wd, _ := os.Getwd()
	return memory.NewManager(wd, "", true), nil
}

// newMemorySearchCommand creates the search subcommand
func newMemorySearchCommand() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search memory entries",
		Long:  `Search through stored memory entries using full-text search.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			configFile, _ := cmd.Root().PersistentFlags().GetString("config")
			mgr, err := newLocalManager(configFile)
			if err != nil {
				return err
			}
			defer mgr.Close()

			ctx := context.Background()
			result, err := mgr.SearchMemory(ctx, query, "", "", limit)
			if err != nil {
				return fmt.Errorf("failed to search memory: %w", err)
			}

			fmt.Println("Memory Search Results")
			fmt.Println(strings.Repeat("=", 26))
			if result == "" {
				fmt.Println("No memories found.")
			} else {
				fmt.Println(result)
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 10, "Maximum number of results")
	return cmd
}

// newMemorySaveCommand creates the save subcommand
func newMemorySaveCommand() *cobra.Command {
	var role string

	cmd := &cobra.Command{
		Use:   "save [content]",
		Short: "Save a fact to local memory",
		Long:  `Save important information to the local memory system for future retrieval.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fact := args[0]

			configFile, _ := cmd.Root().PersistentFlags().GetString("config")
			mgr, err := newLocalManager(configFile)
			if err != nil {
				return err
			}
			defer mgr.Close()

			ctx := context.Background()
			messages := []llm.Message{
				{Role: role, Content: fact},
			}

			if err := mgr.SaveMemory(ctx, messages, "", "", nil); err != nil {
				return fmt.Errorf("failed to save memory: %w", err)
			}

			fmt.Println("Memory Saved Successfully")
			fmt.Printf("Saved (%s): %s\n", role, fact)
			return nil
		},
	}

	cmd.Flags().StringVar(&role, "role", "user", "Message role: user, assistant, system")
	return cmd
}

// newMemoryStatsCommand shows memory system statistics
func newMemoryStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show memory system statistics",
		Long:  `Display statistics about the local memory system.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			configFile, _ := cmd.Root().PersistentFlags().GetString("config")
			mgr, err := newLocalManager(configFile)
			if err != nil {
				return err
			}
			defer mgr.Close()

			stats := mgr.GetMemoryStats()

			fmt.Println("Local Memory System Statistics")
			fmt.Println(strings.Repeat("=", 35))
			fmt.Printf("Available:  %v\n", stats.Mem0ServiceAvailable)
			fmt.Printf("Configured: %v\n", stats.Mem0ServiceConfigured)
			fmt.Printf("Updated at: %s\n", stats.Timestamp.Format("2006-01-02 15:04:05"))
			return nil
		},
	}
	return cmd
}
