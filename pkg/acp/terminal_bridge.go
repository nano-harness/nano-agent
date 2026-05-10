package acp

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// TerminalBridge bridges nano-agent shell commands to ACP terminal/* operations
type TerminalBridge struct {
	acpSessionID string
	transport    *Transport
	mu           sync.Mutex
	processes    map[string]*TerminalProcess
	nextID       int
}

// TerminalProcess represents a running terminal process
type TerminalProcess struct {
	ID       string
	Cmd      *exec.Cmd
	Stdin    io.WriteCloser
	Stdout   io.ReadCloser
	Stderr   io.ReadCloser
	Running  bool
	ExitCode *int
	mu       sync.Mutex
}

// NewTerminalBridge creates a new terminal bridge
func NewTerminalBridge(acpSessionID string, transport *Transport) *TerminalBridge {
	return &TerminalBridge{
		acpSessionID: acpSessionID,
		transport:    transport,
		processes:    make(map[string]*TerminalProcess),
	}
}

// Run implements ACP terminal/run operation
func (b *TerminalBridge) Run(ctx context.Context, command string, cwd string, env map[string]string) (string, error) {
	b.mu.Lock()
	processID := fmt.Sprintf("term-%s-%d", b.acpSessionID, b.nextID)
	b.nextID++
	b.mu.Unlock()

	logger.Infof("ACP: Running terminal command %s: %s", processID, command)

	// Create command
	cmd := exec.CommandContext(ctx, "sh", "-c", command)

	// Set working directory
	if cwd != "" {
		cmd.Dir = cwd
	}

	// Set environment variables
	if len(env) > 0 {
		cmd.Env = append(cmd.Env, cmd.Environ()...)
		for k, v := range env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Create pipes for stdin/stdout/stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("create stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start process: %w", err)
	}

	// Create process record
	proc := &TerminalProcess{
		ID:      processID,
		Cmd:     cmd,
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
		Running: true,
	}

	b.mu.Lock()
	b.processes[processID] = proc
	b.mu.Unlock()

	// Monitor process in background
	go b.monitorProcess(proc)

	return processID, nil
}

// monitorProcess monitors a running process and sends output events
func (b *TerminalBridge) monitorProcess(proc *TerminalProcess) {
	// Read stdout in background
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := proc.Stdout.Read(buf)
			if n > 0 {
				output := string(buf[:n])
				// Send terminal/output notification
				_ = b.transport.SendNotification("terminal/output", map[string]interface{}{
					"sessionId": b.acpSessionID,
					"processId": proc.ID,
					"stream":    "stdout",
					"data":      output,
				})
			}
			if err != nil {
				break
			}
		}
	}()

	// Read stderr in background
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := proc.Stderr.Read(buf)
			if n > 0 {
				output := string(buf[:n])
				// Send terminal/output notification
				_ = b.transport.SendNotification("terminal/output", map[string]interface{}{
					"sessionId": b.acpSessionID,
					"processId": proc.ID,
					"stream":    "stderr",
					"data":      output,
				})
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for process to finish
	err := proc.Cmd.Wait()

	proc.mu.Lock()
	proc.Running = false
	exitCode := proc.Cmd.ProcessState.ExitCode()
	proc.ExitCode = &exitCode
	proc.mu.Unlock()

	// Send terminal/exit notification
	exitEvent := map[string]interface{}{
		"sessionId": b.acpSessionID,
		"processId": proc.ID,
		"exitCode":  exitCode,
	}
	if err != nil && exitCode != 0 {
		exitEvent["error"] = err.Error()
	}
	_ = b.transport.SendNotification("terminal/exit", exitEvent)

	logger.Infof("ACP: Terminal process %s exited with code %d", proc.ID, exitCode)
}

// Input implements ACP terminal/input operation
func (b *TerminalBridge) Input(ctx context.Context, processID string, data string) error {
	b.mu.Lock()
	proc, ok := b.processes[processID]
	b.mu.Unlock()

	if !ok {
		return fmt.Errorf("process not found: %s", processID)
	}

	proc.mu.Lock()
	running := proc.Running
	proc.mu.Unlock()

	if !running {
		return fmt.Errorf("process not running: %s", processID)
	}

	// Write to stdin
	if _, err := proc.Stdin.Write([]byte(data)); err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}

	return nil
}

// Kill implements ACP terminal/kill operation
func (b *TerminalBridge) Kill(ctx context.Context, processID string) error {
	b.mu.Lock()
	proc, ok := b.processes[processID]
	b.mu.Unlock()

	if !ok {
		return fmt.Errorf("process not found: %s", processID)
	}

	proc.mu.Lock()
	running := proc.Running
	proc.mu.Unlock()

	if !running {
		return fmt.Errorf("process not running: %s", processID)
	}

	logger.Infof("ACP: Killing terminal process: %s", processID)

	// Kill the process
	if err := proc.Cmd.Process.Kill(); err != nil {
		return fmt.Errorf("kill process: %w", err)
	}

	// Wait with timeout for process to exit
	done := make(chan struct{})
	go func() {
		proc.Cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Infof("ACP: Terminal process %s killed successfully", processID)
	case <-time.After(5 * time.Second):
		logger.Warnf("ACP: Terminal process %s did not exit after kill", processID)
	}

	return nil
}
