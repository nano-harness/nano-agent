# Contributing to nano-agent

[English](./CONTRIBUTING.md)

感谢你有兴趣为本项目做贡献！本文档涵盖了基础知识。

## 快速上手

1. 安装 [Go](https://go.dev/dl/) 1.25 或更高版本。
2. 克隆仓库。
3. 运行 `make deps` 安装开发依赖。

## 开发工作流

```bash
make lint-check
make test
```

合并前所有测试必须通过。

## Pull request 指南

- 保持变更聚焦且最小化。
- 变更行为时同步更新测试。
- 更新相关文档（`README.md`、`docs/` 或 `AGENTS.md`）。
- 不要提交密钥、token 或个人的 `.env` 文件。
- 使用清晰的 commit message，说明*为什么*需要这个变更。

## 代码风格

- Go 代码使用 `gofmt` / `goimports` 格式化。
- 优先使用小函数和显式的错误处理。
- 在库代码中避免 `panic`；改为返回错误。
- 保持包职责聚焦，避免循环导入。

## 报告问题

报告 bug 时，请包含：

- 复现步骤
- 预期行为与实际行为
- `go version` 的输出
- 相关日志（密钥需脱敏）

## 许可证

提交贡献即表示你同意你的贡献将以 MIT 许可证授权。
