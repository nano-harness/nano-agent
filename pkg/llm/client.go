package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/tools"
	"github.com/openai/openai-go/v3"
)

// MessageContent represents content that can be text or image
type MessageContent struct {
	Type     string    `json:"type"` // "text" or "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL represents an image URL with optional detail level
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}

// Message represents a conversation message
type Message struct {
	Role        string             `json:"role"`
	Content     string             `json:"content"`
	Contents    []MessageContent   `json:"contents,omitempty"` // Multimodal content support
	ToolCalls   []tools.ToolCall   `json:"tool_calls,omitempty"`
	ToolResults []tools.ToolResult `json:"tool_results,omitempty"`
	ToolCallID  string             `json:"tool_call_id,omitempty"`
	Reasoning   string             `json:"reasoning,omitempty"` // Reasoning tokens from the model
}

// StreamClient interface for streaming completions
type StreamClient interface {
	StreamCompletion(ctx context.Context, messages []Message, onEvent func(event.StreamEvent)) error
	StreamCompletionWithoutReasoning(ctx context.Context, messages []Message, onEvent func(event.StreamEvent)) error
}

// LLMClient represents a complete LLM client interface including streaming and tools update
type LLMClient interface {
	StreamClient
	GenerateContent(ctx context.Context, prompt string) (string, error)
	UpdateTools(tools []interfaces.Tool)
}

// Client represents an optimized LLM client using official OpenAI SDK
type Client struct {
	client              openai.Client
	model               string
	baseURL             string
	provider            ProviderInfo
	tools               []interfaces.Tool
	tokenCounter        *TokenCounter
	toolGate            interfaces.ToolGate
	validator           *event.EventValidator
	config              *config.Config // Add config to access reasoning settings
	circuitBreaker      *CircuitBreaker
	truncationDetection bool
}

// NewClient creates a new optimized LLM client using official OpenAI SDK
func NewClient(apiKey, baseURL, model string, tools []interfaces.Tool) *Client {
	cfg := config.Get()
	tokenCounter, _ := NewTokenCounter(model)
	opts := newOpenAIRequestOptions(apiKey, baseURL, cfg)

	client := &Client{
		client:              openai.NewClient(opts...),
		model:               model,
		baseURL:             baseURL,
		provider:            NewProviderInfo(baseURL),
		tools:               tools,
		tokenCounter:        tokenCounter,
		config:              cfg, // Store config for reasoning support
		circuitBreaker:      newCircuitBreakerForRoute(InferProviderID(baseURL, model), baseURL, cfg),
		truncationDetection: truncationDetectionEnabled(cfg),
	}

	return client
}

// GenerateContent generates content from prompt
func (c *Client) GenerateContent(ctx context.Context, prompt string) (string, error) {
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
		Model: openai.ChatModel(c.model),
	}

	completion, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", err
	}

	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("no response from LLM")
	}

	return completion.Choices[0].Message.Content, nil
}

