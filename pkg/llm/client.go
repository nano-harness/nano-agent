package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/tools"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
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
	client         openai.Client
	model          string
	baseURL        string
	tools          []interfaces.Tool
	tokenCounter   *TokenCounter
	validator      *event.EventValidator
	config         *config.Config // Add config to access reasoning settings
	circuitBreaker *CircuitBreaker
}

const defaultOpenAIBaseURL = "https://api.openai.com/v1"

// loggingRoundTripper wraps an http.RoundTripper to log requests and responses
type loggingRoundTripper struct {
	wrapped http.RoundTripper
}

// Log truncation limits for HTTP debug output.
const (
	requestBodyDisplayLimit  = 2000                         // max chars shown from request body
	requestBodyReadLimit     = requestBodyDisplayLimit + 1  // +1 detects truncation
	responseBodyDisplayLimit = 1000                         // max chars shown from error response body
	responseBodyReadLimit    = responseBodyDisplayLimit + 1 // +1 detects truncation
)

// RoundTrip implements http.RoundTripper interface with logging
func (l *loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Log request
	logger.Debugf("[HTTP] → %s %s", req.Method, req.URL.String())

	// Log headers (with Authorization redaction)
	for key, values := range req.Header {
		for _, value := range values {
			if strings.EqualFold(key, "Authorization") {
				// Redact authorization header: log only the auth scheme, never any token characters
				fields := strings.Fields(value)
				if len(fields) > 0 {
					logger.Debugf("[HTTP]   %s: %s [REDACTED]", key, fields[0])
				} else {
					logger.Debugf("[HTTP]   %s: [REDACTED]", key)
				}
			} else {
				logger.Debugf("[HTTP]   %s: %s", key, value)
			}
		}
	}

	// Log request body without consuming the live request stream
	if req.Body != nil {
		if req.GetBody != nil {
			bodyReader, err := req.GetBody()
			if err != nil {
				logger.Debugf("[HTTP] Request body: unable to clone body for logging: %v", err)
			} else {
				defer func() { _ = bodyReader.Close() }()
				bodyBytes, err := io.ReadAll(io.LimitReader(bodyReader, requestBodyReadLimit))
				if err != nil {
					logger.Debugf("[HTTP] Request body: unable to read cloned body for logging: %v", err)
				} else {
					bodyStr := string(bodyBytes)
					if len(bodyStr) >= requestBodyReadLimit {
						bodyStr = bodyStr[:requestBodyDisplayLimit] + "..."
					}
					logger.Debugf("[HTTP] Request body (%d bytes): %s", len(bodyBytes), bodyStr)
				}
			}
		} else {
			logger.Debugf("[HTTP] Request body present but cannot be logged safely: GetBody is nil")
		}
	}

	// Execute the actual request
	resp, err := l.wrapped.RoundTrip(req)
	if err != nil {
		logger.Debugf("[HTTP] ← Error: %v", err)
		return resp, err
	}

	// Log response
	logger.Debugf("[HTTP] ← %s", resp.Status)

	// Log error response bodies (4xx/5xx)
	if resp.StatusCode >= 400 {
		if resp.Body != nil {
			bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, responseBodyReadLimit))
			// Always restore the body: re-combine what we read with remaining stream
			resp.Body = io.NopCloser(io.MultiReader(bytes.NewBuffer(bodyBytes), resp.Body))
			if readErr != nil {
				logger.Debugf("[HTTP] Error response body: failed to read: %v", readErr)
			} else {
				bodyStr := string(bodyBytes)
				if len(bodyStr) >= responseBodyReadLimit {
					bodyStr = bodyStr[:responseBodyDisplayLimit] + "..."
				}
				logger.Debugf("[HTTP] Error response body: %s", bodyStr)
			}
		}
	}

	return resp, nil
}

