// Package hookservice provides behavior-preserving execution of user-defined
// lifecycle hooks.
package hookservice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/patternutil"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

// Action is the result of a hook decision.
type Action int

const (
	ActionAllow Action = iota
	ActionConfirm
	ActionBlock
)

func (a Action) String() string {
	switch a {
	case ActionAllow:
		return "allow"
	case ActionConfirm:
		return "confirm"
	case ActionBlock:
		return "block"
	default:
		return "unknown"
	}
}

// Event identifies when a hook fires.
type Event string

const (
	// Tool execution lifecycle events
	EventPreToolUse         Event = "pre_tool_use"
	EventPostToolUse        Event = "post_tool_use"
	EventPostToolUseFailure Event = "post_tool_use_failure"
	EventPermissionRequest  Event = "permission_request"
	EventPermissionDenied   Event = "permission_denied"

	// Session management events
	EventSessionStart Event = "session_start"
	EventSessionEnd   Event = "session_end"
	EventPreCompact   Event = "pre_compact"
	EventPostCompact  Event = "post_compact"

	// User interaction events
	EventUserPromptSubmit Event = "user_prompt_submit"
	EventNotification     Event = "notification"

	// Stop and finalization events
	EventStop        Event = "stop"
	EventStopFailure Event = "stop_failure"

	// Subagent and team events
	EventSubagentStart Event = "subagent_start"
	EventSubagentStop  Event = "subagent_stop"
	EventTeammateIdle  Event = "teammate_idle"

	// Task management events
	EventTaskStart    Event = "task_start"
	EventTaskComplete Event = "task_complete"

	// File operation events
	EventFileRead   Event = "file_read"
	EventFileWrite  Event = "file_write"
	EventFileDelete Event = "file_delete"

	// System events
	EventShellCommand   Event = "shell_command"
	EventNetworkRequest Event = "network_request"

	// Checkpoint events
	EventCheckpointCreate  Event = "checkpoint_create"
	EventCheckpointRestore Event = "checkpoint_restore"
)

// HookType selects how a hook is executed. Defaults to Command (shell).
type HookType string

const (
	HookTypeCommand HookType = "command"
	HookTypeHTTP    HookType = "http"
	HookTypePrompt  HookType = "prompt"
	HookTypeAgent   HookType = "agent"
)

