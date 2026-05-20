# Embed nano-agent as an engine

This document defines the supported contract for orchestrators that drive nano-agent as an embedded execution engine while preserving standalone CLI usage.

## Invocation mode

Use binary mode for one-shot jobs:

```bash
nano binary exec "short prompt"
cat prompt.txt | nano binary exec
nano binary exec < prompt.txt
nano binary exec --goal "all Go package tests pass" --goal-max-turns 30 < prompt.txt
```

Use daemon mode when the orchestrator needs a long-lived process, shared sessions, WebSocket events, or lower cold-start overhead.

## Prompt input

`nano binary exec` accepts prompt arguments for backwards compatibility. When no prompt arguments are provided and stdin is not a TTY, it reads the full prompt from stdin until EOF. Prefer stdin for large prompts or prompts containing sensitive workflow context because it avoids shell quoting issues, command-line length limits, and `ps` exposure.

## Structured result contract

By default, binary commands emit normal human/patch output and then append one machine-readable sentinel line:

```text
<<<NANO_RESULT>>>{"status":"success","tool_calls":12,"duration_ms":45000,"tokens":{"input":3200,"output":850}}
```

When a goal is active, the sentinel includes `goal_state` with the current
condition, evaluation counters, token spend, max turns, last judge reason, and
`achieved_at` when the judge marks the goal complete. If the goal reaches
`max_turns` without being achieved, binary mode reports `status=needs_retry`
so orchestrators can decide whether to retry or abandon.

Set `NANO_BINARY_RESULT_FORMAT` to control this behavior:

| Value | Behavior |
|---|---|
| `both` or empty | normal output plus `<<<NANO_RESULT>>>` JSON sentinel |
| `json` | JSON summary only |
| `plain`, `none`, `off` | suppress structured summary |

Status values are `success`, `needs_retry`, `abandoned`, and `timeout`.

## Goal-driven workflows

Binary mode supports goal-driven inner loops for embedded orchestrators:

- pass `--goal "<verifiable condition>"`;
- optionally pass `--goal-max-turns <n>` to override the configured turn limit;
- or put `/goal <verifiable condition>` on the first prompt line. That line is
  removed before the remaining prompt is sent to the agent.

If both `--goal` and a prompt `/goal` line are present, the flag takes
precedence and the prompt line is still stripped.

Exit codes:

| Code | Meaning |
|---:|---|
| 0 | success |
| 10 | needs_retry |
| 20 | abandoned |
| 30 | timeout |
| 1 | unclassified failure |

Recommended orchestrator precedence is: explicit MCP completion event if present, stdout sentinel, then exit code.

## MCP and skill registration

When `SYMPHONY_MCP_URL` and `SYMPHONY_TOKEN` are present, nano-agent applies the built-in nano-symphony orchestrator profile. The profile enables MCP, injects a `symphony` streamable MCP server with `X-Symphony-Token`, enables skills, and auto-activates the embedded `nano-symphony` skill.

Manual configuration remains supported:

```yaml
mcp:
  servers:
    - name: symphony
      transport: streamable
      url: "${env:SYMPHONY_MCP_URL}"
      headers:
        X-Symphony-Token: "${env:SYMPHONY_TOKEN}"
```

The legacy MCP transport name `http` is accepted as a deprecated alias for `streamable`. `sse` and `websocket` remain unsupported.

## Environment variable interpolation

Config files support `${env:VAR_NAME}` in string values. Loading fails if the named environment variable is missing, which prevents accidentally running with literal placeholders. This is preferred for tokens and URLs that should not be written to disk.

## Prompt cache key metadata

For retry-heavy orchestrators, set `NANO_CACHE_KEY` or `SYMPHONY_ISSUE_ID`. Binary mode records prompt cache metadata under `~/.cache/nano/<key>/prompt-cache.json`, making repeated attempts observable and giving providers with prompt caching a stable execution key to coordinate around. Anthropic prompt caching continues to use the existing cache-control boundaries in the system prompt and tool schema.

## Sandbox mode

`nano binary exec` and `nano binary swebench` support:

```bash
--sandbox=auto|on|off
```

`auto` is the default. It enables sandboxing when an embedded environment is detected, such as `SYMPHONY_WORKSPACE`, `SYMPHONY_MCP_URL`, or `NANO_ORCHESTRATOR_PROFILE`. In embedded mode, writes are restricted to the current project path plus runtime cache/temp exceptions. Use `--sandbox=off` for legacy behavior or config-level sandbox paths for additional allowlists.

## Exit hooks

Use `--on-exit-cmd` or `NANO_ON_EXIT` to run a shell hook after binary execution:

```bash
nano binary exec --on-exit-cmd 'curl -X POST "$SYMPHONY_MCP_URL/session_completed" -d "$NANO_RESULT_JSON"' < prompt.txt
```

The hook receives `NANO_RESULT_JSON`, `NANO_RESULT_STATUS`, and `NANO_RESULT_EXIT_CODE` in its environment. Hook failures are logged and do not replace the agent result.

## Troubleshooting

- Missing `${env:VAR}` values cause config load failure; set the variable or remove the interpolation.
- If a legacy MCP config uses `http`, update it to `streamable` to silence the deprecation warning.
- If embedded writes are denied, verify the workspace path and sandbox allowlists, or temporarily run with `--sandbox=off` while debugging.
- If the sentinel is absent, check `NANO_BINARY_RESULT_FORMAT` and fall back to the process exit code.
