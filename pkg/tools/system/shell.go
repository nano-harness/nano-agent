package system //nolint:revive

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/middleware"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

// MaxShellOutputBytes is the maximum size of command output to capture (16MB, aligned with Gemini CLI)
const MaxShellOutputBytes = 16 * 1024 * 1024

// OutputCallback is a function that receives streaming output from a command
type OutputCallback func(stream string, chunk string) // stream: "stdout" or "stderr"

// outputCallbackKey is the context key for OutputCallback
type outputCallbackKey struct{}

// WithOutputCallback returns a new context with the OutputCallback attached
func WithOutputCallback(ctx context.Context, cb OutputCallback) context.Context {
	return context.WithValue(ctx, outputCallbackKey{}, cb)
}

// outputCallbackFromContext retrieves the OutputCallback from context, if present
func outputCallbackFromContext(ctx context.Context) OutputCallback {
	if cb, ok := ctx.Value(outputCallbackKey{}).(OutputCallback); ok {
		return cb
	}
	return nil
}

// ShellTool implements shell command execution with safety controls.
// Command security is enforced via the four-layer CommandGuard in pkg/middleware.
type ShellTool struct {
	workingDir     string
	config         map[string]interface{}
	allowedEnvVars []string
	blockedEnvVars []string
	strict         bool
	sandboxRuntime sandbox.Runtime
	guard          *middleware.CommandGuard
	bgManager      *BackgroundTaskManager // Optional: for background task support
}

// NewShellTool creates a new ShellTool instance.
// sandboxCfg may be nil to disable process-level sandboxing.
// secCfg may be nil to use default command security rules.
func NewShellTool(workingDir string, cfg map[string]interface{}, sandboxCfg *config.SandboxConfig) *ShellTool {
	if cfg == nil {
		cfg = make(map[string]interface{})
	}

	tool := &ShellTool{
		workingDir:     workingDir,
		config:         cfg,
		sandboxRuntime: sandbox.NewRuntime(sandboxCfg, workingDir),
	}

	// Load env var filters and strict mode from tool config.
	if allowedEnv, ok := cfg["allowed_env_vars"].([]string); ok {
		tool.allowedEnvVars = allowedEnv
	}
	if blockedEnv, ok := cfg["blocked_env_vars"].([]string); ok {
		tool.blockedEnvVars = blockedEnv
	}
	if strict, ok := cfg["strict"].(bool); ok {
		tool.strict = strict
	}

	// Build CommandGuard from config allow/deny rule lists.
	var allowRules, denyRules []string
	if ar, ok := cfg["allow_rules"].([]string); ok {
		allowRules = ar
	}
	if dr, ok := cfg["deny_rules"].([]string); ok {
		denyRules = dr
	}
	var sensitiveReadPaths, arbitraryExecCommands []string
	if paths, ok := cfg["sensitive_read_paths"].([]string); ok {
		sensitiveReadPaths = paths
	}
	if commands, ok := cfg["arbitrary_exec_commands"].([]string); ok {
		arbitraryExecCommands = commands
	}
	tool.guard = middleware.NewCommandGuardWithConfig(allowRules, denyRules, nil, workingDir, sensitiveReadPaths, arbitraryExecCommands)

	return tool
}

// NewShellToolWithBgManager creates a new ShellTool with background task support
func NewShellToolWithBgManager(workingDir string, cfg map[string]interface{}, sandboxCfg *config.SandboxConfig, bgManager *BackgroundTaskManager) *ShellTool {
	tool := NewShellTool(workingDir, cfg, sandboxCfg)
	tool.bgManager = bgManager
	return tool
}

func (t *ShellTool) Name() string { //nolint:revive
	return "run_shell_command"
}

func (t *ShellTool) Description() string { //nolint:revive
	return "Execute shell commands with safety controls and output streaming"
}

func (t *ShellTool) Category() interfaces.ToolCategory { //nolint:revive
	return interfaces.CategoryShell
}