// NewClient creates a new optimized LLM client using official OpenAI SDK
func NewClient(apiKey, baseURL, model string, tools []interfaces.Tool) *Client {
	// Get timeout from config
	cfg := config.Get()
	httpTimeout := 60 * time.Second // default fallback
	if cfg != nil {
		httpTimeout = cfg.HTTPTimeout
	}

	// Configure HTTP client for streaming-friendly behavior:
	// - Do NOT set http.Client.Timeout (set to 0) because it limits total body read time and breaks long-lived streams
	// - Use Transport.ResponseHeaderTimeout to bound the time to first byte/headers
	transport := &http.Transport{
		ResponseHeaderTimeout: httpTimeout,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	// Wrap transport with HTTP logger when verbose mode is enabled
	var httpTransport http.RoundTripper = transport
	if cfg != nil && cfg.Verbose {
		httpTransport = &loggingRoundTripper{wrapped: transport}
	}

	// Configure client options
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(&http.Client{
			Timeout:   0, // rely on context deadlines for total request lifetime
			Transport: httpTransport,
		}),
	}

	// Add custom base URL if provided (for DeepSeek compatibility)
	if baseURL != "" && baseURL != defaultOpenAIBaseURL {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	tokenCounter, _ := NewTokenCounter(model)

	// Configure circuit breaker with config overrides if available
	cbCfg := DefaultCircuitBreakerConfig()
	if cfg != nil && cfg.Advanced != nil && cfg.Advanced.CircuitBreaker != nil {
		cbAdv := cfg.Advanced.CircuitBreaker
		if cbAdv.MaxRetries > 0 {
			cbCfg.MaxRetries = cbAdv.MaxRetries
		}
		if cbAdv.BaseDelayMs > 0 {
			cbCfg.BaseDelay = time.Duration(cbAdv.BaseDelayMs) * time.Millisecond
		}
		if cbAdv.MaxDelayMs > 0 {
			cbCfg.MaxDelay = time.Duration(cbAdv.MaxDelayMs) * time.Millisecond
		}
		if cbAdv.OpenTimeoutMs > 0 {
			cbCfg.OpenTimeout = time.Duration(cbAdv.OpenTimeoutMs) * time.Millisecond
		}
	}

	client := &Client{
		client:         openai.NewClient(opts...),
		model:          model,
		baseURL:        baseURL,
		tools:          tools,
		tokenCounter:   tokenCounter,
		config:         cfg, // Store config for reasoning support
		circuitBreaker: NewCircuitBreaker(cbCfg),
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

	// Show thinking indicator with reasoning info if enabled
	thinkingContent := "正在思考..."
	if c.config != nil && c.config.Reasoning != nil && c.config.Reasoning.IsEffectivelyEnabled() {
		if c.config.Reasoning.MaxTokens > 0 {
			thinkingContent = fmt.Sprintf("正在思考（推理令牌限制: %d）...", c.config.Reasoning.MaxTokens)
		} else if c.config.Reasoning.Effort != "" {
			thinkingContent = fmt.Sprintf("正在思考（推理强度: %s）...", c.config.Reasoning.Effort)
		} else {
			thinkingContent = "正在思考（推理模式）..."
		}
	}

	sanitizedOnEvent(event.StreamEvent{
		Type:    event.EventTypeThinking,
		Content: thinkingContent,
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

	// Add reasoning parameters if enabled with graceful degradation
	reasoningEnabled := false
	if c.config != nil && c.config.Reasoning != nil && c.config.Reasoning.IsEffectivelyEnabled() {
		logger.Debugf("Reasoning tokens enabled for model %s", c.model)

		// Set effort level or max tokens
		if c.config.Reasoning.MaxTokens > 0 {
			extraFields["reasoning"] = map[string]interface{}{
				"max_tokens": c.config.Reasoning.MaxTokens,
			}
			logger.Debugf("Using reasoning max_tokens: %d", c.config.Reasoning.MaxTokens)
			reasoningEnabled = true
		} else if c.config.Reasoning.Effort != "" {
			extraFields["reasoning"] = map[string]interface{}{
				"effort": c.config.Reasoning.Effort,
			}
			logger.Debugf("Using reasoning effort level: %s", c.config.Reasoning.Effort)
			reasoningEnabled = true
		} else {
			logger.Warnf("推理功能已启用但未指定effort级别或max_tokens，将使用默认配置")
		}

		// Set exclude parameter
		if c.config.Reasoning.Exclude {
			extraFields["reasoning_exclude"] = true
			logger.Debugf("Reasoning tokens will be excluded from response")
		}

		// Set reasoning statistics
		if reasoningEnabled {
			tokenStats.SetReasoningEnabled(true, c.config.Reasoning.Effort)
		}
	} else {
		logger.Debugf("Reasoning tokens disabled or not configured")
	}

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

		var responseContent strings.Builder
		var reasoningContent strings.Builder // Accumulate reasoning content
		var toolCalls []tools.ToolCall
		var lastThinkingSendTime time.Time
		const thinkingSendInterval = 300 * time.Millisecond
		var lastSentReasoningLen int

		// A map to store partial tool call data, keyed by tool ID.
		// This allows handling multiple tool calls streamed in parallel.
		type toolCallBuilder struct {
			ID          string
			Name        string
			Arguments   strings.Builder
			NameCounted bool
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
						reasoningContent.WriteString(reasoningStr)
						// Send streaming thinking event with throttling to enable real-time display.
						// Content is intentionally left empty to avoid overwriting any more
						// informative title set by the initial thinking event.
						if time.Since(lastThinkingSendTime) >= thinkingSendInterval {
							fullContent := reasoningContent.String()
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
						// Capture name when available and count once
						if toolCallChunk.Function.Name != "" && builder.Name == "" {
							builder.Name = toolCallChunk.Function.Name
							if !builder.NameCounted {
								nameTokens := 0
								if c.tokenCounter != nil {
									nameTokens = c.tokenCounter.CountTokens(builder.Name)
								} else {
									nameTokens = EstimateTokensFromChars(builder.Name)
								}
								tokenStats.AddOutputTokens(nameTokens)
								builder.NameCounted = true
								statsEvent := event.NewStreamEvent(event.EventTypeTokenStats, "llm_client")
								statsEvent.TokenStats = tokenStats.GetEvent()
								sanitizedOnEvent(statsEvent)
							}
						}
						// Append argument chunks and count incrementally
						if toolCallChunk.Function.Arguments != "" {
							builder.Arguments.WriteString(toolCallChunk.Function.Arguments)
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
			}

			// Check if stream is finished
			if choice.FinishReason != "" {
				// Finalize all tool calls
				for _, idx := range toolCallOrder {
					if builder, ok := partialToolCalls[idx]; ok {
						// Validate tool call has required fields
						if builder.Name == "" {
							logger.Warnf("第%d个工具调用缺少名称，已跳过", idx)
							continue
						}

						toolCall := tools.ToolCall{
							ID:   builder.ID,
							Name: builder.Name,
						}

						// Parse accumulated arguments
						var args map[string]interface{}
						argumentsStr := builder.Arguments.String()
						logger.Debugf("Raw arguments for tool %s: %s", toolCall.Name, argumentsStr)

						if strings.TrimSpace(argumentsStr) != "" {
							if err := json.Unmarshal([]byte(argumentsStr), &args); err != nil {
								logger.Warnf("Failed to parse tool call arguments for %s (raw: %s): %v", toolCall.Name, argumentsStr, err)
								args = make(map[string]interface{})
							}
						} else {
							// Check if tool requires parameters but none provided
							if c.toolRequiresParameters(toolCall.Name) {
								logger.Warnf("Tool %s requires parameters but none provided; proceeding with empty arguments", toolCall.Name)
							}
							// Initialize empty args map when no arguments provided
							args = make(map[string]interface{})
						}

						toolCall.Arguments = args
						toolCalls = append(toolCalls, toolCall)
						logger.Debugf("Successfully parsed tool call: %s with args: %v", toolCall.Name, toolCall.Arguments)
					}
				}
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
			if attempt < maxRetries && responseContent.Len() == 0 && IsRetryableError(err) {
				lastErr = err
				if c.circuitBreaker != nil {
					c.circuitBreaker.RecordFailure()
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
			if c.circuitBreaker != nil {
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
		return c.finalizeResponse(responseContent.String(), reasoningContent.String(), toolCalls, sanitizedOnEvent, tokenStats)
	}

	// All retries exhausted (should not normally reach here)
	totalAttempts := maxRetries + 1
	if lastErr != nil {
		return fmt.Errorf("LLM API request failed after %d total attempts: %w", totalAttempts, lastErr)
	}
	return fmt.Errorf("LLM API request failed after %d total attempts", totalAttempts)
}

func shouldFallbackWithoutReasoning(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized") || strings.Contains(msg, "invalid api key") {
		return false
	}
	if strings.Contains(msg, "403") || strings.Contains(msg, "forbidden") {
		return false
	}
	if strings.Contains(msg, "429") && (strings.Contains(msg, "insufficient_quota") ||
		strings.Contains(msg, "exceeded your current quota") ||
		strings.Contains(msg, "billing") ||
		strings.Contains(msg, "token-limit")) {
		return false
	}
	return true
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
					c.circuitBreaker.RecordFailure()
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
			if c.circuitBreaker != nil {
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
		return c.finalizeResponse(responseContent.String(), "", toolCalls, sanitizedOnEvent, tokenStats)
	}

	// All retries exhausted
	totalAttempts := maxRetries + 1
	if lastErr != nil {
		return fmt.Errorf("fallback LLM API request failed after %d total attempts: %w", totalAttempts, lastErr)
	}
	return fmt.Errorf("fallback LLM API request failed after %d total attempts", totalAttempts)
}

// needsReasoningContentInMessages checks if the current API provider requires
// reasoning_content field in assistant messages when reasoning/thinking is enabled.
func (c *Client) needsReasoningContentInMessages() bool {
	// Only needed when reasoning is actually enabled
	if c.config == nil || c.config.Reasoning == nil || !c.config.Reasoning.IsEffectivelyEnabled() {
		return false
	}
	effectiveBaseURL := strings.TrimSpace(c.baseURL)
	if effectiveBaseURL == "" {
		effectiveBaseURL = defaultOpenAIBaseURL
	}
	// Known providers that require reasoning_content in messages
	lowerURL := strings.ToLower(effectiveBaseURL)
	knownProviders := []string{"deepseek", "moonshot"}
	for _, provider := range knownProviders {
		if strings.Contains(lowerURL, provider) {
			return true
		}
	}
	return false
}

func (c *Client) maybeSetReasoningMessagesOverride(extraFields map[string]interface{}, messages []Message) {
	if extraFields == nil {
		return
	}
	if !c.needsReasoningContentInMessages() {
		return
	}

	openaiMessages := c.convertMessages(messages)
	rawMessages := make([]map[string]interface{}, 0, len(openaiMessages))

	for idx, msg := range openaiMessages {
		msgJSON, err := json.Marshal(msg)
		if err != nil {
			logger.Warnf("Failed to marshal message for reasoning override: %v", err)
			continue
		}

		var msgMap map[string]interface{}
		if err := json.Unmarshal(msgJSON, &msgMap); err != nil {
			logger.Warnf("Failed to unmarshal message for reasoning override: %v", err)
			continue
		}

		if role, ok := msgMap["role"].(string); ok && role == "assistant" {
			reasoning := ""
			if idx < len(messages) && messages[idx].Role == "assistant" {
				reasoning = messages[idx].Reasoning
			}
			msgMap["reasoning_content"] = reasoning
		}

		rawMessages = append(rawMessages, msgMap)
	}

	if len(rawMessages) > 0 {
		extraFields["messages"] = rawMessages
	}
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
func (c *Client) finalizeResponse(content string, reasoning string, toolCalls []tools.ToolCall, onEvent func(event.StreamEvent), tokenStats *TokenStats) error {
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
	onEvent(doneEvent)

	return nil
}

// validateMessageSequence checks if messages follow OpenAI API requirements
func (c *Client) validateMessageSequence(messages []Message) error {
	for i, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Assistant message with tool calls must be followed by tool messages
			hasFollowingToolMessage := false

			// Look for tool messages after this assistant message
			for j := i + 1; j < len(messages); j++ {
				nextMsg := messages[j]
				if nextMsg.Role == "tool" {
					hasFollowingToolMessage = true
					break
				}
				// If we encounter another assistant message before finding a tool message,
				// the sequence is invalid
				if nextMsg.Role == "assistant" {
					break
				}
			}

			if !hasFollowingToolMessage {
				return fmt.Errorf("assistant message at index %d has tool_calls but no following tool messages", i)
			}
		}
	}
	return nil
}

// cleanupMessages removes incomplete tool call sequences from messages
func (c *Client) cleanupMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
	}

	var cleanedMessages []Message

	for i, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Check if this assistant message with tool calls has following tool messages
			hasFollowingToolMessage := false

			for j := i + 1; j < len(messages); j++ {
				nextMsg := messages[j]
				if nextMsg.Role == "tool" {
					hasFollowingToolMessage = true
					break
				}
				if nextMsg.Role == "assistant" {
					break
				}
			}

			if hasFollowingToolMessage {
				// This is a valid sequence, keep the message
				cleanedMessages = append(cleanedMessages, msg)
			} else {
				// This is an incomplete tool call sequence, remove the tool calls
				cleanedMsg := msg
				cleanedMsg.ToolCalls = nil
				cleanedMessages = append(cleanedMessages, cleanedMsg)
				logger.Warn("Removed incomplete tool calls from assistant message at index %d during message conversion", i)
			}
		} else {
			// Regular message, keep it
			cleanedMessages = append(cleanedMessages, msg)
		}
	}

	return cleanedMessages
}

// convertMessages converts our Message format to OpenAI format
func (c *Client) convertMessages(messages []Message) []openai.ChatCompletionMessageParamUnion {
	// Validate message sequence before conversion to prevent OpenAI API errors
	if err := c.validateMessageSequence(messages); err != nil {
		logger.Warn("Invalid message sequence detected in convertMessages: %v", err)
		// Clean up the messages to ensure valid sequence
		messages = c.cleanupMessages(messages)
	}

	openaiMessages := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			if len(msg.Contents) > 0 {
				// Handle multimodal system messages - convert to user message for multimodal content
				contentParts := []openai.ChatCompletionContentPartUnionParam{}
				for _, content := range msg.Contents {
					if content.Type == "text" {
						contentParts = append(contentParts, openai.TextContentPart(content.Text))
					} else if content.Type == "image_url" && content.ImageURL != nil {
						imageURLParam := openai.ChatCompletionContentPartImageImageURLParam{
							URL:    content.ImageURL.URL,
							Detail: content.ImageURL.Detail, // Direct string assignment
						}
						contentParts = append(contentParts, openai.ImageContentPart(imageURLParam))
					}
				}
				// System messages with multimodal content need special handling
				// Convert to user message with system prefix for multimodal support
				systemPrefix := "System: "
				// Always prepend system prefix as first text part
				contentParts = append([]openai.ChatCompletionContentPartUnionParam{
					openai.TextContentPart(systemPrefix),
				}, contentParts...)
				userMsg := openai.UserMessage(contentParts)
				openaiMessages = append(openaiMessages, userMsg)
			} else {
				openaiMessages = append(openaiMessages, openai.SystemMessage(msg.Content))
			}

		case "user":
			if len(msg.Contents) > 0 {
				// Handle multimodal user messages
				contentParts := []openai.ChatCompletionContentPartUnionParam{}
				for _, content := range msg.Contents {
					if content.Type == "text" {
						contentParts = append(contentParts, openai.TextContentPart(content.Text))
					} else if content.Type == "image_url" && content.ImageURL != nil {
						imageURLParam := openai.ChatCompletionContentPartImageImageURLParam{
							URL:    content.ImageURL.URL,
							Detail: content.ImageURL.Detail, // Direct string assignment
						}
						contentParts = append(contentParts, openai.ImageContentPart(imageURLParam))
					}
				}
				userMsg := openai.UserMessage(contentParts)
				openaiMessages = append(openaiMessages, userMsg)
			} else {
				openaiMessages = append(openaiMessages, openai.UserMessage(msg.Content))
			}

		case "assistant":
			if len(msg.ToolCalls) > 0 {
				// Assistant message with tool calls - use proper API structure
				toolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, len(msg.ToolCalls))
				for i, tc := range msg.ToolCalls {
					argsJSON, _ := json.Marshal(tc.Arguments)
					toolCalls[i] = openai.ChatCompletionMessageToolCallUnionParam{
						OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
							ID: tc.ID,
							Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
								Name:      tc.Name,
								Arguments: string(argsJSON),
							},
							// Type field will default to "function"
						},
					}
				}

				// Create assistant message with tool calls
				assistantMsg := openai.ChatCompletionAssistantMessageParam{
					Content: openai.ChatCompletionAssistantMessageParamContentUnion{
						OfString: openai.String(msg.Content),
					},
					ToolCalls: toolCalls,
					// Role field will default to "assistant"
				}

				// Convert to union type
				assistantUnion := openai.ChatCompletionMessageParamUnion{
					OfAssistant: &assistantMsg,
				}
				openaiMessages = append(openaiMessages, assistantUnion)
			} else {
				// Regular assistant message
				openaiMessages = append(openaiMessages, openai.AssistantMessage(msg.Content))
			}

		case "tool":
			// Tool result message
			openaiMessages = append(openaiMessages, openai.ToolMessage(msg.Content, msg.ToolCallID))
		}
	}

	return openaiMessages
}

