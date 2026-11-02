# Pull Request

[中文](./PULL_REQUEST_TEMPLATE.zh-CN.md)

## Description

<!-- Briefly describe what this change does and why -->

## Type of Change

- [ ] Bug fix
- [ ] New feature
- [ ] Refactor
- [ ] Docs
- [ ] Other

## Pre-merge Checklist

### Code Quality
- [ ] Build passes (`make build`)
- [ ] Tests pass (`make test`)
- [ ] Linter passes (`make lint-check`)
- [ ] Race detector clean for touched concurrent code (`go test -race ./pkg/<...>/`)

### Code Review
- [ ] Code logic is clear, with no redundant code
- [ ] No new security vulnerabilities introduced
- [ ] Errors are returned explicitly and wrapped with context (`fmt.Errorf("...: %w", err)`)
- [ ] No secrets are logged (API keys, tokens, credentials)

### Testing
- [ ] Unit tests added/updated next to the code under test (if branching logic changed)
- [ ] E2E / smoke tests considered for user-facing behavior changes

### Documentation
- [ ] README or related docs updated (if needed)
- [ ] AGENTS.md updated if documented architecture or conventions changed

### Other
- [ ] No sensitive information committed (keys, tokens, etc.)
- [ ] Commit messages are clear and follow project conventions
