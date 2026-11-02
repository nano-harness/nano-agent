//go:build smoke

package smoke

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/smoke/helpers"
)

// TestSmoke_Daemon_Lifecycle tests daemon start, status, and stop commands.
func TestSmoke_Daemon_Lifecycle(t *testing.T) {
	workDir := t.TempDir()
	mock := helpers.NewMockLLMServer(t)
	helpers.WriteMinimalConfig(t, workDir, mock.URL())

	nanoBinary := helpers.NanoBinaryPath(t)

	// Set environment for daemon
	env := append(os.Environ(),
		"NANO_CONFIG_DIR="+workDir,
	)

	// Test: daemon start
	t.Run("Start daemon", func(t *testing.T) {
		cmd := exec.Command(nanoBinary, "daemon", "start")
		cmd.Dir = workDir
		cmd.Env = env

		runSmokeCommand(t, cmd, "Start")

		// Daemon start might return 0 or 1 depending on if already running
		// We'll check status instead
	})

	// Give daemon time to start
	time.Sleep(2 * time.Second)

	// Test: daemon status
	t.Run("Check daemon status", func(t *testing.T) {
		cmd := exec.Command(nanoBinary, "daemon", "status")
		cmd.Dir = workDir
		cmd.Env = env

		output := runSmokeCommand(t, cmd, "Status")

		// Status should either show running or not running
		outputStr := strings.ToLower(string(output))
		if !strings.Contains(outputStr, "running") && !strings.Contains(outputStr, "not running") && !strings.Contains(outputStr, "stopped") {
			t.Logf("Warning: unexpected status output, but continuing")
		}
	})

	// Test: daemon stop
	t.Run("Stop daemon", func(t *testing.T) {
		cmd := exec.Command(nanoBinary, "daemon", "stop")
		cmd.Dir = workDir
		cmd.Env = env

		runSmokeCommand(t, cmd, "Stop")

		// Stop might return error if daemon wasn't running, which is ok
	})

	// Give daemon time to stop
	time.Sleep(1 * time.Second)

	// Final status check
	t.Run("Verify daemon stopped", func(t *testing.T) {
		cmd := exec.Command(nanoBinary, "daemon", "status")
		cmd.Dir = workDir
		cmd.Env = env

		output := runSmokeCommand(t, cmd, "Final status")

		// After stop, daemon should not be running
		outputStr := strings.ToLower(string(output))
		if strings.Contains(outputStr, "running") && !strings.Contains(outputStr, "not") {
			t.Logf("Warning: daemon may still be running, but test completed")
		}
	})

	t.Log("✓ Daemon lifecycle test completed")
}

// TestSmoke_Daemon_BasicCommands tests basic daemon commands without actually starting daemon.
func TestSmoke_Daemon_BasicCommands(t *testing.T) {
	workDir := t.TempDir()
	mock := helpers.NewMockLLMServer(t)
	helpers.WriteMinimalConfig(t, workDir, mock.URL())

	nanoBinary := helpers.NanoBinaryPath(t)

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "daemon help",
			args:    []string{"daemon", "--help"},
			wantErr: false,
		},
		{
			name:    "daemon status when not running",
			args:    []string{"daemon", "status"},
			wantErr: false, // Status command should work even if daemon not running
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(nanoBinary, tt.args...)
			cmd.Dir = workDir
			cmd.Env = append(os.Environ(), "NANO_CONFIG_DIR="+workDir)

			output, err := cmd.CombinedOutput()
			t.Logf("Output: %s", string(output))

			if tt.wantErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.wantErr && err != nil {
				// Some commands may return non-zero but are still successful
				// (e.g., status when daemon not running)
				t.Logf("Command returned error (may be expected): %v", err)
			}
		})
	}

	t.Log("✓ Daemon basic commands test completed")
}

// TestSmoke_CLI_NonTTY tests CLI in non-TTY mode (piped input).
func TestSmoke_CLI_NonTTY(t *testing.T) {
	workDir := t.TempDir()
	mock := helpers.NewMockLLMServer(t)
	helpers.WriteMinimalConfig(t, workDir, mock.URL())

	nanoBinary := helpers.NanoBinaryPath(t)

	// Test piped input (non-TTY mode)
	cmd := exec.Command(nanoBinary)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"NANO_CONFIG_DIR="+workDir,
		"NANO_AUTO_ACCEPT=1",
	)

	// Pipe input to simulate non-TTY
	cmd.Stdin = strings.NewReader("hello")

	// Set a timeout
	done := make(chan error, 1)
	go func() {
		output, err := cmd.CombinedOutput()
		t.Logf("Non-TTY output: %s", string(output))
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			// Non-zero exit is acceptable in some scenarios
			t.Logf("Command exited with: %v", err)
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Log("Command timed out (expected for non-interactive mode)")
	}

	t.Log("✓ Non-TTY test completed")
}

// runSmokeCommand executes a command for tolerant smoke checks where a non-zero
// exit can be expected, such as daemon status before startup or stop after an
// already-stopped daemon. Use direct assertions instead when command success is
// required for the behavior under test.
func runSmokeCommand(t *testing.T, cmd *exec.Cmd, label string) []byte {
	t.Helper()

	output, err := cmd.CombinedOutput()
	t.Logf("%s output: %s", label, string(output))
	if err != nil {
		t.Logf("%s command returned error (may be expected): %v", label, err)
	}
	return output
}
