package hookservice

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// WakeupCallback is invoked when an asyncRewake hook exits with code 2.
type WakeupCallback func(ctx context.Context, sessionID string, reason string) error

// AsyncRunner executes hooks in the background.
type AsyncRunner struct {
	wakeupCb WakeupCallback
	pending  sync.WaitGroup
}

func NewAsyncRunner(wakeupCb WakeupCallback) *AsyncRunner {
	return &AsyncRunner{wakeupCb: wakeupCb}
}

func (r *AsyncRunner) Run(ctx context.Context, svc *Service, h *Hook, event Event, inputJSON, toolName string, params map[string]interface{}) {
	if r == nil || svc == nil || h == nil {
		return
	}
	hook := *h
	copiedParams := copyParams(params)
	var decodedParams map[string]interface{}
	_ = json.Unmarshal([]byte(inputJSON), &decodedParams)

	input := Input{
		Event:          event,
		HookEventName:  hookEventName(event),
		SessionID:      stringFromParams(copiedParams, "session_id"),
		TranscriptPath: stringFromParams(copiedParams, "transcript_path"),
		Cwd:            stringFromParams(copiedParams, "cwd"),
		ToolName:       toolName,
		Params:         decodedParams,
		WorkingDir:     svc.options.WorkingDir,
	}
	r.pending.Add(1)
	go func() {
		defer r.pending.Done()
		decision, err := svc.runCommandHook(context.Background(), &hook, input)
		if r.wakeupCb == nil {
			return
		}
		if decision != nil && decision.Action == ActionBlock {
			if err := r.wakeupCb(ctx, input.SessionID, decision.Reason); err != nil {
				logger.Warnf("async rewake callback failed for hook %s: %v", hook.Name, err)
			}
			return
		}
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
			reason := strings.TrimSpace(exitErr.Error())
			if err := r.wakeupCb(ctx, input.SessionID, reason); err != nil {
				logger.Warnf("async rewake callback failed for hook %s: %v", hook.Name, err)
			}
		}
	}()
}

func (r *AsyncRunner) Close() {
	if r == nil {
		return
	}
	r.pending.Wait()
}

func stringFromParams(params map[string]interface{}, key string) string {
	if params == nil {
		return ""
	}
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
