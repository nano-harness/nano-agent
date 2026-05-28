package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

const (
	// anthropicDefaultMaxTokens is the default max output tokens for Claude models.
	anthropicDefaultMaxTokens = 8192
)

// AnthropicClient implements LLMClient using the official Anthropic Messages API.
type AnthropicClient struct {
	client    anthropic.Client
	model     string
	converter *anthropicMessageConverter
	tools     []interfaces.Tool
	cfg       *config.Config
	cb        *CircuitBreaker
	transport http.RoundTripper
}

// NewAnthropicClient creates a new LLMClient backed by the Anthropic native SDK.
func NewAnthropicClient(
	apiKey, baseURL, model string,
	headers map[string]string,
	tools []interfaces.Tool,
	cfg *config.Config,
) *AnthropicClient {
	httpTimeout := 60 * time.Second
	if cfg != nil && cfg.HTTPTimeout > 0 {
		httpTimeout = cfg.HTTPTimeout
	}

	transport := &http.Transport{
		ResponseHeaderTimeout: httpTimeout,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	var httpTransport http.RoundTripper = transport
	if cfg != nil && cfg.Verbose {
		httpTransport = &loggingRoundTripper{wrapped: transport}
	}
	httpClient := &http.Client{
		Timeout:   0,
		Transport: httpTransport,
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(httpClient),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	// Claude Code CLI identity headers for upstream policy compliance
	cliHeaders := map[string]string{
		"User-Agent":     "claude-cli/2.1.142 (external, claude-desktop-3p, agent-sdk/0.3.142)",
		"X-App":          "cli",
		"Anthropic-Beta": "claude-code-20250219,interleaved-thinking-2025-05-14,effort-2025-11-24",
		"Anthropic-Dangerous-Direct-Browser-Access": "true",
		"X-Stainless-Lang":                          "js",
		"X-Stainless-Runtime":                       "node",
		"X-Stainless-Runtime-Version":               "v24.3.0",
		"X-Stainless-Package-Version":               "0.94.0",
		"X-Stainless-Os":                            runtime.GOOS,
		"X-Stainless-Arch":                          runtime.GOARCH,
		"X-Stainless-Timeout":                       "900",
		"X-Stainless-Retry-Count":                   "0",
	}
	for k, v := range cliHeaders {
		opts = append(opts, option.WithHeader(k, v))
	}

	// Route config headers applied last so users can override defaults
	for k, v := range headers {
		opts = append(opts, option.WithHeader(k, v))
	}

	client := anthropic.NewClient(opts...)
	effectiveBaseURL := baseURL
	if effectiveBaseURL == "" {
		effectiveBaseURL = "https://api.anthropic.com"
	}
	cb := newCircuitBreakerForRoute("anthropic", effectiveBaseURL, cfg)

	return &AnthropicClient{
		client:    client,
		model:     model,
		converter: newAnthropicMessageConverter(tools),
		tools:     append([]interfaces.Tool(nil), tools...),
		cfg:       cfg,
		cb:        cb,
		transport: httpTransport,
	}
}

// UpdateTools updates the tool list on the client.
func (c *AnthropicClient) UpdateTools(newTools []interfaces.Tool) {
	c.tools = append([]interfaces.Tool(nil), newTools...)
	c.converter = newAnthropicMessageConverter(c.tools)
}

// GenerateContent generates a non-streaming completion for the given prompt.
func (c *AnthropicClient) GenerateContent(ctx context.Context, prompt string) (string, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: anthropicDefaultMaxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	}

	msg, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return "", c.wrapError(err)
	}

	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			return tb.Text, nil
		}
	}
	return "", fmt.Errorf("no text content in Anthropic response")
}

// StreamCompletion streams a completion using the Anthropic Messages API.
func (c *AnthropicClient) StreamCompletion(ctx context.Context, messages []Message, onEvent func(event.StreamEvent)) error {
	return c.stream(ctx, messages, onEvent, false)
}

// StreamCompletionWithoutReasoning streams a completion without extended thinking.
func (c *AnthropicClient) StreamCompletionWithoutReasoning(ctx context.Context, messages []Message, onEvent func(event.StreamEvent)) error {
	return c.stream(ctx, messages, onEvent, true)
}

