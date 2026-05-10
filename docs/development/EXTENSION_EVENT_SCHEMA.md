# Extension and Event Schemas

This document records the stable public schemas introduced by the architecture refactor.

## Extension manifest

`pkg/extension.Manifest` is the normalized view returned by `manage_extension` and internal extension registries.

```json
{
  "schema_version": "1",
  "id": "agent:reviewer",
  "name": "reviewer",
  "kind": "agent",
  "description": "Review code changes",
  "source": "/repo/.nano/agents/reviewer.yaml",
  "enabled": true,
  "installed": true,
  "permissions": [
    { "type": "agent_spawn", "scope": "in_process" },
    { "type": "permission_profile", "scope": "acceptEdits" },
    { "type": "tool_execution", "scope": "read_file" }
  ],
  "trust": {
    "trusted": true,
    "level": "local",
    "reason": "loaded from local configuration or filesystem"
  },
  "health": {
    "status": "healthy",
    "message": "agent profile discovered"
  },
  "metadata": {
    "permission_mode": "acceptEdits",
    "allowed_tools": ["read_file"]
  }
}
```

Supported `kind` values:

- `skill`
- `mcp`
- `tool`
- `agent`
- `command`

Agent manifests include both runtime agent tools and `.nano/agents` profiles. Command manifests come from `.nano/commands` and compatible `.claude/commands`.

Trust levels:

- `runtime`: registered in the current process.
- `local` / `configured`: loaded from local config or filesystem.
- `remote`: HTTPS remote source, not trusted until explicitly confirmed.
- `remote_insecure`: plain HTTP remote source, not trusted and should be upgraded to HTTPS.

`manage_extension` refuses remote install/update operations when no confirmation handler is available, so untrusted remote extensions cannot be added silently.

## AgentProfile config

Project agent profiles live under `.nano/agents` and may be YAML, JSON, or Markdown with YAML frontmatter.

```yaml
name: reviewer
description: Review code changes
initial_prompt: Review the requested patch and report risks.
permission_mode: acceptEdits
allowed_tools: [read_file, run_shell_command]
model: gpt-4.1
kind: in_process
color: "#00ff00"
```

No migration is required for existing configs. `.nano/agents` is additive, and older static sub-agent config remains supported. When a profile declares `allowed_tools`, those tools become the spawned teammate's independent enabled tool set.

## Slash command metadata

Custom slash commands support:

```yaml
allowed-tools: [run_shell_command]
permission-profile: acceptEdits
```

The daemon `/api/v1/commands` response exposes these as `allowedTools` and `permissionProfile`.

## Audit JSONL schema

Audit entries written by `pkg/middleware.AuditMiddleware` include a schema version:

```json
{
  "schema_version": "1",
  "ts": "2026-04-30T12:00:00Z",
  "tool": "run_shell_command",
  "params": { "command": "git status" },
  "success": true,
  "duration_ms": 12,
  "session_id": "session_abc",
  "security_decision": {
    "action": "allow",
    "reason": "session allowlist",
    "rule": "run_shell_command(git status)",
    "layer": 2,
    "confidence": 1,
    "suggestions": [],
    "auto_whitelist": false
  }
}
```

The in-code schema descriptor is `middleware.AuditSchema()`.

## Event replay and approval

Team-lead streams support:

- `subscribe` for replay plus live events.
- `replay` for replay-only use cases.
- `tool_approval` with `approved`.
- `approve` / `reject` aliases for explicit UI actions.

Replay state is sequence-based (`seq` / `since_seq`) and uses the same public `event.StreamEvent` envelope for CLI, TUI, and daemon consumers.
