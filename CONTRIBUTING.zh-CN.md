# 为 nano-agent 做贡献

[English](./CONTRIBUTING.md)

感谢你有兴趣为项目做贡献！本文档介绍基础知识。

## 快速上手

1. 安装 Go 1.25+ 和 `make`。
2. 克隆仓库并运行 `make deps` 安装开发依赖。
3. 复制 `.env.example` 为 `.env`，如需运行二进制请设置 LLM API key。

## 开发工作流

```bash
make fmt          # 格式化代码
make test         # 单元测试
make lint-check   # 运行 linter（不自动修复）
make build        # 构建带版本信息的二进制
```

端到端测试位于 `e2e/`，可能需要运行中的 daemon（`make test-e2e`）；PTY 冒烟测试位于 `smoke/`（`make smoke`）。所有测试必须在合并前通过——`make check` 可一次性运行格式检查、vet、lint 和单元测试。

## Pull request 规范

- 保持变更聚焦且最小化。
- 变更行为时同步更新测试。
- 更新相关文档（`README.md`、`docs/` 或 `AGENTS.md`）——文档为中英双语，请保持每个 `.md` 与其 `.zh-CN.md` 对应文件同步。
- 不要提交密钥、token 或个人的 `.env` 文件。
- 使用清晰的提交信息，说明变更的*原因*。

## 代码风格

完整约定见 [AGENTS.md](./AGENTS.zh-CN.md)。简要说明：

- 显式返回错误；用 `fmt.Errorf("...: %w", err)` 包装上下文；库代码中避免 `panic`。
- Go 代码使用 `gofmt` / `goimports` 格式化（`make fmt`）。
- 保持包的小型化，避免循环依赖。
- 分支逻辑使用表驱动测试。
- 绝不记录密钥（API key、token、凭据）。
- 优先组合而非深层继承。

## 报告 issue

报告 bug 时，请附上：

- 复现步骤
- 预期行为与实际行为
- `go version` 和 `nano --version` 的输出
- 相关日志（隐去密钥等敏感信息）

## 许可证

提交贡献即表示你同意你的贡献将以 MIT License 授权发布。
