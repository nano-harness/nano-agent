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
	"github.com/nano-harness/nano-agent/pkg/openspec"
	"github.com/nano-harness/nano-agent/pkg/skill"
	"github.com/nano-harness/nano-agent/pkg/tools"
	"github.com/nano-harness/nano-agent/pkg/tools/builtin"
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

	// Memory tracking to avoid duplicate saves
	lastMemorySaveIndex int

	// Multimodal images
	images []llm.MultimodalImage

	// Flag to track if user message has been added to conversation history
	userMessageAdded bool

	// TUIScheduler for /loop and /schedule slash commands
	TUIScheduler *TUIScheduler
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
	TUIScheduler              *TUIScheduler        // optional: enables /loop and /schedule commands
	CachedSystemPromptBuilder *SystemPromptBuilder // optional: reuse preloaded user info
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

	// Create default compression strategy - use no-args constructor
	compressionStrategy := NewCompressionStrategy()

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

	// Use correct SystemPromptBuilder constructor with proper arguments
	tools := configInput.Tools
	if tools == nil {
		tools = []interfaces.Tool{}
	}
	spb := NewSystemPromptBuilder(configInput.WorkingDir, tools, configInput.MemoryManager, cfg)

	// Reuse preloaded user info from agent if available, avoiding redundant detection.
	// Only enter this path when PreloadUserInfo() was actually called; otherwise the
	// userInfoReady channel is never closed and we would block for the full timeout
	// on every turn creation (e.g. when AutoDetectUserInfo=false in tests).
	if configInput.CachedSystemPromptBuilder != nil && configInput.CachedSystemPromptBuilder.preloadStarted.Load() {
		cached := configInput.CachedSystemPromptBuilder
		// Wait briefly for the preload to finish before copying the result.
		// Only copy cachedUserInfo after observing userInfoReady closed to avoid a
		// data race with the writer goroutine in cached.loadUserInfo().
		select {
		case <-cached.userInfoReady:
			// Pre-populate the cache only after synchronization with the writer.
			// loadUserInfo() will see it's already set and skip detection.
			spb.cachedUserInfo = cached.cachedUserInfo
			spb.preloadStarted.Store(true)
			// Start a goroutine that will fast-path through loadUserInfo (cachedUserInfo != nil)
			// and close userInfoReady, unblocking any concurrent getUserInfo() calls.
			go spb.loadUserInfo()
		case <-time.After(5 * time.Second):
			// Preload timed out; let the new builder run its own detection when needed.
			logger.Warn("Timed out waiting for cached system prompt builder preload; proceeding without cached user info")
		}
	}

	// Wire skill manager into the system prompt builder
	if configInput.SkillManager != nil {
		spb.SetSkillManager(configInput.SkillManager)
	}

	return &Turn{
		ID:                  fmt.Sprintf("turn_%d", time.Now().Unix()),
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
		images:              images,

		Goals:       make([]TaskGoal, 0),
		Status:      CompletionStatus{IsComplete: false, Progress: 0.0},
		History:     make([]ExecutionStep, 0),
		Actions:     make([]string, 0),
		ToolResults: make([]interfaces.ToolResult, 0),
		TokenStats:  &event.TokenStats{},
	}
}

