# Configuration

## Environment Variables

`nano` reads environment variables with the following priority:

- `NANO_API_KEY` (preferred) — your LLM provider API key
- `API_KEY` (legacy fallback) — same as above, kept for backward compatibility
- `NANO_BASE_URL` / `BASE_URL` — custom base URL for the provider
- `NANO_MODEL` / `MODEL` — model name (e.g. `claude-sonnet-4-20250514`)
- `NANO_BINARY_TIMEOUT_MS` — timeout for `nano binary exec` (0 = no limit)
- `NANO_PERMISSION_MODE` — permission mode: `auto`, `ask`, `off`
- `NANO_SESSION_ID` / `SYMPHONY_ISSUE_ID` — session ID for hook routing
- `NANO_ORCHESTRATOR_PROFILE` — embedded execution profile

The `NANO_*` prefix is preferred to avoid collisions with other tools. The bare
names are still accepted for backward compatibility.

## MCP Configuration Files

`nano binary exec` accepts a Claude Code-compatible `.mcp.json` via the
`--mcp-config` flag. This lets you register MCP servers without editing
`nano.yaml`:

```bash
nano binary exec --mcp-config ./symphony.mcp.json --allowedTools 'mcp_symphony_*' --output-dir ./out "your prompt"
```

The `.mcp.json` shape is a standard JSON object with `mcpServers`:

```json
{
  "mcpServers": {
    "symphony": {
      "type": "http",
      "url": "http://localhost:8080/sse",
      "headers": {
        "X-Symphony-Token": "secret-token"
      }
    }
  }
}
```

Supported transports:

| `type` | nano transport | required fields |
|---|---|---|
| `http`, `sse`, `streamable` | `streamable` | `url` |
| `stdio` | `stdio` | `command` (and optionally `args`) |
| omitted (auto-detect) | inferred from `command` vs `url` | one of `command` or `url` |

## Tool Permissions

`nano binary exec` supports fine-grained tool allow/deny lists:

```bash
nano binary exec \
  --allowedTools 'mcp_symphony_*' \
  --allowedTools 'ReadFile' \
  --disallowedTools 'Bash' \
  --disallowedTools 'WriteFile' \
  --output-dir ./out "prompt"
```

Rules:

- `--allowedTools` is repeatable. The first prefix match wins for a given tool name.
- `--disallowedTools` overrides `--allowedTools` for matching prefixes.
- If neither flag is provided, the config-level `mcp.allowed_tools` / `mcp.disallowed_tools` lists are used.
- If no lists are configured at all, all tools are permitted (subject to the usual permission-mode guards).

## Configuration Inspection

View the merged configuration (including env overrides and defaults):

```bash
nano config show --effective
```

Read a single key (supports dot-path):

```bash
nano config get api_key
nano config get advanced.fork.max_depth
```

## Configuration Persistence

`nano mcp add` and `nano mcp auth` only update the `mcp:` block in `config.yaml`.
Other keys, comments, and ordering are preserved. This is a surgical update — not a full config rewrite.