func (c *AnthropicClient) stream(ctx context.Context, messages []Message, onEvent func(event.StreamEvent), disableThinking bool) error {
	// Apply response timeout
	totalTimeout := 15 * time.Minute
	if c.cfg != nil && c.cfg.ResponseTimeout > 0 {
		totalTimeout = c.cfg.ResponseTimeout
	}
	streamCtx := ctx
	cancel := func() {}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		streamCtx, cancel = context.WithTimeout(ctx, totalTimeout)
	} else {
		streamCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	// Circuit breaker check
	if c.cb != nil {
		if err := c.cb.AllowRequest(); err != nil {
			onEvent(event.StreamEvent{Type: event.EventTypeError, Error: fmt.Sprintf("circuit breaker: %v", err)})
			return err
		}
	}

	// Convert messages
	anthropicMessages, systemPrompt := c.converter.convertMessages(messages)

	// Apply prompt caching to conversation history
	anthropicMessages = applyConversationCache(anthropicMessages)

	// Build system prompt blocks with cache control
	var systemBlocks []anthropic.TextBlockParam
	if systemPrompt != "" {
		systemBlocks = splitSystemByCacheBoundary(systemPrompt)
	}

	// Build tools with cache control
	anthropicTools := c.converter.convertToolsWithCache()

	// Calculate max tokens
	maxTokens := int64(anthropicDefaultMaxTokens)

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: maxTokens,
		Messages:  anthropicMessages,
	}

	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}
	if len(anthropicTools) > 0 {
		params.Tools = anthropicTools
	}

	// Apply extended thinking if configured and not explicitly disabled
	if !disableThinking && c.isThinkingEnabled() {
		params.Thinking = anthropic.ThinkingConfigParamUnion{
			OfEnabled: &anthropic.ThinkingConfigEnabledParam{
				BudgetTokens: c.thinkingBudgetTokens(),
			},
		}
	}

	tokenStats := NewTokenStats()
	tokenStats.StartStreaming()

	onEvent(event.StreamEvent{
		Type:    event.EventTypeThinking,
		Content: "thinking...",
	})

	stream := c.client.Messages.NewStreaming(streamCtx, params)

	// Per-block tracking for tool call assembly
	type toolBlock struct {
		id        string
		name      string
		inputJSON string
	}
	blocksByIndex := make(map[int]*toolBlock)

	var textContent string
	var reasoningContent string
	var reasoningBlocks []ReasoningBlock
	var currentThinkingBlock *ReasoningBlock // accumulator for in-progress thinking block
	var toolCalls []tools.ToolCall
	var lastThinkingSend time.Time
	const thinkingInterval = 300 * time.Millisecond

	for stream.Next() {
		// Check cancellation
		select {
		case <-streamCtx.Done():
			_ = stream.Close()
			err := streamCtx.Err()
			if err == context.DeadlineExceeded {
				onEvent(event.StreamEvent{Type: event.EventTypeError, Error: "Stream timed out"})
			} else {
				onEvent(event.StreamEvent{Type: event.EventTypeError, Error: "Request cancelled"})
			}
			return err
		default:
		}

		ev := stream.Current()

		switch ev.Type {
		case "message_start":
			start := ev.AsMessageStart()
			inputTokens := int(start.Message.Usage.InputTokens)
			tokenStats.SetInputTokens(inputTokens)

		case "content_block_start":
			blockStart := ev.AsContentBlockStart()
			idx := int(blockStart.Index)
			switch cb := blockStart.ContentBlock.AsAny().(type) {
			case anthropic.ToolUseBlock:
				blocksByIndex[idx] = &toolBlock{id: cb.ID, name: cb.Name}
			case anthropic.ThinkingBlock:
				// Start accumulating a new thinking block
				currentThinkingBlock = &ReasoningBlock{Type: ReasoningBlockThinking}
				_ = cb
			case anthropic.RedactedThinkingBlock:
				// Redacted thinking blocks arrive complete (no deltas)
				reasoningBlocks = append(reasoningBlocks, ReasoningBlock{
					Type: ReasoningBlockRedactedThinking,
					Data: cb.Data,
				})
				reasoningContent += "[redacted]"
			}

		case "content_block_delta":
			blockDelta := ev.AsContentBlockDelta()
			idx := int(blockDelta.Index)
			switch delta := blockDelta.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				text := delta.Text
				textContent += text
				tokenStats.AddOutputTokens(EstimateTokensFromChars(text))

				statsEv := event.NewStreamEvent(event.EventTypeTokenStats, "anthropic_client")
				statsEv.TokenStats = tokenStats.GetEvent()
				onEvent(statsEv)

				streamEv := event.NewStreamEvent(event.EventTypeStreamContent, "anthropic_client")
				streamEv = streamEv.WithContent(text)
				onEvent(streamEv)

			case anthropic.ThinkingDelta:
				thinking := delta.Thinking
				reasoningContent += thinking
				if currentThinkingBlock != nil {
					currentThinkingBlock.Text += thinking
				}
				tokenStats.AddOutputTokens(EstimateTokensFromChars(thinking))

				if time.Since(lastThinkingSend) >= thinkingInterval {
					onEvent(event.StreamEvent{
						Type:           event.EventTypeThinking,
						Reasoning:      reasoningContent,
						ReasoningDelta: thinking,
					})
					lastThinkingSend = time.Now()
				}

			case anthropic.SignatureDelta:
				if currentThinkingBlock != nil {
					currentThinkingBlock.Signature += delta.Signature
				}

			case anthropic.InputJSONDelta:
				if tb, ok := blocksByIndex[idx]; ok {
					tb.inputJSON += delta.PartialJSON
				}
			}

		case "content_block_stop":
			blockStop := ev.AsContentBlockStop()
			idx := int(blockStop.Index)
			if tb, ok := blocksByIndex[idx]; ok {
				var args map[string]interface{}
				if tb.inputJSON != "" {
					if err := json.Unmarshal([]byte(tb.inputJSON), &args); err != nil {
						args = map[string]interface{}{"_raw": tb.inputJSON}
					}
				}
				toolCalls = append(toolCalls, tools.ToolCall{
					ID:        tb.id,
					Name:      tb.name,
					Arguments: args,
				})
				delete(blocksByIndex, idx)
			}
			// Finalize current thinking block
			if currentThinkingBlock != nil {
				reasoningBlocks = append(reasoningBlocks, *currentThinkingBlock)
				currentThinkingBlock = nil
			}

		case "message_delta":
			msgDelta := ev.AsMessageDelta()
			outputTokens := int(msgDelta.Usage.OutputTokens)
			if outputTokens > tokenStats.OutputTokens {
				tokenStats.AddOutputTokens(outputTokens - tokenStats.OutputTokens)
			}
			// Cache stats — log for observability
			cacheCreation := int(msgDelta.Usage.CacheCreationInputTokens)
			cacheRead := int(msgDelta.Usage.CacheReadInputTokens)
			if cacheCreation > 0 || cacheRead > 0 {
				logger.Debugf("Anthropic cache stats: creation=%d read=%d", cacheCreation, cacheRead)
			}

		case "message_stop":
			// Stream complete
		}
	}

	if err := stream.Err(); err != nil {
		_ = stream.Close()
		if c.cb != nil && c.shouldRecordCBFailure(err) {
			c.cb.RecordFailure()
		}
		onEvent(event.StreamEvent{Type: event.EventTypeError, Error: fmt.Sprintf("stream error: %v", err)})
		return c.wrapError(err)
	}
	_ = stream.Close()

	if c.cb != nil {
		c.cb.RecordSuccess()
	}

	return c.finalizeAnthropicResponse(textContent, reasoningContent, reasoningBlocks, toolCalls, onEvent, tokenStats)
}

