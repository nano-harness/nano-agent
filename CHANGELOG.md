# Changelog

All notable changes to nano-agent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Breaking / UI Daemon Refactor
- BREAKING ui: `Adapter` now uses `Run(ctx, EventSource) error`; `SendEvent`, `SubmitChannel`, and `CancelChannel` were removed.
- BREAKING cli: removed `lead-chat` readline/plain rendering and daemon streaming `fmt.Print` path; use `nano daemon execute --json` for scripts.
- feat(ui): added shared EventSource abstraction consumed by BubbleTea and tview.
- feat(daemon-client): exposed WebSocket URLs and team cancel helper for renderer-owned daemon streams.
- docs(daemon-api): rewrote daemon API as a Web client implementation guide.

### Added
- `ShouldFailback` field on `APIErrorInfo`, with dedicated `ContextOverflow`, `Aborted`, and `OutputFormat` API error categories.
- Stream event metadata `truncated` and `finish_reason` for `finish_reason=length` detection.
- `advanced.circuit_breaker.exclude_non_failback` and `advanced.circuit_breaker.truncation_detection` configuration options.
- **Multi-Agent Mailbox System**: Asynchronous message passing infrastructure for fork-based parallel execution
  - Core interfaces: `Mailbox`, `Backend`, `Manager` for structured agent-to-agent communication
  - Memory backend for single-process CLI mode and testing
  - File backend with JSONL + flock for daemon mode with crash recovery
  - `send_message` tool for sub-agents to communicate with parent agents
  - Support for message topics: `progress`, `finding`, `amend_task`
  - Rate limiting: 20 messages per agent run
  - Configurable TTL (default 7 days) and capacity limits (default 1000 messages)
- **Enhanced Bubble Tea Banner**: New cinematic animated banner with Standard Figlet thin-line font
  - 20-frame animation (~3000ms) with atomic mascot, NANO-AGENT text, and sweep shine effects
  - Replaced old box-drawing frames with elegant thin-line ASCII art
  - Added `ElemSubtitle` semantic color role for muted gray subtitles
- **Expert System**: New expert/sub-agent architecture aligned with Gemini CLI
  - Three built-in experts: `@investigator`, `@help`, `@generalist`
  - Explicit `@expert-name` trigger syntax (users only, LLM cannot call directly)
  - Custom expert support via markdown files in `~/.config/nano/agents/` and `.nano/agents/`
  - `/agents` slash commands to list and inspect available experts
  - Expert event types: `expert_started`, `expert_progress`, `expert_finished`

### Changed
- **BREAKING**: Sub-agents now use `@kebab-case-name` trigger syntax exclusively
  - Old syntax (`使用[xxx]`, `with:xxx`, implicit triggers) no longer supported
  - YAML `sub_agents` config still works but requires `@name` to trigger
  - Names automatically converted to kebab-case (e.g., `myAgent` → `@my-agent`)

### Fixed
- Context overflow, authentication, aborted, and output-format errors no longer pollute circuit breaker failure counters.

### Removed
- **BREAKING**: Removed `fork` tool - LLM can no longer autonomously fork child agents
  - All sub-agent invocations must be explicitly triggered by users via `@expert-name`
  - Improves observability: users always know when experts are invoked and what they cost
