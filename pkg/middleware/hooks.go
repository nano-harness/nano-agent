package middleware

import (
	"context"

	"github.com/nano-harness/nano-agent/pkg/hookservice"
)

// HookEvent identifies when a hook fires.
type HookEvent = hookservice.Event

const (
	HookPreToolUse         HookEvent = hookservice.EventPreToolUse
	HookPostToolUse        HookEvent = hookservice.EventPostToolUse
	HookPostToolUseFailure HookEvent = hookservice.EventPostToolUseFailure
	HookSessionStart       HookEvent = hookservice.EventSessionStart
	HookSessionEnd         HookEvent = hookservice.EventSessionEnd
	HookPreCompact         HookEvent = hookservice.EventPreCompact
	HookPostCompact        HookEvent = hookservice.EventPostCompact
	HookUserPromptSubmit   HookEvent = hookservice.EventUserPromptSubmit
	HookStop               HookEvent = hookservice.EventStop
	HookStopFailure        HookEvent = hookservice.EventStopFailure
	HookSubagentStart      HookEvent = hookservice.EventSubagentStart
	HookSubagentStop       HookEvent = hookservice.EventSubagentStop
	HookTeammateIdle       HookEvent = hookservice.EventTeammateIdle
)

// Hook is a user-defined shell script that fires before or after tool execution.
type Hook = hookservice.Hook

// HookEngine manages and executes registered hooks.
type HookEngine struct {
	service           *hookservice.Service
	programmaticHooks []ProgrammaticHook
}

// NewHookEngine creates a HookEngine from a slice of hooks.
func NewHookEngine(hooks []Hook) *HookEngine {
	return NewHookEngineWithOptions(hooks, HookOptions{})
}

// HookOptions configures hook execution.
type HookOptions = hookservice.Options

// NewHookEngineWithOptions creates a HookEngine from a slice of hooks and options.
func NewHookEngineWithOptions(hooks []Hook, options HookOptions) *HookEngine {
	return &HookEngine{service: hookservice.NewWithOptions(hooks, options)}
}

func (e *HookEngine) SetAsyncRunner(runner *hookservice.AsyncRunner) {
	if e == nil || e.service == nil {
		return
	}
	e.service.SetAsyncRunner(runner)
}

func (e *HookEngine) Close() error {
	if e == nil || e.service == nil {
		return nil
	}
	return e.service.Close()
}

// Hooks returns a snapshot of all registered hooks.
func (e *HookEngine) Hooks() []Hook {
	if e == nil || e.service == nil {
		return nil
	}
	return e.service.Hooks()
}

// RegisterProgrammaticHook registers a Go-implemented hook.
func (e *HookEngine) RegisterProgrammaticHook(h ProgrammaticHook) {
	if e == nil || h == nil {
		return
	}
	e.programmaticHooks = append(e.programmaticHooks, h)
}

// HasProgrammaticHook reports whether a Go-implemented hook is registered.
func (e *HookEngine) HasProgrammaticHook(name string) bool {
	if e == nil {
		return false
	}
	for _, hook := range e.programmaticHooks {
		if hook != nil && hook.Name() == name {
			return true
		}
	}
	return false
}

// Execute runs all matching hooks for the given event/tool combination.
// The hook script receives the tool input as NANO_TOOL_INPUT (JSON).
// Exit codes: 0 = allow, 1 = confirm, 2 = block.
// The first non-allow decision wins.
func (e *HookEngine) Execute(ctx context.Context, event HookEvent, toolName string, params map[string]interface{}) (*Decision, error) {
	decision, err := e.execute(ctx, event, toolName, params)
	if err != nil {
		return nil, err
	}
	if decision != nil && decision.Action != ActionAllow {
		decision.Layer = LayerHook
		return decision, nil
	}

	programmaticParams := params
	if decision != nil && len(decision.ModifiedParams) > 0 {
		programmaticParams = MergeDecisionParams(params, decision.ModifiedParams)
	}
	programmaticDecision := e.executeProgrammatic(ctx, event, toolName, programmaticParams)
	if programmaticDecision != nil && programmaticDecision.Action != ActionAllow {
		programmaticDecision.Layer = LayerHook
		return programmaticDecision, nil
	}
	if decision == nil || (len(decision.ModifiedParams) == 0 && programmaticDecision != nil) {
		decision = programmaticDecision
	}
	if decision == nil {
		decision = &Decision{Action: ActionAllow, Reason: "all hooks passed"}
	}
	decision.Layer = LayerHook
	return decision, nil
}

func (e *HookEngine) execute(ctx context.Context, event HookEvent, toolName string, params map[string]interface{}) (*Decision, error) {
	if e == nil || e.service == nil {
		return &Decision{Action: ActionAllow, Reason: "no hooks configured"}, nil
	}
	decision, err := e.service.Execute(ctx, event, toolName, params)
	if err != nil {
		return nil, err
	}
	return hookDecisionToMiddleware(decision), nil
}

func (e *HookEngine) executeProgrammatic(ctx context.Context, event HookEvent, toolName string, params map[string]interface{}) *Decision {
	if e == nil || len(e.programmaticHooks) == 0 {
		return &Decision{Action: ActionAllow, Reason: "no programmatic hooks configured"}
	}
	for _, hook := range e.programmaticHooks {
		if hook == nil || hook.Event() != hookservice.Event(event) {
			continue
		}
		decision, err := hook.Execute(ctx, hookservice.Event(event), toolName, params)
		if err != nil {
			return &Decision{
				Action: ActionConfirm,
				Reason: "programmatic hook " + hook.Name() + " execution error: " + err.Error(),
				Rule:   hook.Name(),
			}
		}
		middlewareDecision := hookDecisionToMiddleware(decision)
		if middlewareDecision == nil {
			continue
		}
		if middlewareDecision.Rule == "" {
			middlewareDecision.Rule = hook.Name()
		}
		if middlewareDecision.Action != ActionAllow {
			return middlewareDecision
		}
	}
	return &Decision{Action: ActionAllow, Reason: "all programmatic hooks passed"}
}

func hookDecisionToMiddleware(decision *hookservice.Decision) *Decision {
	if decision == nil {
		return nil
	}
	return &Decision{
		Action:         hookActionToMiddleware(decision.Action),
		Reason:         decision.Reason,
		Rule:           decision.Rule,
		Suggestions:    append([]string(nil), decision.Warnings...),
		AuditMetadata:  copyDecisionParams(decision.AuditMetadata),
		ModifiedParams: copyDecisionParams(decision.ModifiedParams),
	}
}

func hookActionToMiddleware(action hookservice.Action) Action {
	switch action {
	case hookservice.ActionAllow:
		return ActionAllow
	case hookservice.ActionConfirm:
		return ActionConfirm
	case hookservice.ActionBlock:
		return ActionBlock
	default:
		return ActionConfirm
	}
}