// StreamCompletion creates a streaming completion with function calling support
func (c *Client) StreamCompletion(ctx context.Context, messages []Message, onEvent func(event.StreamEvent)) error {
	logger.Debugf("Starting LLM stream completion with %d messages", len(messages))

	// Wrap onEvent with sanitizer to prevent secret leakage at the source
	sanitizedOnEvent := func(ev event.StreamEvent) {
		clean := c.sanitizeEvent(ev)
		onEvent(clean)
	}

	// Apply a total response timeout via context if none is set upstream
	// This prevents indefinite blocking on HTTP/2 gzip SSE reads when the server stalls.
	totalTimeout := 15 * time.Minute
	if c.config != nil && c.config.ResponseTimeout > 0 {
		totalTimeout = c.config.ResponseTimeout
	}
	streamCtx := ctx
	cancel := func() {} //nolint:ineffassign,staticcheck
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		streamCtx, cancel = context.WithTimeout(ctx, totalTimeout)
		logger.Debugf("Applying streaming response timeout: %v", totalTimeout)
	} else {
		// Upstream already set a deadline; attach a cancellable child to allow early termination.
		streamCtx, cancel = context.WithCancel(ctx)
		logger.Debugf("Upstream context already has deadline; using existing timeout")
	}
	defer cancel()

	// Count input tokens
	inputTokens := 0
	if c.tokenCounter != nil {
		inputTokens = c.tokenCounter.CountMessagesTokens(messages)
	} else {
		// Fallback to rough estimate
		for _, msg := range messages {
			inputTokens += EstimateTokensFromChars(msg.Content)
		}
	}

	// Track token statistics with streaming support
	tokenStats := NewTokenStats()
	tokenStats.SetInputTokens(inputTokens)
	tokenStats.StartStreaming() // Initialize streaming mode

	sanitizedOnEvent(event.StreamEvent{
		Type:    event.EventTypeThinking,
		Content: c.thinkingStatusMessage(),
	})

	// Convert our messages to OpenAI format
	openaiMessages := c.convertMessages(messages)

	// Convert tools to OpenAI format
	openaiTools := c.convertTools()

	// Create chat completion request
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(c.model),
		Messages: openaiMessages,
	}

	// Add tools if available
	if len(openaiTools) > 0 {
		params.Tools = openaiTools
	}

	extraFields := make(map[string]interface{})
	c.maybeSetReasoningMessagesOverride(extraFields, messages)

	// Add reasoning parameters if enabled with graceful degradation.
	reasoningEnabled := c.applyReasoningRequestOptions(extraFields, tokenStats)

	// When reasoning/thinking is enabled and tools are present, explicitly set
	// tool_choice to "auto" to prevent providers (dashscope, gemini, etc.) from
	// defaulting to "required" which is incompatible with thinking mode.
	if reasoningEnabled && len(openaiTools) > 0 {
		extraFields["tool_choice"] = "auto"
		logger.Debugf("Explicitly setting tool_choice='auto' for reasoning mode compatibility")
	}

	if len(extraFields) > 0 {
		params.SetExtraFields(extraFields)
		logger.Debugf("Added extra fields to request: %+v", extraFields)
	}

	// Calculate request size
	requestJSON, _ := json.Marshal(params)
	tokenStats.RequestSizeBytes = len(requestJSON)

	// Circuit breaker retry loop for transient errors (e.g. 429 rate limit)
	maxRetries := 0
	if c.circuitBreaker != nil {
		maxRetries = c.circuitBreaker.config.MaxRetries
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Check circuit breaker before making request
		if c.circuitBreaker != nil {
			if err := c.circuitBreaker.AllowRequest(); err != nil {
				sanitizedOnEvent(event.StreamEvent{
					Type:  event.EventTypeError,
					Error: fmt.Sprintf("circuit breaker: %v", err),
				})
				return err
			}
		}

		// Create streaming request with reasoning fallback mechanism
		stream := c.client.Chat.Completions.NewStreaming(streamCtx, params)

		assembler := NewStreamAssembler()
		var toolCalls []tools.ToolCall
		var lastThinkingSendTime time.Time
		const thinkingSendInterval = 300 * time.Millisecond
		var lastSentReasoningLen int
		var finishReason string
		truncated := false

		// Process streaming response
		for stream.Next() {
			// Check for context cancellation or timeout
			select {
			case <-streamCtx.Done():
				_ = stream.Close()
				err := streamCtx.Err()
				if err == context.DeadlineExceeded {
					logger.Warnf("LLM stream timed out after %v", totalTimeout)
					sanitizedOnEvent(event.StreamEvent{
						Type:  event.EventTypeError,
						Error: "Stream timed out",
					})
					// If reasoning was enabled, attempt fallback without reasoning
					if reasoningEnabled {
						tokenStats.SetReasoningFallback(true)
						return c.streamCompletionWithoutReasoning(ctx, messages, onEvent)
					}
					return err
				}
				logger.Debugf("LLM stream cancelled by context: %v", err)
				sanitizedOnEvent(event.StreamEvent{
					Type:  event.EventTypeError,
					Error: "Request cancelled",
				})
				return err
			default:
				// Continue processing
			}

			chunk := stream.Current()

			if len(chunk.Choices) == 0 {
				continue
			}

			choice := chunk.Choices[0]
			delta := choice.Delta

			// Handle reasoning content from raw JSON if available
			// The reasoning_content field is not directly exposed in SDK v1.9.0,
			// but available via raw JSON parsing for models that support it
			if rawJSON := delta.RawJSON(); rawJSON != "" {
				var deltaMap map[string]interface{}
				if err := json.Unmarshal([]byte(rawJSON), &deltaMap); err == nil {
					if reasoningStr, ok := deltaMap["reasoning_content"].(string); ok && reasoningStr != "" {
						assembler.AddReasoning(reasoningStr)
						// Send streaming thinking event with throttling to enable real-time display.
						// Content is intentionally left empty to avoid overwriting any more
						// informative title set by the initial thinking event.
						if time.Since(lastThinkingSendTime) >= thinkingSendInterval {
							fullContent := assembler.Reasoning()
							delta := fullContent[lastSentReasoningLen:]
							sanitizedOnEvent(event.StreamEvent{
								Type:           event.EventTypeThinking,
								Reasoning:      fullContent,
								ReasoningDelta: delta,
							})
							lastSentReasoningLen = len(fullContent)
							lastThinkingSendTime = time.Now()
						}
					}
				}
			}

			// Handle text content
			if delta.Content != "" {
				assembler.AddContent(delta.Content)

				// Count output tokens
				outputTokens := 0
				if c.tokenCounter != nil {
					outputTokens = c.tokenCounter.CountTokens(delta.Content)
				} else {
					outputTokens = EstimateTokensFromChars(delta.Content)
				}
				tokenStats.AddOutputTokens(outputTokens)

				// Send real-time token stats update
				statsEvent := event.NewStreamEvent(event.EventTypeTokenStats, "llm_client")
				statsEvent.TokenStats = tokenStats.GetEvent()
				sanitizedOnEvent(statsEvent)

				// Send stream content for real-time rendering
				streamEvent := event.NewStreamEvent(event.EventTypeStreamContent, "llm_client")
				streamEvent = streamEvent.WithContent(delta.Content)
				sanitizedOnEvent(streamEvent)
			}

			// Handle tool calls - parse and collect without execution
			if len(delta.ToolCalls) > 0 {
				for _, toolCallChunk := range delta.ToolCalls {
					// Aggregate by per-call index because subsequent chunks may omit the ID
					idx := int(toolCallChunk.Index)
					if nameStarted := assembler.AddToolCallDelta(
						idx,
						toolCallChunk.ID,
						toolCallChunk.Function.Name,
						toolCallChunk.Function.Arguments,
					); nameStarted {
						nameTokens := 0
						if c.tokenCounter != nil {
							nameTokens = c.tokenCounter.CountTokens(assembler.ToolCallName(idx))
						} else {
							nameTokens = EstimateTokensFromChars(assembler.ToolCallName(idx))
						}
						tokenStats.AddOutputTokens(nameTokens)
						statsEvent := event.NewStreamEvent(event.EventTypeTokenStats, "llm_client")
						statsEvent.TokenStats = tokenStats.GetEvent()
						sanitizedOnEvent(statsEvent)
					}
					if toolCallChunk.Function.Arguments != "" {
						argTokens := 0
						if c.tokenCounter != nil {
							argTokens = c.tokenCounter.CountTokens(toolCallChunk.Function.Arguments)
						} else {
							argTokens = EstimateTokensFromChars(toolCallChunk.Function.Arguments)
						}
						tokenStats.AddOutputTokens(argTokens)
						statsEvent := event.NewStreamEvent(event.EventTypeTokenStats, "llm_client")
						statsEvent.TokenStats = tokenStats.GetEvent()
						sanitizedOnEvent(statsEvent)
					}
				}
			}

			// Check if stream is finished
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
				truncated = c.truncationDetection && finishReason == "length"
				toolCalls = assembler.FinalizeToolCalls(c.toolRequiresParameters)
				break
			}
		}

		// Check for stream errors with reasoning fallback
		if err := stream.Err(); err != nil {
			_ = stream.Close()

			// If reasoning was enabled and we get an error, try fallback without reasoning
			if reasoningEnabled {
				if shouldFallbackWithoutReasoning(err) {
					logger.Warnf("推理模式请求失败，正在切换到标准模式重试: %v", err)
					tokenStats.SetReasoningFallback(true)
					return c.streamCompletionWithoutReasoning(ctx, messages, onEvent)
				}
			}

			// Only retry if no content was streamed (safe to retry)
			// and the error is retryable (rate limit, server error, etc.)
			if attempt < maxRetries && assembler.ContentLen() == 0 && IsRetryableError(err) {
				lastErr = err
				if c.circuitBreaker != nil {
					if c.shouldRecordCBFailure(err) {
						c.circuitBreaker.RecordFailure()
					}
					c.circuitBreaker.RecordRetry()
				}

				httpStatus := ExtractHTTPStatus(err)
				delay := c.circuitBreaker.CalculateRetryDelay(attempt, httpStatus)
				logger.Warnf("LLM API request failed (attempt %d/%d): %v, retrying in %v",
					attempt+1, maxRetries+1, err, delay)

				// Send retry event to caller
				retryEvent := event.NewStreamEvent(event.EventTypeRetry, "llm_client")
				retryEvent = retryEvent.WithContent(
					fmt.Sprintf("API请求失败(HTTP %d)，%v后重试 (第%d/%d次)", httpStatus, delay.Round(time.Second), attempt+1, maxRetries+1),
				).WithMetadata("retry_delay_ms", delay.Milliseconds()).
					WithMetadata("attempt", attempt+1).
					WithMetadata("max_attempts", maxRetries+1).
					WithMetadata("http_status", httpStatus).
					WithRetryCount(attempt + 1)
				sanitizedOnEvent(retryEvent)

				// Wait before retrying, but respect the overall stream timeout.
				select {
				case <-streamCtx.Done():
					return streamCtx.Err()
				case <-time.After(delay):
					continue
				}
			}

			// Non-retryable or exhausted retries
			if c.circuitBreaker != nil && c.shouldRecordCBFailure(err) {
				c.circuitBreaker.RecordFailure()
			}
			sanitizedOnEvent(event.StreamEvent{
				Type:  event.EventTypeError,
				Error: fmt.Sprintf("stream error: %v", err),
			})
			return err
		}

		// Success - close stream and record
		_ = stream.Close()
		if c.circuitBreaker != nil {
			c.circuitBreaker.RecordSuccess()
		}

		// Finalize response without tool execution
		return c.finalizeResponse(assembler.Content(), assembler.Reasoning(), toolCalls, sanitizedOnEvent, tokenStats, finishMetadata(truncated, finishReason))
	}

	// All retries exhausted (should not normally reach here)
	totalAttempts := maxRetries + 1
	if lastErr != nil {
		return fmt.Errorf("LLM API request failed after %d total attempts: %w", totalAttempts, lastErr)
	}
	return fmt.Errorf("LLM API request failed after %d total attempts", totalAttempts)
}