// RequiresConfirmationForParams uses the CommandGuard four-layer analysis to
// determine if the command needs user confirmation.
func (t *ShellTool) RequiresConfirmationForParams(params map[string]interface{}) bool {
	command, ok := params["command"].(string)
	if !ok {
		return true
	}
	return t.RequiresConfirmationForCommand(command)
}

func (t *ShellTool) RequiresConfirmation() bool { //nolint:revive
	return false // Confirmation is determined dynamically by CommandGuard
}

// ConcurrencySafe returns false: shell commands can modify the filesystem and system state.
func (t *ShellTool) ConcurrencySafe() bool { return false }

// RequiresConfirmationForCommand runs the CommandGuard analysis and returns true
// only when ActionConfirm is returned. ActionBlock is handled separately by
// AnalyzeSecurity / tool_scheduler and does not require a confirmation dialog.
func (t *ShellTool) RequiresConfirmationForCommand(command string) bool {
	if t.guard == nil {
		return false
	}
	decision, err := t.AnalyzeCommand(context.Background(), command)
	if err != nil {
		return true
	}
	return decision.Action == middleware.ActionConfirm
}

// AnalyzeCommand runs the CommandGuard analysis and returns the full Decision.
// This is the single source of truth for command security analysis; the result
// is propagated via context so downstream layers (SecurityMiddleware,
// ShellTool.Execute) can skip redundant re-analysis.
func (t *ShellTool) AnalyzeCommand(ctx context.Context, command string) (*middleware.Decision, error) {
	if t.guard == nil {
		return &middleware.Decision{Action: middleware.ActionAllow, Reason: "no guard configured"}, nil
	}
	return t.guard.Analyze(ctx, command)
}

// AnalyzeSecurityDecision returns the full security decision, including any
// hook-proposed parameter modifications that should be applied before execution.
func (t *ShellTool) AnalyzeSecurityDecision(ctx context.Context, params map[string]interface{}) (*middleware.Decision, error) {
	command, ok := params["command"].(string)
	if !ok {
		return &middleware.Decision{Action: middleware.ActionConfirm, Reason: "command parameter missing"}, nil
	}
	return t.AnalyzeCommand(ctx, command)
}

// AnalyzeSecurity implements interfaces.SecurityAnalyzableTool.
// It returns the security action as an int so the interfaces package does not
// need to import the middleware package (which would create a cycle).
//
//	0 = middleware.ActionAllow
//	1 = middleware.ActionConfirm
//	2 = middleware.ActionBlock
func (t *ShellTool) AnalyzeSecurity(ctx context.Context, params map[string]interface{}) (action int, reason string, err error) {
	command, ok := params["command"].(string)
	if !ok {
		return int(middleware.ActionConfirm), "command parameter missing", nil
	}
	d, err := t.AnalyzeCommand(ctx, command)
	if err != nil {
		return int(middleware.ActionConfirm), "analysis error", err
	}
	return int(d.Action), d.Reason, nil
}

// validateCommand checks whether the command is permitted by the CommandGuard.
// Returns a non-nil error when the command is empty, blocked, or requires confirmation.
// Called directly by TestShellToolValidation.
func (t *ShellTool) validateCommand(command string) error { //nolint:unused
	if command == "" {
		return fmt.Errorf("command cannot be empty")
	}
	if t.guard == nil {
		return nil
	}
	decision, err := t.guard.Analyze(context.Background(), command)
	if err != nil {
		return fmt.Errorf("command validation error: %w", err)
	}
	if decision.Action == middleware.ActionBlock {
		return fmt.Errorf("command blocked by security policy: %s", decision.Reason)
	}
	if decision.Action == middleware.ActionConfirm {
		return fmt.Errorf("command requires confirmation: %s", decision.Reason)
	}
	return nil
}

