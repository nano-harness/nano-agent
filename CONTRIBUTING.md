# Contributing to nano-agent

[中文](./CONTRIBUTING.zh-CN.md)

Thank you for your interest in contributing! This document covers the basics.

## Getting started

1. Install Go 1.25+ and `make`.
2. Clone the repo and run `make deps` to install development dependencies.
3. Copy `.env.example` to `.env` and set your LLM API key if you want to run the binary.

## Development workflow

```bash
make fmt          # format code
make test         # unit tests
make lint-check   # lint without auto-fixing
make build        # build the binary with version info
```

E2E tests live in `e2e/` and may require a running daemon (`make test-e2e`); PTY smoke tests live in `smoke/` (`make smoke`). All tests must pass before merging — `make check` runs format check, vet, lint, and unit tests in one go.

## Pull request guidelines

- Keep changes focused and minimal.
- Update tests when changing behavior.
- Update relevant documentation (`README.md`, `docs/`, or `AGENTS.md`) — docs are bilingual, so keep each `.md` in sync with its `.zh-CN.md` counterpart.
- Do not commit secrets, tokens, or personal `.env` files.
- Use clear commit messages that explain *why* the change is needed.

## Code style

See [AGENTS.md](./AGENTS.md) for the full conventions. In short:

- Return errors explicitly; wrap context with `fmt.Errorf("...: %w", err)`; avoid `panic` in library code.
- Format Go code with `gofmt` / `goimports` (`make fmt`).
- Keep packages small and avoid circular dependencies.
- Use table-driven tests for branching logic.
- Never log secrets (API keys, tokens, credentials).
- Prefer composition over deep inheritance patterns.

## Reporting issues

When reporting bugs, please include:

- Steps to reproduce
- Expected vs actual behavior
- `go version` and `nano --version` output
- Relevant logs (with secrets redacted)

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