// CompressMessages compresses conversation history when needed
func (t *Turn) CompressMessages(ctx context.Context, force bool) error {
	compressionStrategy := t.compressionStrategy
	if compressionStrategy == nil {
		logger.Warn("Compression strategy is nil, creating new one with default config")
		compressionStrategy = NewCompressionStrategy()
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

	compressedMessages, compressionInfo, err := compressionStrategy.CompressMessages(ctx, t.LLMClient, t.Messages, force)
	if err != nil {
		logger.Errorf("Compression failed: %v", err)
		return err
	}

	if compressedMessages != nil && compressionInfo != nil {
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
	} else {
		logger.Info("No compression performed - messages or info is nil")
	}
	return nil
}

// ShouldCompress determines if compression is needed
func (t *Turn) ShouldCompress() bool {
	compressionStrategy := t.compressionStrategy
	if compressionStrategy == nil {
		logger.Warn("Compression strategy is nil in ShouldCompress, creating new one")
		compressionStrategy = NewCompressionStrategy()
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
	}
	t.Status.IsComplete = true
	logger.Infof("Task marked as completed: %s", reason)
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
	logger.Infof("Starting turn execution: %s", t.ID)
	t.StartTime = time.Now()

	// Reset the read-file cache so every new turn must re-read files before editing.
	// This prevents the model from editing files based on memory from a previous turn
	// in which the file may have changed.
	if t.Toolbox != nil {
		t.Toolbox.ResetReadFileState()
	}

	var turnErr error
	cfg := config.Get()

	// Preprocess /opsx: commands — enrich the user input with OpenSpec context
	t.preprocessOpenSpecCommand(cfg)

	// Preprocess /skill: commands and auto-match skills
	t.preprocessSkillCommand(ctx, cfg)

	// Preprocess /loop and /schedule commands
	t.preprocessLoopCommand()

	// Preprocess /watcher commands
	t.preprocessWatcherCommand()
	if t.eventHandler != nil {
		planEvt := event.NewStreamEvent(event.EventTypePlannerPlanSnapshot, "agent_turn")
		planEvt = planEvt.WithContent("planner plan snapshot")
		planEvt = planEvt.WithMetadata("turn_id", t.ID)
		planEvt = planEvt.WithMetadata("steps", []map[string]interface{}{
			{"id": "understand", "title": "理解需求", "status": "pending"},
			{"id": "execute", "title": "执行与调用工具", "status": "pending"},
			{"id": "synthesize", "title": "整理输出", "status": "pending"},
		})
		t.eventHandler(planEvt)

		stateEvt := event.NewStreamEvent(event.EventTypeExecutorState, "agent_turn")
		stateEvt = stateEvt.WithContent("running")
		stateEvt = stateEvt.WithMetadata("turn_id", t.ID)
		t.eventHandler(stateEvt)
	}

	// Main execution loop — runs until task_done is called or a termination condition is met
	for {
		// Check if we should terminate
		if t.shouldTerminate() {
			logger.Infof("Turn termination condition met")
			break
		}

		if t.eventHandler != nil {
			dec := event.NewStreamEvent(event.EventTypePlannerDecision, "agent_turn")
			dec = dec.WithContent("request_llm")
			dec = dec.WithMetadata("turn_id", t.ID)
			dec = dec.WithMetadata("iteration", t.CompletionCriteria.CurrentIteration+1)
			t.eventHandler(dec)
		}

		// Request LLM response — retry up to 3 times on transient errors with recovery paths
		var response string
		var toolCalls []*tools.ToolCall
		var err error
		const llmMaxRetries = 3
		for attempt := 1; attempt <= llmMaxRetries; attempt++ {
			response, toolCalls, err = t.requestOpenAIAPI(ctx)
			if err == nil {
				break
			}

			// Attempt LLM recovery paths before giving up
			if recovered := t.attemptLLMRecovery(ctx, err, attempt); recovered {
				continue // Recovery succeeded, retry immediately
			}

			if attempt < llmMaxRetries {
				backoff := time.Duration(attempt) * 2 * time.Second
				logger.Warnf("LLM API request failed (attempt %d/%d): %v — retrying in %v",
					attempt, llmMaxRetries, err, backoff)
				select {
				case <-ctx.Done():
					return fmt.Errorf("LLM API request cancelled: %w", ctx.Err())
				case <-time.After(backoff):
				}
			} else {
				// Last attempt failed - record error but don't terminate immediately
				logger.Errorf("LLM API request failed after %d attempts: %v", llmMaxRetries, err)
				t.CompletionCriteria.ConsecutiveErrors++
				if t.CompletionCriteria.ConsecutiveErrors >= t.CompletionCriteria.ErrorThreshold {
					return fmt.Errorf("LLM API request failed after exhausting all recovery paths: %w", err)
				}
				// Exponential backoff: 5s, 10s, 20s... capped at 60s
				shift := t.CompletionCriteria.ConsecutiveErrors - 1
				if shift < 0 {
					shift = 0
				}
				backoffDelay := time.Duration(5<<shift) * time.Second
				if backoffDelay > 60*time.Second {
					backoffDelay = 60 * time.Second
				}
				logger.Warnf("Waiting %v before retrying turn loop after LLM failure (consecutive errors: %d/%d)",
					backoffDelay, t.CompletionCriteria.ConsecutiveErrors, t.CompletionCriteria.ErrorThreshold)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(backoffDelay):
				}
				// Break inner loop to continue outer turn loop
				break
			}
		}

		// If LLM request still failed after all retries, skip the rest of this iteration
		// to avoid treating an empty response as implicit completion
		if err != nil {
			continue
		}

		if t.eventHandler != nil {
			dec := event.NewStreamEvent(event.EventTypePlannerDecision, "agent_turn")
			if len(toolCalls) > 0 {
				dec = dec.WithContent("tool_calls_detected")
			} else {
				dec = dec.WithContent("no_tool_calls_detected")
			}
			dec = dec.WithMetadata("turn_id", t.ID)
			dec = dec.WithMetadata("response_chars", len(response))
			dec = dec.WithMetadata("tool_calls_count", len(toolCalls))
			t.eventHandler(dec)
		}

		if len(toolCalls) == 0 && t.CompletionCriteria != nil && !t.CompletionCriteria.TaskCompleted {
			// Implicit completion: model returned pure text (no tool calls, finish_reason="stop") = natural task completion
			// All models accessed through OpenAI SDK follow the same finish_reason semantics:
			//   finish_reason="stop"       → model naturally finished
			//   finish_reason="tool_calls" → model requests tool calls (won't enter this branch)
			//
			// Safety net: loop detection + diminishing returns + max_turns handle edge cases

			// If loop detection is enabled and detects similar content, treat it as a loop and exit
			if t.CompletionCriteria.LoopDetectionEnabled && t.hasSimilarContent() {
				logger.Infof("Similar-content loop detected: terminating turn early")
				t.emitLoopDetectedEvent("similar_content",
					fmt.Sprintf("LLM output repeated %d times without meaningful change",
						t.CompletionCriteria.MaxSimilarContent))
				break // Exit loop due to detected loop, but don't mark as completed
			}

			// Implicit completion: model returned text without tool calls
			t.MarkTaskCompleted("natural-completion: model returned text without tool calls")
			break
		}

		// Convert tool calls to ToolToExecute format
		toolsToExecute := make([]ToolToExecute, len(toolCalls))
		for i, tc := range toolCalls {
			toolsToExecute[i] = ToolToExecute{
				ID:         tc.ID,
				Name:       tc.Name,
				Parameters: tc.Arguments, // Use Arguments instead of Parameters
			}
		}

		if t.eventHandler != nil && len(toolsToExecute) > 0 {
			names := make([]string, 0, len(toolsToExecute))
			for _, te := range toolsToExecute {
				if te.Name != "" {
					names = append(names, te.Name)
				}
			}
			ev := event.NewStreamEvent(event.EventTypeExecutorSchedule, "agent_turn")
			ev = ev.WithContent("scheduled_workers")
			ev = ev.WithMetadata("turn_id", t.ID)
			ev = ev.WithMetadata("workers_count", len(toolsToExecute))
			ev = ev.WithMetadata("tool_names", names)
			t.eventHandler(ev)
		}

		// Execute tools in parallel
		var toolResults map[string]*interfaces.ToolResult
		if len(toolsToExecute) > 0 {
			toolResults, err = t.executeToolCallsInParallel(ctx, toolsToExecute)
			if err != nil {
				// Tool execution encountered an infrastructure-level error (e.g. scheduler
				// not initialised).  In that case we synthesise failure results for every
				// tool in this batch so that:
				//   • the LLM context remains consistent (every tool_call has a tool reply)
				//   • the Turn loop continues rather than aborting the whole session
				logger.Errorf("Tool execution infrastructure error: %v", err)
				toolResults = t.buildFallbackResults(toolsToExecute, err)
				t.CompletionCriteria.ErrorCount++
			}
			if toolResults == nil {
				toolResults = make(map[string]*interfaces.ToolResult)
			}

		} else {
			toolResults = make(map[string]*interfaces.ToolResult)
		}

		// Add tool results to context
		if err := t.addToolResultsToContext(toolResults); err != nil {
			logger.Errorf("Failed to add tool results to context: %v", err)
			// Non-fatal: record the error but keep running so the LLM can report it
			t.CompletionCriteria.ErrorCount++
			t.CompletionCriteria.ConsecutiveErrors++
		} else {
			// Only reset consecutive errors if at least one tool actually succeeded
			// (not just "results successfully added to context")
			hasSuccess := false
			for _, result := range toolResults {
				if result != nil && result.Success {
					hasSuccess = true
					break
				}
			}
			if hasSuccess {
				t.CompletionCriteria.ConsecutiveErrors = 0
			}
		}

		// Record token gain for diminishing-returns detection after each full round-trip.
		t.recordTokenGain()

		// Check completion criteria
		if t.isComplete() {
			logger.Infof("Turn completion criteria met")
			break
		}
	}

	// Save conversation memory
	if err := t.saveConversationMemory(); err != nil {
		logger.Warnf("Failed to save conversation memory: %v", err)
	}

	if t.eventHandler != nil {
		stateEvt := event.NewStreamEvent(event.EventTypeExecutorState, "agent_turn")
		stateEvt = stateEvt.WithContent("closing")
		stateEvt = stateEvt.WithMetadata("turn_id", t.ID)
		stateEvt = stateEvt.WithMetadata("iterations", t.CompletionCriteria.CurrentIteration)
		t.eventHandler(stateEvt)
	}

	// Close the turn
	t.close()

	return turnErr
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

	err := t.LLMClient.StreamCompletion(ctx, t.Messages, func(streamEvent event.StreamEvent) {
		// Handle streaming events
		switch streamEvent.Type {
		case event.EventTypeContent:
			response = streamEvent.Content
			toolCalls = streamEvent.ToolCalls
			reasoning = streamEvent.Reasoning
		case event.EventTypeTokenStats:
			if streamEvent.TokenStats != nil {
				// Directly use event-provided TokenStats for accuracy
				t.TokenStats = streamEvent.TokenStats
			}
		}

		// Forward events to turn's event handler if available
		if t.eventHandler != nil {
			t.eventHandler(streamEvent)
		}
	})

	if err != nil {
		return "", nil, fmt.Errorf("streaming completion failed: %w", err)
	}

	// Add assistant response to messages
	// Include tool calls on the assistant message when present to satisfy API ordering
	assistantMsg := llm.Message{Role: "assistant", Content: response, Reasoning: reasoning}
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

// attemptLLMRecovery attempts multi-layer recovery for LLM API failures
// Returns true if recovery was successful and the request should be retried
func (t *Turn) attemptLLMRecovery(ctx context.Context, err error, attempt int) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())

	// Recovery Path 1: prompt_too_long → compress context and retry
	if strings.Contains(errMsg, "prompt_too_long") ||
		strings.Contains(errMsg, "context_length") ||
		strings.Contains(errMsg, "maximum context length") ||
		strings.Contains(errMsg, "context length exceeded") {
		logger.Infof("Attempting recovery from context length error via compression (attempt %d)", attempt)
		compErr := t.CompressMessages(ctx, true)
		if compErr == nil {
			logger.Infof("Context compression succeeded, retrying LLM request")
			return true
		}
		logger.Warnf("Context compression failed: %v", compErr)
	}

	// max_tokens related API errors are not recoverable by appending more prompt
	// content; doing so increases context size without changing the token limit.
	if strings.Contains(errMsg, "max_tokens") ||
		strings.Contains(errMsg, "maximum tokens") ||
		strings.Contains(errMsg, "output length") {
		logger.Warnf("Max tokens/output length error is not recoverable via retry in attemptLLMRecovery (attempt %d); reduce context or adjust token limits", attempt)
		return false
	}

	return false
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

	return t.SystemPromptBuilder.BuildEnhancedSystemPrompt(context.Background(), goals)
}