// HTTPHookConfig configures an HTTP-based hook. The hook posts a JSON envelope
// containing the canonical Input payload to URL and parses an Output JSON body
// back as the hook decision.
type HTTPHookConfig struct {
	URL            string            `json:"url"`
	Method         string            `json:"method,omitempty"` // default POST
	Headers        map[string]string `json:"headers,omitempty"`
	URLAllowlist   []string          `json:"url_allowlist,omitempty"`
	AllowedEnvVars []string          `json:"allowed_env_vars,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	MaxResponseKB  int               `json:"max_response_kb,omitempty"`
}

// PromptHookConfig configures an LLM-driven decision hook. The Prompt template
// is rendered with the Input payload and submitted to the configured Model;
// the model is expected to reply with structured Output JSON.
type PromptHookConfig struct {
	Prompt    string `json:"prompt"`
	Model     string `json:"model,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

// AgentHookConfig configures a sub-agent based hook. The named agent profile
// is invoked with the rendered task and its Output JSON is parsed.
type AgentHookConfig struct {
	Agent string `json:"agent"`
	Task  string `json:"task,omitempty"`
}

// Hook is a user-defined shell script (or HTTP/Prompt/Agent invocation) that
// fires for an event.
type Hook struct {
	Name          string
	Event         Event
	Pattern       string
	Type          HookType
	Command       string
	HTTPConfig    *HTTPHookConfig
	PromptConfig  *PromptHookConfig
	AgentConfig   *AgentHookConfig
	Enabled       bool
	FailurePolicy FailurePolicy
	EnvWhitelist  []string
	Async         bool
	AsyncRewake   bool
	Once          bool
	StatusMessage string
}

// Decision is the result of evaluating matching hooks.
type Decision struct {
	Action         Action
	Reason         string
	Rule           string
	Warnings       []string
	AuditMetadata  map[string]interface{}
	ModifiedParams map[string]interface{}
}

// FailurePolicy controls how hook execution failures are converted to decisions.
type FailurePolicy string

const (
	FailurePolicyConfirm     FailurePolicy = "confirm"
	FailurePolicyBlock       FailurePolicy = "block"
	FailurePolicyAllow       FailurePolicy = "allow"
	FailurePolicyIgnoreAudit FailurePolicy = "ignore_but_audit"
)

// Input is the structured JSON payload injected as NANO_HOOK_INPUT.
type Input struct {
	Event               Event                  `json:"event"`
	HookEventName       string                 `json:"hook_event_name,omitempty"`
	SessionID           string                 `json:"session_id,omitempty"`
	TranscriptPath      string                 `json:"transcript_path,omitempty"`
	Cwd                 string                 `json:"cwd,omitempty"`
	StopHookActive      bool                   `json:"stop_hook_active,omitempty"`
	Iteration           int                    `json:"iteration,omitempty"`
	ToolName            string                 `json:"tool_name"`
	Params              map[string]interface{} `json:"params,omitempty"`
	WorkingDir          string                 `json:"working_dir,omitempty"`
	EnvAllowlist        []string               `json:"env_allowlist,omitempty"`
	SandboxEnabled      bool                   `json:"sandbox_enabled"`
	ResourceTimeoutMs   int64                  `json:"resource_timeout_ms,omitempty"`
	LegacyToolInputJSON string                 `json:"legacy_tool_input_json,omitempty"`
}

// Output is the optional structured JSON response a hook can write to stdout.
type Output struct {
	Action         string                 `json:"action,omitempty"`
	Decision       string                 `json:"decision,omitempty"` // Claude Code style decision; takes priority over Action when set.
	Reason         string                 `json:"reason,omitempty"`
	SystemMessage  string                 `json:"systemMessage,omitempty"`
	Continue       *bool                  `json:"continue,omitempty"` // false forces allow/stop even if action or decision would block.
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

// Options configures hook execution.
type Options struct {
	Timeout time.Duration
	// EnvWhitelist filters inherited host environment variables only; NANO_TOOL_NAME
	// and NANO_TOOL_INPUT are always injected for the hook invocation.
	EnvWhitelist   []string
	SandboxRuntime sandbox.Runtime
	WorkingDir     string

	// HTTPClient is used by HookTypeHTTP. nil => http.DefaultClient with the
	// hook's configured timeout, no redirects.
	HTTPClient HTTPDoer
	// LLMDecider is used by HookTypePrompt. When nil, prompt hooks return the
	// configured failure policy.
	LLMDecider LLMDecider
	// AgentDecider is used by HookTypeAgent. When nil, agent hooks return the
	// configured failure policy.
	AgentDecider AgentDecider
}

// HTTPDoer abstracts http.Client so tests can swap it out.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// LLMDecider is the minimum surface a hook needs from an LLM client. The
// returned content must be a JSON object compatible with hookservice.Output.
type LLMDecider interface {
	Decide(ctx context.Context, model string, prompt string, maxTokens int) (string, error)
}

// AgentDecider runs a named subagent with a task description and returns its
// structured response (a JSON object compatible with hookservice.Output).
type AgentDecider interface {
	Run(ctx context.Context, agent string, task string) (string, error)
}

// Service manages and executes registered hooks.
type Service struct {
	mu          sync.RWMutex
	hooks       []Hook
	options     Options
	onceTracker *OnceTracker
	asyncRunner *AsyncRunner
}

// New creates a Service from a slice of hooks.
func New(hooks []Hook) *Service {
	return NewWithOptions(hooks, Options{})
}

// NewWithOptions creates a Service from a slice of hooks and options.
func NewWithOptions(hooks []Hook, options Options) *Service {
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second
	}
	return &Service{
		hooks:       append([]Hook(nil), hooks...),
		options:     options,
		onceTracker: NewOnceTracker(),
	}
}

// SetAsyncRunner configures background hook execution.
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
// The hook script receives the tool input as NANO_TOOL_INPUT (JSON).
// Exit codes: 0 = allow, 1 = confirm, 2 = block.
// The first non-allow decision wins.
func (s *Service) Execute(ctx context.Context, event Event, toolName string, params map[string]interface{}) (*Decision, error) {
	hooks := s.Hooks()
	if len(hooks) == 0 {
		return &Decision{Action: ActionAllow, Reason: "no hooks configured"}, nil
	}

	currentParams := copyParams(params)
	inputJSON, _ := json.Marshal(currentParams)
	var lastAllowDecision *Decision
	var cumulativeModified map[string]interface{}

	for i := range hooks {
		h := &hooks[i]
		if !h.Enabled || h.Event != event {
			continue
		}
		if !matchPattern(h.Pattern, toolName) {
			continue
		}
		if h.Once && !s.onceTracker.TryMark(h.Name) {
			lastAllowDecision = &Decision{Action: ActionAllow, Reason: "once hook " + h.Name + " already executed", Rule: h.Name}
			continue
		}

		decision, err := s.runHook(ctx, h, event, string(inputJSON), toolName, currentParams)
		if err != nil {
			return s.decisionForFailure(h, fmt.Sprintf("hook %q execution error: %v", h.Name, err)), nil
		}
		applyHookMetadata(decision, h)
		if decision.Action != ActionAllow {
			return decision, nil
		}
		if len(decision.ModifiedParams) > 0 {
			cumulativeModified = mergeParams(cumulativeModified, decision.ModifiedParams)
			currentParams = mergeParams(currentParams, decision.ModifiedParams)
			inputJSON, _ = json.Marshal(currentParams)
		}
		if len(decision.Warnings) > 0 || len(decision.AuditMetadata) > 0 || len(decision.ModifiedParams) > 0 {
			decision.ModifiedParams = copyParams(cumulativeModified)
			lastAllowDecision = decision
		}
	}
	if lastAllowDecision != nil {
		return lastAllowDecision, nil
	}
	return &Decision{Action: ActionAllow, Reason: "all hooks passed"}, nil
}

func applyHookMetadata(decision *Decision, h *Hook) {
	if decision == nil || h == nil || h.StatusMessage == "" {
		return
	}
	if decision.AuditMetadata == nil {
		decision.AuditMetadata = make(map[string]interface{})
	}
	if _, exists := decision.AuditMetadata["systemMessage"]; !exists {
		decision.AuditMetadata["systemMessage"] = h.StatusMessage
	}
}

func (s *Service) runHook(ctx context.Context, h *Hook, event Event, inputJSON, toolName string, params map[string]interface{}) (*Decision, error) {
	switch h.Type {
	case HookTypeHTTP:
		return s.executeHTTPHook(ctx, h, event, toolName, params, inputJSON)
	case HookTypePrompt:
		return s.executePromptHook(ctx, h, event, toolName, params, inputJSON)
	case HookTypeAgent:
		return s.executeAgentHook(ctx, h, event, toolName, params, inputJSON)
	}
	if h.Async || h.AsyncRewake {
		runner := s.asyncRunner
		if runner == nil {
			runner = NewAsyncRunner(nil)
			s.SetAsyncRunner(runner)
		}
		runner.Run(ctx, s, h, event, inputJSON, toolName, params)
		return &Decision{Action: ActionAllow, Reason: "hook " + h.Name + " running asynchronously", Rule: h.Name}, nil
	}
	return s.runCommandHook(ctx, h, event, inputJSON, toolName, params)
}

func (s *Service) runCommandHook(ctx context.Context, h *Hook, event Event, inputJSON, toolName string, params map[string]interface{}) (*Decision, error) {
	hookCtx, cancel := context.WithTimeout(ctx, s.options.Timeout)
	defer cancel()

	command := "sh"
	args := []string{"-c", h.Command}
	env := s.environment(h, event, toolName, inputJSON, params)
	workingDir := s.options.WorkingDir
	var sandboxEnv *sandbox.SandboxEnvironment
	if s.options.SandboxRuntime != nil {
		var err error
		sandboxEnv, err = s.options.SandboxRuntime.PrepareCommand(hookCtx, sandbox.SandboxRequest{
			Command:    command,
			Args:       args,
			WorkingDir: workingDir,
			Env:        env,
			ResourceLimits: sandbox.ResourceLimits{
				Timeout: s.options.Timeout,
			},
			Metadata: map[string]interface{}{
				"hook": h.Name,
				"tool": toolName,
			},
		})
		if err != nil {
			return nil, err
		}
		defer func() {
			_ = s.options.SandboxRuntime.Cleanup(context.Background(), sandboxEnv)
		}()
		command = sandboxEnv.Command
		args = sandboxEnv.Args
		if sandboxEnv.WorkingDir != "" {
			workingDir = sandboxEnv.WorkingDir
		}
	}

	cmd := exec.CommandContext(hookCtx, command, args...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	cmd.Env = env

	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if decision, ok := s.decisionFromStructuredOutput(h, stdout.String()); ok {
		return decision, nil
	}
	if err == nil {
		return &Decision{Action: ActionAllow, Reason: "hook " + h.Name + " allowed", Rule: h.Name}, nil
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		reason := strings.TrimSpace(stderr.String())
		if reason == "" {
			reason = fmt.Sprintf("hook %q denied with exit code %d", h.Name, exitErr.ExitCode())
		}
		switch exitErr.ExitCode() {
		case 1:
			return &Decision{Action: ActionConfirm, Reason: reason, Rule: h.Name}, nil
		case 2:
			return &Decision{Action: ActionBlock, Reason: reason, Rule: h.Name}, nil
		default:
			return s.decisionForFailure(h, reason), nil
		}
	}
	return nil, err
}

func (s *Service) environment(h *Hook, event Event, toolName, inputJSON string, params map[string]interface{}) []string {
	var env []string
	envWhitelist := s.options.EnvWhitelist
	if len(h.EnvWhitelist) > 0 {
		envWhitelist = h.EnvWhitelist
	}
	if envWhitelist == nil {
		env = os.Environ()
	} else {
		allowed := make(map[string]struct{}, len(envWhitelist))
		for _, name := range envWhitelist {
			allowed[name] = struct{}{}
		}

		env = make([]string, 0, len(envWhitelist))
		for _, entry := range os.Environ() {
			name, _, ok := strings.Cut(entry, "=")
			if !ok {
				continue
			}
			if _, include := allowed[name]; include {
				env = append(env, entry)
			}
		}
	}

	hookInput := Input{
		Event:               event,
		HookEventName:       hookEventName(event),
		SessionID:           stringParam(params, "session_id"),
		TranscriptPath:      stringParam(params, "transcript_path"),
		Cwd:                 firstNonEmpty(stringParam(params, "cwd"), s.options.WorkingDir),
		StopHookActive:      boolParam(params, "stop_hook_active"),
		Iteration:           intParam(params, "iteration"),
		ToolName:            toolName,
		Params:              params,
		WorkingDir:          s.options.WorkingDir,
		EnvAllowlist:        append([]string(nil), envWhitelist...),
		SandboxEnabled:      s.options.SandboxRuntime != nil,
		ResourceTimeoutMs:   s.options.Timeout.Milliseconds(),
		LegacyToolInputJSON: inputJSON,
	}
	hookInputJSON, _ := json.Marshal(hookInput)
	env = append(env,
		"NANO_TOOL_NAME="+toolName,
		"NANO_TOOL_INPUT="+inputJSON,
		"NANO_HOOK_INPUT="+string(hookInputJSON),
	)
	return env
}

func (s *Service) decisionFromStructuredOutput(h *Hook, raw string) (*Decision, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}
	var output Output
	if err := json.Unmarshal([]byte(trimmed), &output); err != nil {
		return nil, false
	}
	action := strings.ToLower(strings.TrimSpace(output.Decision))
	if action == "" {
		action = strings.ToLower(strings.TrimSpace(output.Action))
	}
	if action == "" && (output.Warning != "" || len(output.Warnings) > 0 || output.AuditMetadata != nil || output.ModifiedParams != nil || output.Params != nil) {
		action = "allow"
	}
	modifiedParams := output.ModifiedParams
	if modifiedParams == nil {
		modifiedParams = output.Params
	}
	if output.Warning != "" {
		output.Warnings = append(output.Warnings, output.Warning)
	}
	auditMetadata := copyAuditMetadata(output.AuditMetadata)
	if auditMetadata == nil && (output.SystemMessage != "" || output.Continue != nil || output.SuppressOutput) {
		auditMetadata = make(map[string]interface{})
	}
	if output.SystemMessage != "" {
		auditMetadata["systemMessage"] = output.SystemMessage
	}
	if output.Continue != nil {
		auditMetadata["continue"] = *output.Continue
	}
	if output.SuppressOutput {
		auditMetadata["suppressOutput"] = true
	}
	decisionAction := actionFromStructuredOutput(action, output)
	if output.Continue != nil && !*output.Continue {
		auditMetadata["originalAction"] = decisionAction.String()
		auditMetadata["continueOverride"] = "continue=false forced allow"
		decisionAction = ActionAllow
	}
	decision := &Decision{
		Action:         decisionAction,
		Reason:         output.Reason,
		Rule:           h.Name,
		Warnings:       append([]string(nil), output.Warnings...),
		AuditMetadata:  auditMetadata,
		ModifiedParams: copyParams(modifiedParams),
	}
	if decision.Reason == "" {
		decision.Reason = fmt.Sprintf("hook %q returned structured action %q", h.Name, action)
	}
	return decision, true
}

func actionFromStructuredOutput(action string, output Output) Action {
	switch action {
	case "allow", "continue", "emit_warning", "warning", "add_context", "modify_params", "redact_output":
		return ActionAllow
	case "confirm":
		return ActionConfirm
	case "block", "deny":
		return ActionBlock
	case "request_sandbox":
		return ActionConfirm
	default:
		if output.RequestSandbox {
			return ActionConfirm
		}
		return ActionAllow
	}
}

func (s *Service) decisionForFailure(h *Hook, reason string) *Decision {
	policy := h.FailurePolicy
	if policy == "" {
		policy = FailurePolicyConfirm
	}
	metadata := map[string]interface{}{
		"failure_policy": string(policy),
	}
	switch policy {
	case FailurePolicyAllow, FailurePolicyIgnoreAudit:
		return &Decision{Action: ActionAllow, Reason: reason, Rule: h.Name, AuditMetadata: metadata}
	case FailurePolicyBlock:
		return &Decision{Action: ActionBlock, Reason: reason, Rule: h.Name, AuditMetadata: metadata}
	default:
		return &Decision{Action: ActionConfirm, Reason: reason, Rule: h.Name, AuditMetadata: metadata}
	}
}

func copyAuditMetadata(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyParams(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeParams(base, override map[string]interface{}) map[string]interface{} {
	if len(override) == 0 {
		return copyParams(base)
	}
	out := copyParams(base)
	if out == nil {
		out = make(map[string]interface{}, len(override))
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

func stringParam(params map[string]interface{}, key string) string {
	if params == nil {
		return ""
	}
	if v, ok := params[key].(string); ok {
		return v
	}
	return ""
}

func boolParam(params map[string]interface{}, key string) bool {
	if params == nil {
		return false
	}
	if v, ok := params[key].(bool); ok {
		return v
	}
	return false
}

func intParam(params map[string]interface{}, key string) int {
	if params == nil {
		return 0
	}
	switch v := params[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func hookEventName(event Event) string {
	switch event {
	case EventStop:
		return "Stop"
	case EventStopFailure:
		return "StopFailure"
	case EventPreToolUse:
		return "PreToolUse"
	case EventPostToolUse:
		return "PostToolUse"
	case EventPostToolUseFailure:
		return "PostToolUseFailure"
	case EventUserPromptSubmit:
		return "UserPromptSubmit"
	default:
		parts := strings.Split(string(event), "_")
		for i, part := range parts {
			if part == "" {
				continue
			}
			runes := []rune(part)
			parts[i] = strings.ToUpper(string(runes[0])) + string(runes[1:])
		}
		return strings.Join(parts, "")
	}
}

// matchPattern matches hook patterns against a tool name. A plain pattern
// matches the tool name directly. A two-part "tool:command" pattern currently
// matches only the tool segment and reserves the command segment for legacy
// compatibility with existing hook declarations. "*" and "*:*" match all tools.
// Examples: "bash:*", "bash:rm*", "*:*", "run_shell_command:*".
func matchPattern(pattern, toolName string) bool {
	if pattern == "*" || pattern == "*:*" {
		return true
	}
	if !strings.Contains(pattern, ":") {
		return patternutil.MatchGlob(pattern, toolName)
	}
	parts := strings.SplitN(pattern, ":", 2)
	toolPattern := parts[0]
	if !patternutil.MatchGlob(toolPattern, toolName) && toolPattern != "*" {
		return false
	}
	return true
}