// streamCompletionWithoutReasoning performs streaming completion without reasoning parameters
// This is used as a fallback when reasoning requests fail
func (c *Client) streamCompletionWithoutReasoning(ctx context.Context, messages []Message, onEvent func(event.StreamEvent)) error {
	logger.Debugf("Starting fallback stream completion without reasoning for model: %s", c.model)

	// Create sanitized event handler
	sanitizedOnEvent := func(ev event.StreamEvent) {
		// Always sanitize events to ensure consistent redaction behavior
		ev = c.sanitizeEvent(ev)
		onEvent(ev)
	}

	// Initialize token statistics
	tokenStats := NewTokenStats()
	tokenStats.StartStreaming()

	// Convert messages to OpenAI format
	openaiMessages := c.convertMessages(messages)

	// Count input tokens
	inputTokens := 0
	for _, msg := range messages {
		if c.tokenCounter != nil {
			inputTokens += c.tokenCounter.CountTokens(msg.Content)
		} else {
			inputTokens += EstimateTokensFromChars(msg.Content)
		}
	}
	tokenStats.InputTokens = inputTokens

	// Convert tools to OpenAI format
	openaiTools := c.convertTools()

	// Send thinking event
	thinkingEvent := event.NewStreamEvent(event.EventTypeThinking, "llm_client")
	sanitizedOnEvent(thinkingEvent)

	// Create request parameters WITHOUT reasoning
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(c.model),
		Messages: openaiMessages,
	}

	// Add tools if available
	if len(openaiTools) > 0 {
		params.Tools = openaiTools
	}

	extraFields := make(map[string]interface{})
	c.maybeSetReasoningMessagesOverride(extraFields, messages)
	if len(extraFields) > 0 {
		params.SetExtraFields(extraFields)
	}

	logger.Debugf("Fallback request created without reasoning parameters")

	// Calculate request size
	requestJSON, _ := json.Marshal(params)
	tokenStats.RequestSizeBytes = len(requestJSON)

	// Apply a total response timeout via context for fallback as well
	totalTimeout := 15 * time.Minute
	if c.config != nil && c.config.ResponseTimeout > 0 {
		totalTimeout = c.config.ResponseTimeout
	}
	streamCtx := ctx
	cancel := func() {} //nolint:ineffassign,staticcheck
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		streamCtx, cancel = context.WithTimeout(ctx, totalTimeout)
		logger.Debugf("Applying fallback streaming response timeout: %v", totalTimeout)
	} else {
		streamCtx, cancel = context.WithCancel(ctx)
		logger.Debugf("Upstream context already has deadline; using existing timeout for fallback")
	}
	defer cancel()

	// Circuit breaker retry loop for transient errors
	maxRetries := 0
	if c.circuitBreaker != nil {
		maxRetries = c.circuitBreaker.config.MaxRetries
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Check circuit breaker before making request
		if c.circuitBreaker != nil {
			if err := c.circuitBreaker.AllowRequest(); err != nil {
				sanitizedOnEvent(event.StreamEvent{
					Type:  event.EventTypeError,
					Error: fmt.Sprintf("circuit breaker: %v", err),
				})
				return err
			}
		}

		// Create streaming request
		stream := c.client.Chat.Completions.NewStreaming(streamCtx, params)

		var responseContent strings.Builder
		var finishReason string
		truncated := false

		// Tool call aggregation
		type toolCallBuilder struct {
			ID        string
			Name      string
			Arguments strings.Builder
		}

		partialToolCalls := make(map[int]*toolCallBuilder)
		toolCallOrder := []int{}

		// Process streaming response
		for stream.Next() {
			// Check for context cancellation or timeout
			select {
			case <-streamCtx.Done():
				_ = stream.Close()
				err := streamCtx.Err()
				if err == context.DeadlineExceeded {
					logger.Warnf("Fallback LLM stream timed out after %v", totalTimeout)
					sanitizedOnEvent(event.StreamEvent{
						Type:  event.EventTypeError,
						Error: "fallback stream timed out",
					})
					return err
				}
				sanitizedOnEvent(event.StreamEvent{
					Type:  event.EventTypeError,
					Error: "request cancelled",
				})
				return err
			default:
				// Continue processing
			}

			chunk := stream.Current()

			if len(chunk.Choices) == 0 {
				continue
			}

			choice := chunk.Choices[0]
			delta := choice.Delta

			// Handle text content
			if delta.Content != "" {
				responseContent.WriteString(delta.Content)

				// Count output tokens
				outputTokens := 0
				if c.tokenCounter != nil {
					outputTokens = c.tokenCounter.CountTokens(delta.Content)
				} else {
					outputTokens = EstimateTokensFromChars(delta.Content)
				}
				tokenStats.AddOutputTokens(outputTokens)

				// Send real-time token stats update
				statsEvent := event.NewStreamEvent(event.EventTypeTokenStats, "llm_client")
				statsEvent.TokenStats = tokenStats.GetEvent()
				sanitizedOnEvent(statsEvent)

				// Send stream content for real-time rendering
				streamEvent := event.NewStreamEvent(event.EventTypeStreamContent, "llm_client")
				streamEvent = streamEvent.WithContent(delta.Content)
				sanitizedOnEvent(streamEvent)
			}

			// Handle tool calls - parse and collect without execution
			if len(delta.ToolCalls) > 0 {
				for _, toolCallChunk := range delta.ToolCalls {
					// Aggregate by per-call index because subsequent chunks may omit the ID
					idx := int(toolCallChunk.Index)

					// Initialize builder for this index if first time seen
					if _, ok := partialToolCalls[idx]; !ok {
						partialToolCalls[idx] = &toolCallBuilder{}
						toolCallOrder = append(toolCallOrder, idx)
					}

					// Update builder with current chunk data
					if builder := partialToolCalls[idx]; builder != nil {
						// Capture ID when available (may be present only in the first chunk)
						if builder.ID == "" && toolCallChunk.ID != "" {
							builder.ID = toolCallChunk.ID
						}
						// Capture name when available
						if toolCallChunk.Function.Name != "" && builder.Name == "" {
							builder.Name = toolCallChunk.Function.Name
						}
						// Append argument chunks
						if toolCallChunk.Function.Arguments != "" {
							builder.Arguments.WriteString(toolCallChunk.Function.Arguments)
						}
					}
				}
			}

			// Check if stream is finished
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
				truncated = c.truncationDetection && finishReason == "length"
				// Finalize all tool calls
				for _, idx := range toolCallOrder {
					if builder, ok := partialToolCalls[idx]; ok {
						// Validate tool call has required fields
						if builder.Name == "" {
							logger.Warnf("Tool call at index %d has empty name, skipping", idx)
							continue
						}

						// Parse arguments JSON string to map
						var args map[string]interface{}
						if err := json.Unmarshal([]byte(builder.Arguments.String()), &args); err != nil {
							logger.Warnf("Failed to parse tool call arguments: %v", err)
							args = make(map[string]interface{})
						}

						// Create tool call
						toolCall := tools.ToolCall{
							ID:        builder.ID,
							Name:      builder.Name,
							Arguments: args,
						}

						logger.Debugf("Parsed tool call: %s with args: %v", toolCall.Name, toolCall.Arguments)
					}
				}
				break
			}
		}

		// Collect finalized tool calls
		var toolCalls []tools.ToolCall
		for _, idx := range toolCallOrder {
			if builder, ok := partialToolCalls[idx]; ok && builder.Name != "" {
				// Parse arguments JSON string to map
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(builder.Arguments.String()), &args); err != nil {
					logger.Warnf("Failed to parse tool call arguments: %v", err)
					args = make(map[string]interface{})
				}

				toolCall := tools.ToolCall{
					ID:        builder.ID,
					Name:      builder.Name,
					Arguments: args,
				}
				toolCalls = append(toolCalls, toolCall)
			}
		}

		// Check for stream errors with retry
		if err := stream.Err(); err != nil {
			_ = stream.Close()

			// Only retry if no content was streamed and the error is retryable
			if attempt < maxRetries && responseContent.Len() == 0 && IsRetryableError(err) {
				lastErr = err
				if c.circuitBreaker != nil {
					if c.shouldRecordCBFailure(err) {
						c.circuitBreaker.RecordFailure()
					}
					c.circuitBreaker.RecordRetry()
				}

				httpStatus := ExtractHTTPStatus(err)
				delay := c.circuitBreaker.CalculateRetryDelay(attempt, httpStatus)
				logger.Warnf("Fallback LLM API request failed (attempt %d/%d): %v, retrying in %v",
					attempt+1, maxRetries+1, err, delay)

				retryEvent := event.NewStreamEvent(event.EventTypeRetry, "llm_client")
				retryEvent = retryEvent.WithContent(
					fmt.Sprintf("API请求失败(HTTP %d)，%v后重试 (第%d/%d次)", httpStatus, delay.Round(time.Second), attempt+1, maxRetries+1),
				).WithMetadata("retry_delay_ms", delay.Milliseconds()).
					WithMetadata("attempt", attempt+1).
					WithMetadata("max_attempts", maxRetries+1).
					WithMetadata("http_status", httpStatus).
					WithRetryCount(attempt + 1)
				sanitizedOnEvent(retryEvent)

				select {
				case <-streamCtx.Done():
					return streamCtx.Err()
				case <-time.After(delay):
					continue
				}
			}

			// Non-retryable or exhausted retries
			if c.circuitBreaker != nil && c.shouldRecordCBFailure(err) {
				c.circuitBreaker.RecordFailure()
			}
			sanitizedOnEvent(event.StreamEvent{
				Type:  event.EventTypeError,
				Error: fmt.Sprintf("fallback stream error: %v", err),
			})
			return err
		}

		// Success
		_ = stream.Close()
		if c.circuitBreaker != nil {
			c.circuitBreaker.RecordSuccess()
		}

		logger.Debugf("Fallback stream completed successfully without reasoning")

		// Finalize response without tool execution
		return c.finalizeResponse(responseContent.String(), "", toolCalls, sanitizedOnEvent, tokenStats, finishMetadata(truncated, finishReason))
	}

	// All retries exhausted
	totalAttempts := maxRetries + 1
	if lastErr != nil {
		return fmt.Errorf("fallback LLM API request failed after %d total attempts: %w", totalAttempts, lastErr)
	}
	return fmt.Errorf("fallback LLM API request failed after %d total attempts", totalAttempts)
}

