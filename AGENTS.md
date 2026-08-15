# AGENTS.md — nano-agent

[中文](./AGENTS.zh-CN.md)

This file contains context for coding agents working on `nano-agent`.

## Project overview

`nano-agent` is a lightweight AI-powered code assistant written in Go. It:

- Provides a TUI interactive mode and a daemon background service mode.
- Implements a modular tool architecture with a turn-based conversation flow.
- Supports any OpenAI-compatible LLM API plus native Anthropic SDK integration.
- Loads skills from `~/.nano/skills/` and project `.nano/skills/` directories.
- Enforces filesystem sandboxing via native backends (Linux `bwrap`, macOS `sandbox-exec`) and path-level access control.

## Toolchain

- **Language**: Go 1.25+
- **Build system**: `make`
- **Testing**: `go test ./...`
- **Linting**: `golangci-lint` via `make lint-check`

## Common commands

```bash
# Install development dependencies
make deps

# Run all tests
make test

# Run linters
make lint-check

# Build the binary
make build

# Build release artifacts for all platforms
make release
```

## Architecture

- `cmd/nano/` — main entrypoint.
- `pkg/agent/` — core turn-based agent loop, planning, and reasoning.
- `pkg/tools/` — tool implementations (filesystem, shell, search, web, etc.).
- `pkg/llm/` — LLM client abstractions and provider integrations.
- `pkg/mcp/` — Model Context Protocol client and server support.
- `pkg/sandbox/` — native sandboxing and path access control.
- `pkg/skill/` — skill discovery, matching, and loading.
- `pkg/daemon/` — HTTP/WebSocket daemon mode.
- `pkg/ui/` — terminal UI backends (tview, Bubble Tea).
- `pkg/version/` — build-time version injection.

## Coding conventions

- Return errors explicitly; wrap context with `fmt.Errorf("...: %w", err)`.
- Keep packages small and avoid circular dependencies.
- Use table-driven tests for branching logic.
- Never log secrets (API keys, tokens, credentials).
- Prefer composition over deep inheritance patterns.

## Testing

- Unit tests live next to the code under test (`*_test.go`).
- E2E tests are in `e2e/` and may require a running daemon.
- Smoke tests are in `smoke/`.

## Security notes

- The sandbox blocks sensitive paths (`~/.aws`, `~/.ssh`, `**/.env*`, etc.) by default even when disabled.
- Filesystem tools validate paths against the working directory and sandbox rules.
- Agent subprocesses spawned by orchestrators should not receive orchestrator secrets.
