# Configuration and Migration Guide

[中文](./CONFIGURATION.zh-CN.md)

This guide records the configuration compatibility rules for the architecture refactor. The refactor is additive: existing CLI flags, `.nano.yaml` fields, daemon APIs, sessions, skills, commands, and team data remain supported unless explicitly listed below.

## Configuration sources and precedence

nano-agent reads configuration from these sources, from lowest to highest precedence:

1. Built-in defaults.
2. User-level config and runtime state under `~/.nano`.
3. Project config such as `.nano.yaml` and project-local `.nano/*` directories.
4. Environment variables such as `NANO_WORKING_DIR`.
5. CLI flags and in-process overrides.

Runtime state remains centralized under `~/.nano`. Project declarations such as custom commands, skills, and agent profiles live under the project working directory.

## No-op migrations

The following refactor features require no migration:

- Tool metadata moved behind `pkg/toolruntime`, while existing `pkg/tools` descriptor APIs remain compatibility aliases.
- Hook execution moved behind `pkg/hookservice`, while middleware hook entry points remain compatibility wrappers.
- Turn orchestration moved behind `turnExecutor`, while public agent processing APIs remain unchanged.
- Session lifecycle gained explicit state and incremental JSONL resume support without changing existing session IDs.
- Event replay uses existing public `event.StreamEvent` envelopes and sequence fields.

## Slash commands

Custom slash commands remain discoverable from project commands and compatible `.claude/commands` locations. New frontmatter fields are optional:

```yaml
allowed-tools: [run_shell_command]
permission-profile: acceptEdits
prelude_timeout: 30
prelude_on_error: abort
prelude_output: summary
```

Migration rule:

- Existing commands without these fields continue to work.
- Add `allowed-tools` to narrow the tools a command may use.
- Add `permission-profile` only when a command needs a temporary permission mode.
- Leading `!shell command` prelude lines are executed through `SandboxRuntime` by `slash.CommandRuntime`.
- `prelude_timeout`, `prelude_on_error`, and `prelude_output` are optional and only affect prelude execution.

Daemon `/api/v1/commands` exposes these as `allowedTools` and `permissionProfile`.

## Hooks

Existing hook declarations remain compatible. Hooks may opt into narrower
environment inheritance and explicit failure handling:

```yaml
security:
  hooks:
    - name: deny-dangerous-shell
      event: pre_tool_use
      pattern: run_shell_command:*
      command: ./hooks/deny-dangerous-shell.sh
      enabled: true
      failure_policy: confirm
      env_whitelist: [PATH]
```

`failure_policy` supports `confirm` (default), `block`, `allow`, and
`ignore_but_audit`. Hooks still receive `NANO_TOOL_NAME` and `NANO_TOOL_INPUT`;
new hooks can read structured `NANO_HOOK_INPUT` and return structured JSON on
stdout.

## Model routing and thinking

Primary model settings remain compatible with the existing top-level fields:

```yaml
model: deepseek-chat
base_url: https://api.deepseek.com/v1
```

The command layer can manage these fields with:

```bash
nano model list
nano model use deepseek-r1 --provider deepseek
nano model status
nano think on --effort high
nano think status
```

Fallback route metadata is stored separately and does not change default primary route behavior until callers explicitly opt into model routing:

```yaml
model_routing:
  fallbacks:
    - name: fast
      model: gpt-4.1
      base_url: https://api.openai.com/v1
```

Manage fallback route metadata with:

```bash
nano model fallback list
nano model fallback add gpt-4.1 --name fast --provider openai
nano model fallback clear
```

## Sandbox configuration

Sandbox configuration remains additive and backward compatible. Existing configs without a sandbox section keep their previous behavior.

```yaml
sandbox:
  enabled: true
  backend: docker # "", none, native, docker
  docker_image: ubuntu:24.04
  network_access: false
```

Migration rule:

- `backend: ""` preserves the platform-native default selection.
- `backend: docker` uses one-shot Docker containers for command execution.
- `docker_image` may be set to a digest-pinned image for stronger reproducibility.
- When CLI permission mode is `yolo` and no sandbox backend is explicitly configured, nano-agent defaults the backend to Docker and logs that choice.
- Docker containers only receive `NANO_*` environment variables; do not store secrets in `NANO_*` variables unless sandboxed commands should receive them.

## Agent profiles

Configurable teammates are declared in `.nano/agents`:

```yaml
# .nano/agents/reviewer.yaml
description: Review code changes
initial_prompt: Review the requested patch and report risks.
permission_mode: acceptEdits
allowed_tools: [read_file, run_shell_command]
kind: in_process
color: "#00ff00"
```

Migration rule:

- Existing static sub-agent config remains supported.
- Add `.nano/agents/<name>.yaml` only when a project wants explicit `/agent-name` slash command invocation or reusable teammate defaults.
- `initial_prompt` is optional for `spawn_teammate` when the matching AgentProfile provides it.
- `permission_mode`, `allowed_tools`, `model`, and `context_providers` are applied to the spawned teammate independently of the parent agent.

Optional multi-agent governance limits:

```yaml
advanced:
  fork:
    max_depth: 1
    max_concurrent: 3
    max_runtime_sec: 3600
```

- Teammates are not allowed to spawn nested teammates, preventing unbounded agent depth by default.
- `max_concurrent` limits active teammates per team when set to a value greater than zero.
- `max_runtime_sec` limits each spawned teammate runtime when set to a value greater than zero.

Supported profile files:

- `.yaml`
- `.yml`
- `.json`
- `.md` with optional YAML frontmatter

## Extension manifests

`manage_extension` provides a unified view of:

- skills
- MCP servers
- tools
- agent tools and `.nano/agents` profiles
- slash commands

Migration rule:

- Use `manage_extension` for status/manifest inspection across extension types.
- Skills and MCP servers can still be managed through their specialized tools.
- Remote extension sources (`http://` or `https://`) require explicit user confirmation before install/update through `manage_extension`.
- Plain HTTP remote sources are reported as `remote_insecure` in trust metadata.

## Event and audit schema

Event consumers should use the public `event.StreamEvent` fields:

- `type`
- `session_id`
- `run_id`
- `seq`
- `metadata`
- `payload`

Team-lead replay supports:

- REST: `GET /api/v1/teams/sessions/{id}/events?since_seq=N`
- WebSocket live/replay: `subscribe`
- WebSocket replay-only: `replay`

Audit JSONL entries include `schema_version` and optional `security_decision`. See `docs/EXTENSION_EVENT_SCHEMA.md` for the field-level schema.

Local audit JSONL rotation is configured under `middleware`:

```yaml
middleware:
  enable_audit: true
  audit_log_path: ~/.nano/audit.jsonl
  audit_max_size_mb: 100
  audit_max_backups: 3
  audit_max_age_days: 28
  audit_compress: true
```

The default path remains `~/.nano/audit.jsonl`; rotated files are retained according to the size, backup count, age, and compression settings.

## Permissions and security migration

Existing permission modes continue to work. The refactor adds narrower opt-in controls:

- command-level `allowed-tools`
- command-level `permission-profile`
- AgentProfile `permission_mode`
- AgentProfile `allowed_tools`
- extension manifest trust/health/permissions metadata

Recommended migration order:

1. Keep existing config unchanged.
2. Add command `allowed-tools` where command behavior is known.
3. Move reusable teammate defaults into `.nano/agents`.
4. Use `manage_extension` to audit extension trust and permissions.
5. Validate daemon clients against the documented replay and approval frames.