// StreamCompletionWithoutReasoning performs streaming completion with reasoning disabled for this call.
// Use this for tasks like context compression summarization where thinking/reasoning tokens are unnecessary.
func (c *Client) StreamCompletionWithoutReasoning(ctx context.Context, messages []Message, onEvent func(event.StreamEvent)) error {
	// Wrap onEvent with sanitizer to prevent secret leakage at the source
	sanitizedOnEvent := func(ev event.StreamEvent) {
		ev = c.sanitizeEvent(ev)
		onEvent(ev)
	}

	return c.streamCompletionWithoutReasoning(ctx, messages, sanitizedOnEvent)
}

// finalizeResponse sends the final response events
func (c *Client) finalizeResponse(content string, reasoning string, toolCalls []tools.ToolCall, onEvent func(event.StreamEvent), tokenStats *TokenStats, finalMetadata ...map[string]interface{}) error {
	metadata := map[string]interface{}(nil)
	if len(finalMetadata) > 0 {
		metadata = finalMetadata[0]
	}
	// Stop streaming and finalize token stats
	tokenStats.StopStreaming()
	tokenStats.ResponseSizeBytes = len(content)
	tokenStats.Finish() // Update session totals

	reasoningActive := false
	reasoningExclude := false
	if c.config != nil && c.config.Reasoning != nil {
		reasoningActive = c.config.Reasoning.IsEffectivelyEnabled()
		reasoningExclude = c.config.Reasoning.Exclude
	}

	// Log reasoning information if present
	if reasoning != "" {
		reasoningLength := len(reasoning)
		logger.Infof("Received reasoning tokens: %d characters", reasoningLength)

		// Update reasoning statistics
		estimatedTokens := EstimateTokensFromChars(reasoning)
		tokenStats.SetReasoningTokens(estimatedTokens)

		// Log reasoning content in debug mode (be careful with sensitive data)
		// Truncate reasoning for logging to avoid overwhelming logs
		maxLogLength := 200
		if reasoningLength > maxLogLength {
			logger.Debugf("Reasoning content (truncated): %s...", reasoning[:maxLogLength])
		} else {
			logger.Debugf("Reasoning content: %s", reasoning)
		}

		// Check if reasoning should be excluded but was still received
		if reasoningActive && reasoningExclude {
			logger.Warnf("尽管设置了exclude=true，仍收到了推理令牌")
		}
	} else {
		// Log when reasoning was expected but not received
		if reasoningActive && !reasoningExclude {
			logger.Debugf("未收到推理令牌（某些模型或请求可能正常）")
		}
	}

	// Send final thinking event with reasoning content if available
	if reasoning != "" && reasoningActive {
		finalThinkingEvent := event.StreamEvent{
			Type:      event.EventTypeThinking,
			Content:   "思考完成",
			Reasoning: reasoning,
			Metadata: map[string]interface{}{
				"thinking_type": "final",
				"is_complete":   true,
			},
		}
		onEvent(finalThinkingEvent)
	}

	// Send complete content for message storage
	if len(content) > 0 || len(toolCalls) > 0 {
		// Convert toolCalls to pointer slice for StreamEvent
		toolCallPtrs := make([]*tools.ToolCall, len(toolCalls))
		for i := range toolCalls {
			toolCallPtrs[i] = &toolCalls[i]
		}

		// Send content event (caller passes sanitized onEvent)
		contentEvent := event.NewStreamEvent(event.EventTypeContent, "llm_client")
		contentEvent = contentEvent.WithContent(content)
		contentEvent.ToolCalls = toolCallPtrs
		contentEvent.Reasoning = reasoning
		for k, v := range metadata {
			contentEvent = contentEvent.WithMetadata(k, v)
		}
		onEvent(contentEvent)

		logger.Debugf("Sent content event with reasoning: %t, tool_calls: %d", reasoning != "", len(toolCalls))
	}

	// Emit final token statistics (already sanitized by caller)
	finalStatsEvent := event.NewStreamEvent(event.EventTypeTokenStats, "llm_client")
	finalStatsEvent.TokenStats = tokenStats.GetEvent()
	onEvent(finalStatsEvent)

	// Send completion event
	doneEvent := event.NewStreamEvent(event.EventTypeDone, "llm_client")
	doneEvent.Done = true
	for k, v := range metadata {
		doneEvent = doneEvent.WithMetadata(k, v)
	}
	onEvent(doneEvent)

	return nil
}

