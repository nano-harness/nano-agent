package telemetry

import (
	"context"
	"errors"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

func attrMap(attrs []attribute.KeyValue) map[string]attribute.Value {
	m := make(map[string]attribute.Value, len(attrs))
	for _, kv := range attrs {
		m[string(kv.Key)] = kv.Value
	}
	return m
}

func TestLLMSpanAttributes(t *testing.T) {
	tests := []struct {
		name         string
		system       string
		model        string
		promptLength int
		wantKeys     []string
		absentKeys   []string
	}{
		{
			name:         "with prompt length",
			system:       "openai",
			model:        "gpt-4.1",
			promptLength: 128,
			wantKeys:     []string{AttrOperationName, AttrSystem, AttrRequestModel, AttrPromptLength},
		},
		{
			name:         "zero prompt length omitted",
			system:       "anthropic",
			model:        "claude-sonnet-4.6",
			promptLength: 0,
			wantKeys:     []string{AttrOperationName, AttrSystem, AttrRequestModel},
			absentKeys:   []string{AttrPromptLength},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := attrMap(LLMSpanAttributes(tt.system, tt.model, tt.promptLength))
			for _, key := range tt.wantKeys {
				if _, ok := attrs[key]; !ok {
					t.Errorf("missing attribute %s", key)
				}
			}
			for _, key := range tt.absentKeys {
				if _, ok := attrs[key]; ok {
					t.Errorf("unexpected attribute %s", key)
				}
			}
			if got := attrs[AttrOperationName].AsString(); got != OperationChat {
				t.Errorf("operation = %q, want %q", got, OperationChat)
			}
			if got := attrs[AttrSystem].AsString(); got != tt.system {
				t.Errorf("system = %q, want %q", got, tt.system)
			}
			if got := attrs[AttrRequestModel].AsString(); got != tt.model {
				t.Errorf("model = %q, want %q", got, tt.model)
			}
		})
	}
}

func TestToolSpanAttributes(t *testing.T) {
	attrs := attrMap(ToolSpanAttributes("read_file", "call-1"))
	if got := attrs[AttrOperationName].AsString(); got != OperationExecuteTool {
		t.Errorf("operation = %q, want %q", got, OperationExecuteTool)
	}
	if got := attrs[AttrToolName].AsString(); got != "read_file" {
		t.Errorf("tool name = %q", got)
	}
	if got := attrs[AttrToolCallID].AsString(); got != "call-1" {
		t.Errorf("tool call id = %q", got)
	}
}

func TestAgentSpanAttributes(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		turnID     string
		wantConvID bool
	}{
		{name: "with session", sessionID: "sess-1", turnID: "turn-1", wantConvID: true},
		{name: "empty session omitted", sessionID: "", turnID: "turn-2", wantConvID: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := attrMap(AgentSpanAttributes(tt.sessionID, tt.turnID))
			if got := attrs[AttrOperationName].AsString(); got != OperationInvokeAgent {
				t.Errorf("operation = %q, want %q", got, OperationInvokeAgent)
			}
			_, hasConv := attrs[AttrConversationID]
			if hasConv != tt.wantConvID {
				t.Errorf("conversation id present = %t, want %t", hasConv, tt.wantConvID)
			}
		})
	}
}

// setupInMemoryTracer installs an in-memory exporter as the global provider
// and returns it along with a cleanup function.
func setupInMemoryTracer(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	enabled.Store(true)
	t.Cleanup(func() {
		enabled.Store(false)
		otel.SetTracerProvider(prev)
	})
	return exporter
}

func TestLLMSpanLifecycle(t *testing.T) {
	exporter := setupInMemoryTracer(t)

	ctx, span := StartLLMSpan(context.Background(), "openai", "gpt-4.1", 64)
	SetLLMUsage(span, 100, 42, "stop", 200)
	EndWithError(span, nil)
	_ = ctx

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	s := spans[0]
	if s.Name != "chat gpt-4.1" {
		t.Errorf("span name = %q", s.Name)
	}
	attrs := attrMap(s.Attributes)
	if got := attrs[AttrUsageInputTokens].AsInt64(); got != 100 {
		t.Errorf("input tokens = %d", got)
	}
	if got := attrs[AttrUsageOutputTokens].AsInt64(); got != 42 {
		t.Errorf("output tokens = %d", got)
	}
	if got := attrs[AttrResponseFinish].AsStringSlice(); len(got) != 1 || got[0] != "stop" {
		t.Errorf("finish reasons = %v", got)
	}
	if s.Status.Code != codes.Unset {
		t.Errorf("status = %v, want unset for success", s.Status.Code)
	}
}

