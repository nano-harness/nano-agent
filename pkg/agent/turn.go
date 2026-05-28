package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/memory"
	"github.com/nano-harness/nano-agent/pkg/middleware"
	"github.com/nano-harness/nano-agent/pkg/skill"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

// ToolToExecute represents a tool call to be executed
type ToolToExecute struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Parameters map[string]interface{} `json:"parameters"`
}

// TaskGoal represents a specific goal to achieve
type TaskGoal struct {
	Description string   `json:"description"`
	Criteria    []string `json:"criteria"`
	UserIntent  string   `json:"user_intent"`
}

// CompletionStatus tracks task completion progress
type CompletionStatus struct {
	IsComplete  bool     `json:"is_complete"`
	Progress    float64  `json:"progress"` // 0.0-1.0
	NextActions []string `json:"next_actions"`
	Blockers    []string `json:"blockers"`
}

// CompletionCriteria defines stopping conditions for a turn
type CompletionCriteria struct {
	TaskCompleted     bool `json:"task_completed"`
	UserSatisfied     bool `json:"user_satisfied"`
	ErrorThreshold    int  `json:"error_threshold"`
	ConsecutiveErrors int  `json:"consecutive_errors"`
	CurrentIteration  int  `json:"current_iteration"`
	ErrorCount        int  `json:"error_count"`

	// Enhanced termination conditions
	ToolCancellationDetected bool    `json:"tool_cancellation_detected"`
	LLMSatisfactionScore     float64 `json:"llm_satisfaction_score"` // 0.0-1.0, measured by LLM assessment
	AutoTaskRecognition      bool    `json:"auto_task_recognition"`  // Automatic task completion recognition
	LoopDetectionEnabled     bool    `json:"loop_detection_enabled"` // Enable loop detection

	// Content-similarity loop detection state
	ContentSimilarityHistory []string `json:"content_similarity_history"` // History of LLM responses for similarity checking
	MaxSimilarContent        int      `json:"max_similar_content"`        // Threshold for similar content detection
	CognitiveLoopThreshold   int      `json:"cognitive_loop_threshold"`   // Threshold for LLM-based cognitive loop detection

	// Diminishing-returns detection
	// If the last DiminishingReturnsWindow token-count samples each show an
	// increment smaller than DiminishingReturnsMinGain, the agent is assumed to
	// be spinning without making progress and the turn is terminated.
	DiminishingReturnsEnabled    bool  `json:"diminishing_returns_enabled"`
	DiminishingReturnsWindow     int   `json:"diminishing_returns_window"`   // consecutive samples to check (default 3)
	DiminishingReturnsMinGain    int   `json:"diminishing_returns_min_gain"` // minimum token gain per sample (default 500)
	diminishingReturnsHistory    []int // ring-buffer of recent per-iteration token deltas
	diminishingReturnsPrevTokens int   // token count from the previous iteration
}