// convertTools converts our tools to OpenAI format
func (c *Client) convertTools() []openai.ChatCompletionToolUnionParam {
	if len(c.tools) == 0 {
		return nil
	}

	tools := make([]openai.ChatCompletionToolUnionParam, 0, len(c.tools))
	var convertProperty func(prop *interfaces.PropertySchema) map[string]interface{}
	convertProperty = func(prop *interfaces.PropertySchema) map[string]interface{} {
		if prop == nil {
			return map[string]interface{}{"type": "string"}
		}
		propType := prop.Type
		if propType == "" {
			propType = "string"
		}
		propDef := map[string]interface{}{
			"type": propType,
		}
		if prop.Description != "" {
			propDef["description"] = prop.Description
		}

		if prop.Enum != nil {
			enumValues := make([]string, 0, len(prop.Enum))
			for _, value := range prop.Enum {
				if value != "" {
					enumValues = append(enumValues, value)
				}
			}
			if len(enumValues) > 0 {
				propDef["enum"] = enumValues
			}
		}
		if prop.Default != nil {
			propDef["default"] = prop.Default
		}
		if prop.Pattern != "" {
			propDef["pattern"] = prop.Pattern
		}
		if prop.MinLength != nil {
			propDef["minLength"] = *prop.MinLength
		}
		if prop.MaxLength != nil {
			propDef["maxLength"] = *prop.MaxLength
		}
		if prop.Minimum != nil {
			propDef["minimum"] = *prop.Minimum
		}
		if prop.Maximum != nil {
			propDef["maximum"] = *prop.Maximum
		}
		if prop.Examples != nil {
			propDef["examples"] = prop.Examples
		}

		if strings.EqualFold(propType, "array") {
			if prop.Items != nil {
				propDef["items"] = convertProperty(prop.Items)
			} else {
				propDef["items"] = map[string]interface{}{"type": "string"}
			}
		}

		return propDef
	}

	for _, tool := range c.tools {
		schema := tool.Schema()
		if schema == nil {
			continue
		}

		// Convert tool schema to OpenAI function parameters
		parameters := openai.FunctionParameters{
			"type":       "object",
			"properties": make(map[string]interface{}),
		}

		if len(schema.Required) > 0 {
			parameters["required"] = schema.Required
		}

		// Convert properties
		if schema.Properties != nil {
			for name, prop := range schema.Properties {
				parameters["properties"].(map[string]interface{})[name] = convertProperty(prop)
			}
		}

		tools = append(tools, openai.ChatCompletionFunctionTool(
			shared.FunctionDefinitionParam{
				Name:        tool.Name(),
				Description: openai.String(tool.Description()),
				Parameters:  parameters,
			},
		))
	}

	return tools
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

// toolRequiresParameters checks if a tool requires mandatory parameters
func (c *Client) toolRequiresParameters(toolName string) bool {
	// List of tools that require mandatory parameters
	requiredParamTools := map[string]bool{
		"glob":            true,
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
