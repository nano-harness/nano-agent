# nano-agent LLM error handling

## Retry, failback, and circuit breaker classification

`pkg/llm` classifies provider errors into separate retry and failback signals. Context overflow is intentionally not retried in the LLM client layer: the agent layer handles it in `pkg/agent/turn_policy.go` by attempting message compression and then retrying the same model.

| Error category | Retryable | ShouldFailback | Count as CB failure | Agent behavior |
|---|---:|---:|---:|---|
| RateLimit (429) | ✅ | ✅ | ✅ | Back off and retry the same model; repeated failures may trigger failback |
| Server (5xx) | ✅ | ✅ | ✅ | Back off and retry the same model; repeated failures may trigger failback |
| Timeout / Network | ✅ | ✅ | ✅ | Back off and retry the same model; repeated failures may trigger failback |
| Authentication (401) | ❌ | ❌ | ❌ | Fail immediately |
| Quota (402/403) | ❌ | ❌ | ❌ | Fail immediately |
| ContextOverflow | ❌ | ❌ | ❌ | Agent calls `CompressMessages` and retries |
| Aborted (context canceled) | ❌ | ❌ | ❌ | Propagate `ctx.Err()` |
| OutputFormat | ❌ | ❌ | ❌ | Agent can correct the prompt/tool format and retry |

Stream responses that end with `finish_reason=length` surface `truncated=true` and `finish_reason=length` in stream event metadata so the agent can request continuation without silently treating the partial output as complete.
