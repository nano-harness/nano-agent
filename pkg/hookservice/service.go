// Package hookservice provides execution of user-defined lifecycle hooks.
package hookservice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/policy"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

// Action is an alias for policy.PermissionAction.
type Action = policy.PermissionAction

// Action constants re-exported from policy for backward compatibility.
const (
	ActionAllow   = policy.PermissionAllow
	ActionConfirm = policy.PermissionConfirm
	ActionBlock   = policy.PermissionBlock
)

// Event identifies when a hook fires.
type Event string

const (
	EventPreToolUse         Event = "pre_tool_use"
	EventPostToolUse        Event = "post_tool_use"
	EventPostToolUseFailure Event = "post_tool_use_failure"
	EventUserPromptSubmit   Event = "user_prompt_submit"
	EventSessionStart       Event = "session_start"
	EventSessionEnd         Event = "session_end"
	EventPreCompact         Event = "pre_compact"
	EventPostCompact        Event = "post_compact"
	EventStop               Event = "stop"
	EventStopFailure        Event = "stop_failure"
	EventSubagentStart      Event = "subagent_start"
	EventSubagentStop       Event = "subagent_stop"
	EventPermissionRequest  Event = "permission_request"
	EventPermissionDenied   Event = "permission_denied"
	EventNotification       Event = "notification"
)

// KnownEventNames is the authoritative set of valid hook event names.
// Every entry exists in Claude Code's hook surface; nano's set is a strict subset.
var KnownEventNames = []Event{
	EventPreToolUse,
	EventPostToolUse,
	EventPostToolUseFailure,
	EventUserPromptSubmit,
	EventSessionStart,
	EventSessionEnd,
	EventPreCompact,
	EventPostCompact,
	EventStop,
	EventStopFailure,
	EventSubagentStart,
	EventSubagentStop,
	EventPermissionRequest,
	EventPermissionDenied,
	EventNotification,
}

// IsKnownEvent reports whether ev is in the KnownEventNames set.
func IsKnownEvent(ev Event) bool {
	for _, known := range KnownEventNames {
		if ev == known {
			return true
		}
	}
	return false
}

// IsToolEvent reports whether ev receives a tool_name in its envelope.
func IsToolEvent(ev Event) bool {
	switch ev {
	case EventPreToolUse, EventPostToolUse, EventPostToolUseFailure,
		EventPermissionRequest, EventPermissionDenied:
		return true
	}
	return false
}

// MatcherTarget returns the per-event field matched against the hook pattern.
// Events that always fire (no meaningful target) return "".
func MatcherTarget(ev Event, input Input) string {
	switch ev {
	case EventPreToolUse, EventPostToolUse, EventPostToolUseFailure,
		EventPermissionRequest, EventPermissionDenied:
		return input.ToolName
	case EventSubagentStart, EventSubagentStop:
		if v, ok := input.Params["agent_type"]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	case EventPreCompact, EventPostCompact:
		return input.Trigger
	case EventSessionStart:
		return input.Source
	case EventSessionEnd:
		return input.Reason
	case EventNotification:
		return input.Message
	default:
		// UserPromptSubmit, Stop, StopFailure: matcher always fires
		return ""
	}
}

// Hook is a user-defined command hook fired for an event.
type Hook struct {
	Name    string
	Event   Event
	Pattern string
	Command string
	Enabled bool
	Timeout time.Duration // optional; overrides service timeout when >0
}

// Decision is an alias for policy.PermissionDecision (unified in P1-1).
type Decision = policy.PermissionDecision

// Input is the structured JSON payload sent to the hook's stdin.
type Input struct {
	Event          Event                  `json:"event"`
	HookEventName  string                 `json:"hook_event_name,omitempty"`
	SessionID      string                 `json:"session_id,omitempty"`
	TranscriptPath string                 `json:"transcript_path,omitempty"`
	Cwd            string                 `json:"cwd,omitempty"`
	WorkingDir     string                 `json:"working_dir,omitempty"`
	PermissionMode string                 `json:"permission_mode,omitempty"`
	StopHookActive bool                   `json:"stop_hook_active,omitempty"`
	ToolName       string                 `json:"tool_name,omitempty"`
	ToolInput      interface{}            `json:"tool_input,omitempty"`
	ToolResponse   interface{}            `json:"tool_response,omitempty"`
	ToolUseID      string                 `json:"tool_use_id,omitempty"`
	Prompt         string                 `json:"prompt,omitempty"`
	Source         string                 `json:"source,omitempty"`
	Reason         string                 `json:"reason,omitempty"`
	Trigger        string                 `json:"trigger,omitempty"`
	Message        string                 `json:"message,omitempty"`
	Params         map[string]interface{} `json:"params,omitempty"`
}

