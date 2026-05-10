# Hooks

Hooks are lifecycle extension points executed through `pkg/hookservice`.

## Compatibility

`pkg/middleware` keeps compatibility wrappers for existing hook callers. New code should depend on `pkg/hookservice.Service` directly when possible.

## Hook execution contract

A hook receives normalized context about the lifecycle event and may return:

- allow/continue;
- require confirmation;
- block with a reason;
- execution metadata for audit.

Hooks receive the legacy `NANO_TOOL_NAME` and `NANO_TOOL_INPUT` variables, plus
structured `NANO_HOOK_INPUT` JSON containing the event, tool name, params,
working directory, environment allowlist, sandbox flag, and timeout summary.

Hooks may write structured JSON to stdout:

```json
{
  "action": "block",
  "reason": "dangerous command",
  "warnings": ["use a read-only command"],
  "audit_metadata": {"risk": "high"}
}
```

Supported actions include `allow`, `confirm`, `block`, `emit_warning`,
`add_context`, `modify_params`, `redact_output`, and `request_sandbox`. Exit
codes remain compatible: `0` allows, `1` requests confirmation, and `2` blocks.

`modify_params` may return `modified_params` as a shallow parameter override.
For shell tools, the rewritten `command` is re-analyzed by `CommandGuard` before
it can execute:

```json
{
  "action": "modify_params",
  "modified_params": {
    "command": "git status"
  },
  "audit_metadata": {"rewrite": "safe-status"}
}
```

Hook failures and timeouts must follow the configured failure policy instead of panicking or bypassing policy.
Supported failure policies are `confirm` (default), `block`, `allow`, and
`ignore_but_audit`.

## Hook types (M2-3)

A hook entry can declare one of four execution backends via the `type` field:

| Type      | What it does                                                                 |
|-----------|------------------------------------------------------------------------------|
| `command` | Default. Spawns a shell process with the canonical `NANO_HOOK_INPUT` env var.|
| `http`    | POSTs the JSON envelope to a configured URL and parses the JSON response.    |
| `prompt`  | Sends a templated prompt to an LLM and parses `{ok,reason}` or `{action,...}`.|
| `agent`   | Delegates the decision to a named subagent profile.                          |

HTTP hooks enforce a host allowlist (`url_allowlist`), reject CR/LF in
configured headers, never auto-follow redirects, and bound the response body
(64 KB by default, override with `max_response_kb`).

## New events wired in M1F

The hook engine now fires the previously stubbed events:

- `pre_compact` / `post_compact` around context compaction.
- `stop` / `stop_failure` when a turn finishes successfully or aborts.
- `subagent_start` / `subagent_stop` / `teammate_idle` for spawned teammates.

## Security expectations

- Hook environment variables should be allowlisted.
- Hook commands should run with explicit timeout.
- Hook decisions should be auditable.
- Hook output should not be treated as trusted code.
- High-risk hooks should not silently expand tool permissions.
- HTTP hook URLs must be in `url_allowlist`; deploy-time policy should reject
  the empty allowlist for production.

## Related docs

- [Permission Policy](../development/PERMISSION_POLICY.md)
- [Extension Event Schema](../development/EXTENSION_EVENT_SCHEMA.md)
- [Configuration Guide](../development/CONFIGURATION.md)