func TestToolSpanRecordsError(t *testing.T) {
	exporter := setupInMemoryTracer(t)

	_, span := StartToolSpan(context.Background(), "run_shell_command", "call-9")
	SetToolOutcome(span, false, errors.New("exit code 1"))
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Errorf("status = %v, want error", spans[0].Status.Code)
	}
	attrs := attrMap(spans[0].Attributes)
	if got := attrs[attrToolSuccess].AsBool(); got {
		t.Errorf("tool success = %t, want false", got)
	}
}

func TestContextSpans(t *testing.T) {
	exporter := setupInMemoryTracer(t)

	_, compressSpan := StartContextSpan(context.Background(), OperationContextCompression, "sess-1")
	SetCompressionOutcome(compressSpan, 20, 8, 10000, 3000)
	compressSpan.End()

	_, editSpan := StartContextSpan(context.Background(), OperationContextEditing, "sess-1")
	SetEditingOutcome(editSpan, 5, 9000, 5000)
	editSpan.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	compressAttrs := attrMap(spans[0].Attributes)
	if got := compressAttrs[AttrOperationName].AsString(); got != OperationContextCompression {
		t.Errorf("operation = %q", got)
	}
	if got := compressAttrs[attrContextTokensIn].AsInt64(); got != 10000 {
		t.Errorf("tokens before = %d", got)
	}
	if got := compressAttrs[attrContextMessagesOut].AsInt64(); got != 8 {
		t.Errorf("messages after = %d", got)
	}
	editAttrs := attrMap(spans[1].Attributes)
	if got := editAttrs[attrContextCleared].AsInt64(); got != 5 {
		t.Errorf("cleared = %d", got)
	}
}

func TestSpanParenting(t *testing.T) {
	exporter := setupInMemoryTracer(t)

	ctx, turnSpan := StartAgentSpan(context.Background(), "sess-1", "turn-1")
	_, llmSpan := StartLLMSpan(ctx, "openai", "gpt-4.1", 0)
	llmSpan.End()
	turnSpan.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	var parent, child tracetest.SpanStub
	for _, s := range spans {
		if s.Name == OperationInvokeAgent {
			parent = s
		} else {
			child = s
		}
	}
	if child.Parent.SpanID() != parent.SpanContext.SpanID() {
		t.Errorf("LLM span is not a child of the turn span")
	}
	if child.SpanContext.TraceID() != parent.SpanContext.TraceID() {
		t.Errorf("trace IDs differ between turn and LLM span")
	}
}

func TestDisabledModeIsNoop(t *testing.T) {
	// Ensure no global provider is installed.
	otel.SetTracerProvider(noop.NewTracerProvider())
	enabled.Store(false)

	ctx, span := StartLLMSpan(context.Background(), "openai", "gpt-4.1", 10)
	if span.IsRecording() {
		t.Errorf("span should not record when tracing is disabled")
	}
	// Must be safe to call all helpers on a non-recording span.
	SetLLMUsage(span, 1, 1, "stop", 1)
	SetToolOutcome(span, true, nil)
	EndWithError(span, errors.New("ignored"))
	_ = ctx

	if Enabled() {
		t.Errorf("Enabled() = true, want false")
	}
}

func TestInit(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.OTelConfig
		wantErr bool
		wantOn  bool
	}{
		{name: "nil config", cfg: nil, wantOn: false},
		{name: "disabled", cfg: &config.OTelConfig{Enabled: false}, wantOn: false},
		{
			name:    "unsupported protocol",
			cfg:     &config.OTelConfig{Enabled: true, Protocol: "carrier-pigeon"},
			wantErr: true,
			wantOn:  false,
		},
		{
			name:   "grpc exporter initializes without collector",
			cfg:    &config.OTelConfig{Enabled: true, Protocol: "grpc", Endpoint: "127.0.0.1:1", Insecure: true},
			wantOn: true,
		},
		{
			name:   "http exporter initializes without collector",
			cfg:    &config.OTelConfig{Enabled: true, Protocol: "http", Endpoint: "127.0.0.1:1", Insecure: true},
			wantOn: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prev := otel.GetTracerProvider()
			t.Cleanup(func() {
				enabled.Store(false)
				otel.SetTracerProvider(prev)
			})
			shutdown, err := Init(context.Background(), tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Init error = %v, wantErr %t", err, tt.wantErr)
			}
			if got := Enabled(); got != tt.wantOn {
				t.Errorf("Enabled() = %t, want %t", got, tt.wantOn)
			}
			if shutdown != nil {
				if err := shutdown(context.Background()); err != nil {
					t.Errorf("shutdown error: %v", err)
				}
				if Enabled() {
					t.Errorf("Enabled() still true after shutdown")
				}
			}
		})
	}
}
