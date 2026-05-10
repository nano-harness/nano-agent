package hookservice

import (
	"context"
	"errors"
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
	r.pending.Add(1)
	go func() {
		defer r.pending.Done()
		decision, err := svc.runCommandHook(context.Background(), &hook, event, inputJSON, toolName, copiedParams)
		if !hook.AsyncRewake || r.wakeupCb == nil {
			return
		}
		reason := ""
		if decision != nil {
			reason = decision.Reason
			if decision.Action == ActionBlock {
				if err := r.wakeupCb(ctx, stringParam(copiedParams, "session_id"), reason); err != nil {
					logger.Warnf("async rewake callback failed for hook %s: %v", hook.Name, err)
				}
				return
			}
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
			if reason == "" {
				reason = strings.TrimSpace(exitErr.Error())
			}
			if err := r.wakeupCb(ctx, stringParam(copiedParams, "session_id"), reason); err != nil {
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