// shouldTerminate checks if the turn should terminate based on completion criteria
func (t *Turn) shouldTerminate() bool {
	if t.CompletionCriteria == nil {
		return false
	}

	// Check task completion
	if t.CompletionCriteria.TaskCompleted {
		return true
	}

	// Check consecutive errors threshold
	if t.CompletionCriteria.ConsecutiveErrors >= t.CompletionCriteria.ErrorThreshold {
		logger.Infof("Too many consecutive errors (%d >= %d): terminating turn",
			t.CompletionCriteria.ConsecutiveErrors, t.CompletionCriteria.ErrorThreshold)
		return true
	}

	// Diminishing-returns detection
	if t.CompletionCriteria.DiminishingReturnsEnabled {
		if t.hasDiminishingReturns() {
			logger.Infof("Diminishing-returns detected: stopping turn after %d consecutive low-gain iterations",
				t.CompletionCriteria.DiminishingReturnsWindow)
			return true
		}
	}

	// Loop detection: similar content only (repeated-tool-calls detection removed)
	if t.CompletionCriteria.LoopDetectionEnabled {
		if t.hasSimilarContent() {
			logger.Infof("Loop detected: %d consecutive similar LLM responses (threshold %d)",
				len(t.CompletionCriteria.ContentSimilarityHistory), t.CompletionCriteria.MaxSimilarContent)
			t.emitLoopDetectedEvent("similar_content",
				fmt.Sprintf("LLM output repeated %d times without meaningful change", t.CompletionCriteria.MaxSimilarContent))
			return true
		}
	}

	return false
}

