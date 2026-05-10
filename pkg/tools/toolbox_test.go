package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestMain(m *testing.M) {
	// Initialize global configuration to prevent panic in tools that call config.Get()
	config.LoadConfig("") //nolint:errcheck
	os.Exit(m.Run())
}

func TestToolboxIntegration(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "toolbox_integration_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create toolbox with test configuration
	config := &ToolboxConfig{
		WorkingDirectory: tempDir,
		Timeout:          5 * time.Second,
		MaxFileSize:      1024 * 1024, // 1MB
		MaxResponseSize:  1024 * 1024, // 1MB
		UserAgent:        "nano-agent-Test/1.0",
		AllowedCommands:  []string{"echo", "pwd", "ls", "dir"},
		BlockedCommands:  []string{"rm", "del", "sudo"},
	}

	toolbox := NewToolbox(tempDir, config, nil)

	// Test that all expected tools are registered
	tools := toolbox.List()
	expectedTools := []string{
		"read_file",
		"read_pdf",
		"write_file",
		"edit_file",
		"delete_file",
		"run_shell_command",
		"todo_write", // Added new todo write tool
		"web_fetch",
		"web_search",
		"image_generate",        // New image generation tool
		"code_skeleton",         // Added new code skeleton tool
		"bash_output",           // Added background task management tool
		"kill_bash",             // Added background task management tool
		"list_background_tasks", // Added background task management tool
		// workspace tools
		"oss_manager",
	}

	if len(tools) != len(expectedTools) {
		t.Errorf("Expected %d tools, got %d", len(expectedTools), len(tools))
	}

	// Check that all expected tools are present
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name()] = true
	}

	for _, expectedTool := range expectedTools {
		if !toolNames[expectedTool] {
			t.Errorf("Expected tool '%s' not found", expectedTool)
		}
	}

	// Test tool schemas
	schemas := toolbox.Schemas()
	expectedSchemas := len(expectedTools)
	if len(schemas) != expectedSchemas {
		t.Errorf("Expected %d schemas, got %d", expectedSchemas, len(schemas))
	}

	for _, expectedTool := range expectedTools {
		if _, exists := schemas[expectedTool]; !exists {
			t.Errorf("Schema for tool '%s' not found", expectedTool)
		}
	}
}

func TestToolboxWorkflow(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "toolbox_workflow_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &ToolboxConfig{
		WorkingDirectory: tempDir,
		AllowedCommands:  []string{"echo", "pwd", "ls"},
	}

	toolbox := NewToolbox(tempDir, config, nil)
	ctx := context.Background()

	// Step 1: Create a file
	t.Run("create file", func(t *testing.T) {
		result, err := toolbox.Execute(ctx, "write_file", map[string]interface{}{
			"file_path": filepath.Join(tempDir, "test.txt"),
			"content":   "Hello, World!\nThis is a test file.\nEnd of file.",
		})

		if err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}

		if !result.Success {
			t.Fatalf("File creation failed: %s", result.Error)
		}
	})

	// Step 2: Read the file
	t.Run("read file", func(t *testing.T) {
		result, err := toolbox.Execute(ctx, "read_file", map[string]interface{}{
			"file_path": filepath.Join(tempDir, "test.txt"),
		})

		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}

		if !result.Success {
			t.Fatalf("File reading failed: %s", result.Error)
		}

		content, ok := result.Data.(string)
		if !ok {
			t.Fatal("Expected string content")
		}

		if !strings.Contains(content, "Hello, World!") {
			t.Error("Expected to find 'Hello, World!' in file content")
		}
	})

	// Step 3: Edit the file
	t.Run("edit file", func(t *testing.T) {
		result, err := toolbox.Execute(ctx, "edit_file", map[string]interface{}{
			"command": "str_replace",
			"path":    filepath.Join(tempDir, "test.txt"),
			"old_str": "Hello, World!",
			"new_str": "Greetings, Universe!",
		})

		if err != nil {
			t.Fatalf("Failed to edit file: %v", err)
		}

		if !result.Success {
			t.Fatalf("File editing failed: %s", result.Error)
		}
	})

	// Step 4: Run shell command
	t.Run("run shell command", func(t *testing.T) {
		result, err := toolbox.Execute(ctx, "run_shell_command", map[string]interface{}{
			"command": "echo 'Integration test successful'",
		})

		if err != nil {
			t.Fatalf("Failed to run shell command: %v", err)
		}

		if !result.Success {
			t.Fatalf("Shell command failed: %s", result.Error)
		}
	})
}

