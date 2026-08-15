# 架构

[English](./ARCHITECTURE.md)

本文档概述了重构后的 nano-agent 架构，并指向各子系统的详细文档。

## 分层

本次重构在保持现有 CLI、daemon、config 和 session 兼容性的同时，将稳定的接缝（seam）收拢到职责聚焦的包中：

- `pkg/agent`：agent 编排、turn 执行、session 生命周期以及存储兼容性。
- `pkg/toolruntime`：tool 元数据、目录（catalog）以及执行运行时。
- `pkg/policy` 和 `pkg/middleware`：权限、审批、审计以及安全兼容性对外接口。
- `pkg/hookservice`：hook 加载与执行服务。
- `pkg/extension`：针对 skills、MCP servers、tools、agents 和 commands 的规范化扩展清单（manifest）。
- `pkg/event`：公共事件封装（envelope）以及与回放兼容的流式事件。
- `pkg/daemon`：HTTP/WebSocket session API、回放、取消与审批。
- `pkg/agentprofile`、`pkg/swarm`、`pkg/team` 和 `pkg/mailbox`：可配置的 teammate 以及团队运行时状态。
- `pkg/llm`：模型路由原语、fallback 以及指标。

## 依赖方向

高层接口应当向内流动：

1. CLI、TUI 和 daemon 处理器解析用户/API 输入。
2. Agent/session 控制器管理 turn 与生命周期。
3. Tool 运行时通过 policy、hook 和 audit 中间件执行已注册的 tool。
4. 存储/事件层记录可恢复的状态与公共事件。

UI 与 daemon 代码应当消费公共的 session/event 状态，而不是读取 agent 的私有内部实现。

## 兼容性规则

- 除非明确文档化了弃用（deprecation），现有的公共 CLI 命令与 daemon 端点保持兼容。
- 现有的 `pkg/tools` descriptor 与 registry API 仍然是围绕 `pkg/toolruntime` 的兼容性别名。
- 现有的 middleware hook API 仍然是围绕 `pkg/hookservice` 的兼容性包装。
- 现有的 session 仍然可读；较新的 JSONL 存储在可用时提供基于序列号（sequence）的恢复（resume）。
- 现有的 command、skill、MCP 和 teammate 声明保持纯增量（additive）演进。

## 详细子系统文档

- 配置与迁移：[Configuration Guide](../development/CONFIGURATION.md)
- 权限策略：[Permission Policy](../development/PERMISSION_POLICY.md)
- 沙箱设计：[Sandbox Design](SANDBOX_DESIGN.md)
- Hooks：[Hooks System](../features/HOOKS.md)
- Tool 运行时：[Tool Runtime](TOOL_RUNTIME.md)
- 扩展：[Extensions](../features/EXTENSIONS.md)
- 事件与审计模式：[Event Schema](../development/EVENT_SCHEMA.md)
- Daemon 运行时：[Daemon Runtime](../operations/DAEMON_RUNTIME.md)
- 多 agent 运行时：[Multi-Agent System](../features/MULTI_AGENT.md)
- 迁移指南：[Migration Guide](../migration/MIGRATION_GUIDE.md)
