# 路线图

[English](./ROADMAP.md)

本路线图列出 nano-agent 正在积极探索的方向。条目不分先后，且**不承诺任何日期**——就绪时才发布。

## 基准评测

- **SWE-bench Verified 完整评测**：将当前的 31 实例子集（见 [docs/testing/SWE_BENCH.zh-CN.md](./docs/testing/SWE_BENCH.zh-CN.md)）扩展到完整的 500 题，并随结果公布单实例耗时、token 用量与成本明细。
- **pass^k 统计**：每个实例多次重复运行，报告方差与稳定性，而非单一的 pass@1 数字。

## 上下文工程

- **上下文编辑（context editing）**：超越简单截断的对话内上下文管理——对过时的工具输出进行选择性剪除与改写。
- **压缩中的制品追踪**：在上下文压缩过程中保持对文件、补丁等制品的稳定引用，确保压缩后早期工作仍可寻址。

## 可观测性

- **OpenTelemetry 导出**：遵循 `gen_ai.*` 语义约定输出 trace 与指标（每次 LLM 调用和工具执行一个 span、token 用量属性），让 nano-agent 接入现有的可观测性技术栈。

## 工具系统

- **MCP 工具懒加载**：推迟 MCP server 连接与工具 schema 加载，直到工具真正被需要时才进行，降低启动开销以及配置大量 MCP server 时的初始上下文占用。

## 如何影响路线图

在 [GitHub](https://github.com/nano-harness/nano-agent/issues) 上提交 issue 或讨论，或者挑选一个条目直接提交 PR——见 [CONTRIBUTING.zh-CN.md](./CONTRIBUTING.zh-CN.md)。
