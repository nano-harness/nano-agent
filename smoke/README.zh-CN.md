# nano-agent 的 PTY 冒烟测试

[English](./README.md)

本目录包含针对 nano-agent TUI 和 CLI 界面的基于 PTY 的冒烟测试。

## 概述

这些测试使用 PTY（伪终端）来模拟真实用户与 nano-agent TUI 和 CLI 的交互。它们验证：
- TUI 启动与基本交互
- 斜杠命令处理
- 会话管理
- Daemon 生命周期
- 工具审批工作流

## 运行测试

```bash
# Run all smoke tests
make smoke

# Run only TUI smoke tests
make smoke-tui

# Run tests directly with go
go test -v -tags=smoke -timeout 5m ./smoke/...
```

## 测试结构

- `helpers/` — 可复用的 PTY 测试工具
  - `pty.go` — PTY 会话管理
  - `mock_llm.go` — Mock LLM 服务器封装
  - `nano_config.go` — 测试配置工厂
  - `snapshot.go` — 终端快照工具（未来）
- `testdata/` — 测试配置文件
- `*_test.go` — 冒烟测试文件

## 依赖

冒烟测试使用：
- `creack/pty` — PTY 的创建与管理
- `Netflix/go-expect` — Expect 风格的交互 DSL
- 现有的 e2e mock 服务器基础设施

## 备注

- 测试通过 `//go:build smoke` tag 隔离
- 每个测试使用临时工作目录
- Mock LLM 服务器避免了网络依赖
- 当 PTY 不可用时（例如 Windows CI），测试会自动跳过