// emitLoopDetectedEvent fires an EventTypeLoopDetected event with context.
func (t *Turn) emitLoopDetectedEvent(loopType, reason string) {
	if t.eventHandler == nil {
		return
	}
	evt := event.NewStreamEvent(event.EventTypeLoopDetected, "agent_turn")
	evt = evt.WithContent(reason)
	evt = evt.WithMetadata("loop_type", loopType)
	evt = evt.WithMetadata("reason", reason)
	if t.CompletionCriteria != nil {
		evt = evt.WithMetadata("iteration", t.CompletionCriteria.CurrentIteration)
	}
	t.eventHandler(evt)
}

// updateContentHistory appends an LLM response snippet and keeps the window trimmed.
// Call this after each requestOpenAIAPI that produced a non-empty text response.
func (t *Turn) updateContentHistory(response string) {
	cc := t.CompletionCriteria
	if cc == nil || !cc.LoopDetectionEnabled || response == "" {
		return
	}

	// Normalise: lowercase, collapse whitespace, trim to 200 chars for comparison
	normalised := strings.ToLower(strings.Join(strings.Fields(response), " "))
	if len(normalised) > 200 {
		normalised = normalised[:200]
	}

	maxHistory := cc.MaxSimilarContent + 1
	if maxHistory < 2 {
		maxHistory = 2
	}
	cc.ContentSimilarityHistory = append(cc.ContentSimilarityHistory, normalised)
	if len(cc.ContentSimilarityHistory) > maxHistory {
		cc.ContentSimilarityHistory = cc.ContentSimilarityHistory[len(cc.ContentSimilarityHistory)-maxHistory:]
	}
}

