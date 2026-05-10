package slash

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

// CommandRuntime executes slash-command automation such as prelude shell lines.
type CommandRuntime struct {
	workingDir     string
	sandboxRuntime sandbox.Runtime
}

// PreludeResult records one prelude command execution.
type PreludeResult struct {
	Command string                 `json:"command"`
	Success bool                   `json:"success"`
	Error   string                 `json:"error,omitempty"`
	Output  string                 `json:"output,omitempty"`
	Sandbox map[string]interface{} `json:"sandbox,omitempty"`
}

// NewCommandRuntime creates a runtime for slash command automation.
func NewCommandRuntime(workingDir string, sandboxRuntime sandbox.Runtime) *CommandRuntime {
	return &CommandRuntime{workingDir: workingDir, sandboxRuntime: sandboxRuntime}
}

// ExecutePrelude runs a custom command's leading !shell lines through SandboxRuntime.
func (r *CommandRuntime) ExecutePrelude(ctx context.Context, cmd Command) ([]PreludeResult, error) {
	if r == nil || len(cmd.Prelude) == 0 {
		return nil, nil
	}
	timeout := time.Duration(cmd.PreludeTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	onError := strings.ToLower(strings.TrimSpace(cmd.PreludeOnError))
	if onError == "" {
		onError = "continue"
	}
	outputMode := strings.ToLower(strings.TrimSpace(cmd.PreludeOutput))
	if outputMode == "" {
		outputMode = "summary"
	}

	results := make([]PreludeResult, 0, len(cmd.Prelude))
	for _, prelude := range cmd.Prelude {
		result := PreludeResult{Command: prelude}
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		env, err := r.prepare(runCtx, prelude, timeout, cmd.Name)
		if err != nil {
			cancel()
			result.Error = err.Error()
			results = append(results, result)
			if onError == "abort" {
				return results, err
			}
			continue
		}

		execCmd := exec.CommandContext(runCtx, env.Command, env.Args...)
		if env.WorkingDir != "" {
			execCmd.Dir = env.WorkingDir
		} else if r.workingDir != "" {
			execCmd.Dir = r.workingDir
		}
		execCmd.Env = os.Environ()
		if runtime.GOOS != "windows" {
			execCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		}
		output, runErr := execCmd.CombinedOutput()
		result.Success = runErr == nil
		result.Sandbox = env.AuditMetadata()
		if outputMode == "full" {
			result.Output = string(output)
		} else if outputMode == "summary" && len(output) > 0 {
			result.Output = summarizeOutput(string(output))
		}
		if runErr != nil {
			result.Error = runErr.Error()
		}
		if r.sandboxRuntime != nil {
			_ = r.sandboxRuntime.Cleanup(context.Background(), env)
		}
		cancel()
		results = append(results, result)
		if runErr != nil && onError == "abort" {
			return results, fmt.Errorf("slash prelude %q failed: %w", prelude, runErr)
		}
	}
	return results, nil
}

func (r *CommandRuntime) prepare(ctx context.Context, command string, timeout time.Duration, commandName string) (*sandbox.SandboxEnvironment, error) {
	rawCmd := "sh"
	rawArgs := []string{"-c", command}
	if runtime.GOOS == "windows" {
		rawCmd = "cmd"
		rawArgs = []string{"/C", command}
	}
	if r.sandboxRuntime == nil {
		return &sandbox.SandboxEnvironment{
			Backend:    sandbox.BackendNone,
			Command:    rawCmd,
			Args:       rawArgs,
			WorkingDir: r.workingDir,
		}, nil
	}
	return r.sandboxRuntime.PrepareCommand(ctx, sandbox.SandboxRequest{
		Command:    rawCmd,
		Args:       rawArgs,
		WorkingDir: r.workingDir,
		Env:        os.Environ(),
		ResourceLimits: sandbox.ResourceLimits{
			Timeout: timeout,
		},
		Metadata: map[string]interface{}{
			"tool":          "slash_command_prelude",
			"slash_command": commandName,
		},
	})
}

func summarizeOutput(output string) string {
	output = strings.TrimSpace(output)
	if len(output) <= 512 {
		return output
	}
	return output[:512] + "…"
}
