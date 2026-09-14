package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Attribute keys following the OTel GenAI semantic conventions.
// See https://opentelemetry.io/docs/specs/semconv/gen-ai/
const (
	AttrOperationName     = "gen_ai.operation.name"
	AttrConversationID    = "gen_ai.conversation.id"
	AttrSystem            = "gen_ai.system"
	AttrRequestModel      = "gen_ai.request.model"
	AttrUsageInputTokens  = "gen_ai.usage.input_tokens"
	AttrUsageOutputTokens = "gen_ai.usage.output_tokens"
	AttrResponseFinish    = "gen_ai.response.finish_reasons"
	AttrToolName          = "gen_ai.tool.name"
	AttrToolCallID        = "gen_ai.tool.call.id"
	AttrPromptLength      = "gen_ai.request.prompt_length"
	AttrCompletionLength  = "gen_ai.response.completion_length"
)

// Operation names for the four observability pillars.
const (
	OperationInvokeAgent        = "invoke_agent"
	OperationChat               = "chat"
	OperationExecuteTool        = "execute_tool"
	OperationContextCompression = "context_compression"
	OperationContextEditing     = "context_editing"
)

// nano-agent specific extension attributes (not part of upstream semconv).
const (
	attrTurnID             = "nano.turn.id"
	attrContextMessagesIn  = "nano.context.messages_before"
	attrContextMessagesOut = "nano.context.messages_after"
	attrContextTokensIn    = "nano.context.tokens_before"
	attrContextTokensOut   = "nano.context.tokens_after"
	attrContextCleared     = "nano.context.cleared_results"
	attrToolSuccess        = "nano.tool.success"
)

// LLMSpanAttributes builds the attribute set for an LLM (chat) span. Prompt
// content is deliberately excluded; only its length may be recorded.
func LLMSpanAttributes(system, model string, promptLength int) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(AttrOperationName, OperationChat),
		attribute.String(AttrSystem, system),
		attribute.String(AttrRequestModel, model),
	}
	if promptLength > 0 {
		attrs = append(attrs, attribute.Int(AttrPromptLength, promptLength))
	}
	return attrs
}

// ToolSpanAttributes builds the attribute set for a tool execution span.
func ToolSpanAttributes(toolName, callID string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(AttrOperationName, OperationExecuteTool),
		attribute.String(AttrToolName, toolName),
		attribute.String(AttrToolCallID, callID),
	}
}

// AgentSpanAttributes builds the attribute set for an orchestration (turn) span.
func AgentSpanAttributes(sessionID, turnID string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(AttrOperationName, OperationInvokeAgent),
		attribute.String(attrTurnID, turnID),
	}
	if sessionID != "" {
		attrs = append(attrs, attribute.String(AttrConversationID, sessionID))
	}
	return attrs
}

// StartAgentSpan opens an orchestration span for one agent turn.
func StartAgentSpan(ctx context.Context, sessionID, turnID string) (context.Context, trace.Span) {
	return tracer().Start(ctx, OperationInvokeAgent,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(AgentSpanAttributes(sessionID, turnID)...),
	)
}

// StartLLMSpan opens a span for a single model call. promptLength is the
// character length of the request payload; the content itself is never
// recorded.
func StartLLMSpan(ctx context.Context, system, model string, promptLength int) (context.Context, trace.Span) {
	return tracer().Start(ctx, OperationChat+" "+model,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(LLMSpanAttributes(system, model, promptLength)...),
	)
}

// StartToolSpan opens a span for a single tool execution.
func StartToolSpan(ctx context.Context, toolName, callID string) (context.Context, trace.Span) {
	return tracer().Start(ctx, OperationExecuteTool+" "+toolName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(ToolSpanAttributes(toolName, callID)...),
	)
}

// StartContextSpan opens a span for a memory/context operation such as
// compression or context editing.
func StartContextSpan(ctx context.Context, operation, sessionID string) (context.Context, trace.Span) {
	attrs := []attribute.KeyValue{attribute.String(AttrOperationName, operation)}
	if sessionID != "" {
		attrs = append(attrs, attribute.String(AttrConversationID, sessionID))
	}
	return tracer().Start(ctx, operation,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
}

// SetLLMUsage records token usage and finish reason on an LLM span.
func SetLLMUsage(span trace.Span, inputTokens, outputTokens int, finishReason string, completionLength int) {
	if span == nil || !span.IsRecording() {
		return
	}
	attrs := make([]attribute.KeyValue, 0, 4)
	if inputTokens > 0 {
		attrs = append(attrs, attribute.Int(AttrUsageInputTokens, inputTokens))
	}
	if outputTokens > 0 {
		attrs = append(attrs, attribute.Int(AttrUsageOutputTokens, outputTokens))
	}
	if finishReason != "" {
		attrs = append(attrs, attribute.StringSlice(AttrResponseFinish, []string{finishReason}))
	}
	if completionLength > 0 {
		attrs = append(attrs, attribute.Int(AttrCompletionLength, completionLength))
	}
	span.SetAttributes(attrs...)
}

// SetToolOutcome records the execution outcome on a tool span.
func SetToolOutcome(span trace.Span, success bool, err error) {
	if span == nil || !span.IsRecording() {
		return
	}
	span.SetAttributes(attribute.Bool(attrToolSuccess, success))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
}

// SetCompressionOutcome records compression statistics on a context span.
func SetCompressionOutcome(span trace.Span, messagesBefore, messagesAfter, tokensBefore, tokensAfter int) {
	if span == nil || !span.IsRecording() {
		return
	}
	span.SetAttributes(
		attribute.Int(attrContextMessagesIn, messagesBefore),
		attribute.Int(attrContextMessagesOut, messagesAfter),
		attribute.Int(attrContextTokensIn, tokensBefore),
		attribute.Int(attrContextTokensOut, tokensAfter),
	)
}

// SetEditingOutcome records context editing statistics on a context span.
func SetEditingOutcome(span trace.Span, clearedResults, tokensBefore, tokensAfter int) {
	if span == nil || !span.IsRecording() {
		return
	}
	span.SetAttributes(
		attribute.Int(attrContextCleared, clearedResults),
		attribute.Int(attrContextTokensIn, tokensBefore),
		attribute.Int(attrContextTokensOut, tokensAfter),
	)
}

// EndWithError ends a span, recording err (if non-nil) as the span error.
func EndWithError(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil && span.IsRecording() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
