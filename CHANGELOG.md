# Changelog

All notable changes to nano-agent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
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

### Removed
- **BREAKING**: Removed `fork` tool - LLM can no longer autonomously fork child agents
  - All sub-agent invocations must be explicitly triggered by users via `@expert-name`
  - Improves observability: users always know when experts are invoked and what they cost

## Background

This release refactors nano-agent's sub-agent system to align with Gemini CLI v0.38.1's expert architecture, with two key differences:

1. **More aggressive than Gemini**: Completely removes LLM's ability to implicitly fork. Only explicit user `@expert-name` triggers work.
2. **Kebab-case naming**: Uses `@investigator` / `@help` / `@generalist` (not `@codebase_investigator` / `@cli_help`). No alias support.

These breaking changes improve:
- **Observability**: Users always see when experts are invoked
- **Cost Control**: No hidden token usage from autonomous forks
- **Explicit Intent**: Clear user control over expert delegation
