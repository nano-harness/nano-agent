# PTY Smoke Tests for nano-agent

This directory contains PTY-based smoke tests for the TUI and CLI interfaces of nano-agent.

## Overview

These tests use PTY (pseudo-terminal) to simulate real user interactions with the nano-agent TUI and CLI. They validate:
- TUI startup and basic interaction
- Slash command handling
- Session management
- Daemon lifecycle
- Tool approval workflows

## Running Tests

```bash
# Run all smoke tests
make smoke

# Run only TUI smoke tests
make smoke-tui

# Run tests directly with go
go test -v -tags=smoke -timeout 5m ./smoke/...
```

## Test Structure

- `helpers/` - Reusable utilities for PTY testing
  - `pty.go` - PTY session management
  - `mock_llm.go` - Mock LLM server wrapper
  - `nano_config.go` - Test configuration factory
  - `snapshot.go` - Terminal snapshot utilities (future)
- `testdata/` - Test configuration files
- `*_test.go` - Smoke test files

## Dependencies

The smoke tests use:
- `creack/pty` - PTY creation and management
- `Netflix/go-expect` - Expect-style interaction DSL
- Existing e2e mock server infrastructure

## Notes

- Tests are isolated with `//go:build smoke` tag
- Each test uses temporary working directories
- Mock LLM server prevents network dependencies
- Tests skip if PTY is not available (e.g., Windows CI)
