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

---

## Security defaults (changed in this release)

### Sandbox (A2)

The process-level sandbox is now **enabled by default**.

| Platform | Sandbox backend | Effect |
|---|---|---|
| macOS | `sandbox-exec` (seatbelt) | Shell commands run under an Apple Sandbox profile |
| Linux | `bwrap` (bubblewrap) | Shell commands run in a bubblewrap container |
| Other | Noop (no isolation) | **A prominent warning is printed at startup** |

**Network access inside the sandbox is disabled by default.** Workflows that need outbound network access must enable it explicitly:

```yaml
# nano config file
sandbox:
  enabled: true         # default: true (was false)
  network_access: true  # default: false (was true)
```

Or via environment variables:
```
NANO_SANDBOX_ENABLED=true
NANO_SANDBOX_NETWORK_ACCESS=true
```

To restore the previous (less secure) behavior for a specific run:
```
nano --sandbox=off ...
```

> **CI / headless usage:** If your CI pipeline runs nano-agent non-interactively and does not have `bwrap`/`sandbox-exec` available, set `NANO_SANDBOX_ENABLED=false` explicitly. A warning will appear but the agent will continue. For Linux CI with bubblewrap available, the default sandbox is recommended.