func TestToolboxCategories(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "toolbox_categories_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	toolbox := NewToolbox(tempDir, nil, nil)
	tools := toolbox.List()

	// Test getting tools by category
	// Note: Category filtering test simplified for now
	filesystemTools := 0
	for _, tool := range tools {
		if tool.Category() == "filesystem" {
			filesystemTools++
		}
	}

	if filesystemTools == 0 {
		t.Error("Expected to find filesystem tools")
	}
}

func TestToolboxErrorHandling(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "toolbox_error_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	toolbox := NewToolbox(tempDir, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name     string
		toolName string
		params   map[string]interface{}
		wantErr  bool
	}{
		{
			name:     "nonexistent tool",
			toolName: "nonexistent_tool",
			params:   map[string]interface{}{},
			wantErr:  true,
		},
		{
			name:     "tool with invalid parameters",
			toolName: "read_file",
			params:   map[string]interface{}{},
			wantErr:  true,
		},
		{
			name:     "tool with wrong parameter types",
			toolName: "read_file",
			params: map[string]interface{}{
				"file_path": 123, // Should be string
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := toolbox.Execute(ctx, tt.toolName, tt.params)

			if tt.wantErr {
				if err == nil && result.Success {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !result.Success {
				t.Errorf("Tool execution failed: %s", result.Error)
			}
		})
	}
}

func TestToolboxConfiguration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "toolbox_config_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test with nil config (should use defaults)
	t.Run("nil config", func(t *testing.T) {
		toolbox := NewToolbox(tempDir, nil, nil)
		config := toolbox.GetConfig()

		if config == nil { //nolint:staticcheck
			t.Error("Expected default config but got nil")
		}

		if config.WorkingDirectory != tempDir { //nolint:staticcheck
			t.Errorf("Expected working directory %s, got %s", tempDir, config.WorkingDirectory)
		}
	})

	// Test with custom config
	t.Run("custom config", func(t *testing.T) {
		customConfig := &ToolboxConfig{
			WorkingDirectory: tempDir,
			Timeout:          10 * time.Second,
			MaxFileSize:      2048,
			MaxResponseSize:  4096,
			UserAgent:        "CustomAgent/1.0",
			AllowedCommands:  []string{"echo"},
			BlockedCommands:  []string{"rm"},
		}

		toolbox := NewToolbox(tempDir, customConfig, nil)
		cfg := toolbox.GetConfig()

		if cfg == nil {
			t.Fatal("Expected config but got nil")
		}

		if cfg.Timeout != 10*time.Second {
			t.Errorf("Expected timeout 10s, got %v", cfg.Timeout)
		}

		if cfg.MaxFileSize != 2048 {
			t.Errorf("Expected max file size 2048, got %d", cfg.MaxFileSize)
		}

		if cfg.MaxResponseSize != 4096 {
			t.Errorf("Expected max response size 4096, got %d", cfg.MaxResponseSize)
		}

		if cfg.UserAgent != "CustomAgent/1.0" {
			t.Errorf("Expected user agent 'CustomAgent/1.0', got '%s'", cfg.UserAgent)
		}

		if len(cfg.AllowedCommands) != 1 || cfg.AllowedCommands[0] != "echo" {
			t.Errorf("Expected allowed commands ['echo'], got %v", cfg.AllowedCommands)
		}

		if len(cfg.BlockedCommands) != 1 || cfg.BlockedCommands[0] != "rm" {
			t.Errorf("Expected blocked commands ['rm'], got %v", cfg.BlockedCommands)
		}
	})
}

func TestToolboxConcurrency(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "toolbox_concurrency_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	toolbox := NewToolbox(tempDir, &ToolboxConfig{AllowedCommands: []string{"echo"}}, nil)
	ctx := context.Background()

	results := make(chan string, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			res, err := toolbox.Execute(ctx, "run_shell_command", map[string]interface{}{
				"command": fmt.Sprintf("echo Task %d", i),
			})
			if err != nil || !res.Success {
				results <- fmt.Sprintf("error: %v", err)
				return
			}
			// Use user-facing content which includes STDOUT
			results <- res.UserContent
		}(i)
	}

	count := 0
	for i := 0; i < 10; i++ {
		select {
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for results")
		case out := <-results:
			if !strings.Contains(out, "Task ") {
				t.Errorf("unexpected output: %s", out)
			}
			count++
		}
	}

	if count != 10 {
		t.Errorf("expected 10 results, got %d", count)
	}
}