func (t *ShellTool) Schema() *interfaces.ToolSchema { //nolint:revive
	commandProp := interfaces.NewStringProperty("Shell command to execute")
	commandProp.Examples = []string{"git status", "npm install", "go test ./...", "docker ps"}
	commandProp.Usage = "IMPORTANT: This tool is for terminal operations like git, npm, docker, build tools, etc. " +
		"DO NOT use it for: file reading (use read_file), file writing (use write_file/edit_file), " +
		"web fetching (use web_fetch), " +
		"Use it for file listing and search with commands like ls, tree, find, fd, rg, git grep, or grep. " +
		"skill installation (use manage_skill with action=install). " +
		"Commands are security-filtered. Dangerous commands (rm, sudo, curl, wget, etc.) are blocked in daemon mode."

	descProp := interfaces.NewStringProperty("Optional description of what the command does")
	descProp.Examples = []string{"List files in current directory", "Search for TODO comments", "Find Python files"}
	descProp.Usage = "Helps track and document command purpose. Displayed in execution output."

	dirProp := interfaces.NewStringProperty("Directory to run the command in (defaults to working directory)")
	dirProp.Examples = []string{"./src", "/Users/user/project", "."}
	dirProp.Usage = "Must be within workspace. Relative paths are resolved against working directory."

	timeoutProp := interfaces.NewNumberProperty("Command timeout in seconds (default: 120, max: 600)")
	timeoutProp.Examples = []string{"5", "30", "60", "120", "300"}
	timeoutProp.Usage = "Commands exceeding timeout are automatically converted to background tasks. Use bash_output to monitor them."

	captureProp := interfaces.NewBooleanProperty("Capture and return command output")
	captureProp.Examples = []string{"true", "false"}
	captureProp.Usage = "When true, stdout/stderr are captured. When false, command runs without capturing output."

	envProp := interfaces.NewStringProperty("Additional environment variables (KEY=value format, separated by ;)")
	envProp.Examples = []string{"NODE_ENV=production", "DEBUG=true;LOG_LEVEL=info", "PATH=/usr/local/bin:$PATH"}
	envProp.Usage = "Format: KEY=value;KEY2=value2. Variables are filtered by policy; blocked keys are dropped. In strict mode, only allowed keys are accepted."

	isBackgroundProp := interfaces.NewBooleanProperty("Run command in background and return task_id immediately")
	isBackgroundProp.Examples = []string{"true", "false"}
	isBackgroundProp.Usage = "Best for dev servers, watchers, and long-running commands. Use bash_output/kill_bash to manage background tasks."

	return interfaces.CreateSchema(
		"Execute shell commands with safety controls",
		map[string]*interfaces.PropertySchema{
			"command":         commandProp,
			"description":     descProp,
			"directory":       dirProp,
			"timeout_seconds": timeoutProp,
			"capture_output":  captureProp,
			"environment":     envProp,
			"is_background":   isBackgroundProp,
		},
		[]string{"command"},
	)
}

