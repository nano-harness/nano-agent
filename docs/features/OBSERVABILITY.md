# Observability: OpenTelemetry GenAI Tracing

[中文](./OBSERVABILITY.zh-CN.md)

nano-agent can emit traces that follow the OpenTelemetry GenAI semantic
conventions, informed by the September 2026 harness-engineering field
research: agent runs are only debuggable at scale when orchestration, model
calls, tool executions, and memory operations are captured as structured
spans with stable attribute names.

Tracing is **disabled by default**. When disabled, every instrumentation
point resolves to the OTel no-op tracer provider — zero export, zero
behavior change, and negligible overhead.

## The four span pillars

| Pillar | Span | Where it is created | Key attributes |
|--------|------|--------------------|----------------|
| Orchestration | `invoke_agent` | one per agent turn (`pkg/agent/turn_executor.go`) | `gen_ai.operation.name`, `gen_ai.conversation.id`, `nano.turn.id` |
| LLM | `chat <model>` | every model call (`pkg/llm/client.go`, `pkg/llm/anthropic_client.go`) | `gen_ai.system`, `gen_ai.request.model`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.response.finish_reasons` |
| Tool | `execute_tool <name>` | every tool execution (`pkg/agent/tool_scheduler.go`) | `gen_ai.tool.name`, `gen_ai.tool.call.id`, `nano.tool.success` |
| Memory/Context | `context_compression`, `context_editing` | compaction and stale-tool-result clearing (`pkg/agent/turn.go`) | `nano.context.tokens_before/after`, `nano.context.messages_before/after`, `nano.context.cleared_results` |

Spans nest naturally: `invoke_agent` → `chat` / `execute_tool` /
`context_compression`, so a single trace reconstructs the whole turn.

### Attribute naming

Attribute keys follow the OTel GenAI semantic conventions
(`gen_ai.operation.name`, `gen_ai.system`, `gen_ai.request.model`,
`gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`,
`gen_ai.response.finish_reasons`, `gen_ai.tool.name`, `gen_ai.tool.call.id`,
`gen_ai.conversation.id`). nano-agent specific extensions use the `nano.*`
prefix.

### Privacy

Prompt and completion **content is never attached to spans** — only
metadata. At most the request payload character length
(`gen_ai.request.prompt_length`) and completion length
(`gen_ai.response.completion_length`) are recorded. No API keys, user
messages, or tool payloads appear in attributes.

## Configuration

Tracing is configured via the `otel` section (TUI, daemon, and binary modes
share it, since all modes boot through the same CLI initialization):

```yaml
otel:
  enabled: true                  # default: false
  endpoint: "localhost:4317"     # OTLP collector address (4317 gRPC / 4318 HTTP)
  protocol: "grpc"               # "grpc" (default) or "http"
  insecure: true                 # disable TLS (local collectors)
  service_name: "nano-agent"     # service.name resource attribute
  sample_ratio: 1.0              # trace sampling ratio in (0, 1]
  headers: {}                    # extra export headers (e.g. managed collector auth)
```

Initialization failures (unsupported protocol, exporter construction errors)
are logged and leave tracing disabled — they never abort startup. On
shutdown the pipeline is flushed with a 5-second timeout.

## Viewing traces

Point any OTLP-compatible backend at the configured endpoint. For a local
Jaeger all-in-one:

```bash
docker run -p 4317:4317 -p 16686:16686 jaegertracing/all-in-one:latest
```

then browse `http://localhost:16686` and select the `nano-agent` service.

## Implementation notes

- `pkg/telemetry/` holds the pipeline setup (`telemetry.go`) and the GenAI
  span/attribute constructors (`genai.go`). Call sites use small helpers
  (`StartAgentSpan`, `StartLLMSpan`, `StartToolSpan`, `StartContextSpan`,
  `SetLLMUsage`, `SetToolOutcome`, `EndWithError`) so attribute naming stays
  centralized and testable.
- Both LLM client implementations (OpenAI-compatible and native Anthropic)
  are instrumented, including the reasoning-fallback path.
- The OpenTelemetry Go SDK (`go.opentelemetry.io/otel`,
  `go.opentelemetry.io/otel/sdk`, OTLP gRPC/HTTP trace exporters) is the de
  facto standard in this space and was added as a new dependency for this
  feature.