// hasSimilarContent returns true when the last MaxSimilarContent responses are
// all identical after normalisation.
func (t *Turn) hasSimilarContent() bool {
	cc := t.CompletionCriteria
	threshold := cc.MaxSimilarContent
	if threshold <= 0 {
		threshold = 3
	}
	if len(cc.ContentSimilarityHistory) < threshold {
		return false
	}
	// Check that the last `threshold` entries are identical
	recent := cc.ContentSimilarityHistory[len(cc.ContentSimilarityHistory)-threshold:]
	first := recent[0]
	for _, s := range recent[1:] {
		if s != first {
			return false
		}
	}
	return true
}

// recordTokenGain samples the current context token count and appends the delta
// to the diminishing-returns history ring buffer. Call this after each LLM round-trip.
func (t *Turn) recordTokenGain() {
	cc := t.CompletionCriteria
	if cc == nil || !cc.DiminishingReturnsEnabled {
		return
	}

	currentTokens := t.compressionStrategy.EstimateTokenCount(t.Messages)
	delta := currentTokens - cc.diminishingReturnsPrevTokens
	if delta < 0 {
		delta = 0 // after compression the count may drop; treat as zero gain
	}
	cc.diminishingReturnsPrevTokens = currentTokens

	// Keep only the last Window samples
	cc.diminishingReturnsHistory = append(cc.diminishingReturnsHistory, delta)
	window := cc.DiminishingReturnsWindow
	if window <= 0 {
		window = 3
	}
	if len(cc.diminishingReturnsHistory) > window {
		cc.diminishingReturnsHistory = cc.diminishingReturnsHistory[len(cc.diminishingReturnsHistory)-window:]
	}
}