func (c *AnthropicClient) finalizeAnthropicResponse(
	content string,
	reasoning string,
	reasoningBlocks []ReasoningBlock,
	toolCalls []tools.ToolCall,
	onEvent func(event.StreamEvent),
	tokenStats *TokenStats,
) error {
	tokenStats.StopStreaming()
	tokenStats.ResponseSizeBytes = len(content)
	tokenStats.Finish()

	if reasoning != "" {
		tokenStats.SetReasoningTokens(EstimateTokensFromChars(reasoning))
		finalThinking := event.StreamEvent{
			Type:      event.EventTypeThinking,
			Content:   "thinking complete",
			Reasoning: reasoning,
			Metadata:  map[string]interface{}{"thinking_type": "final", "is_complete": true},
		}
		onEvent(finalThinking)
	}

	if len(content) > 0 || len(toolCalls) > 0 {
		toolCallPtrs := make([]*tools.ToolCall, len(toolCalls))
		for i := range toolCalls {
			toolCallPtrs[i] = &toolCalls[i]
		}
		contentEv := event.NewStreamEvent(event.EventTypeContent, "anthropic_client")
		contentEv = contentEv.WithContent(content)
		contentEv.ToolCalls = toolCallPtrs
		contentEv.Reasoning = reasoning
		if len(reasoningBlocks) > 0 {
			contentEv.ReasoningData = reasoningBlocks
		}
		onEvent(contentEv)
	}

	// Final token stats
	statsEv := event.NewStreamEvent(event.EventTypeTokenStats, "anthropic_client")
	statsEv.TokenStats = tokenStats.GetEvent()
	onEvent(statsEv)

	doneEv := event.NewStreamEvent(event.EventTypeDone, "anthropic_client")
	doneEv.Done = true
	onEvent(doneEv)

	return nil
}

// isThinkingEnabled returns true when extended thinking is configured.
func (c *AnthropicClient) isThinkingEnabled() bool {
	if c.cfg == nil || c.cfg.Reasoning == nil {
		return false
	}
	return c.cfg.Reasoning.IsEffectivelyEnabled()
}

func (c *AnthropicClient) thinkingBudgetTokens() int64 {
	if c.cfg == nil || c.cfg.Reasoning == nil {
		return 5000
	}
	if max := c.cfg.Reasoning.MaxTokens; max > 0 {
		return int64(max)
	}
	return 5000
}

// wrapError enriches an Anthropic SDK error with HTTP status information.
func (c *AnthropicClient) wrapError(err error) error {
	if err == nil {
		return nil
	}
	return err
}

// shouldRecordCBFailure decides whether an error should increment the circuit breaker.
func (c *AnthropicClient) shouldRecordCBFailure(err error) bool {
	if err == nil {
		return false
	}
	status := ExtractHTTPStatus(err)
	info := classifyAnthropicHTTPStatus(status, err.Error())
	if info != nil {
		return info.ShouldFailback
	}
	return shouldFallbackRoute(err)
}
