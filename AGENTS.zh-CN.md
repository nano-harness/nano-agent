# AGENTS.md — nano-agent

[English](./AGENTS.md)

本文件为参与 `nano-agent` 开发的编码 agent 提供上下文信息。

## 项目概览

`nano-agent` 是一个用 Go 编写的轻量级 AI 驱动的代码助手。它：

- 提供 TUI 交互模式和 daemon 后台服务模式。
- 实现模块化的工具架构和基于轮次（turn-based）的对话流程。
- 支持任何兼容 OpenAI 的 LLM API，以及原生 Anthropic SDK 集成。
- 从 `~/.nano/skills/` 和项目 `.nano/skills/` 目录加载 skill。
- 通过原生后端（Linux `bwrap`、macOS `sandbox-exec`）和路径级访问控制实施文件系统沙箱隔离。

## 工具链

- **语言**：Go 1.25+
- **构建系统**：`make`
- **测试**：`go test ./...`
- **代码检查**：通过 `make lint-check` 运行 `golangci-lint`

## 常用命令

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

## 架构

- `cmd/nano/` — 主入口。
- `pkg/agent/` — 核心的基于轮次的 agent 循环、规划与推理。
- `pkg/tools/` — 工具实现（文件系统、shell、搜索、Web 等）。
- `pkg/llm/` — LLM 客户端抽象与提供商集成。
- `pkg/mcp/` — Model Context Protocol 客户端与服务器支持。
- `pkg/sandbox/` — 原生沙箱隔离与路径访问控制。
- `pkg/skill/` — skill 的发现、匹配与加载。
- `pkg/daemon/` — HTTP/WebSocket daemon 模式。
- `pkg/ui/` — 终端 UI 后端（tview、Bubble Tea）。
- `pkg/version/` — 构建期版本注入。

## 编码约定

- 显式返回错误；用 `fmt.Errorf("...: %w", err)` 包装上下文信息。
- 保持包小巧，避免循环依赖。
- 对分支逻辑使用表驱动测试（table-driven tests）。
- 绝不记录密钥（API key、token、凭据）。
- 优先使用组合而非深层继承模式。

## 测试

- 单元测试与被测代码放在一起（`*_test.go`）。
- E2E 测试位于 `e2e/`，可能需要运行中的 daemon。
- 冒烟测试位于 `smoke/`。

## 安全说明

- 即使在沙箱被禁用的情况下，沙箱默认也会阻止敏感路径（`~/.aws`、`~/.ssh`、`**/.env*` 等）。
- 文件系统工具会根据工作目录和沙箱规则校验路径。
- 由编排器（orchestrator）派生的 agent 子进程不应获得编排器的密钥。
