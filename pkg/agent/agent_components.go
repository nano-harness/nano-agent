package agent

import (
	"context"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/memory"
	"github.com/nano-harness/nano-agent/pkg/middleware"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

// TurnRequest groups the immutable input data for a turn.
type TurnRequest struct {
	ID        string
	SessionID string
	UserInput string
	Images    []llm.MultimodalImage
	StartTime time.Time
}

// TurnStream holds the streaming/response state of a turn.
type TurnStream struct {
	Response strings.Builder
	Messages []llm.Message
}

// TurnToolBatch tracks tool execution results for the turn.
type TurnToolBatch struct {
	Actions     []string
	ToolResults []interfaces.ToolResult
}

// TurnDecisions holds the completion/termination decisions for the turn.
type TurnDecisions struct {
	CompletionCriteria *CompletionCriteria
	TerminationCause   string // e.g., "task_done", "error_threshold", "diminishing_returns"
	BlockerFingerprint string // Normalized blocker ID
	TerminationReason  string // Human-readable explanation
	ContinuationReason string
}

// TurnTelemetry holds metrics and stats for the turn.
type TurnTelemetry struct {
	TokenStats                *event.TokenStats
	ContextUsageStreamCounter int
	HasError                  bool
	ErrorMsg                  string
}

// TurnDeps groups the injected dependencies for a turn.
type TurnDeps struct {
	Toolbox       *tools.Toolbox
	LLMClient     llm.StreamClient
	MemoryManager *memory.Manager
	ToolScheduler *ToolScheduler
	HookEngine    *middleware.HookEngine
	AgentConfig   *config.Config
	EventHandler  func(event.StreamEvent)
	Agent         *Agent
	Ctx           context.Context
}

// AgentToolRunner groups tool execution dependencies on Agent.
type AgentToolRunner struct {
	Toolbox       *tools.Toolbox
	ToolScheduler *ToolScheduler
}

// AgentHookRunner groups hook execution dependencies on Agent.
type AgentHookRunner struct {
	HookEngine *middleware.HookEngine
}

// AgentMemoryStore groups memory dependencies on Agent.
type AgentMemoryStore struct {
	MemoryManager *memory.Manager
}

// AgentLLMClient groups LLM client dependencies on Agent.
type AgentLLMClient struct {
	Client llm.LLMClient
}

// AgentSession groups session management dependencies on Agent.
type AgentSession struct {
	SessionManager *SessionManager
	ActiveID       string
}

// These interfaces are satisfied by the full Agent, enabling tests to
// construct minimal mocks that only implement what they need.

// ToolRunner is the interface for executing tools.
type ToolRunner interface {
	GetToolbox() *tools.Toolbox
	GetToolScheduler() *ToolScheduler
}

// HookRunner is the interface for executing hooks.
type HookRunner interface {
	GetHookEngine() *middleware.HookEngine
}

// MemoryStore is the interface for memory access.
type MemoryStore interface {
	GetMemoryManager() *memory.Manager
}

// Ensure Agent satisfies the decomposed interfaces.
var _ ToolRunner = (*Agent)(nil)
var _ HookRunner = (*Agent)(nil)
var _ MemoryStore = (*Agent)(nil)
