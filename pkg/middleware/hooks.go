package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// HookEvent identifies when a hook fires.
type HookEvent string

const (
	HookPreToolUse  HookEvent = "pre_tool_use"
	HookPostToolUse HookEvent = "post_tool_use"
)

// Hook is a user-defined shell script that fires before or after tool execution.
type Hook struct {
	Name    string    // Human-readable name
	Event   HookEvent // "pre_tool_use" | "post_tool_use"
	Pattern string    // Glob pattern: "bash:*" or "bash:rm*" or "*:*"
	Command string    // Shell script body to execute
	Enabled bool
}

// HookEngine manages and executes registered hooks.
type HookEngine struct {
	hooks   []Hook
	timeout time.Duration // Per-hook execution timeout (default 5s)
}

// NewHookEngine creates a HookEngine from a slice of hooks.
func NewHookEngine(hooks []Hook) *HookEngine {
	return &HookEngine{
		hooks:   hooks,
		timeout: 5 * time.Second,
	}
}

// Execute runs all matching hooks for the given event/tool combination.
// The hook script receives the tool input as NANO_TOOL_INPUT (JSON).
// Exit codes: 0 = allow, 1 = confirm, 2 = block.
// The first non-allow decision wins.
func (e *HookEngine) Execute(ctx context.Context, event HookEvent, toolName string, params map[string]interface{}) (*Decision, error) {
	if e == nil || len(e.hooks) == 0 {
		return &Decision{Action: ActionAllow, Reason: "no hooks configured", Layer: LayerHook}, nil
	}

	inputJSON, _ := json.Marshal(params)

	for i := range e.hooks {
		h := &e.hooks[i]
		if !h.Enabled || h.Event != event {
			continue
		}
		if !matchPattern(h.Pattern, toolName) {
			continue
		}

		decision, err := e.runHook(ctx, h, string(inputJSON), toolName)
		if err != nil {
			// Hook execution error → treat as Confirm (be safe but not blocking).
			return &Decision{Action: ActionConfirm, Reason: fmt.Sprintf("hook %q execution error: %v", h.Name, err), Layer: LayerHook}, nil
		}
		if decision.Action != ActionAllow {
			return decision, nil
		}
	}
	return &Decision{Action: ActionAllow, Reason: "all hooks passed", Layer: LayerHook}, nil
}

func (e *HookEngine) runHook(ctx context.Context, h *Hook, inputJSON, toolName string) (*Decision, error) {
	hookCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(hookCtx, "sh", "-c", h.Command)
	cmd.Env = append(os.Environ(),
		"NANO_TOOL_NAME="+toolName,
		"NANO_TOOL_INPUT="+inputJSON,
	)

	var stderr strings.Builder
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return &Decision{Action: ActionAllow, Reason: "hook " + h.Name + " allowed", Layer: LayerHook}, nil
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		reason := strings.TrimSpace(stderr.String())
		if reason == "" {
			reason = fmt.Sprintf("hook %q denied with exit code %d", h.Name, exitErr.ExitCode())
		}
		switch exitErr.ExitCode() {
		case 1:
			return &Decision{Action: ActionConfirm, Reason: reason, Rule: h.Name, Layer: LayerHook}, nil
		case 2:
			return &Decision{Action: ActionBlock, Reason: reason, Rule: h.Name, Layer: LayerHook}, nil
		default:
			return &Decision{Action: ActionConfirm, Reason: reason, Rule: h.Name, Layer: LayerHook}, nil
		}
	}
	return nil, err
}

// matchPattern matches "tool:pattern" where '*' is a wildcard.
// Examples: "bash:*", "bash:rm*", "*:*", "run_shell_command:*"
func matchPattern(pattern, toolName string) bool {
	if pattern == "*" || pattern == "*:*" {
		return true
	}
	// If pattern has no colon, treat as plain tool name prefix pattern.
	if !strings.Contains(pattern, ":") {
		return matchGlob(pattern, toolName)
	}
	parts := strings.SplitN(pattern, ":", 2)
	toolPattern := parts[0]
	if !matchGlob(toolPattern, toolName) && toolPattern != "*" {
		return false
	}
	return true
}

// matchGlob performs simple * wildcard matching.
func matchGlob(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(s, strings.TrimSuffix(pattern, "*"))
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(s, strings.TrimPrefix(pattern, "*"))
	}
	return pattern == s
}
