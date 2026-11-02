package middleware

import (
	"context"

	"github.com/nano-harness/nano-agent/pkg/hookservice"
)

// ProgrammaticHook is a Go-implemented hook that can be registered alongside
// external shell/HTTP hooks in HookEngine.
type ProgrammaticHook interface {
	Name() string
	Event() hookservice.Event
	Execute(ctx context.Context, event hookservice.Event, toolName string, params map[string]interface{}) (*hookservice.Decision, error)
}