// hasDiminishingReturns returns true when the last DiminishingReturnsWindow
// iterations all produced a token gain below DiminishingReturnsMinGain.
func (t *Turn) hasDiminishingReturns() bool {
	cc := t.CompletionCriteria

	// Skip diminishing returns detection if we're in error recovery mode
	// because error recovery naturally has low token counts
	if cc.ConsecutiveErrors > 0 {
		return false
	}

	window := cc.DiminishingReturnsWindow
	if window <= 0 {
		window = 3
	}
	minGain := cc.DiminishingReturnsMinGain
	if minGain <= 0 {
		minGain = 500
	}

	if len(cc.diminishingReturnsHistory) < window {
		return false // not enough samples yet
	}

	for _, gain := range cc.diminishingReturnsHistory {
		if gain >= minGain {
			return false // at least one recent iteration had meaningful progress
		}
	}
	return true
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
		t.Messages = append(t.Messages, llm.Message{
			Role:       "tool",
			Content:    "Tool execution result unavailable.",
			ToolCallID: callID,
		})
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
	cfg := config.Get()
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

// preprocessOpenSpecCommand detects /opsx: commands in user input and enriches
// the turn with OpenSpec context, system prompt additions, and modified user messages.
func (t *Turn) preprocessOpenSpecCommand(cfg *config.Config) {
	if cfg == nil || cfg.OpenSpec == nil || !cfg.OpenSpec.Enabled {
		return
	}

	cmd := openspec.ParseCommand(t.UserInput)
	if cmd == nil {
		return
	}

	logger.Infof("Detected OpenSpec command: /opsx:%s (change: %s)", cmd.Type, cmd.ChangeName)

	rootDir := cfg.OpenSpec.RootDir
	if rootDir == "" {
		rootDir = "openspec"
	}
	am := openspec.NewArtifactManager(rootDir, t.WorkingDir)
	if cfg.OpenSpec.MaxArtifactSize > 0 {
		am.SetMaxArtifactSize(cfg.OpenSpec.MaxArtifactSize)
	}
	engine := openspec.NewWorkflowEngine(am, cfg.OpenSpec.DefaultSchema)

	result, err := engine.HandleCommand(cmd)
	if err != nil {
		logger.Errorf("OpenSpec command failed: %v", err)
		// Convert the error to a user-friendly message
		t.UserInput = fmt.Sprintf("The user ran the OpenSpec command '%s' but it failed: %v\nHelp them resolve the issue.", cmd.RawInput, err)
		return
	}

	// If there's a status message but no user override, just prepend it
	if result.StatusMessage != "" && result.UserMessageOverride == "" {
		t.UserInput = result.StatusMessage
		return
	}

	// Override user message with enriched context
	if result.UserMessageOverride != "" {
		t.UserInput = result.UserMessageOverride
	}

	// Inject additional system prompt context
	if result.SystemPromptAddition != "" {
		if t.systemPrompt == "" {
			// Build the unified system prompt before appending the OpenSpec addition
			t.systemPrompt = t.buildUnifiedSystemPrompt() + result.SystemPromptAddition
		} else {
			t.systemPrompt += result.SystemPromptAddition
		}
	}
}

// preprocessSkillCommand detects /skill: commands in user input and handles
// skill activation/deactivation. It also performs auto-matching of skills
// based on user input when auto_invoke is enabled.
func (t *Turn) preprocessSkillCommand(ctx context.Context, cfg *config.Config) {
	if cfg == nil || cfg.Skills == nil || !cfg.Skills.Enabled {
		return
	}

	if t.SystemPromptBuilder == nil {
		return
	}
	sm := t.SystemPromptBuilder.skillManager
	if sm == nil {
		return
	}

	input := strings.TrimSpace(t.UserInput)

	// Handle /skill: commands
	if strings.HasPrefix(input, "/skill:") {
		t.handleSkillSlashCommand(ctx, sm, input)
		return
	}

	// Auto-match skills if global auto_invoke is enabled
	if sm.IsAutoInvokeEnabled() {
		matches := sm.Match(&skill.MatchContext{
			UserInput: input,
		}, false)

		activated := false
		for _, m := range matches {
			if err := sm.ActivateSkill(m.Skill.Name); err != nil {
				logger.Warnf("Failed to auto-activate skill %q: %v", m.Skill.Name, err)
				break // likely hit max active skills
			}
			activated = true
			logger.Infof("Auto-activated skill %q (reason: %s, score: %.2f)", m.Skill.Name, m.Reason, m.Score)
		}

		// If skills were activated, rebuild system prompt
		if activated {
			t.systemPrompt = "" // Force rebuild on next access
		}
	}
}

// handleSkillSlashCommand processes /skill: slash commands.
func (t *Turn) handleSkillSlashCommand(ctx context.Context, sm *skill.Manager, input string) {
	parts := strings.SplitN(input, " ", 2)
	cmd := strings.TrimPrefix(parts[0], "/skill:")
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case "list":
		listing := sm.ListSkillNames()
		t.UserInput = fmt.Sprintf("The user requested a list of available skills. Here is the current status:\n\n%s\nPresent this information to the user.", listing)

	case "use":
		if arg == "" {
			t.UserInput = "The user tried to activate a skill but didn't specify a name. Ask them which skill they want to use. Available skills:\n\n" + sm.ListSkillNames()
			return
		}
		if err := sm.ActivateSkill(arg); err != nil {
			t.UserInput = fmt.Sprintf("The user tried to activate skill %q but it failed: %v\nHelp them resolve the issue.", arg, err)
			return
		}
		logger.Infof("Manually activated skill %q", arg)
		s := sm.GetByName(arg)
		t.systemPrompt = "" // Force rebuild
		t.UserInput = fmt.Sprintf("The user activated skill '%s'. Acknowledge that the skill is now active and briefly describe what it does: %s", arg, s.Description)

	case "off":
		if arg == "" {
			t.UserInput = "The user tried to deactivate a skill but didn't specify a name. Ask them which skill to deactivate."
			return
		}
		sm.DeactivateSkill(arg) //nolint:errcheck // non-fatal: skill is deactivated in-memory even if persist fails
		logger.Infof("Deactivated skill %q", arg)
		t.systemPrompt = "" // Force rebuild
		t.UserInput = fmt.Sprintf("The user deactivated skill '%s'. Acknowledge that the skill has been deactivated.", arg)

	case "info":
		if arg == "" {
			t.UserInput = "The user tried to get skill info but didn't specify a name. Ask them which skill they want info about."
			return
		}
		s := sm.GetByName(arg)
		if s == nil {
			t.UserInput = fmt.Sprintf("The user requested info about skill %q but it was not found. Available skills:\n\n%s", arg, sm.ListSkillNames())
			return
		}
		t.UserInput = fmt.Sprintf("The user requested info about skill '%s'. Present the following:\n\nName: %s\nDescription: %s\nScope: %s\nTriggers: %s\nGlobs: %s\nAuto-invoke: %t\nPriority: %d\nActive: %t\n\nFull Instructions:\n%s",
			arg, s.Name, s.Description, s.Scope,
			strings.Join(s.Triggers, ", "), strings.Join(s.Globs, ", "),
			s.IsAutoInvoke(), s.Priority, sm.IsActive(s.Name), s.Instructions)

	case "install":
		if arg == "" {
			t.UserInput = "The user tried to install a skill but didn't provide a URL. Ask them for the URL of the SKILL.md file to install."
			return
		}
		ctx, cancel := context.WithTimeout(ctx, skill.InstallHTTPTimeout)
		defer cancel()
		installed, err := sm.InstallSkill(ctx, arg)
		if err != nil {
			t.UserInput = fmt.Sprintf("The user tried to install a skill from %q but it failed: %v\nHelp them resolve the issue.", arg, err)
			return
		}
		logger.Infof("Installed skill %q from %s", installed.Name, arg)
		t.systemPrompt = "" // Force rebuild
		t.UserInput = fmt.Sprintf("The user installed skill '%s' from %s. The skill is now available. Briefly describe what it does: %s\n\nAvailable commands: /skill:use %s (to activate), /skill:info %s (for details)",
			installed.Name, arg, installed.Description, installed.Name, installed.Name)

	default:
		t.UserInput = fmt.Sprintf("Unknown skill command '/skill:%s'. Available commands: /skill:list, /skill:use <name>, /skill:off <name>, /skill:info <name>, /skill:install <url>", cmd)
	}
}