// Output is the optional structured JSON response a hook can write to stdout.
type Output struct {
	// HookSpecificOutput is the per-event discriminated output (Claude Code parity).
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`

	// Event-agnostic fields retained on Output.
	SuppressOutput bool                   `json:"suppressOutput,omitempty"`
	Warnings       []string               `json:"warnings,omitempty"`
	Warning        string                 `json:"warning,omitempty"`
	AuditMetadata  map[string]interface{} `json:"audit_metadata,omitempty"`
	AddContext     []string               `json:"add_context,omitempty"`
	ModifiedParams map[string]interface{} `json:"modified_params,omitempty"`
	Params         map[string]interface{} `json:"params,omitempty"`
	RequestSandbox bool                   `json:"request_sandbox,omitempty"`
	RedactOutput   bool                   `json:"redact_output,omitempty"`
}

// HookSpecificOutput carries per-event typed decisions (Claude Code parity).
type HookSpecificOutput struct {
	// PreToolUse fields
	PermissionDecision       string      `json:"permissionDecision,omitempty"`       // allow|deny|ask
	PermissionDecisionReason string      `json:"permissionDecisionReason,omitempty"` // reason for decision
	UpdatedInput             interface{} `json:"updatedInput,omitempty"`
	AdditionalContext        string      `json:"additionalContext,omitempty"`

	// PostToolUse fields
	UpdatedMCPToolOutput interface{} `json:"updatedMCPToolOutput,omitempty"`

	// SessionStart fields
	InitialUserMessage string   `json:"initialUserMessage,omitempty"`
	WatchPaths         []string `json:"watchPaths,omitempty"`

	// PermissionRequest fields
	PermissionRequestDecision *PermissionRequestDecision `json:"decision,omitempty"`

	// PermissionDenied fields
	Retry *bool `json:"retry,omitempty"`
}

// PermissionRequestDecision is the typed decision for PermissionRequest hooks.
type PermissionRequestDecision struct {
	Behavior           string      `json:"behavior"`                     // allow|deny
	UpdatedInput       interface{} `json:"updatedInput,omitempty"`       // for allow
	UpdatedPermissions interface{} `json:"updatedPermissions,omitempty"` // for allow
	Message            string      `json:"message,omitempty"`            // for deny
	Interrupt          bool        `json:"interrupt,omitempty"`          // for deny
}

// Options configures hook execution.
type Options struct {
	Timeout        time.Duration
	SandboxRuntime sandbox.Runtime
	WorkingDir     string
}

// Service manages and executes registered hooks.
type Service struct {
	mu          sync.RWMutex
	hooks       []Hook
	options     Options
	asyncRunner *AsyncRunner
}

// New creates a Service from a slice of hooks.
func New(hooks []Hook) *Service { return NewWithOptions(hooks, Options{}) }

// NewWithOptions creates a Service from a slice of hooks and options.
func NewWithOptions(hooks []Hook, options Options) *Service {
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second
	}
	return &Service{
		hooks:   append([]Hook(nil), hooks...),
		options: options,
	}
}

// SetAsyncRunner configures background hook execution (currently unused for command-only hooks).
func (s *Service) SetAsyncRunner(runner *AsyncRunner) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asyncRunner = runner
}

// Close waits for asynchronous hooks to complete.
func (s *Service) Close() error {
	if s == nil || s.asyncRunner == nil {
		return nil
	}
	s.asyncRunner.Close()
	return nil
}

// Register adds a hook for subsequent executions.
func (s *Service) Register(hook Hook) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks = append(s.hooks, hook)
}

// Hooks returns a snapshot of all registered hooks.
func (s *Service) Hooks() []Hook {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Hook(nil), s.hooks...)
}

// HooksForEvent returns a snapshot of hooks registered for an event.
func (s *Service) HooksForEvent(event Event) []Hook {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	hooks := make([]Hook, 0, len(s.hooks))
	for _, hook := range s.hooks {
		if hook.Event == event {
			hooks = append(hooks, hook)
		}
	}
	return hooks
}

// Execute runs all matching hooks for the given event/tool combination.
// Stdin JSON + exit code contract:
//   - exit 0 => allow
//   - exit 2 => block
//   - other  => allow + warning
//
// Hooks may optionally write JSON to stdout; if stdout contains
// {"decision":"confirm"} then ActionConfirm is returned.
func (s *Service) Execute(ctx context.Context, event Event, toolName string, params map[string]interface{}) (*Decision, error) {
	input := Input{
		Event:         event,
		HookEventName: hookEventName(event),
		ToolName:      toolName,
		Params:        copyParams(params),
		WorkingDir:    s.options.WorkingDir,
	}
	promoteWellKnownParams(&input, params)
	return s.Dispatch(ctx, event, toolName, input)
}

// promoteWellKnownParams lifts standard keys from the loose params map to
// typed top-level Input fields so external hook consumers can rely on the
// documented JSON envelope shape without digging into params. Only sets
// fields that are not already populated, so explicit Dispatch callers
// retain control.
func promoteWellKnownParams(in *Input, p map[string]interface{}) {
	if p == nil {
		return
	}
	if in.SessionID == "" {
		if v, ok := p["session_id"].(string); ok {
			in.SessionID = v
		}
	}
	if in.Cwd == "" {
		if v, ok := p["cwd"].(string); ok {
			in.Cwd = v
		}
	}
	if in.ToolUseID == "" {
		if v, ok := p["tool_use_id"].(string); ok {
			in.ToolUseID = v
		}
	}
	if in.ToolInput == nil {
		if v, ok := p["input"]; ok {
			in.ToolInput = v
		}
	}
	if in.PermissionMode == "" {
		if v, ok := p["permission_mode"].(string); ok {
			in.PermissionMode = v
		}
	}
	if in.Reason == "" {
		if v, ok := p["reason"].(string); ok {
			in.Reason = v
		}
	}
	if in.Message == "" {
		if v, ok := p["message"].(string); ok {
			in.Message = v
		}
	}
	if in.Source == "" {
		if v, ok := p["source"].(string); ok {
			in.Source = v
		}
	}
}

// Dispatch executes hooks for an event with a pre-built Input payload.
func (s *Service) Dispatch(ctx context.Context, event Event, toolName string, input Input) (*Decision, error) {
	hooks := s.Hooks()
	if len(hooks) == 0 {
		return &Decision{Action: ActionAllow, Reason: "no hooks configured"}, nil
	}

	// Use per-event matcher target instead of raw toolName.
	target := MatcherTarget(event, input)

	var lastAllowDecision *Decision
	for i := range hooks {
		h := &hooks[i]
		if !h.Enabled || h.Event != event {
			continue
		}
		if !matchPattern(h.Pattern, target) {
			continue
		}
		decision, err := s.runCommandHook(ctx, h, input)
		if err != nil {
			return nil, err
		}
		if decision.Action != ActionAllow {
			return decision, nil
		}
		if len(decision.Warnings) > 0 || len(decision.AuditMetadata) > 0 || len(decision.ModifiedParams) > 0 {
			lastAllowDecision = decision
		}
	}
	if lastAllowDecision != nil {
		return lastAllowDecision, nil
	}
	return &Decision{Action: ActionAllow, Reason: "all hooks passed"}, nil
}

func (s *Service) runCommandHook(ctx context.Context, h *Hook, input Input) (*Decision, error) {
	timeout := s.options.Timeout
	if h.Timeout > 0 {
		timeout = h.Timeout
	}
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payload, truncated := marshalInputWithLimit(input, 1<<20 /* 1 MiB */)
	if truncated {
		logger.Warnf("hook %q input truncated to 1MiB", h.Name)
	}

	command := "sh"
	args := []string{"-c", h.Command}
	workingDir := s.options.WorkingDir
	env := sanitizeHookEnv(os.Environ())

	var sandboxEnv *sandbox.SandboxEnvironment
	if s.options.SandboxRuntime != nil {
		var err error
		sandboxEnv, err = s.options.SandboxRuntime.PrepareCommand(hookCtx, sandbox.SandboxRequest{
			Command:    command,
			Args:       args,
			WorkingDir: workingDir,
			Env:        env,
			ResourceLimits: sandbox.ResourceLimits{
				Timeout: timeout,
			},
			Metadata: map[string]interface{}{
				"hook":  h.Name,
				"event": string(input.Event),
			},
		})
		if err != nil {
			return nil, err
		}
		defer func() { _ = s.options.SandboxRuntime.Cleanup(context.Background(), sandboxEnv) }()

		command = sandboxEnv.Command
		args = sandboxEnv.Args
		if sandboxEnv.WorkingDir != "" {
			workingDir = sandboxEnv.WorkingDir
		}
	}

	cmd := exec.CommandContext(hookCtx, command, args...) //nolint:gosec
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return &Decision{
			Action:   ActionAllow,
			Reason:   "hook warning",
			Rule:     h.Name,
			Warnings: []string{fmt.Sprintf("hook %q failed to start: %v", h.Name, err)},
		}, nil
	}

	var wg sync.WaitGroup
	wg.Add(3)
	var outBytes, errBytes []byte
	var writeErr error

	go func() {
		defer wg.Done()
		_, writeErr = stdin.Write(payload)
		_ = stdin.Close()
	}()
	go func() {
		defer wg.Done()
		outBytes, _ = io.ReadAll(stdout)
	}()
	go func() {
		defer wg.Done()
		errBytes, _ = io.ReadAll(stderr)
	}()

	waitErr := cmd.Wait()
	wg.Wait()

	// A broken-pipe stdin write error is expected when the hook process exits
	// before reading all its input (e.g. `exit 2` runs immediately). Log it as
	// a debug note but let the exit code take precedence.
	var stdinWarning string
	if writeErr != nil {
		stdinWarning = fmt.Sprintf("hook %q stdin write failed: %v", h.Name, writeErr)
		logger.Debugf("%s", stdinWarning)
	}

	stdoutText := strings.TrimSpace(string(outBytes))
	stderrText := strings.TrimSpace(string(errBytes))

	if dec, ok := decisionFromStructuredOutput(h.Name, stdoutText, stderrText); ok {
		return dec, nil
	}

	// Exit-code protocol: 0=allow, 2=block, others=warn+allow.
	if waitErr == nil {
		d := &Decision{Action: ActionAllow, Reason: "hook " + h.Name + " allowed", Rule: h.Name}
		if stdinWarning != "" {
			d.Warnings = append(d.Warnings, stdinWarning)
		}
		return d, nil
	}
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		switch exitErr.ExitCode() {
		case 2:
			reason := firstNonEmpty(stderrText, "hook blocked")
			return &Decision{Action: ActionBlock, Reason: reason, Rule: h.Name}, nil
		default:
			warn := firstNonEmpty(stderrText, fmt.Sprintf("hook %q exited with code %d", h.Name, exitErr.ExitCode()))
			return &Decision{Action: ActionAllow, Reason: "hook warning", Rule: h.Name, Warnings: []string{warn}}, nil
		}
	}
	return &Decision{
		Action:   ActionAllow,
		Reason:   "hook warning",
		Rule:     h.Name,
		Warnings: []string{firstNonEmpty(stderrText, waitErr.Error())},
	}, nil
}

func decisionFromStructuredOutput(ruleName, stdoutText, stderrText string) (*Decision, bool) {
	if strings.TrimSpace(stdoutText) == "" {
		return nil, false
	}
	var out Output
	if err := json.Unmarshal([]byte(stdoutText), &out); err != nil {
		return nil, false
	}

	// Route decision via HookSpecificOutput when present.
	if hso := out.HookSpecificOutput; hso != nil {
		decision := strings.ToLower(strings.TrimSpace(hso.PermissionDecision))
		action := ActionAllow
		switch decision {
		case "allow":
			action = ActionAllow
		case "deny", "block":
			action = ActionBlock
		case "ask", "confirm":
			action = ActionConfirm
		}
		reason := strings.TrimSpace(firstNonEmpty(hso.PermissionDecisionReason, stderrText))

		// PermissionRequest typed decision
		if hso.PermissionRequestDecision != nil {
			switch strings.ToLower(hso.PermissionRequestDecision.Behavior) {
			case "allow":
				action = ActionAllow
			case "deny":
				action = ActionBlock
			}
			if hso.PermissionRequestDecision.Message != "" {
				reason = hso.PermissionRequestDecision.Message
			}
		}

		return &Decision{
			Action:         action,
			Reason:         reason,
			Rule:           ruleName,
			Warnings:       normalizeWarnings(out.Warnings, out.Warning),
			Suggestions:    normalizeWarnings(out.Warnings, out.Warning),
			AuditMetadata:  out.AuditMetadata,
			ModifiedParams: firstNonNilMap(out.ModifiedParams, out.Params),
		}, true
	}

	// No HookSpecificOutput: allow with metadata only.
	return &Decision{
		Action:         ActionAllow,
		Reason:         strings.TrimSpace(stderrText),
		Rule:           ruleName,
		Warnings:       normalizeWarnings(out.Warnings, out.Warning),
		Suggestions:    normalizeWarnings(out.Warnings, out.Warning),
		AuditMetadata:  out.AuditMetadata,
		ModifiedParams: firstNonNilMap(out.ModifiedParams, out.Params),
	}, true
}

func normalizeWarnings(ws []string, w string) []string {
	out := append([]string(nil), ws...)
	if strings.TrimSpace(w) != "" {
		out = append(out, strings.TrimSpace(w))
	}
	return out
}

func firstNonNilMap(a, b map[string]interface{}) map[string]interface{} {
	if len(a) > 0 {
		return a
	}
	if len(b) > 0 {
		return b
	}
	return nil
}

func marshalInputWithLimit(input Input, limit int) ([]byte, bool) {
	b, err := json.Marshal(input)
	if err == nil && len(b) <= limit {
		return b, false
	}

	// Try again with params replaced by minimal placeholder to keep valid JSON.
	input.Params = map[string]interface{}{"_truncated": true}
	b, err = json.Marshal(input)
	if err == nil && len(b) <= limit {
		return b, true
	}

	// Final fallback: omit optional heavy fields.
	input.Params = nil
	b, _ = json.Marshal(input)
	return b, true
}

func matchPattern(pattern, target string) bool {
	if strings.TrimSpace(pattern) == "" || pattern == "*" {
		return true
	}
	// If target is empty (no-matcher events), always match.
	if target == "" {
		return true
	}
	// Determine pattern type: if it contains only [A-Za-z0-9_|], treat as exact/alternation.
	if isAlternationPattern(pattern) {
		for _, alt := range strings.Split(pattern, "|") {
			if strings.TrimSpace(alt) == target {
				return true
			}
		}
		return false
	}
	// Otherwise treat as RE2 regex.
	return matchRegex(pattern, target)
}

// isAlternationPattern returns true if pattern contains only alphanumeric, underscore, and pipe chars.
func isAlternationPattern(pattern string) bool {
	for _, c := range pattern {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '|') {
			return false
		}
	}
	return true
}

func matchRegex(pattern, target string) bool {
	re, err := regexp.Compile("^(?:" + pattern + ")$")
	if err != nil {
		// Invalid regex — fall back to exact match.
		return pattern == target
	}
	return re.MatchString(target)
}

func hookEventName(event Event) string {
	switch event {
	case EventPreToolUse:
		return "PreToolUse"
	case EventPostToolUse:
		return "PostToolUse"
	case EventPostToolUseFailure:
		return "PostToolUseFailure"
	case EventSessionStart:
		return "SessionStart"
	case EventSessionEnd:
		return "SessionEnd"
	case EventPreCompact:
		return "PreCompact"
	case EventPostCompact:
		return "PostCompact"
	case EventUserPromptSubmit:
		return "UserPromptSubmit"
	case EventStop:
		return "Stop"
	case EventStopFailure:
		return "StopFailure"
	case EventSubagentStart:
		return "SubagentStart"
	case EventSubagentStop:
		return "SubagentStop"
	case EventPermissionRequest:
		return "PermissionRequest"
	case EventPermissionDenied:
		return "PermissionDenied"
	case EventNotification:
		return "Notification"
	default:
		return string(event)
	}
}

func copyParams(params map[string]interface{}) map[string]interface{} {
	if len(params) == 0 {
		return nil
	}
	cp := make(map[string]interface{}, len(params))
	for k, v := range params {
		cp[k] = v
	}
	return cp
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// sensitiveEnvSuffixes lists case-insensitive variable name suffixes that are
// considered credential-bearing and must never be forwarded to hook subprocesses.
var sensitiveEnvSuffixes = []string{
	"_API_KEY",
	"_SECRET",
	"_TOKEN",
	"_PASSWORD",
	"_CREDENTIALS",
	"_ACCESS_KEY",
	"_PRIVATE_KEY",
}

// sanitizeHookEnv removes credential-bearing environment variables from environ
// before they are passed to a hook subprocess.  This prevents hook scripts from
// exfiltrating secrets regardless of whether a sandbox runtime is configured.
func sanitizeHookEnv(environ []string) []string {
	result := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			result = append(result, entry)
			continue
		}
		upper := strings.ToUpper(key)
		sensitive := false
		for _, suffix := range sensitiveEnvSuffixes {
			if strings.HasSuffix(upper, suffix) {
				sensitive = true
				break
			}
		}
		if !sensitive {
			result = append(result, entry)
		}
	}
	return result
}
