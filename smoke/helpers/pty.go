//go:build smoke

package helpers

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	expect "github.com/Netflix/go-expect"
	"github.com/creack/pty"
)

const waitAfterKillTimeout = time.Second

// PTYSession represents a PTY session for testing nano-agent.
type PTYSession struct {
	t       *testing.T
	console *expect.Console
	cmd     *exec.Cmd
	pty     *os.File
}

// NewPTYSession creates a new PTY session for testing.
func NewPTYSession(t *testing.T, workDir string, args ...string) (*PTYSession, error) {
	// Skip on Windows where PTY support is limited
	if runtime.GOOS == "windows" {
		t.Skip("PTY tests not supported on Windows")
	}

	nanoBinary := NanoBinaryPath(t)

	// Create console with timeout
	console, err := expect.NewConsole(
		expect.WithDefaultTimeout(15 * time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create console: %w", err)
	}

	cmdArgs := append([]string{"--no-banner"}, args...)
	cmd := exec.Command(nanoBinary, cmdArgs...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"NANO_AUTO_ACCEPT=1",
		"TERM=xterm-256color",
	)

	// Start with PTY
	cmd.Stdin = console.Tty()
	cmd.Stdout = console.Tty()
	cmd.Stderr = console.Tty()

	session := &PTYSession{
		t:       t,
		console: console,
		cmd:     cmd,
	}

	// Cleanup on test completion
	t.Cleanup(func() {
		session.Close()
	})

	return session, nil
}

// NanoBinaryPath locates the development nano binary by finding the repository
// root via go.mod. It skips the test if the binary has not been built yet.
func NanoBinaryPath(t testing.TB) string {
	t.Helper()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	repoRoot, err := findRepoRoot(cwd)
	if err != nil {
		t.Fatalf("failed to locate repository root from %s: %v", cwd, err)
	}
	nanoBinary := filepath.Join(repoRoot, "bin", "nano")
	if _, err := os.Stat(nanoBinary); os.IsNotExist(err) {
		t.Skip("nano binary not found, run 'make dev' first")
	} else if err != nil {
		t.Fatalf("failed to stat nano binary at %s: %v", nanoBinary, err)
	}

	return nanoBinary
}

// findRepoRoot walks upward from the start directory until it finds go.mod,
// returning an error if the filesystem root is reached first.
func findRepoRoot(start string) (string, error) {
	dir := filepath.Clean(start)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found starting from %s", start)
		}
		dir = parent
	}
}

// Start starts the PTY session.
func (s *PTYSession) Start() error {
	// Start the command with PTY
	ptmx, err := pty.Start(s.cmd)
	if err != nil {
		return fmt.Errorf("failed to start PTY: %w", err)
	}

	s.pty = ptmx

	// Copy PTY output to console for expect to work
	go func() {
		_, _ = io.Copy(s.console.Tty(), ptmx)
	}()

	// Give the TUI time to initialize
	time.Sleep(500 * time.Millisecond)

	return nil
}

// Expect waits for the given string to appear in output.
func (s *PTYSession) Expect(str string) error {
	_, err := s.console.ExpectString(str)
	return err
}

// ExpectWithTimeout waits for the given string with a custom timeout.
func (s *PTYSession) ExpectWithTimeout(str string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Buffered so the goroutine can finish if the context timeout case returns first.
	done := make(chan error, 1)
	go func() {
		_, err := s.console.ExpectString(str)
		done <- err
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for %q", str)
	}
}

// Send sends a string to the PTY.
func (s *PTYSession) Send(str string) error {
	_, err := s.console.Send(str)
	return err
}

// SendLine sends a string followed by Enter.
func (s *PTYSession) SendLine(str string) error {
	_, err := s.console.SendLine(str)
	return err
}

// SendCtrlC sends Ctrl+C signal.
func (s *PTYSession) SendCtrlC() error {
	_, err := s.console.Send("\x03")
	return err
}

// Wait waits for the command to complete.
func (s *PTYSession) Wait() error {
	if s.cmd.Process == nil {
		return nil
	}
	return s.cmd.Wait()
}

// WaitWithTimeout waits for the command to complete up to the given timeout.
func (s *PTYSession) WaitWithTimeout(timeout time.Duration) error {
	if s.cmd.Process == nil {
		return nil
	}

	// Buffered so the goroutine can finish if the timeout branch returns first.
	done := make(chan error, 1)
	go func() {
		done <- s.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		if err := s.Kill(); err != nil {
			return fmt.Errorf("timeout waiting for command after %s; failed to kill process: %w", timeout, err)
		}
		select {
		case err := <-done:
			return fmt.Errorf("timeout waiting for command after %s; process terminated with: %w", timeout, err)
		case <-time.After(waitAfterKillTimeout):
			return fmt.Errorf("timeout waiting for command after %s; process did not report termination within %s after kill", timeout, waitAfterKillTimeout)
		}
	}
}

// Kill forcefully terminates the process.
func (s *PTYSession) Kill() error {
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

// Close cleans up the PTY session.
func (s *PTYSession) Close() {
	if s.console != nil {
		_ = s.console.Close()
	}
	if s.pty != nil {
		_ = s.pty.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

// WaitForPrompt waits for the TUI input prompt to appear.
func (s *PTYSession) WaitForPrompt() error {
	// TUI typically shows ">" or similar prompt
	return s.ExpectWithTimeout(">", 10*time.Second)
}