func (c *Client) shouldRecordCBFailure(err error) bool {
	if c.circuitBreaker == nil {
		return false
	}
	if !c.circuitBreaker.config.ExcludeNonFailback {
		return true
	}
	return ShouldRecordCBFailure(err)
}

func finishMetadata(truncated bool, finishReason string) map[string]interface{} {
	if !truncated && finishReason == "" {
		return nil
	}
	metadata := make(map[string]interface{})
	if truncated {
		metadata["truncated"] = true
	}
	if finishReason != "" {
		metadata["finish_reason"] = finishReason
	}
	return metadata
}

// CompressionConfig holds configuration for conversation compression
type CompressionConfig struct {
	MaxTokens             int
	CompressionThreshold  float64
	PreserveRecentTurns   int
	EnableAutoCompression bool
}

// StreamingConversation manages a streaming conversation with tool support and compression
// Inspired by google-gemini/gemini-cli's conversation management
type StreamingConversation struct {
	client                *Client
	messages              []Message
	onEvent               func(event.StreamEvent)
	compressionConfig     CompressionConfig
	lastCompressionInfo   map[string]interface{} //nolint:unused
	enableAutoCompression bool
	compressionThreshold  float64
	maxConversationTurns  int
	apiErrorHandler       *APIErrorHandler
}