// preprocessLoopCommand detects /loop and /schedule slash commands.
func (t *Turn) preprocessLoopCommand() {
	input := strings.TrimSpace(t.UserInput)
	if !strings.HasPrefix(input, "/loop") && !strings.HasPrefix(input, "/schedule") {
		return
	}

	if t.TUIScheduler == nil {
		t.UserInput = "Scheduling is not available in the current mode. Start the TUI to use /loop and /schedule commands."
		return
	}

	var cmdStr string
	if strings.HasPrefix(input, "/loop") {
		cmdStr = input
	} else {
		// /schedule — treat remainder as natural language schedule + command
		// e.g. "/schedule every 5 minutes check build status"
		// Rewrite as "/loop every 5 minutes check build status"
		rest := strings.TrimPrefix(input, "/schedule")
		rest = strings.TrimSpace(rest)
		if rest == "" {
			t.UserInput = "Usage: /schedule <interval> <command>  e.g. '/schedule every 5 minutes check build status'"
			return
		}
		// Extract natural-language schedule + command
		t.handleScheduleCommand(rest)
		return
	}

	lc, err := builtin.ParseLoopCommand(cmdStr)
	if err != nil {
		t.UserInput = fmt.Sprintf("Invalid /loop command: %v\nUsage: /loop <interval> <command>  e.g. '/loop 5m check build'\n       /loop stop <task-id>\n       /loop list", err)
		return
	}

	switch lc.Action {
	case "list":
		tasks := t.TUIScheduler.ListTasks()
		if len(tasks) == 0 {
			t.UserInput = "No active scheduled tasks. Use '/loop <interval> <command>' to create one."
			return
		}
		var sb strings.Builder
		sb.WriteString("Active scheduled tasks:\n")
		for _, task := range tasks {
			fmt.Fprintf(&sb, "- [%s] %s | cron: %s\n", task.ID[:8], task.Command, task.CronExpr)
		}
		sb.WriteString("\nTo stop a task: /loop stop <task-id>")
		t.UserInput = sb.String()

	case "stop":
		if err := t.TUIScheduler.CancelTask(lc.TaskID); err != nil {
			t.UserInput = fmt.Sprintf("Failed to stop task %q: %v", lc.TaskID, err)
			return
		}
		t.UserInput = fmt.Sprintf("Task %q stopped.", lc.TaskID)

	case "start":
		task, err := t.TUIScheduler.ScheduleCron(lc.CronExpr, lc.Command)
		if err != nil {
			t.UserInput = fmt.Sprintf("Failed to schedule task: %v", err)
			return
		}
		t.UserInput = fmt.Sprintf("Task scheduled.\n  ID: %s\n  Interval: %s (cron: %s)\n  Command: %s\nUse '/loop stop %s' to cancel.",
			task.ID, lc.Interval, lc.CronExpr, lc.Command, task.ID)
	}
}

