// Package runtime defines behavior-preserving runtime boundaries shared by
// higher-level agent orchestration code.
package runtime

import (
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/memory"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

// SessionManager is the session lifecycle surface required by AgentRuntime.
type SessionManager interface {
	Shutdown()
}

// EventBus is the event publication surface used by runtime components.
type EventBus interface {
	Publish(event.StreamEvent)
}

// EventHandler adapts an event callback into an EventBus.
type EventHandler func(event.StreamEvent)

// Publish emits an event through the handler when one is configured.
func (h EventHandler) Publish(ev event.StreamEvent) {
	if h != nil {
		h(ev)
	}
}

// AgentRuntime groups the core dependencies an agent turn needs without
// owning their construction. It is intentionally behavior-preserving: Agent
// still wires the concrete dependencies while future phases move execution
// code behind this boundary.
type AgentRuntime struct {
	LLM      llm.LLMClient
	Toolbox  *tools.Toolbox
	Sessions SessionManager
	Memory   *memory.Manager
	EventBus EventBus
}

// NewAgentRuntime creates a runtime boundary from already-initialized
// dependencies.
func NewAgentRuntime(llmClient llm.LLMClient, toolbox *tools.Toolbox, sessions SessionManager, memoryManager *memory.Manager, eventBus EventBus) *AgentRuntime {
	return &AgentRuntime{
		LLM:      llmClient,
		Toolbox:  toolbox,
		Sessions: sessions,
		Memory:   memoryManager,
		EventBus: eventBus,
	}
}
