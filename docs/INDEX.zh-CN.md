# 文档索引

[English](./INDEX.md)

欢迎使用 nano-agent 文档。本索引按类别整理了所有可用文档，帮助你快速找到所需内容。

## 快速上手

- [主 README](../README.md) - 项目概览、功能特性和快速上手指南
- [README（中文）](../README.zh-CN.md) - 项目概览、功能特性和快速上手指南（中文）
- [CHANGELOG](../CHANGELOG.md) - 版本历史和发布说明

## 架构

核心架构与设计文档：

- [架构概览](architecture/ARCHITECTURE.md) - 重构后的架构总结与子系统概览
- [沙箱设计](architecture/SANDBOX_DESIGN.md) - 安全沙箱实现细节
- [工具运行时](architecture/TOOL_RUNTIME.md) - 工具元数据、目录与执行运行时

## 功能特性

各功能特性专属文档：

- [多 Agent 系统](features/MULTI_AGENT.md) - 多 agent 运行时与团队协调
- [Swarm 系统](features/SWARM.md) - Swarm 多 agent 协调细节
- [Mailbox 系统](features/MAILBOX.md) - agent 间通信基础设施
- [智能编排](features/INTELLIGENT_ORCHESTRATION.md) - 智能任务分发与子 agent 路由
- [Hooks 系统](features/HOOKS.md) - Hook 加载与执行服务
- [扩展](features/EXTENSIONS.md) - skill、MCP server、工具和 agent 的扩展清单
- [MCP OAuth](features/MCP_OAUTH.md) - MCP server 的 OAuth 集成

## 运维

部署、daemon 与运维文档：

- [Daemon API](operations/DAEMON_API.md) - HTTP/WebSocket 会话 API、回放、取消与审批
- [Daemon 运行时](operations/DAEMON_RUNTIME.md) - Daemon 生命周期与运行时行为
- [WebSocket 分块](operations/WEBSOCKET_CHUNKING.md) - WebSocket 消息分块协议
- [部署指南](../deployment/README.md) - AWS EC2 部署说明

## 开发

开发者指南与配置：

- [配置指南](development/CONFIGURATION.md) - 配置选项与迁移指引
- [LLM 错误处理](../NANO.md) - 重试/回退/熔断分类与截断元数据
- [权限策略](development/PERMISSION_POLICY.md) - 权限模式、审批、审计与安全
- [权限自动审批](development/PERMISSION_AUTO_APPROVAL.md) - 安全操作的自动审批
- [事件模式](development/EVENT_SCHEMA.md) - 公开事件封装与流事件
- [扩展事件模式](development/EXTENSION_EVENT_SCHEMA.md) - 扩展专属的事件模式
- [Agent 实现](development/AGENT_IMPLEMENTATION.md) - Agent 编排与实现细节

## 迁移

版本升级迁移指南：

- [迁移指南](migration/MIGRATION_GUIDE.md) - 综合迁移指南，内容包括：
  - 架构重构迁移
  - 单 agent 到 swarm 模式迁移
  - 特定版本的迁移说明

## 集成

跨仓库契约与互操作文档：

- [Symphony 互操作契约](symphony-interop.md) - 与 nano-symphony 编排器的接口契约（退出码、stdout JSON、文件产物、flag 优先级）
- [沙箱 × 权限矩阵](sandbox-permission-matrix.md) - 跨平台的 flag 组合行为矩阵

## 测试

测试基础设施与规范：

- [E2E 测试](../e2e/README.md) - 端到端测试基础设施
- [SWE-bench 测试](../swe_bench_test/README.md) - SWE-bench 评测脚本
- [SWE-bench 测试（中文）](../swe_bench_test/README.zh-CN.md) - SWE-bench 评测脚本（中文）

## 参考资料

更多参考资料：

- [Claude Code & Gemini CLI 高级指南](references/Claude%20Code%20与%20Gemini%20CLI%20高级用法与黑科技实战指南.pdf) - 高级用法指南（PDF，中文）

## 按使用场景快速链接

### 我想要……

**开始使用 nano-agent**
- 从[主 README](../README.md) 开始
- 查阅[配置指南](development/CONFIGURATION.md)

**部署 nano-agent**
- 参见[部署指南](../deployment/README.md)
- 查阅 [Daemon API](operations/DAEMON_API.md)

**理解架构**
- 阅读[架构概览](architecture/ARCHITECTURE.md)
- 查阅[工具运行时](architecture/TOOL_RUNTIME.md)

**使用多 agent 功能**
- 阅读[多 Agent 系统](features/MULTI_AGENT.md)
- 查看 [Swarm 系统](features/SWARM.md)
- 查阅 [Mailbox 系统](features/MAILBOX.md)

**扩展 nano-agent**
- 查阅[扩展](features/EXTENSIONS.md)
- 查看 [Hooks 系统](features/HOOKS.md)
- 阅读 [Agent 实现](development/AGENT_IMPLEMENTATION.md)

**从旧版本迁移**
- 遵循[迁移指南](migration/MIGRATION_GUIDE.md)

**参与开发贡献**
- 查阅 [E2E 测试](../e2e/README.md)
- 查看[配置指南](development/CONFIGURATION.md)
- 阅读[事件模式](development/EVENT_SCHEMA.md)

## 文档组织

文档按以下类别组织：

- **architecture/** - 核心系统架构与设计
- **features/** - 各功能特性专属文档
- **operations/** - 部署与运维
- **development/** - 开发者指南与参考
- **migration/** - 迁移与升级指南
- **references/** - 更多参考资料

## 贡献

添加新文档时：

1. 将其放入合适的类别目录
2. 在本索引中更新对应链接
3. 使用清晰、描述性的标题
4. 包含指向相关文档的交叉引用
5. 遵循现有文档风格

## 获取帮助

- **Issues**：https://github.com/nano-harness/nano-agent/issues
- **文档**：本索引及其链接的文档
- **示例**：参见 `e2e/` 目录中的可运行示例