// handleScheduleCommand processes "/schedule <natural-language>" commands.
func (t *Turn) handleScheduleCommand(rest string) {
	// Try to extract a schedule expression from the beginning of the string.
	// The LLM can also handle this naturally, so we just pass enriched context.
	t.UserInput = fmt.Sprintf(
		"The user wants to schedule a recurring task: %q\n"+
			"Parse the schedule from the natural language and create the task using the manage_schedule tool.\n"+
			"Examples of schedule formats: 'every 5 minutes', 'daily at 9am', 'every monday at 9am'.",
		rest,
	)
}

// preprocessWatcherCommand detects and transforms /watcher slash commands.
// Structured sub-commands (list, status, delete) are handled directly;
// all other input is passed to the LLM to be interpreted as natural language
// and converted into a manage_watcher tool call.
func (t *Turn) preprocessWatcherCommand() {
	input := strings.TrimSpace(t.UserInput)
	if !strings.HasPrefix(input, "/watcher") {
		return
	}

	rest := strings.TrimSpace(strings.TrimPrefix(input, "/watcher"))

	switch {
	case rest == "" || rest == "list":
		// Direct: list all watcher rules via manage_watcher tool.
		t.UserInput = "Please list all active watcher rules using the manage_watcher tool with action='list'."
		return
	case rest == "status":
		t.UserInput = "Please show the status of all active watcher rules using the manage_watcher tool with action='status'."
		return
	case strings.HasPrefix(rest, "delete "):
		ruleID := strings.TrimSpace(strings.TrimPrefix(rest, "delete "))
		if ruleID == "" {
			t.UserInput = "Usage: /watcher delete <rule-id>"
			return
		}
		t.UserInput = fmt.Sprintf(
			"Please delete the watcher rule with ID %q using the manage_watcher tool with action='delete' and rule_id=%q.",
			ruleID, ruleID,
		)
		return
	}

	// For all other input, delegate to the LLM so it can interpret natural
	// language and create a watcher rule via the manage_watcher tool.
	t.handleWatcherCommand(rest)
}

// handleWatcherCommand processes natural-language /watcher input by enriching
// it into a prompt that instructs the LLM to call manage_watcher.
func (t *Turn) handleWatcherCommand(rest string) {
	t.UserInput = fmt.Sprintf(
		"The user wants to configure an event watcher: %q\n"+
			"Parse the user's intent and create a watcher rule using the manage_watcher tool.\n"+
			"Supported sources: 'aone' (for Aone MR/CI events), 'shell' (for custom commands).\n"+
			"Examples:\n"+
			"- '监听 aone/a1 的新 MR，自动评审' → source='aone', event='new_mr', filter='repo:aone/a1 state:opened'\n"+
			"- '每10分钟检查 CI 是否失败' → source='aone', event='ci_failure', interval='10m'\n"+
			"- '每小时运行 my-script.sh 检查状态' → source='shell', shell_command='my-script.sh'\n",
		rest,
	)
}
