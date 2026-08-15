# Contributing to nano-agent

[中文](./CONTRIBUTING.zh-CN.md)

Thank you for your interest in contributing! This document covers the basics.

## Getting started

1. Install [Go](https://go.dev/dl/) 1.25 or later.
2. Clone the repo.
3. Run `make deps` to install development dependencies.

## Development workflow

```bash
make lint-check
make test
```

All tests must pass before merging.

## Pull request guidelines

- Keep changes focused and minimal.
- Update tests when changing behavior.
- Update relevant documentation (`README.md`, `docs/`, or `AGENTS.md`).
- Do not commit secrets, tokens, or personal `.env` files.
- Use clear commit messages that explain *why* the change is needed.

## Code style

- Go code is formatted with `gofmt` / `goimports`.
- Prefer small functions and explicit error handling.
- Avoid `panic` in library code; return errors.
- Keep packages focused and avoid circular imports.

## Reporting issues

When reporting bugs, please include:

- Steps to reproduce
- Expected vs actual behavior
- `go version` output
- Relevant logs (with secrets redacted)

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