// ExecutionStep represents a single step in the execution history
type ExecutionStep struct {
	Timestamp  time.Time              `json:"timestamp"`
	Action     string                 `json:"action"`
	Tool       string                 `json:"tool,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Result     *interfaces.ToolResult `json:"result,omitempty"`
	Status     string                 `json:"status"`
}

// Turn represents a single interaction turn in the agentic loop
// Combines the simplicity of basic turn with the intelligence of enhanced turn
type Turn struct {
	// Core turn fields
	ID            string
	SessionID     string // Session ID for task isolation
	UserInput     string
	WorkingDir    string
	Toolbox       *tools.Toolbox
	LLMClient     llm.StreamClient
	MemoryManager *memory.Manager
	StartTime     time.Time

	// Enhanced planning and tracking fields
	Goals   []TaskGoal
	Status  CompletionStatus
	History []ExecutionStep

	// Conversation state
	Messages []llm.Message

	// System prompt builder for unified prompt construction
	SystemPromptBuilder *SystemPromptBuilder
	systemPrompt        string

	// Results tracking
	Response    strings.Builder
	Actions     []string
	ToolResults []interfaces.ToolResult
	HasError    bool
	ErrorMsg    string

	// Token tracking
	TokenStats *event.TokenStats

	// Context compression
	compressionStrategy *CompressionStrategy
	wasCompressed       bool
	lastCompressionInfo *CompressionInfo

	// Completion criteria and stopping conditions
	CompletionCriteria *CompletionCriteria

	// Event handling
	eventHandler func(event.StreamEvent)

	// Tool scheduler
	ToolScheduler *ToolScheduler

	// Sub-agent identification
	IsSubAgent bool

	// Agent configuration
	agentConfig *config.Config

	// Context for execution
	ctx context.Context

	// Memory tracking to avoid duplicate saves
	lastMemorySaveIndex int

	// Multimodal images
	images []llm.MultimodalImage

	// Flag to track if user message has been added to conversation history
	userMessageAdded bool

	// TUIScheduler for /loop and /schedule slash commands
	TUIScheduler *TUIScheduler

	// Agent reference for mailbox injection
	agent *Agent

	// hookEngine for lifecycle event dispatch
	hookEngine *middleware.HookEngine

	ralphContext       *RalphContext
	goalContext        *GoalContext
	goalEvaluator      *GoalEvaluator
	transcript         *TranscriptWriter
	continuationReason string

	// Structured termination tracking (for binary sentinel and orchestrators)
	terminationCause   string // e.g., "task_done", "error_threshold", "diminishing_returns"
	blockerFingerprint string // Normalized blocker ID, e.g., "dr_no_progress:3rounds"
	terminationReason  string // Human-readable explanation

	// Context usage stream throttling
	contextUsageStreamCounter int // Count of streaming TokenStats events
}

// TurnConfig holds configuration for Turn creation
type TurnConfig struct {
	WorkingDir                string
	Toolbox                   *tools.Toolbox
	LLMClient                 llm.StreamClient
	MemoryManager             *memory.Manager
	Tools                     []interfaces.Tool
	ToolScheduler             *ToolScheduler
	InitialMessages           []llm.Message
	IsSubAgent                bool
	AgentConfig               *config.Config
	SkillManager              *skill.Manager
	TUIScheduler              *TUIScheduler          // optional: enables /loop and /schedule commands
	CachedSystemPromptBuilder *SystemPromptBuilder   // optional: reuse preloaded user info
	SessionID                 string                 // optional: session ID for task isolation (Phase 2)
	Agent                     *Agent                 // optional: agent reference for mailbox injection
	HookEngine                *middleware.HookEngine // optional: hook engine for lifecycle events
	RalphContext              *RalphContext
	GoalContext               *GoalContext
	GoalEvaluator             *GoalEvaluator
	Transcript                *TranscriptWriter
}

// NewTurn creates a new Turn instance
func NewTurn(userInput string, configInput *TurnConfig) *Turn {
	return NewTurnWithMultimodal(userInput, nil, configInput)
}

// NewTurnWithMultimodal creates a new Turn instance with multimodal content support
func NewTurnWithMultimodal(userInput string, images []llm.MultimodalImage, configInput *TurnConfig) *Turn {
	if configInput == nil {
		configInput = &TurnConfig{}
	}

	// Create default compression strategy
	compressionStrategy := NewCompressionStrategy(configInput.AgentConfig)

	// Default completion criteria with enhanced settings
	completionCriteria := &CompletionCriteria{
		TaskCompleted:            false,
		UserSatisfied:            false,
		ErrorThreshold:           10,
		ConsecutiveErrors:        0,
		CurrentIteration:         0,
		ErrorCount:               0,
		ToolCancellationDetected: false,
		LLMSatisfactionScore:     0.0,
		AutoTaskRecognition:      true,
		LoopDetectionEnabled:     true,
		ContentSimilarityHistory: make([]string, 0),
		MaxSimilarContent:        3,
		CognitiveLoopThreshold:   5,

		// Diminishing-returns detection (enabled by default)
		DiminishingReturnsEnabled: true,
		DiminishingReturnsWindow:  3,
		DiminishingReturnsMinGain: 500,
	}

	// Set loop detection enabled from config with safe defaults
	cfg := configInput.AgentConfig
	if cfg == nil {
		cfg = config.Get() // fallback to global config if not provided
	}
	if cfg != nil && cfg.LoopDetection != nil {
		completionCriteria.LoopDetectionEnabled = cfg.LoopDetection.Enabled
	} else {
		completionCriteria.LoopDetectionEnabled = true
	}

	tools := configInput.Tools
	if tools == nil {
		tools = []interfaces.Tool{}
	}
	spb := configInput.CachedSystemPromptBuilder
	if spb != nil {
		spb.UpdateContext(configInput.WorkingDir, configInput.MemoryManager, cfg)
		spb.UpdateTools(tools)
	} else {
		spb = NewSystemPromptBuilder(configInput.WorkingDir, tools, configInput.MemoryManager, cfg)
	}

	// Wire skill manager into the system prompt builder
	if configInput.SkillManager != nil {
		spb.SetSkillManager(configInput.SkillManager)
	}

	return &Turn{
		ID:                  fmt.Sprintf("turn_%d", time.Now().Unix()),
		SessionID:           configInput.SessionID, // Session ID for background task isolation
		UserInput:           userInput,
		WorkingDir:          configInput.WorkingDir,
		Toolbox:             configInput.Toolbox,
		LLMClient:           configInput.LLMClient,
		MemoryManager:       configInput.MemoryManager,
		StartTime:           time.Now(),
		Messages:            append([]llm.Message{}, configInput.InitialMessages...),
		SystemPromptBuilder: spb,
		compressionStrategy: compressionStrategy,
		CompletionCriteria:  completionCriteria,
		ToolScheduler:       configInput.ToolScheduler,
		TUIScheduler:        configInput.TUIScheduler,
		IsSubAgent:          configInput.IsSubAgent,
		agentConfig:         cfg,
		images:              images,
		agent:               configInput.Agent,
		hookEngine:          configInput.HookEngine,
		ralphContext:        configInput.RalphContext,
		goalContext:         configInput.GoalContext,
		goalEvaluator:       configInput.GoalEvaluator,
		transcript:          configInput.Transcript,

		Goals:       make([]TaskGoal, 0),
		Status:      CompletionStatus{IsComplete: false, Progress: 0.0},
		History:     make([]ExecutionStep, 0),
		Actions:     make([]string, 0),
		ToolResults: make([]interfaces.ToolResult, 0),
		TokenStats:  &event.TokenStats{},
	}
}

func (t *Turn) ContinuationReason() string {
	if t == nil {
		return ""
	}
	if t.continuationReason != "" {
		return t.continuationReason
	}
	return "Continue the task."
}

// CompressMessages compresses conversation history when needed
func (t *Turn) CompressMessages(ctx context.Context, force bool) error {
	compressionStrategy := t.compressionStrategy
	if compressionStrategy == nil {
		logger.Warn("Compression strategy is nil, creating new one with default config")
		compressionStrategy = NewCompressionStrategy(t.agentConfig)
		t.compressionStrategy = compressionStrategy // 保存策略实例避免重复创建
	}

	// 确保有消息需要压缩
	if len(t.Messages) == 0 {
		logger.Info("No messages to compress")
		return nil
	}

	logger.Infof("Starting compression with %d messages, force=%t", len(t.Messages), force)

	// Snapshot messages before compression to build event previews
	beforeMessages := make([]llm.Message, len(t.Messages))
	copy(beforeMessages, t.Messages)

	// M1F: PreCompact hook — allow user-defined hooks to observe or block
	// compaction before it happens.
	if t.hookEngine != nil {
		preParams := map[string]interface{}{
			"session_id":             t.SessionID,
			"turn_id":                t.ID,
			"original_message_count": len(t.Messages),
			"force":                  force,
		}
		if dec, herr := t.hookEngine.Execute(ctx, middleware.HookPreCompact, "context_compaction", preParams); herr != nil {
			logger.Warnf("PreCompact hook execution error: %v", herr)
		} else if dec != nil && dec.Action == middleware.ActionBlock {
			return fmt.Errorf("compaction blocked by hook: %s", dec.Reason)
		}
	}

	compressedMessages, compressionInfo, err := compressionStrategy.CompressMessages(ctx, t.LLMClient, t.Messages, force)
	if err != nil {
		logger.Errorf("Compression failed: %v", err)
		return err
	}

	if compressedMessages != nil && compressionInfo != nil {
		// Filter orphaned thinking-only messages that compression may have created
		compressedMessages = llm.FilterOrphanedThinkingOnlyMessages(compressedMessages)
		t.Messages = compressedMessages
		t.wasCompressed = true
		t.lastCompressionInfo = compressionInfo
		logger.Infof("Turn context compressed: %d → %d tokens (%.2f%% reduction)",
			compressionInfo.OriginalTokens,
			compressionInfo.CompressedTokens,
			(1.0-compressionInfo.CompressionRatio)*100)

		// Emit compression event if event handler is available
		if t.eventHandler != nil {
			compressionEvent := event.NewStreamEvent(event.EventTypeCompression, "agent_turn")
			compressionEvent = compressionEvent.WithContent(fmt.Sprintf("🗜️ Context compressed: %d → %d tokens (%.2f%% reduction)",
				compressionInfo.OriginalTokens,
				compressionInfo.CompressedTokens,
				(1.0-compressionInfo.CompressionRatio)*100))

			// Build full formatted context without truncation
			formatFull := func(ms []llm.Message) string {
				if len(ms) == 0 {
					return ""
				}
				var sb strings.Builder
				for _, msg := range ms {
					role := msg.Role
					if role == "" {
						role = "unknown"
					}
					fmt.Fprintf(&sb, "[%s] %s\n", role, msg.Content)
					// Include tool calls
					if len(msg.ToolCalls) > 0 {
						sb.WriteString("  Tools called:\n")
						for _, tc := range msg.ToolCalls {
							fmt.Fprintf(&sb, "    - %s: %v\n", tc.Name, tc.Arguments)
						}
					}
					// Include tool responses
					if msg.ToolCallID != "" {
						fmt.Fprintf(&sb, "  Tool response [%s]: %s\n", msg.ToolCallID, msg.Content)
					}
					sb.WriteString("\n")
				}
				return sb.String()
			}

			// Full versions for trajectory logging
			beforeFull := formatFull(beforeMessages)
			afterFull := formatFull(t.Messages)
			summaryFull := compressionInfo.Summary

			// Attach detailed metadata
			compressionEvent = compressionEvent.
				WithMetadata("original_tokens", compressionInfo.OriginalTokens).
				WithMetadata("compressed_tokens", compressionInfo.CompressedTokens).
				WithMetadata("tokens_saved", compressionInfo.TokensSaved).
				WithMetadata("compression_ratio", compressionInfo.CompressionRatio).
				WithMetadata("messages_before", compressionInfo.MessagesBefore).
				WithMetadata("messages_after", compressionInfo.MessagesAfter).
				WithMetadata("triggered_by", compressionInfo.TriggeredBy).
				WithMetadata("summary_full", summaryFull).
				WithMetadata("before_full", beforeFull).
				WithMetadata("after_full", afterFull)
			t.eventHandler(compressionEvent)
		}

		// M1F: PostCompact hook — fired after a successful compaction, with stats.
		if t.hookEngine != nil {
			postParams := map[string]interface{}{
				"session_id":               t.SessionID,
				"turn_id":                  t.ID,
				"original_tokens":          compressionInfo.OriginalTokens,
				"compressed_tokens":        compressionInfo.CompressedTokens,
				"compressed_message_count": compressionInfo.MessagesAfter,
				"messages_before":          compressionInfo.MessagesBefore,
				"tokens_saved":             compressionInfo.TokensSaved,
				"compression_ratio":        compressionInfo.CompressionRatio,
				"triggered_by":             compressionInfo.TriggeredBy,
			}
			if _, herr := t.hookEngine.Execute(ctx, middleware.HookPostCompact, "context_compaction", postParams); herr != nil {
				logger.Warnf("PostCompact hook execution error: %v", herr)
			}
		}
	} else {
		logger.Info("No compression performed - messages or info is nil")
	}
	return nil
}

// ShouldCompress determines if compression is needed
func (t *Turn) ShouldCompress() bool {
	if t.agentConfig != nil && !t.agentConfig.ContextConfig.EnableCompression {
		logger.Info("Skipping compression: disabled in config")
		return false
	}

	compressionStrategy := t.compressionStrategy
	if compressionStrategy == nil {
		logger.Warn("Compression strategy is nil in ShouldCompress, creating new one")
		compressionStrategy = NewCompressionStrategy(t.agentConfig)
		t.compressionStrategy = compressionStrategy // 保存策略实例
	}

	// Guard: do not compress when turn is complete or completion is flagged
	if t.Status.IsComplete || (t.CompletionCriteria != nil && (t.CompletionCriteria.TaskCompleted || t.CompletionCriteria.UserSatisfied)) {
		logger.Info("Skipping compression: turn is complete or satisfaction flagged")
		return false
	}

	// 确保系统提示符已构建
	if t.systemPrompt == "" {
		logger.Debug("System prompt is empty, building it for compression check")
		t.systemPrompt = t.buildUnifiedSystemPrompt()
	}

	// Include system prompt tokens in estimation to avoid underestimation
	currentTokens := compressionStrategy.EstimateTokenCountWithSystemPrompt(t.Messages, t.systemPrompt)
	shouldCompress := compressionStrategy.ShouldCompress(t.Messages, currentTokens)

	logger.Debugf("Compression check: %d messages, %d tokens (including system prompt), should compress: %t",
		len(t.Messages), currentTokens, shouldCompress)

	return shouldCompress
}

// GetCompressionInfo returns compression statistics
func (t *Turn) GetCompressionInfo() *CompressionInfo {
	return t.lastCompressionInfo
}

// SetEventHandler sets the event handler for streaming events
func (t *Turn) SetEventHandler(handler func(event.StreamEvent)) {
	t.eventHandler = handler

}

// AddGoal adds a new task goal to the turn
func (t *Turn) AddGoal(description string, criteria []string, userIntent string) {
	goal := TaskGoal{
		Description: description,
		Criteria:    criteria,
		UserIntent:  userIntent,
	}
	t.Goals = append(t.Goals, goal)
	logger.Debugf("Added goal: %s", description)
}

// UpdateProgress updates the completion progress
func (t *Turn) UpdateProgress(progress float64, nextActions []string, blockers []string) {
	if progress < 0.0 {
		progress = 0.0
	} else if progress > 1.0 {
		progress = 1.0
	}

	t.Status.Progress = progress
	t.Status.NextActions = nextActions
	t.Status.Blockers = blockers

	logger.Debugf("Progress updated: %.2f%%, next actions: %v, blockers: %v",
		progress*100, nextActions, blockers)
}

// AddExecutionStep records an execution step in the history
func (t *Turn) AddExecutionStep(action, tool string, parameters map[string]interface{}, result *interfaces.ToolResult, status string) {
	step := ExecutionStep{
		Timestamp:  time.Now(),
		Action:     action,
		Tool:       tool,
		Parameters: parameters,
		Result:     result,
		Status:     status,
	}
	t.History = append(t.History, step)
	logger.Debugf("Added execution step: %s (%s)", action, status)
}

// GetExecutionSummary returns a summary of the execution history
func (t *Turn) GetExecutionSummary() string {
	var summary strings.Builder

	fmt.Fprintf(&summary, "Turn %s Execution Summary:\n", t.ID)
	fmt.Fprintf(&summary, "Started: %s\n", t.StartTime.Format(time.RFC3339))
	fmt.Fprintf(&summary, "Duration: %s\n", time.Since(t.StartTime))
	fmt.Fprintf(&summary, "Goals: %d\n", len(t.Goals))
	fmt.Fprintf(&summary, "Execution Steps: %d\n", len(t.History))
	fmt.Fprintf(&summary, "Progress: %.2f%%\n", t.Status.Progress*100)
	fmt.Fprintf(&summary, "Complete: %t\n", t.Status.IsComplete)

	if len(t.Status.Blockers) > 0 {
		fmt.Fprintf(&summary, "Blockers: %v\n", t.Status.Blockers)
	}

	if t.CompletionCriteria != nil {
		fmt.Fprintf(&summary, "Iterations: %d\n",
			t.CompletionCriteria.CurrentIteration)
		fmt.Fprintf(&summary, "Errors: %d (consecutive: %d)\n",
			t.CompletionCriteria.ErrorCount, t.CompletionCriteria.ConsecutiveErrors)
	}

	return summary.String()
}

// ShouldStop checks if the turn should stop based on completion criteria
func (t *Turn) ShouldStop() (bool, string) {
	if t.CompletionCriteria == nil {
		return false, ""
	}

	// Check consecutive errors
	if t.CompletionCriteria.ConsecutiveErrors >= t.CompletionCriteria.ErrorThreshold {
		return true, fmt.Sprintf("Too many consecutive errors: %d", t.CompletionCriteria.ConsecutiveErrors)
	}

	// Check if task is marked as completed
	if t.CompletionCriteria.TaskCompleted {
		return true, "Task marked as completed"
	}

	return false, ""
}

// UpdateCompletionStatus updates the completion criteria based on execution results
func (t *Turn) UpdateCompletionStatus(success bool, errorMsg string) {
	if t.CompletionCriteria == nil {
		return
	}

	t.CompletionCriteria.CurrentIteration++

	if success {
		// Reset consecutive errors on success
		t.CompletionCriteria.ConsecutiveErrors = 0
	} else {
		// Increment error counts
		t.CompletionCriteria.ErrorCount++
		t.CompletionCriteria.ConsecutiveErrors++
		logger.Debugf("Turn error recorded: %s (consecutive: %d, total: %d)",
			errorMsg, t.CompletionCriteria.ConsecutiveErrors, t.CompletionCriteria.ErrorCount)
	}
}

// MarkTaskCompleted manually marks the task as completed
func (t *Turn) MarkTaskCompleted(reason string) {
	if t.CompletionCriteria != nil {
		t.CompletionCriteria.TaskCompleted = true
		// Reset ConsecutiveErrors since the task completed successfully
		t.CompletionCriteria.ConsecutiveErrors = 0
	}
	t.Status.IsComplete = true

	// Set structured termination cause for task_done path
	t.terminationCause = "task_done"
	t.terminationReason = reason
	t.blockerFingerprint = "" // Success path doesn't need fingerprint

	logger.Infof("Task marked as completed: %s", reason)

	// [swarm] Trigger stop hooks before emitting events
	if t.agent != nil {
		for _, hook := range t.agent.StopHooks() {
			hook(t.ctx, reason)
		}
	}

	// Emit task completion event with rich context
	if t.eventHandler != nil {
		evt := event.NewStreamEvent(event.EventTypeTaskCompletion, "agent_turn")
		if reason != "" {
			evt = evt.WithContent(reason)
		}
		// include useful metadata
		evt = evt.WithMetadata("reason", reason)
		evt = evt.WithMetadata("iteration", t.CompletionCriteria.CurrentIteration)
		evt = evt.WithMetadata("progress", t.Status.Progress)
		// last execution step context
		if len(t.History) > 0 {
			last := t.History[len(t.History)-1]
			evt = evt.WithMetadata("last_action", last.Action)
			if last.Tool != "" {
				evt = evt.WithMetadata("last_tool", last.Tool)
			}
		}
		// token stats if available
		if t.TokenStats != nil {
			evt = evt.WithMetadata("token_stats", map[string]interface{}{
				"input_tokens":           t.TokenStats.InputTokens,
				"output_tokens":          t.TokenStats.OutputTokens,
				"total_tokens":           t.TokenStats.TotalTokens,
				"tokens_per_second":      t.TokenStats.TokensPerSecond,
				"peak_tokens_per_second": t.TokenStats.PeakTokensPerSecond,
				"is_streaming":           t.TokenStats.IsStreaming,
			})
		}
		t.eventHandler(evt)
	}
}

// updateConsecutiveErrorsFromToolResults adjusts CompletionCriteria.ConsecutiveErrors
// based on the outcomes of a tool batch:
//   - if at least one tool succeeded, resets ConsecutiveErrors to 0
//   - if all tools failed (and at least one failure occurred), increments
//     ConsecutiveErrors once per iteration so that hasDiminishingReturns() skips
//     detection during error recovery (preventing tool failures from being
//     misclassified as "diminishing returns")
//   - otherwise (e.g. empty batch), leaves ConsecutiveErrors unchanged
func (t *Turn) updateConsecutiveErrorsFromToolResults(toolResults map[string]*interfaces.ToolResult) {
	if t.CompletionCriteria == nil {
		return
	}
	hasSuccess := false
	hasFailure := false
	for _, result := range toolResults {
		if result == nil {
			continue
		}
		// Skip interrupted results (e.g. parent context cancel) from failure tracking
		if meta, ok := result.Metadata["interrupted"]; ok && meta == true {
			continue
		}
		if result.Success {
			hasSuccess = true
		} else {
			hasFailure = true
		}
	}
	if hasSuccess {
		// Error recovery: clear diminishing-returns history to prevent pollution.
		// Otherwise, low-gain samples accumulated during error recovery will
		// trigger hasDiminishingReturns() immediately after ConsecutiveErrors
		// is reset to 0, causing premature task termination.
		// Design reference: Claude Code query.ts checkTokenBudget only counts
		// "healthy" iterations (continuationCount).
		if t.CompletionCriteria.ConsecutiveErrors > 0 {
			t.CompletionCriteria.diminishingReturnsHistory = nil
			// prevTokens is preserved so next iteration's delta is calculated correctly
		}
		t.CompletionCriteria.ConsecutiveErrors = 0
	} else if hasFailure {
		t.CompletionCriteria.ConsecutiveErrors++
	}
}

// MarkUserSatisfied marks that user satisfaction has been achieved
func (t *Turn) MarkUserSatisfied(reason string) {
	if t.CompletionCriteria != nil {
		t.CompletionCriteria.UserSatisfied = true
	}
	logger.Infof("User satisfaction achieved: %s", reason)
	// Emit satisfaction evaluation event as achieved
	if t.eventHandler != nil {
		evt := event.NewStreamEvent(event.EventTypeSatisfactionEval, "agent_turn")
		content := "User satisfaction achieved"
		if reason != "" {
			content = fmt.Sprintf("%s: %s", content, reason)
		}
		evt = evt.WithContent(content)
		evt = evt.WithMetadata("achievement_status", "achieved")
		evt = evt.WithMetadata("reason", reason)
		evt = evt.WithMetadata("iteration", t.CompletionCriteria.CurrentIteration)
		// include score if available
		if t.CompletionCriteria != nil && t.CompletionCriteria.LLMSatisfactionScore > 0 {
			evt = evt.WithMetadata("score", t.CompletionCriteria.LLMSatisfactionScore)
			evt = evt.WithMetadata("threshold", 0.95)
		}
		// include a snippet of the latest response if present
		if s := t.Response.String(); len(s) > 0 {
			snippet := s
			if len(snippet) > 150 {
				snippet = snippet[:150] + "..."
			}
			evt = evt.WithMetadata("response_snippet", snippet)
		}
		t.eventHandler(evt)
	}
}

// Execute runs the turn execution loop
func (t *Turn) Execute(ctx context.Context) error {
	return newTurnExecutor(t).Execute(ctx)
}

// requestOpenAIAPI makes a request to the OpenAI API and returns response and tool calls
func (t *Turn) requestOpenAIAPI(ctx context.Context) (string, []*tools.ToolCall, error) {
	if t.LLMClient == nil {
		return "", nil, fmt.Errorf("LLM client is not initialized")
	}

	// 1) Ensure System Prompt
	t.ensureSystemPrompt()

	// 2) Append current user input once (avoid duplicates)
	t.ensureUserMessage()

	// 3) Check and perform context compression before LLM call
	// System prompt is already ensured above; ShouldCompress counts system tokens.
	if t.ShouldCompress() {
		logger.Infof("Context nearing token threshold, performing compression before LLM call")
		if err := t.CompressMessages(ctx, false); err != nil {
			logger.Warnf("Compression attempt failed, proceeding without compression: %v", err)
		}
	}

	// Use streaming completion directly
	var response string
	var toolCalls []*tools.ToolCall
	var reasoning string
	var reasoningBlocks []llm.ReasoningBlock

	wrappedHandler := func(streamEvent event.StreamEvent) {
		if streamEvent.Type == event.EventTypeTokenStats &&
			streamEvent.TokenStats != nil {
			t.populateContextUsage(streamEvent.TokenStats)
		}
		if t.eventHandler != nil {
			t.eventHandler(streamEvent)
		}
	}

	err := t.LLMClient.StreamCompletion(ctx, t.Messages, func(streamEvent event.StreamEvent) {
		// Handle streaming events
		switch streamEvent.Type {
		case event.EventTypeContent:
			response = streamEvent.Content
			toolCalls = streamEvent.ToolCalls
			reasoning = streamEvent.Reasoning
			if blocks, ok := streamEvent.ReasoningData.([]llm.ReasoningBlock); ok && len(blocks) > 0 {
				reasoningBlocks = blocks
			}
		case event.EventTypeToolCall:
			toolCalls = streamEvent.ToolCalls
		case event.EventTypeTokenStats:
			if streamEvent.TokenStats != nil {
				// Directly use event-provided TokenStats for accuracy
				t.TokenStats = streamEvent.TokenStats
				streamEvent.TokenStats = t.TokenStats
				// Note: populateContextUsage is called by wrappedHandler below
			}
		}

		// Forward events to turn's event handler after lifecycle metadata is filled.
		// wrappedHandler will call populateContextUsage for TokenStats events.
		wrappedHandler(streamEvent)
	})

	if err != nil {
		// Classify the LLM error for better retry logic
		errType := ClassifyLLMError(err)
		logger.Debugf("LLM error classified as %s: %v", errType.String(), err)

		// Permanent errors should not be retried
		if errType == LLMErrorPermanent {
			return "", nil, WrapLLMError(err, errType)
		}

		// For transient and rate limit errors, wrap with classification
		return "", nil, WrapLLMError(err, errType)
	}

	// Add assistant response to messages
	// Include tool calls on the assistant message when present to satisfy API ordering
	assistantMsg := llm.Message{Role: "assistant", Content: response, Reasoning: reasoning, ReasoningBlocks: reasoningBlocks}
	if len(toolCalls) > 0 {
		converted := make([]tools.ToolCall, len(toolCalls))
		for i, tc := range toolCalls {
			converted[i] = *tc
		}
		assistantMsg.ToolCalls = converted
	}
	// Append assistant message to conversation
	if response != "" || len(toolCalls) > 0 {
		t.Messages = append(t.Messages, assistantMsg)
		// Apply trailing thinking filter before next API call
		t.Messages = llm.FilterTrailingThinkingFromLastAssistant(t.Messages)
		t.appendTranscriptMessage("assistant", assistantMsg)
	}
	if response != "" {
		t.Response.WriteString(response)
		// Update content-similarity loop-detection history
		t.updateContentHistory(response)
	}

	// Increment iteration counter
	t.CompletionCriteria.CurrentIteration++

	return response, toolCalls, nil
}

func (t *Turn) populateContextUsage(stats *event.TokenStats) {
	if stats == nil {
		return
	}
	if t.compressionStrategy != nil {
		// Throttle: streaming chunks fire many TokenStats per round; we only
		// need periodic snapshots. Update every 10th streaming event or when
		// streaming stops (TotalTokens > 0 indicates final stats from provider).
		t.contextUsageStreamCounter++
		isLikelyFinal := stats.TotalTokens > 0 || !stats.IsStreaming
		shouldUpdate := isLikelyFinal || (t.contextUsageStreamCounter%10 == 0)

		if shouldUpdate {
			ctxStatus := t.compressionStrategy.Status(t.Messages, t.systemPrompt, t.lastCompressionInfo)
			stats.ContextWindowMax = ctxStatus.MaxTokens
			stats.ContextUsedTokens = ctxStatus.EstimatedTokens
		}
		return
	}
	if max := t.deriveContextWindowMaxFallback(); max > 0 {
		stats.ContextWindowMax = max
		stats.ContextUsedTokens = stats.InputTokens
	}
}

func (t *Turn) deriveContextWindowMaxFallback() int {
	if t.agentConfig == nil {
		return 0
	}
	if max := t.agentConfig.ContextConfig.MaxTokens; max > 0 {
		return max
	}
	if window := t.agentConfig.ContextConfig.ModelContextWindow; window > 0 {
		return window
	}
	if t.agentConfig.Model == "" {
		return 0
	}
	return llm.InferModelProfile(t.agentConfig.Model).ContextWindow
}

// buildUnifiedSystemPrompt builds the system prompt using the SystemPromptBuilder
func (t *Turn) buildUnifiedSystemPrompt() string {
	if t.SystemPromptBuilder == nil {
		logger.Warn("SystemPromptBuilder is nil, using empty system prompt")
		return ""
	}

	// Extract goals as strings for the enhanced system prompt
	goals := make([]string, len(t.Goals))
	for i, goal := range t.Goals {
		goals[i] = goal.Description
	}

	return t.SystemPromptBuilder.BuildEnhancedSystemPrompt(t.ctx, goals)
}

// buildFallbackResults creates synthetic failure ToolResults for every tool in the
// batch when an infrastructure-level error prevents normal execution.  This ensures
// that each tool_call in the LLM conversation history has a matching tool message,
// avoiding API-level 400 errors on the next round-trip.
func (t *Turn) buildFallbackResults(toolsToExecute []ToolToExecute, execErr error) map[string]*interfaces.ToolResult {
	results := make(map[string]*interfaces.ToolResult, len(toolsToExecute))
	errMsg := "tool execution failed"
	if execErr != nil {
		errMsg = execErr.Error()
	}
	for _, te := range toolsToExecute {
		llmMsg := fmt.Sprintf("Tool '%s' could not be executed: %s", te.Name, errMsg)
		results[te.ID] = &interfaces.ToolResult{
			Success:     false,
			Error:       errMsg,
			LLMContent:  llmMsg,
			UserContent: llmMsg,
			Metadata: map[string]interface{}{
				"tool_name":      te.Name,
				"code":           "execution_failed",
				"error_category": "unrecoverable",
			},
		}
	}
	return results
}

// executeToolCallsInParallel executes tool calls using the tool scheduler
func (t *Turn) executeToolCallsInParallel(ctx context.Context, toolsToExecute []ToolToExecute) (map[string]*interfaces.ToolResult, error) {
	if t.ToolScheduler == nil {
		return nil, fmt.Errorf("tool scheduler is not initialized")
	}

	// Use the tool scheduler to execute tools in parallel
	return t.ToolScheduler.ExecuteParallel(ctx, toolsToExecute)
}

// addToolResultsToContext adds tool execution results to the conversation context
func (t *Turn) addToolResultsToContext(toolResults map[string]*interfaces.ToolResult) error {
	if len(toolResults) == 0 {
		return nil
	}

	// Collect the set of tool-call IDs that appear in the last assistant message so
	// we can guarantee every pending tool_call gets a matching tool message.
	// Some LLM APIs (e.g. OpenAI) will return HTTP 400 if any tool_call_id is left
	// without a corresponding tool role message.
	pendingCallIDs := make(map[string]struct{})
	for i := len(t.Messages) - 1; i >= 0; i-- {
		msg := t.Messages[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				pendingCallIDs[tc.ID] = struct{}{}
			}
			break
		}
	}

	// Remove IDs that already have a tool message in the conversation.
	for i := len(t.Messages) - 1; i >= 0; i-- {
		msg := t.Messages[i]
		if msg.Role != "tool" {
			break // tool messages immediately follow the assistant message
		}
		delete(pendingCallIDs, msg.ToolCallID)
	}

	// Add tool results as tool messages to the conversation
	for toolID, result := range toolResults {
		if result == nil {
			continue
		}

		// Create tool message with the result
		toolMessage := llm.Message{
			Role:       "tool",
			Content:    result.LLMContent,
			ToolCallID: toolID,
		}

		t.Messages = append(t.Messages, toolMessage)
		t.appendTranscriptMessage("tool_result", toolMessage)
		delete(pendingCallIDs, toolID)

		// Add to tool results tracking
		t.ToolResults = append(t.ToolResults, *result)

		// Extract tool name from metadata if available
		var toolName string
		if result.Metadata != nil {
			if tn, ok := result.Metadata["tool_name"].(string); ok {
				toolName = tn
			}
		}

		// Add execution step to history
		t.AddExecutionStep(
			"tool_result",
			toolName,
			map[string]interface{}{"tool_id": toolID},
			result,
			func() string {
				if result.Success {
					return "success"
				}
				return "error"
			}(),
		)
	}

	// For any tool_call IDs still pending (not covered by toolResults), inject a
	// synthetic "tool unavailable" message so the conversation stays well-formed.
	for callID := range pendingCallIDs {
		logger.Warnf("No tool result found for tool_call_id %s – injecting placeholder to keep context consistent", callID)
		placeholder := llm.Message{
			Role:       "tool",
			Content:    "Tool execution result unavailable.",
			ToolCallID: callID,
		}
		t.Messages = append(t.Messages, placeholder)
		t.appendTranscriptMessage("tool_result", placeholder)
	}

	logger.Debugf("Added %d tool results to conversation context", len(toolResults))
	return nil
}

// isComplete checks if the turn is complete based on completion criteria
func (t *Turn) isComplete() bool {
	if t.CompletionCriteria == nil {
		return false
	}

	return t.CompletionCriteria.TaskCompleted
}

// saveConversationMemory saves the conversation to memory if configured
func (t *Turn) saveConversationMemory() error {
	// Only save for sub-agents and if memory manager is available
	if !t.IsSubAgent || t.MemoryManager == nil || len(t.Messages) <= t.lastMemorySaveIndex {
		return nil
	}

	// Create conversation metadata
	metadata := map[string]interface{}{
		"type":            "conversation",
		"task_completed":  t.CompletionCriteria != nil && t.CompletionCriteria.TaskCompleted,
		"completion_time": time.Now().Unix(),
		"iteration_count": func() int {
			if t.CompletionCriteria != nil {
				return t.CompletionCriteria.CurrentIteration
			}
			return 0
		}(),
		"duration_seconds":  time.Since(t.StartTime).Seconds(),
		"working_directory": t.WorkingDir,
		"user_input":        t.UserInput,
		"turn_id":           t.ID,
		"message_range":     fmt.Sprintf("%d-%d", t.lastMemorySaveIndex, len(t.Messages)-1),
	}

	// Add execution statistics
	if t.TokenStats != nil {
		metadata["token_stats"] = map[string]interface{}{
			"input_tokens":  t.TokenStats.InputTokens,
			"output_tokens": t.TokenStats.OutputTokens,
			// Remove TotalTokens as it doesn't exist in llm.TokenStats
		}
	}

	// Add tool usage statistics
	toolUsage := make(map[string]int)
	for _, step := range t.History {
		if step.Tool != "" {
			toolUsage[step.Tool]++
		}
	}
	if len(toolUsage) > 0 {
		metadata["tools_used"] = toolUsage
	}

	// Get user and agent IDs from config
	cfg := t.agentConfig
	var userID, agentID string
	if cfg != nil && cfg.Memory != nil {
		userID = cfg.Memory.UserID
		agentID = cfg.Memory.AgentID
	}

	// Save only new messages - SaveConversationMemory doesn't return error
	newMessages := t.Messages[t.lastMemorySaveIndex:]
	t.MemoryManager.SaveConversationMemory(context.Background(), newMessages, userID, agentID, metadata)

	t.lastMemorySaveIndex = len(t.Messages)
	logger.Debugf("Saved conversation memory with %d new messages", len(newMessages))

	return nil
}

// close finalizes the turn execution
func (t *Turn) close() {
	logger.Debugf("Closing turn %s", t.ID)

	// Mark turn as complete
	t.Status.IsComplete = true

	// Clean up resources if needed
	if t.ToolScheduler != nil {
		// Clear completed tool calls to prevent memory leaks
		t.ToolScheduler.ClearCompletedToolCalls()
	}

	// Log final statistics
	duration := time.Since(t.StartTime)
	logger.Infof("Turn %s completed in %v with %d iterations",
		t.ID, duration, func() int {
			if t.CompletionCriteria != nil {
				return t.CompletionCriteria.CurrentIteration
			}
			return 0
		}())
}

// ── Termination tracking accessors ──────────────────────────────────────────

// TerminationCause returns the classification enum of why the turn terminated.
func (t *Turn) TerminationCause() string {
	return t.terminationCause
}

// BlockerFingerprint returns a stable, normalized short string identifying the blocker.
func (t *Turn) BlockerFingerprint() string {
	return t.blockerFingerprint
}

// TerminationReason returns a human-readable explanation of termination.
func (t *Turn) TerminationReason() string {
	return t.terminationReason
}

// SetTerminationInfo sets structured termination metadata.
func (t *Turn) SetTerminationInfo(cause, fingerprint, reason string) {
	t.terminationCause = cause
	t.blockerFingerprint = fingerprint
	t.terminationReason = reason
}
