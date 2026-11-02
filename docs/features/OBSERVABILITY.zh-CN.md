# 可观测性：OpenTelemetry GenAI 追踪

[English](./OBSERVABILITY.md)

nano-agent 可以发出遵循 OpenTelemetry GenAI 语义约定的 trace。该设计依据
2026 年 9 月马具工程调研：只有当编排、模型调用、工具执行与记忆操作都以
结构化 span、稳定的属性名被采集时，agent 运行才能在规模化场景下被调试。

追踪**默认关闭**。关闭时所有插桩点都解析到 OTel no-op tracer provider——
零导出、零行为变化、开销可忽略。

## 四大 span 支柱

| 支柱 | Span | 创建位置 | 关键属性 |
|------|------|----------|----------|
| 编排 | `invoke_agent` | 每个 agent 轮次（`pkg/agent/turn_executor.go`） | `gen_ai.operation.name`、`gen_ai.conversation.id`、`nano.turn.id` |
| LLM | `chat <model>` | 每次模型调用（`pkg/llm/client.go`、`pkg/llm/anthropic_client.go`） | `gen_ai.system`、`gen_ai.request.model`、`gen_ai.usage.input_tokens`、`gen_ai.usage.output_tokens`、`gen_ai.response.finish_reasons` |
| 工具 | `execute_tool <name>` | 每次工具执行（`pkg/agent/tool_scheduler.go`） | `gen_ai.tool.name`、`gen_ai.tool.call.id`、`nano.tool.success` |
| 记忆/上下文 | `context_compression`、`context_editing` | 压缩与过期工具结果清理（`pkg/agent/turn.go`） | `nano.context.tokens_before/after`、`nano.context.messages_before/after`、`nano.context.cleared_results` |

span 自然嵌套：`invoke_agent` → `chat` / `execute_tool` /
`context_compression`，单条 trace 即可还原整个轮次。

### 属性命名

属性键遵循 OTel GenAI 语义约定（`gen_ai.operation.name`、`gen_ai.system`、
`gen_ai.request.model`、`gen_ai.usage.input_tokens`、
`gen_ai.usage.output_tokens`、`gen_ai.response.finish_reasons`、
`gen_ai.tool.name`、`gen_ai.tool.call.id`、`gen_ai.conversation.id`）。
nano-agent 自有扩展使用 `nano.*` 前缀。

### 隐私

prompt 与补全**内容绝不写入 span**——只记录元数据。至多记录请求载荷的
字符长度（`gen_ai.request.prompt_length`）与补全长度
（`gen_ai.response.completion_length`）。属性中不会出现 API key、用户
消息或工具载荷。

## 配置

通过 `otel` 配置段启用（TUI、daemon、binary 三种模式共用同一 CLI 初始化
路径，因此行为一致）：

```yaml
otel:
  enabled: true                  # 默认：false
  endpoint: "localhost:4317"     # OTLP collector 地址（4317 gRPC / 4318 HTTP）
  protocol: "grpc"               # "grpc"（默认）或 "http"
  insecure: true                 # 关闭 TLS（本地 collector）
  service_name: "nano-agent"     # service.name 资源属性
  sample_ratio: 1.0              # 采样率，(0, 1] 区间
  headers: {}                    # 额外导出请求头（如托管 collector 鉴权）
```

初始化失败（不支持的协议、exporter 构造错误）只会记录日志并保持追踪关闭，
绝不会中断启动。退出时以 5 秒超时冲刷导出管道。

## 查看 trace

将任意 OTLP 兼容后端指向配置的 endpoint。本地 Jaeger all-in-one 示例：

```bash
docker run -p 4317:4317 -p 16686:16686 jaegertracing/all-in-one:latest
```

然后打开 `http://localhost:16686`，选择 `nano-agent` 服务。

## 实现说明

- `pkg/telemetry/` 包含管道初始化（`telemetry.go`）与 GenAI span/属性构造
  函数（`genai.go`）。调用点使用小型辅助函数（`StartAgentSpan`、
  `StartLLMSpan`、`StartToolSpan`、`StartContextSpan`、`SetLLMUsage`、
  `SetToolOutcome`、`EndWithError`），保证属性命名集中且可测试。
- 两个 LLM 客户端实现（OpenAI 兼容与 Anthropic 原生）均已插桩，包括
  reasoning 降级路径。
- OpenTelemetry Go SDK（`go.opentelemetry.io/otel`、
  `go.opentelemetry.io/otel/sdk`、OTLP gRPC/HTTP trace exporter）是该领域
  事实标准，作为本特性的合理新增依赖引入。
