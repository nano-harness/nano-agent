# Architecture

This document summarizes the refactored nano-agent architecture and points to the detailed subsystem documents.

## Layering

The refactor keeps existing CLI, daemon, config, and session compatibility while moving stable seams behind focused packages:

- `pkg/agent`: agent orchestration, turn execution, session lifecycle, and storage compatibility.
- `pkg/toolruntime`: tool metadata, catalog, and execution runtime.
- `pkg/policy` and `pkg/middleware`: permission, approval, audit, and security compatibility surfaces.
- `pkg/hookservice`: hook loading and execution service.
- `pkg/extension`: normalized extension manifests for skills, MCP servers, tools, agents, and commands.
- `pkg/event`: public event envelopes and replay-compatible stream events.
- `pkg/daemon`: HTTP/WebSocket session APIs, replay, cancellation, and approval.
- `pkg/agentprofile`, `pkg/swarm`, `pkg/team`, and `pkg/mailbox`: configurable teammates and team runtime state.
- `pkg/llm`: model routing primitives, fallback, and metrics.

## Dependency direction

High-level interfaces should flow inward:

1. CLI, TUI, and daemon handlers parse user/API input.
2. Agent/session controllers manage turns and lifecycle.
3. Tool runtime executes registered tools through policy, hook, and audit middleware.
4. Storage/event layers record resumable state and public events.

UI and daemon code should consume public session/event state instead of reading private agent internals.

## Compatibility rules

- Existing public CLI commands and daemon endpoints remain compatible unless a deprecation is explicitly documented.
- Existing `pkg/tools` descriptor and registry APIs remain compatibility aliases around `pkg/toolruntime`.
- Existing middleware hook APIs remain compatibility wrappers around `pkg/hookservice`.
- Existing sessions remain readable; newer JSONL storage adds sequence-based resume where available.
- Existing command, skill, MCP, and teammate declarations remain additive.

## Detailed subsystem docs

- Configuration and migration: [Configuration Guide](../development/CONFIGURATION.md)
- Permission policy: [Permission Policy](../development/PERMISSION_POLICY.md)
- Sandbox design: [Sandbox Design](SANDBOX_DESIGN.md)
- Hooks: [Hooks System](../features/HOOKS.md)
- Tool runtime: [Tool Runtime](TOOL_RUNTIME.md)
- Extensions: [Extensions](../features/EXTENSIONS.md)
- Event and audit schemas: [Event Schema](../development/EVENT_SCHEMA.md)
- Daemon runtime: [Daemon Runtime](../operations/DAEMON_RUNTIME.md)
- Multi-agent runtime: [Multi-Agent System](../features/MULTI_AGENT.md)
- Migration guide: [Migration Guide](../migration/MIGRATION_GUIDE.md)

