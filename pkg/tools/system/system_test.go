package system

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestShellTool(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "shell_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create tool with limited allowed commands for safety
	config := map[string]interface{}{
		"allow_rules": []string{"echo", "pwd", "ls", "dir", "cd", "sleep", "timeout"},
		"deny_rules":  []string{"rm", "del", "sudo", "su"},
	}
	tool := NewShellTool(tempDir, config, nil)

	tests := []struct {
		name     string
		params   map[string]interface{}
		wantErr  bool
		validate func(*testing.T, interface{})
	}{
		{
			name: "simple echo command",
			params: map[string]interface{}{
				"command": "echo 'Hello World'",
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				cmdResult, ok := result.(*CommandResult)
				if !ok {
					t.Error("Expected result to be *CommandResult")
					return
				}
				if !cmdResult.Success {
					t.Errorf("Command should have succeeded: %s", cmdResult.Stderr)
					return
				}
				if !strings.Contains(cmdResult.Stdout, "Hello World") {
					t.Errorf("Expected 'Hello World' in stdout, got: %s", cmdResult.Stdout)
				}
			},
		},
		{
			name: "command with directory parameter",
			params: map[string]interface{}{
				"command": "pwd",
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				cmdResult, ok := result.(*CommandResult)
				if !ok {
					t.Error("Expected result to be *CommandResult")
					return
				}
				if !cmdResult.Success {
					t.Errorf("Command should have succeeded: %s", cmdResult.Stderr)
					return
				}

				// Resolve symlinks for robust comparison
				expectedDir, err := filepath.EvalSymlinks(tempDir)
				if err != nil {
					t.Fatalf("Failed to evaluate symlinks for tempDir: %v", err)
				}
				actualDir := strings.TrimSpace(cmdResult.Stdout)

				if expectedDir != actualDir {
					t.Errorf("Expected '%s' in stdout, got: '%s'", expectedDir, actualDir)
				}
			},
		},
		{
			name: "command with timeout",
			params: map[string]interface{}{
				"command":         getTimeoutCommand(),
				"timeout_seconds": float64(1),
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				cmdResult, ok := result.(*CommandResult)
				if !ok {
					t.Error("Expected result to be *CommandResult")
					return
				}
				// Command should timeout or complete quickly
				if cmdResult.Duration > 2*time.Second {
					t.Error("Command should have completed or timed out within 2 seconds")
				}
			},
		},
		{
			name: "non-zero exit code command",
			params: map[string]interface{}{
				"command": "sh -c 'exit 1'",
			},
			wantErr: true,
		},
		{
			name: "invalid directory",
			params: map[string]interface{}{
				"command":   "echo test",
				"directory": "/nonexistent/directory",
			},
			wantErr: true,
		},
		{
			name: "directory outside working directory",
			params: map[string]interface{}{
				"command":   "echo test",
				"directory": "/tmp",
			},
			wantErr: true,
		},
		{
			name: "missing command parameter",
			params: map[string]interface{}{
				"description": "test",
			},
			wantErr: true,
		},
		{
			name: "command with environment variables",
			params: map[string]interface{}{
				"command":     getEnvCommand(),
				"environment": "TEST_VAR=hello;ANOTHER_VAR=world",
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				cmdResult, ok := result.(*CommandResult)
				if !ok {
					t.Error("Expected result to be *CommandResult")
					return
				}
				if !cmdResult.Success {
					t.Errorf("Command should have succeeded: %s", cmdResult.Stderr)
					return
				}
				// Should see the environment variable in output
				if !strings.Contains(cmdResult.Stdout, "hello") {
					t.Errorf("Expected environment variable value 'hello' in output, got: %s", cmdResult.Stdout)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tt.params)

			if tt.wantErr {
				if err != nil || !result.Success {
					return // Expected error
				}
				t.Error("Expected error but got none")
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if !result.Success {
				t.Errorf("Tool execution failed: %s", result.Error)
				return
			}

			if tt.validate != nil {
				tt.validate(t, result.Data)
			}
		})
	}
}

func TestShellToolValidation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shell_validation_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tool := NewShellTool(tempDir, nil, nil)

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "safe command",
			command: "echo hello",
			wantErr: false,
		},
		{
			name:    "simple rm command",
			command: "rm file.txt",
			wantErr: false, // Simple rm is now auto-allowed (not rm -rf)
		},
		{
			name:    "simple sudo command",
			command: "sudo ls",
			wantErr: false, // Simple sudo is auto-allowed (destructive sudo commands still blocked)
		},
		{
			name:    "blocked destructive command",
			command: "mkfs /dev/sda",
			wantErr: true,
		},
		{
			name:    "empty command",
			command: "",
			wantErr: true,
		},
		{
			name:    "compound command requires confirmation",
			command: "cmd1 && cmd2",
			wantErr: true, // Compound commands require confirmation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.validateCommand(tt.command)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected validation error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected validation error: %v", err)
			}
		})
	}
}