// NewStreamingConversation creates a new streaming conversation
func NewStreamingConversation(client *Client, systemPrompt string, onEvent func(event.StreamEvent)) *StreamingConversation {
	messages := []Message{}
	if systemPrompt != "" {
		messages = append(messages, Message{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	// Get global config for context management
	globalConfig := config.Get()

	// Infer model profile from configured model name for dynamic context window sizing.
	// Pass the model name as-is (empty string allowed); InferModelProfile applies
	// its own conservative default when the model is unknown or empty.
	modelName := ""
	if globalConfig != nil {
		modelName = globalConfig.Model
	}
	profile := InferModelProfile(modelName)

	// Derive compression parameters from the inferred profile
	maxTokens := int(float64(profile.ContextWindow) * 0.95) // 5% headroom
	compressionThreshold := profile.ThresholdRatio
	preserveRecentTurns := 6
	enableAutoCompression := true

	if globalConfig != nil {
		// If the user explicitly specified a context window, recompute from it
		if globalConfig.ContextConfig.ModelContextWindow > 0 {
			overrideProfile := ComputeProfileFromContextWindow(globalConfig.ContextConfig.ModelContextWindow)
			maxTokens = int(float64(overrideProfile.ContextWindow) * 0.95)
			compressionThreshold = overrideProfile.ThresholdRatio
		}
		// User explicit max_tokens is the absolute highest priority
		if v := globalConfig.ContextConfig.MaxTokens; v > 0 {
			maxTokens = v
		}
		if r := globalConfig.ContextConfig.CompressionRatio; r > 0 && r < 1 {
			// Convert compression ratio to threshold (threshold = 1 - ratio)
			compressionThreshold = 1.0 - r
		}
		if globalConfig.ContextConfig.PreserveRecentTurns > 0 {
			preserveRecentTurns = globalConfig.ContextConfig.PreserveRecentTurns
		}
		enableAutoCompression = globalConfig.ContextConfig.EnableCompression
	}

	return &StreamingConversation{
		client:   client,
		messages: messages,
		onEvent:  onEvent,
		compressionConfig: CompressionConfig{
			EnableAutoCompression: enableAutoCompression,
			MaxTokens:             maxTokens,
			CompressionThreshold:  compressionThreshold,
			PreserveRecentTurns:   preserveRecentTurns,
		},
		enableAutoCompression: enableAutoCompression,
		compressionThreshold:  compressionThreshold,
		maxConversationTurns:  100, // Increase for longer conversations
		apiErrorHandler:       NewAPIErrorHandler(onEvent),
	}
}

// SendMessage sends a user message and handles the streaming response
func (sc *StreamingConversation) SendMessage(ctx context.Context, content string) error {
	// Add user message
	if content != "" {
		sc.messages = append(sc.messages, Message{
			Role:    "user",
			Content: content,
		})
	}

	// Process with tool calling support
	return sc.Execute(ctx)
}

// Execute runs the conversation with the current messages
func (sc *StreamingConversation) Execute(ctx context.Context) error {
	return sc.processWithToolCalling(ctx)
}

// processWithToolCalling handles the recursive tool calling process
func (sc *StreamingConversation) processWithToolCalling(ctx context.Context) error {
	var currentResponse strings.Builder
	var assistantMessageAdded bool
	var toolCalls []tools.ToolCall

	// Start streaming completion with retry mechanism
	maxRetries := 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := sc.client.StreamCompletion(ctx, sc.messages, func(streamEvent event.StreamEvent) {
			// Forward event to callback
			if sc.onEvent != nil {
				sc.onEvent(streamEvent)
			}

			switch streamEvent.Type {
			case event.EventTypeStreamContent:
				// Handle streaming content for real-time display
				currentResponse.WriteString(streamEvent.Content)

			case event.EventTypeContent:
				// Handle complete content for message storage
				// This overwrites any accumulated streaming content
				currentResponse.Reset()
				currentResponse.WriteString(streamEvent.Content)

				// Extract tool calls from the event
				if len(streamEvent.ToolCalls) > 0 {
					toolCalls = make([]tools.ToolCall, len(streamEvent.ToolCalls))
					for i, tc := range streamEvent.ToolCalls {
						toolCalls[i] = *tc
					}
				}
			}
		})

		if err == nil {
			// Success, exit retry loop
			lastErr = nil
			break
		}

		lastErr = err

		// Use API error handler to analyze and handle the error
		_, shouldRetry := sc.apiErrorHandler.HandleAPIError(ctx, err, 0, attempt)
		if !shouldRetry || attempt >= maxRetries {
			break
		}
	}

	if lastErr != nil {
		return lastErr
	}

	// Add assistant message with tool calls if present
	if !assistantMessageAdded {
		assistantMsg := Message{
			Role:      "assistant",
			Content:   currentResponse.String(),
			ToolCalls: toolCalls,
		}
		sc.messages = append(sc.messages, assistantMsg)
	}

	return nil
}

// GetMessages returns the conversation history
func (sc *StreamingConversation) GetMessages() []Message {
	return sc.messages
}

// SetMessages sets the conversation history
func (sc *StreamingConversation) SetMessages(messages []Message) {
	sc.messages = messages
}

// AddMessages appends messages to the conversation history
func (sc *StreamingConversation) AddMessages(messages []Message) {
	sc.messages = append(sc.messages, messages...)
}

// UpdateTools updates the tools available to the LLM client
func (c *Client) UpdateTools(tools []interfaces.Tool) {
	c.tools = tools
	logger.Debugf("Updated LLM client with %d tools", len(tools))
}

// SetToolGate updates the progressive disclosure gate used when exposing tool schemas.
func (c *Client) SetToolGate(gate interfaces.ToolGate) {
	c.toolGate = gate
}

// toolRequiresParameters checks if a tool requires mandatory parameters
func (c *Client) toolRequiresParameters(toolName string) bool {
	// List of tools that require mandatory parameters
	requiredParamTools := map[string]bool{
		"search_codebase": true,
		"view_files":      true,
		"write_file":      true,
		"update_file":     true,
		"run_command":     true,
	}

	return requiredParamTools[toolName]
}

// sanitizeEvent applies configured redaction rules to events at the LLM client boundary
func (c *Client) sanitizeEvent(ev event.StreamEvent) event.StreamEvent {
	// Lazy init validator once per client
	if c.validator == nil {
		v := event.NewEventValidator()
		if cfg := config.Get(); cfg != nil {
			var sr *config.SecretRedactionConfig
			if cfg.Daemon != nil && cfg.Daemon.SecretRedaction != nil {
				sr = cfg.Daemon.SecretRedaction
			} else {
				sr = cfg.SecretRedaction
			}
			if sr != nil && sr.Enabled {
				if !sr.IncludeDefaults {
					v.ClearRedactionPatterns()
					v.SetSensitiveKeys(nil)
				}
				if len(sr.SensitiveKeys) > 0 {
					v.MergeSensitiveKeys(sr.SensitiveKeys)
				}
				for _, p := range sr.Additional {
					if err := v.AddRedactionPatternString(p.Regex, p.Replacement); err != nil {
						logger.Warnf("Invalid redaction pattern '%s': %v", p.Name, err)
					}
				}
			}
		}
		c.validator = v
	}
	return c.validator.SanitizeEvent(ev)
}
