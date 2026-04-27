package event

import "github.com/nano-harness/nano-agent/pkg/tools"

type EventType string //nolint:revive

const (
	EventTypeContent        EventType = "content"        //nolint:revive
	EventTypeStreamContent  EventType = "stream_content" // Stream content for real-time rendering
	EventTypeToolCall       EventType = "tool_call"
	EventTypeToolResult     EventType = "tool_result"
	EventTypeToolUse        EventType = "tool_use"
	EventTypeError          EventType = "error"
	EventTypeDone           EventType = "done"
	EventTypeThinking       EventType = "thinking"
	EventTypeCompression    EventType = "compression"
	EventTypeTokenStats     EventType = "token_stats"
	EventTypeWaitingForUser EventType = "waiting_for_user" // Execution paused, waiting for user intervention
	EventTypeTodoListUpdate EventType = "todo_list_update" // Dynamic todo list updates
	EventTypeTodoUpdate     EventType = "todo_update"      // Individual todo updates
	EventTypeFinalSummary   EventType = "final_summary"    // Final summary response after synthesis completion
	// EventTypeTaskStart Task execution started
	EventTypeTaskStart    EventType = "task_start"    // Task execution started
	EventTypeTaskProgress EventType = "task_progress" // Task progress update
	EventTypeTaskCancel   EventType = "task_cancel"   // Task was cancelled
	EventTypeRetry        EventType = "retry"         // Retry attempt notification
	EventTypeWarning      EventType = "warning"       // Warning message
	EventTypeDebug        EventType = "debug"         // Debug information
	// EventTypeTaskCompletion Task completion notification with details
	EventTypeTaskCompletion    EventType = "task_completion"    // Task completion notification with details
	EventTypeSatisfactionEval  EventType = "satisfaction_eval"  // LLM satisfaction evaluation result
	EventTypeTerminationSignal EventType = "termination_signal" // Graceful termination signal
	EventTypeSessionInfo       EventType = "session_info"       // Session information event

	// EventTypePlannerPlanSnapshot Planner / Executor / Worker observability events
	EventTypePlannerPlanSnapshot EventType = "planner_plan_snapshot"
	EventTypePlannerPlanUpdate   EventType = "planner_plan_update"
	EventTypePlannerDecision     EventType = "planner_decision"
	EventTypeExecutorState       EventType = "executor_state"
	EventTypeExecutorSchedule    EventType = "executor_schedule"
	EventTypeWorkerStart         EventType = "worker_start"
	EventTypeWorkerUpdate        EventType = "worker_update"
	EventTypeWorkerLog           EventType = "worker_log"
	EventTypeWorkerEnd           EventType = "worker_end"

	// EventTypeLoopDetected is emitted when the Turn detects a behavioral loop
	// (repeated identical tool calls or highly-similar LLM output) and decides to
	// terminate early. Metadata fields: "reason", "loop_type", "iteration".
	EventTypeLoopDetected EventType = "loop_detected"

	// Expert system events
	EventTypeExpertStarted  EventType = "expert_started"  // Expert execution started
	EventTypeExpertProgress EventType = "expert_progress" // Expert execution progress
	EventTypeExpertFinished EventType = "expert_finished" // Expert execution finished

	// Mailbox system events
	EventTypeMailboxSent     EventType = "mailbox_sent"     // Message sent to agent mailbox
	EventTypeMailboxReceived EventType = "mailbox_received" // Message received from mailbox
)

// ToolUse represents the usage of a tool
type ToolUse struct {
	ID         string                 `json:"id"`
	ToolName   string                 `json:"tool_name"`
	Parameters map[string]interface{} `json:"parameters"`
	Status     string                 `json:"status"`
	Result     string                 `json:"result,omitempty"`
}

// StreamEvent represents different types of events in the stream
type StreamEvent struct {
	Type           EventType         `json:"type"`
	Content        string            `json:"content,omitempty"`
	Reasoning      string            `json:"reasoning,omitempty"`
	ReasoningDelta string            `json:"reasoning_delta,omitempty"` // Incremental reasoning content fragment
	ToolCalls      []*tools.ToolCall `json:"tool_calls,omitempty"`
	ToolResult     *tools.ToolResult `json:"tool_result,omitempty"`
	ToolUse        *ToolUse          `json:"tool_use,omitempty"`
	Error          string            `json:"error,omitempty"`
	Done           bool              `json:"done,omitempty"`
	TokenStats     *TokenStats       `json:"token_stats,omitempty"`

	// Task management fields
	TaskList interface{} `json:"task_list,omitempty"` // Will hold *agent.TaskList
	Task     interface{} `json:"task,omitempty"`      // Will hold *agent.Task

	// 增强的事件信息字段
	ID            string                 `json:"id,omitempty"`             // Unique event ID
	Timestamp     int64                  `json:"timestamp,omitempty"`      // Event timestamp
	TaskID        string                 `json:"task_id,omitempty"`        // Associated task ID
	SessionID     string                 `json:"session_id,omitempty"`     // Associated session ID for isolation
	RunID         string                 `json:"run_id,omitempty"`         // Execution run ID (daemon task ID)
	Seq           int64                  `json:"seq,omitempty"`            // Monotonic sequence number within a run
	Priority      string                 `json:"priority,omitempty"`       // e.g. high/normal/low
	WorkerID      string                 `json:"worker_id,omitempty"`      // Tool/sub-agent worker identifier
	Title         string                 `json:"title,omitempty"`          // Title for the task or session
	Progress      float64                `json:"progress,omitempty"`       // Progress percentage (0.0-1.0)
	Metadata      map[string]interface{} `json:"metadata,omitempty"`       // Additional metadata
	Severity      string                 `json:"severity,omitempty"`       // Error/warning severity
	RetryCount    int                    `json:"retry_count,omitempty"`    // Retry attempt count
	Source        string                 `json:"source,omitempty"`         // Event source component
	CorrelationID string                 `json:"correlation_id,omitempty"` // For event correlation
	Payload       interface{}            `json:"payload,omitempty"`        // Optional typed event payload
}

// TokenStats represents token usage statistics with real-time capabilities
type TokenStats struct {
	InputTokens       int   `json:"input_tokens"`
	OutputTokens      int   `json:"output_tokens"`
	TotalTokens       int   `json:"total_tokens"`
	RequestSizeBytes  int   `json:"request_size_bytes"`
	ResponseSizeBytes int   `json:"response_size_bytes"`
	StartTime         int64 `json:"start_time"`
	EndTime           int64 `json:"end_time,omitempty"`
	Duration          int64 `json:"duration_ms,omitempty"`

	// Session totals
	SessionInputTokens  int `json:"session_input_tokens"`
	SessionOutputTokens int `json:"session_output_tokens"`
	SessionTotalTokens  int `json:"session_total_tokens"`

	// Real-time streaming statistics
	TokensPerSecond     float64 `json:"tokens_per_second,omitempty"`
	PeakTokensPerSecond float64 `json:"peak_tokens_per_second,omitempty"`
	IsStreaming         bool    `json:"is_streaming,omitempty"`
	UpdateCount         int     `json:"update_count,omitempty"`

	// Reasoning-specific statistics
	ReasoningEnabled  bool   `json:"reasoning_enabled,omitempty"`
	ReasoningTokens   int    `json:"reasoning_tokens,omitempty"`
	ReasoningEffort   string `json:"reasoning_effort,omitempty"`
	ReasoningFallback bool   `json:"reasoning_fallback,omitempty"`
	ReasoningLatency  int64  `json:"reasoning_latency_ms,omitempty"`
}