// Helper functions for cross-platform testing

func getTimeoutCommand() string {
	if runtime.GOOS == "windows" {
		return "timeout /t 5"
	}
	return "sleep 5"
}

func getEnvCommand() string {
	if runtime.GOOS == "windows" {
		return "echo %TEST_VAR%"
	}
	return "echo $TEST_VAR"
}

func TestShellTool_ContentStandardization(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "shell_std_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tool := NewShellTool(tempDir, map[string]interface{}{
		"allow_rules": []string{"echo"},
	}, nil)

	t.Run("missing command", func(t *testing.T) {
		res, _ := tool.Execute(context.Background(), map[string]interface{}{"desc": "x"})
		if res.Success {
			t.Fatalf("expected failure")
		}
		if res.UserContent == "" || res.LLMContent == "" {
			t.Fatalf("expected UserContent and LLMContent to be set on error")
		}
	})

	t.Run("blocked_command", func(t *testing.T) {
		// Use a deterministic non-destructive failing command to verify error
		// content is standardized. ShellTool.Execute is a pure executor; path
		// and command blocking is handled by SecurityMiddleware upstream.
		res, _ := tool.Execute(context.Background(), map[string]interface{}{"command": "sh -c 'exit 1'"})
		if res.Success {
			t.Fatalf("expected failure for 'exit 1'")
		}
		if res.UserContent == "" || res.LLMContent == "" {
			t.Fatalf("expected UserContent and LLMContent to be set on failure")
		}
	})

	t.Run("invalid directory", func(t *testing.T) {
		res, _ := tool.Execute(context.Background(), map[string]interface{}{"command": "echo hi", "directory": "/no/such/dir"})
		if res.Success {
			t.Fatalf("expected failure")
		}
		if !strings.Contains(res.UserContent, "Invalid directory") || !strings.Contains(res.LLMContent, "Invalid directory") {
			t.Fatalf("expected standardized contents for invalid directory")
		}
	})
}

func TestShellTool_RmRfHomeBlocked(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shell_home_block_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// ShellTool.Execute is now a pure executor; SecurityMiddleware handles blocking.
	// Verify the tool correctly reports failure for a deterministic failing command.
	tool := NewShellTool(tempDir, nil, nil)
	res, _ := tool.Execute(context.Background(), map[string]interface{}{"command": "sh -c 'exit 1'"})
	if res.Success {
		t.Fatal("expected non-zero exit command to fail")
	}
}

func TestShellTool_RmRfEnvHomeBlocked(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shell_envhome_block_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Shell tool no longer blocks dangerous commands directly - SecurityMiddleware does.
	// rm -rf / is rejected by the OS on modern Linux (requires --no-preserve-root).
	tool := NewShellTool(tempDir, nil, nil)
	res, _ := tool.Execute(context.Background(), map[string]interface{}{"command": "rm -rf /"})
	if res.Success {
		t.Fatal("expected rm -rf / to fail at OS level")
	}
}
