# Multi-Agent Runtime

The multi-agent runtime supports team-lead sessions, configurable teammates, mailboxes, and replayable team events.

## Agent profiles

Project-local profiles live under `.nano/agents` and can be YAML, JSON, or Markdown with YAML frontmatter.

Example:

```yaml
description: Review code changes
initial_prompt: Review the requested patch and report risks.
permission_mode: acceptEdits
allowed_tools: [read_file, run_shell_command]
kind: in_process
color: "#00ff00"
```

Profiles are loaded by `pkg/agentprofile`.

## Explicit invocation

User input can explicitly target a profile:

```text
@reviewer check pkg/agent for regressions
```

The preprocessor rewrites this to a `spawn_teammate` request with profile defaults.

## Teammate permissions

`permission_mode` and `allowed_tools` are independent teammate constraints. They should be copied into the teammate identity/config and must not mutate the parent agent's config.

Agent profiles and `spawn_teammate` may also set `model` for a teammate-specific model override. The override is applied to a copied child config for in-process teammates and passed through `nano teammate --model` for subprocess teammates.

Agent profiles and `spawn_teammate` may set `context_providers` to constrain context sources for a teammate. Supported provider names are `memory`, `skills`, and `openspec`; omitted values inherit the parent context configuration.

## Governance limits

`spawn_teammate` is team-lead only. Teammates cannot spawn nested teammates, which prevents unbounded agent depth by default.

Teams can also cap concurrent active teammates:

```yaml
advanced:
  fork:
    max_concurrent: 3
    max_runtime_sec: 3600
```

When `advanced.fork.max_concurrent` is greater than zero, `spawn_teammate` counts active members in the target team and rejects new spawns once the limit is reached. A value of `0` or an omitted setting preserves the existing unlimited behavior.

When `advanced.fork.max_runtime_sec` is greater than zero, spawned teammates receive a runtime deadline. In-process teammates are cancelled through their context and marked inactive when the deadline expires; subprocess teammates receive the same limit through the hidden `nano teammate --max-runtime-sec` flag.

## Team events and replay

Team-lead sessions store sequenced events and expose replay through the daemon REST/WebSocket APIs. Clients should use sequence-based replay instead of reading mailbox or session internals directly.

## Runtime paths

User runtime state is centralized under `~/.nano`. Team mailboxes live under:

```text
~/.nano/teams/<team>/mailbox
```