type CommandResult struct { //nolint:revive
	Command   string                 `json:"command"`
	ExitCode  int                    `json:"exit_code"`
	Stdout    string                 `json:"stdout"`
	Stderr    string                 `json:"stderr"`
	Duration  time.Duration          `json:"duration"`
	Directory string                 `json:"directory"`
	Success   bool                   `json:"success"`
	TimedOut  bool                   `json:"timed_out"`
	PID       int                    `json:"pid"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

func (t *ShellTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) { //nolint:revive
	if decision, ok := middleware.GetSecurityDecision(ctx); ok && len(decision.ModifiedParams) > 0 {
		params = middleware.MergeDecisionParams(params, decision.ModifiedParams)
	}

	// Extract parameters
	command, ok := params["command"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "command parameter is required and must be a string",
			UserContent: "❌ Failed to run command: command parameter is required and must be a string",
			LLMContent:  "run_shell_command failed: command parameter is required and must be a string",
		}, nil
	}

	// Get optional parameters
	description := ""
	if descParam, ok := params["description"].(string); ok {
		description = descParam
	}

	directory := t.workingDir
	if dirParam, ok := params["directory"].(string); ok && dirParam != "" {
		directory = dirParam
	}

	timeout := 120 * time.Second // Default 120s (Phase 2)
	maxTimeout := 600 * time.Second
	if timeoutParam, ok := params["timeout_seconds"]; ok {
		if timeoutFloat, ok := timeoutParam.(float64); ok {
			timeout = time.Duration(timeoutFloat) * time.Second
			if timeout > maxTimeout {
				timeout = maxTimeout
			}
		}
	}

	captureOutput := true
	if captureParam, ok := params["capture_output"]; ok {
		captureOutput, _ = captureParam.(bool)
	}

	environment := ""
	if envParam, ok := params["environment"].(string); ok {
		environment = envParam
	}

	isBackground := false
	if bgParam, ok := params["is_background"].(bool); ok {
		isBackground = bgParam
	}

	// Validate directory
	absDir, err := t.validateDirectory(directory)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("Invalid directory: %v", err),
			UserContent: "❌ Failed to run command: Invalid directory: " + err.Error(),
			LLMContent:  "run_shell_command failed: Invalid directory: " + err.Error(),
		}, nil
	}

	// Guard check: only run if no security decision was already computed upstream
	// (by tool_scheduler). This acts as a fallback for callers that bypass the
	// scheduler and invoke the tool directly.
	if !middleware.HasSecurityDecision(ctx) && t.guard != nil {
		decision, err := t.AnalyzeCommand(ctx, command)
		if err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				Error:       fmt.Sprintf("Command security analysis failed: %v", err),
				UserContent: "❌ Failed to run command: security analysis error: " + err.Error(),
				LLMContent:  "run_shell_command failed: security analysis error: " + err.Error(),
			}, nil
		}
		if decision.Action == middleware.ActionBlock {
			return &interfaces.ToolResult{
				Success:     false,
				Error:       "command blocked by security policy: " + decision.Reason,
				UserContent: "❌ Command blocked: " + decision.Reason,
				LLMContent:  "run_shell_command blocked by security: " + decision.Reason,
			}, nil
		}
		if decision.Action == middleware.ActionConfirm {
			return &interfaces.ToolResult{
				Success:     false,
				Error:       "command requires user confirmation: " + decision.Reason,
				UserContent: "⚠️ Command requires confirmation: " + decision.Reason,
				LLMContent:  "run_shell_command requires user confirmation: " + decision.Reason,
			}, nil
		}
	}

	// Handle background execution if requested
	if isBackground {
		return t.spawnBackground(ctx, command, absDir, description, false)
	}

	// Execute command with timeout (auto-background on timeout)
	return t.executeWithAutoBackground(ctx, command, absDir, timeout, captureOutput, environment, description)
}

// spawnBackground spawns a command as a background task
func (t *ShellTool) spawnBackground(ctx context.Context, command, directory, description string, autoBackgrounded bool) (*interfaces.ToolResult, error) {
	if t.bgManager == nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "background task manager not available",
			UserContent: "❌ Background tasks not supported in this configuration",
			LLMContent:  "Background tasks require background task manager to be initialized",
		}, nil
	}

	// Get session ID from Turn context (added in Phase 2).
	sessionID := "default"
	if tc, ok := ctx.Value(interfaces.TurnContextKey{}).(interfaces.TurnContext); ok && tc.SessionID != "" {
		sessionID = tc.SessionID
	}

	task, err := t.bgManager.Spawn(ctx, sessionID, command, directory)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("Failed to spawn background task: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to start background task: %v", err),
			LLMContent:  fmt.Sprintf("Failed to spawn background task: %v", err),
		}, nil
	}

	var content string
	if autoBackgrounded {
		content = fmt.Sprintf("⏰ Command exceeded timeout and was converted to background task\n"+
			"Task ID: %s\n"+
			"PID: %d\n"+
			"Command: %s\n"+
			"Directory: %s\n"+
			"\n"+
			"⚠️ Note: Command was restarted in background. Use bash_output tool to monitor output.\n"+
			"Example: bash_output(task_id=\"%s\")",
			task.ID, task.Pid, command, directory, task.ID)
	} else {
		content = fmt.Sprintf("✅ Background task started\n"+
			"Task ID: %s\n"+
			"PID: %d\n"+
			"Command: %s\n"+
			"Directory: %s\n"+
			"\n"+
			"Use bash_output tool to monitor output.\n"+
			"Example: bash_output(task_id=\"%s\")",
			task.ID, task.Pid, command, directory, task.ID)
	}

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: content,
		LLMContent:  content,
		Metadata: map[string]interface{}{
			"task_id":           task.ID,
			"pid":               task.Pid,
			"command":           command,
			"directory":         directory,
			"auto_backgrounded": autoBackgrounded,
		},
	}, nil
}

// executeWithAutoBackground executes a command with automatic background conversion on timeout
func (t *ShellTool) executeWithAutoBackground(ctx context.Context, command, directory string, timeout time.Duration, captureOutput bool, environment, description string) (*interfaces.ToolResult, error) {
	syncCtx, syncCancel := context.WithTimeout(ctx, timeout)
	defer syncCancel()

	resultCh := make(chan *interfaces.ToolResult, 1)
	go func() {
		result, err := t.executeCommand(syncCtx, command, directory, timeout, captureOutput, environment)
		if err != nil {
			resultCh <- &interfaces.ToolResult{
				Success:     false,
				Error:       fmt.Sprintf("Command execution failed: %v", err),
				UserContent: "❌ Failed to run command: Command execution failed: " + err.Error(),
				LLMContent:  "run_shell_command failed: Command execution failed: " + err.Error(),
			}
			return
		}

		// Prepare metadata
		metadata := map[string]interface{}{
			"command":        command,
			"description":    description,
			"directory":      directory,
			"timeout":        timeout.Seconds(),
			"capture_output": captureOutput,
			"environment":    environment,
		}

		// Add truncation information from result metadata if present
		if result.Metadata != nil {
			if truncated, ok := result.Metadata["output_truncated"].(bool); ok && truncated {
				metadata["output_truncated"] = true
				metadata["max_output_bytes"] = MaxShellOutputBytes
			}
		}

		// Format content for display
		userContent := t.formatForUser(result, metadata)
		llmContent := t.formatForLLM(result, metadata)

		resultCh <- &interfaces.ToolResult{
			Success:     result.Success,
			Data:        result,
			Metadata:    metadata,
			LLMContent:  llmContent,
			UserContent: userContent,
		}
	}()

	select {
	case r := <-resultCh:
		return r, nil
	case <-syncCtx.Done():
		if syncCtx.Err() == context.DeadlineExceeded && t.bgManager != nil {
			// Timeout - convert to background task
			return t.spawnBackground(ctx, command, directory, description, true)
		}
		// No background manager - wait for the command to finish timing out naturally
		// The command will complete shortly with a timeout result
		r := <-resultCh
		return r, nil
	}
}

func (t *ShellTool) validateDirectory(directory string) (string, error) {
	// Clean the path
	cleaned := filepath.Clean(directory)

	// Convert to absolute path
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %v", err)
	}

	// Check if directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return "", fmt.Errorf("directory does not exist: %s", absPath)
	}

	// Check if path is within working directory (security check)
	workingDirAbs, err := filepath.Abs(t.workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to get working directory absolute path: %v", err)
	}

	relPath, err := filepath.Rel(workingDirAbs, absPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("directory outside working directory not allowed: %s", directory)
	}

	return absPath, nil
}

func (t *ShellTool) executeCommand(ctx context.Context, command, directory string, timeout time.Duration, captureOutput bool, environment string) (*CommandResult, error) {
	start := time.Now()

	// Create command context with timeout
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Choose shell based on OS and build initial (pre-sandbox) command.
	var rawCmd string
	var rawArgs []string
	if runtime.GOOS == "windows" {
		rawCmd = "cmd"
		rawArgs = []string{"/C", command}
	} else {
		rawCmd = "sh"
		rawArgs = []string{"-c", command}
	}

	cmdEnv := t.buildEnvironment(environment)

	// Prepare the command through SandboxRuntime. The initial implementation
	// adapts existing bwrap / sandbox-exec / noop behavior behind a stable API.
	sandboxEnv, err := t.sandboxRuntime.PrepareCommand(ctx, sandbox.SandboxRequest{
		Command:        rawCmd,
		Args:           rawArgs,
		WorkingDir:     directory,
		Env:            cmdEnv,
		ResourceLimits: sandbox.ResourceLimits{Timeout: timeout},
		Metadata: map[string]interface{}{
			"tool": "run_shell_command",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox wrap failed: %w", err)
	}

	cmd := exec.CommandContext(cmdCtx, sandboxEnv.Command, sandboxEnv.Args...)
	sandboxPublisher := sandbox.EventPublisherFromContext(ctx)

	// Set working directory
	cmd.Dir = directory

	cmd.Env = cmdEnv

	// Set process group for better process management
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	result := &CommandResult{
		Command:   command,
		Directory: directory,
		Duration:  0,
		Success:   false,
		TimedOut:  false,
		Metadata: map[string]interface{}{
			"sandbox": sandboxEnv.AuditMetadata(),
		},
	}

	if captureOutput {
		// Capture stdout and stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout pipe: %v", err)
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			return nil, fmt.Errorf("failed to create stderr pipe: %v", err)
		}

		// Start the command
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("failed to start command: %v", err)
		}

		result.PID = cmd.Process.Pid
		publishCommandStarted(sandboxPublisher, sandboxEnv, result.PID)

		// Enhanced process cleanup with graceful shutdown (Unix only)
		// waitedCh signals when cmd.Wait() returns
		waitedCh := make(chan struct{})

		if runtime.GOOS != "windows" {
			go func() {
				<-cmdCtx.Done()
				if cmd.Process == nil {
					return
				}
				pgid := -cmd.Process.Pid
				// Try graceful SIGTERM first
				_ = syscall.Kill(pgid, syscall.SIGTERM)
				// Give it 200ms grace period
				timer := time.NewTimer(200 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
					// Grace period expired, force kill
					_ = syscall.Kill(pgid, syscall.SIGKILL)
				case <-waitedCh:
					// Process already exited gracefully
				}
			}()
		}

		// Read stdout and stderr concurrently
		stdoutDone := make(chan string, 1)
		stderrDone := make(chan string, 1)
		truncatedOut := false
		truncatedErr := false

		// Get output callback from context
		outputCb := outputCallbackFromContext(cmdCtx)

		go func() {
			var accum bytes.Buffer
			t.streamPipe(stdout, "stdout", outputCb, &accum, &truncatedOut)
			stdoutDone <- accum.String()
		}()

		go func() {
			var accum bytes.Buffer
			t.streamPipe(stderr, "stderr", outputCb, &accum, &truncatedErr)
			stderrDone <- accum.String()
		}()

		// Get outputs (must happen before Wait as Wait closes the pipes)
		result.Stdout = <-stdoutDone
		result.Stderr = <-stderrDone

		// Wait for command to complete
		err = cmd.Wait()
		close(waitedCh) // Signal that process has exited
		result.Duration = time.Since(start)

		// Record truncation in metadata if it occurred
		if truncatedOut || truncatedErr {
			if result.Metadata == nil {
				result.Metadata = make(map[string]interface{})
			}
			result.Metadata["output_truncated"] = true
			result.Metadata["max_output_bytes"] = MaxShellOutputBytes
		}

		// Check for timeout
		if cmdCtx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
			result.ExitCode = -1
			result.Success = true // Timeout is considered a successful execution
		} else if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				result.ExitCode = exitError.ExitCode()
			} else {
				result.ExitCode = -1
			}
		} else {
			result.ExitCode = 0
			result.Success = true
		}
	} else {
		// Run without capturing output
		err := cmd.Start()
		if err != nil {
			return nil, fmt.Errorf("failed to start command: %v", err)
		}
		result.PID = cmd.Process.Pid
		publishCommandStarted(sandboxPublisher, sandboxEnv, result.PID)
		err = cmd.Wait()
		result.Duration = time.Since(start)

		if cmdCtx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
			result.ExitCode = -1
			result.Success = true // Timeout is considered a successful execution
		} else if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				result.ExitCode = exitError.ExitCode()
			} else {
				result.ExitCode = -1
			}
		} else {
			result.ExitCode = 0
			result.Success = true
		}
	}
	publishCommandFinished(sandboxPublisher, sandboxEnv, result)

	if cleanupErr := t.sandboxRuntime.Cleanup(ctx, sandboxEnv); cleanupErr != nil && result.Metadata != nil {
		result.Metadata["sandbox_cleanup_error"] = cleanupErr.Error()
	}

	return result, nil
}

func publishCommandStarted(publisher sandbox.EventPublisher, env *sandbox.SandboxEnvironment, pid int) {
	sandbox.PublishEvent(publisher, sandbox.EventTypeSandboxCommandStarted, env, "sandbox command started", map[string]interface{}{
		"pid": pid,
	})
}

func publishCommandFinished(publisher sandbox.EventPublisher, env *sandbox.SandboxEnvironment, result *CommandResult) {
	if result == nil {
		return
	}
	sandbox.PublishEvent(publisher, sandbox.EventTypeSandboxCommandFinished, env, "sandbox command finished", map[string]interface{}{
		"pid":         result.PID,
		"exit_code":   result.ExitCode,
		"duration_ms": result.Duration.Milliseconds(),
		"timed_out":   result.TimedOut,
		"success":     result.Success,
	})
}

func (t *ShellTool) buildEnvironment(environment string) []string {
	baseEnv := os.Environ()
	// Convert base env to map for easier filtering/overrides
	envMap := make(map[string]string, len(baseEnv))
	for _, kv := range baseEnv {
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			key := kv[:eq]
			val := kv[eq+1:]
			// If strict mode and key not explicitly allowed, drop from base
			if t.strict && len(t.allowedEnvVars) > 0 && !containsInsensitive(t.allowedEnvVars, key) {
				continue
			}
			// Drop blocked keys from base
			if containsInsensitive(t.blockedEnvVars, key) {
				continue
			}
			envMap[key] = val
		}
	}

	// Apply user-provided environment entries
	if environment != "" {
		envVars := strings.Split(environment, ";")
		for _, pair := range envVars {
			kv := strings.TrimSpace(pair)
			if kv == "" {
				continue
			}
			if !strings.Contains(kv, "=") {
				continue
			}
			eq := strings.IndexByte(kv, '=')
			key := strings.TrimSpace(kv[:eq])
			val := strings.TrimSpace(kv[eq+1:])

			// Enforce filtering
			if containsInsensitive(t.blockedEnvVars, key) {
				// skip blocked
				continue
			}
			if t.strict && len(t.allowedEnvVars) > 0 && !containsInsensitive(t.allowedEnvVars, key) {
				// not allowed under strict
				continue
			}
			envMap[key] = val
		}
	}

	// Convert env map back to slice
	env := make([]string, 0, len(envMap))
	for k, v := range envMap {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

func (t *ShellTool) formatForUser(result *CommandResult, metadata map[string]interface{}) string {
	var output strings.Builder

	command := result.Command
	description := metadata["description"].(string)

	if description != "" {
		fmt.Fprintf(&output, "📝 %s\n", description)
	}

	fmt.Fprintf(&output, "🔧 Command: %s\n", command)
	fmt.Fprintf(&output, "📁 Directory: %s\n", result.Directory)
	fmt.Fprintf(&output, "⏱️  Duration: %v\n", result.Duration)

	if result.TimedOut {
		output.WriteString("⏰ Status: TIMED OUT\n")
	} else if result.Success {
		output.WriteString("✅ Status: SUCCESS\n")
	} else {
		fmt.Fprintf(&output, "❌ Status: FAILED (exit code: %d)\n", result.ExitCode)
	}

	output.WriteString("─────────────────────────────────────\n")

	if result.Stdout != "" {
		output.WriteString("📤 STDOUT:\n")
		output.WriteString(result.Stdout)
		output.WriteString("\n")
	}

	if result.Stderr != "" {
		output.WriteString("📥 STDERR:\n")
		output.WriteString(result.Stderr)
		output.WriteString("\n")
	}

	// Add truncation warning if output was truncated
	if truncated, ok := metadata["output_truncated"].(bool); ok && truncated {
		output.WriteString("⚠️  Output was truncated to ")
		if maxBytes, ok := metadata["max_output_bytes"].(int); ok {
			fmt.Fprintf(&output, "%d bytes\n", maxBytes)
		} else {
			output.WriteString("maximum size\n")
		}
	}

	return output.String()
}

func (t *ShellTool) formatForLLM(result *CommandResult, metadata map[string]interface{}) string {
	var output strings.Builder

	command := result.Command
	description := metadata["description"].(string)

	if description != "" {
		fmt.Fprintf(&output, "Description: %s\n", description)
	}

	fmt.Fprintf(&output, "Command: %s\n", command)
	fmt.Fprintf(&output, "Directory: %s\n", result.Directory)
	fmt.Fprintf(&output, "Duration: %v\n", result.Duration)
	fmt.Fprintf(&output, "Exit Code: %d\n", result.ExitCode)
	fmt.Fprintf(&output, "Success: %t\n", result.Success)

	if result.TimedOut {
		output.WriteString("Timed Out: true\n")
	}

	output.WriteString("\n")

	if result.Stdout != "" {
		output.WriteString("STDOUT:\n")
		output.WriteString(result.Stdout)
		output.WriteString("\n")
	}

	if result.Stderr != "" {
		output.WriteString("STDERR:\n")
		output.WriteString(result.Stderr)
		output.WriteString("\n")
	}

	// Add truncation warning if output was truncated
	if truncated, ok := metadata["output_truncated"].(bool); ok && truncated {
		output.WriteString("Note: Output was truncated to ")
		if maxBytes, ok := metadata["max_output_bytes"].(int); ok {
			fmt.Fprintf(&output, "%d bytes\n", maxBytes)
		} else {
			output.WriteString("maximum size\n")
		}
	}

	return output.String()
}

// streamPipe reads from a pipe with streaming callbacks and output size limits
func (t *ShellTool) streamPipe(pipe io.Reader, stream string, cb OutputCallback, accum *bytes.Buffer, truncated *bool) {
	buf := make([]byte, 4096)
	var mu sync.Mutex
	var totalStreamed int

	for {
		n, err := pipe.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			mu.Lock()
			// Check if adding this chunk would exceed the limit
			if accum.Len()+n > MaxShellOutputBytes {
				*truncated = true
				// Calculate how much to drop from the beginning
				drop := accum.Len() + n - MaxShellOutputBytes
				if drop >= accum.Len() {
					// Drop everything accumulated so far
					accum.Reset()
				} else {
					// Drop 'drop' bytes from the beginning
					kept := accum.Bytes()[drop:]
					accum.Reset()
					accum.Write(kept)
				}
			}
			accum.Write(chunk)

			// Call the streaming callback if provided, but only if we haven't exceeded the limit
			willExceedLimit := totalStreamed+n > MaxShellOutputBytes
			if cb != nil && !willExceedLimit {
				cb(stream, string(chunk))
				totalStreamed += n
			} else if cb != nil && totalStreamed <= MaxShellOutputBytes {
				// Send partial chunk to reach the limit exactly
				remaining := MaxShellOutputBytes - totalStreamed
				if remaining > 0 {
					cb(stream, string(chunk[:remaining]))
					totalStreamed = MaxShellOutputBytes
				}
			}
			mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// containsInsensitive checks if a slice contains a string, case-insensitive
func containsInsensitive(arr []string, target string) bool {
	for _, v := range arr {
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}
