// Package telemetry provides OpenTelemetry tracing for nano-agent following
// the OTel GenAI semantic conventions (gen_ai.* attributes). Tracing is
// disabled by default; when disabled every entry point degrades to the global
// no-op tracer provider, so call sites carry no measurable overhead and no
// behavior change.
//
// Privacy: prompt and completion content is never attached to spans. Only
// metadata (model name, token usage, tool names, message counts) is recorded.
package telemetry

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/nano-harness/nano-agent/pkg/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// tracerName identifies nano-agent spans in the exported traces.
const tracerName = "github.com/nano-harness/nano-agent"

// DefaultServiceName is the default service.name resource attribute.
const DefaultServiceName = "nano-agent"

var enabled atomic.Bool

// Enabled reports whether trace export has been activated via Init.
func Enabled() bool { return enabled.Load() }

// ShutdownFunc flushes and stops the trace pipeline.
type ShutdownFunc func(ctx context.Context) error

// Init initializes the global tracer provider from the given configuration.
// When cfg is nil or tracing is disabled, Init is a no-op and returns a nil
// shutdown function. Initialization failures leave tracing disabled and
// return the error so callers can log it without aborting startup.
func Init(ctx context.Context, cfg *config.OTelConfig) (ShutdownFunc, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	exporter, err := newExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("otel exporter: %w", err)
	}

	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		serviceName = DefaultServiceName
	}
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	sampleRatio := cfg.SampleRatio
	if sampleRatio <= 0 || sampleRatio > 1 {
		sampleRatio = 1.0
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	enabled.Store(true)

	return func(shutdownCtx context.Context) error {
		enabled.Store(false)
		err := provider.Shutdown(shutdownCtx)
		// Restore a no-op provider so late spans after shutdown stay cheap.
		otel.SetTracerProvider(noop.NewTracerProvider())
		return err
	}, nil
}

// newExporter builds an OTLP trace exporter for the configured protocol.
func newExporter(ctx context.Context, cfg *config.OTelConfig) (sdktrace.SpanExporter, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	protocol := strings.ToLower(strings.TrimSpace(cfg.Protocol))
	if protocol == "" {
		protocol = "grpc"
	}

	switch protocol {
	case "grpc":
		opts := []otlptracegrpc.Option{}
		if endpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
		}
		return otlptracegrpc.New(ctx, opts...)
	case "http", "http/protobuf":
		opts := []otlptracehttp.Option{}
		if endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
		}
		return otlptracehttp.New(ctx, opts...)
	default:
		return nil, fmt.Errorf("unsupported otel protocol %q (want \"grpc\" or \"http\")", cfg.Protocol)
	}
}

// tracer returns the package tracer. When Init has not run this resolves to
// the global no-op provider, keeping disabled-mode call sites allocation-free.
func tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}
