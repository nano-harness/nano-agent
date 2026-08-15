# Changelog

[English](./CHANGELOG.md)

nano-agent 的所有重要变更都将记录在本文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/en/1.0.0/)，
本项目遵循 [语义化版本](https://semver.org/spec/v2.0.0.html)。

## [0.8.8] - 2026-06-16

### 新增
- **社区文件**：`AGENTS.md`、`CONTRIBUTING.md` 和 `CODE_OF_CONDUCT.md`。

### 变更
- **移除编排器耦合**：nano-agent 不再硬编码 `nano-symphony` profile 或 skill。编排器专属 skill 的自动激活改由 `NANO_ORCHESTRATOR_PROFILE`（逗号分隔的 skill 名称）驱动。
- **契约简化**：移除了内置的 `nano-symphony` skill；agent 通过通用二进制结果契约和 MCP 工具命名约定（`mcp_<server>_<tool>`）传递结果。

### 修复
- **sandbox**：移除了文件系统工具的工作目录硬性围栏；跨目录读写不再被错误拒绝
- **agent**：满意度/目标评估器的 JSON 解析失败不再将分数重置为 0；保留之前的分数
- **agent**：二进制 sentinel 现在为所有强制终止路径包含 `termination_cause` 和 `blocker_fingerprint`

### 变更
- **sandbox**：默认敏感文件黑名单现在即使在沙箱禁用时也会强制执行
- **sandbox**：默认阻止列表扩展至包含 `~/.aws`、`~/.ssh`、`~/.gnupg`、`~/.kube`、`~/.docker`、`**/.env*`、`**/credentials`、`**/*.pem`、`**/*.key`
- **sandbox**：在阻止 nano 配置文件的同时，显式允许 `~/.nano/skills/**`
- **agent**：满意度阈值现在可通过 `turn.satisfaction_threshold` 配置（默认 0.7，原为硬编码 0.95）
- **cli**：二进制模式的 `allowed_paths` 默认值现在除项目路径外还包含 `TempDir` 和 `UserCacheDir`
- **cli**：沙箱自动启用现在会向 stderr 输出生效配置日志

### 性能
- **agent**：上下文压缩热路径日志降级为 DEBUG（使 nano.log 的 INFO 级别日志量减少约 60%）
- **agent**：消除了冗余的 token 估算：`Status()` 不再调用 `ShouldCompress`；流式 TokenStats 节流为每 10 个 chunk 更新一次
- **agent**：每轮 LLM 调用的 token 估算次数从 O(N chunks) 降至 O(1)

### 安全
- **sandbox**：敏感文件黑名单的强制执行得到改进，支持 home 目录路径展开和 doublestar 模式匹配
- **sandbox**：在拒绝消息中新增提示："use run_shell_command if you genuinely need access"

### 破坏性变更 / UI Daemon 重构
- BREAKING ui：`Adapter` 现在使用 `Run(ctx, EventSource) error`；`SendEvent`、`SubmitChannel` 和 `CancelChannel` 已被移除。
- BREAKING cli：移除了 `lead-chat` 的 readline/纯文本渲染和 daemon 流式 `fmt.Print` 路径；脚本请改用 `nano daemon execute --json`。
- feat(ui)：新增了供 BubbleTea 和 tview 使用的共享 EventSource 抽象。
- feat(daemon-client)：为渲染器持有的 daemon 流暴露了 WebSocket URL 和 team cancel 辅助方法。
- docs(daemon-api)：将 daemon API 文档重写为 Web 客户端实现指南。

### 新增
- `APIErrorInfo` 新增 `ShouldFailback` 字段，并设有专门的 `ContextOverflow`、`Aborted` 和 `OutputFormat` API 错误类别。
- 流事件元数据新增 `truncated` 和 `finish_reason`，用于检测 `finish_reason=length`。
- 新增 `advanced.circuit_breaker.exclude_non_failback` 和 `advanced.circuit_breaker.truncation_detection` 配置项。
- **多 Agent 信箱系统（Multi-Agent Mailbox System）**：为基于 fork 的并行执行提供异步消息传递基础设施
  - 核心接口：`Mailbox`、`Backend`、`Manager`，用于结构化的 agent 间通信
  - 内存后端，用于单进程 CLI 模式和测试
  - 文件后端，采用 JSONL + flock，用于 daemon 模式并支持崩溃恢复
  - `send_message` 工具，供子 agent 与父 agent 通信
  - 支持消息主题：`progress`、`finding`、`amend_task`
  - 速率限制：每次 agent 运行最多 20 条消息
  - 可配置的 TTL（默认 7 天）和容量限制（默认 1000 条消息）
- **增强版 Bubble Tea Banner**：全新电影感动态 banner，采用 Standard Figlet 细线字体
  - 20 帧动画（约 3000ms），包含原子吉祥物、NANO-AGENT 文字和扫光效果
  - 用优雅的细线 ASCII 艺术替换了旧的制表符边框帧
  - 新增 `ElemSubtitle` 语义颜色角色，用于暗灰色副标题
- **专家系统（Expert System）**：与 Gemini CLI 对齐的全新专家/子 agent 架构
  - 三个内置专家：`@investigator`、`@help`、`@generalist`
  - 显式 `@expert-name` 触发语法（仅限用户，LLM 无法直接调用）
  - 通过 `~/.config/nano/agents/` 和 `.nano/agents/` 中的 markdown 文件支持自定义专家
  - `/agents` 斜杠命令，用于列出和查看可用专家
  - 专家事件类型：`expert_started`、`expert_progress`、`expert_finished`

### 变更
- **破坏性变更**：子 agent 现在仅使用 `@kebab-case-name` 触发语法
  - 旧语法（`使用[xxx]`、`with:xxx`、隐式触发）不再支持
  - YAML 中的 `sub_agents` 配置仍然有效，但需要通过 `@name` 触发
  - 名称会自动转换为 kebab-case（例如 `myAgent` → `@my-agent`）

### 修复
- 上下文溢出、认证、中止和输出格式错误不再污染熔断器（circuit breaker）的失败计数器。

### 移除
- **破坏性变更**：移除了 `fork` 工具 — LLM 不再能自主 fork 子 agent
  - 所有子 agent 调用必须由用户通过 `@expert-name` 显式触发
  - 提升可观测性：用户始终知道专家何时被调用以及其开销
