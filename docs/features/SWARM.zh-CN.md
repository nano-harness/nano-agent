# Swarm 多 Agent 系统

[English](./SWARM.md)

Swarm 系统使 nano-agent 能够作为一个协同工作的 AI agent 团队运行：一个 team-lead agent 可以派生（spawn）并协调多个 teammate agent，并行处理复杂任务的不同方面。

## 概述

Swarm 架构由三个主要组件构成：

1. **Team-Lead Agent**：负责协调团队并委派任务的主 agent
2. **Teammate Agents**：由 team-lead 派生的子进程 agent，用于处理特定的子任务
3. **Mailbox 系统**：用于 agent 间通信的消息传递基础设施

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                        Team-Lead Agent                       │
│  - Coordinates overall task execution                        │
│  - Spawns teammates via spawn_teammate tool                  │
│  - Receives status updates via mailbox                       │
│  - Aggregates results from all teammates                     │
└──────────────┬──────────────────────────────┬───────────────┘
               │                               │
        ┌──────┴──────┐               ┌───────┴────────┐
        │ Mailbox     │               │  Mailbox       │
        │ (Team Lead) │               │  (Team Lead)   │
        └──────┬──────┘               └───────┬────────┘
               │                               │
    ┌──────────▼──────────┐         ┌─────────▼──────────┐
    │  Teammate Agent 1   │         │  Teammate Agent 2   │
    │  - Executes subtask │         │  - Executes subtask │
    │  - Sends updates    │         │  - Sends updates    │
    │  - Uses send_message│         │  - Uses send_message│
    └─────────────────────┘         └────────────────────┘
```

## 使用方法

### 1. Team-Lead REPL 模式

启动一个交互式 team-lead 会话：

```bash
nano chat --team alpha
```

这会启动一个 REPL，agent 在其中充当带有 mailbox 支持的 team-lead。该 agent 可以：
- 使用 `spawn_teammate` 工具派生 teammate
- 在每个回合自动接收来自 teammate 的消息
- 协调复杂的多步骤任务

对于基于 daemon 的长会话，先启动 daemon，然后使用 WebSocket REPL 客户端：

```bash
nano daemon start
nano lead-chat --team alpha
```

`nano lead-chat` 会创建或恢复一个 daemon team-lead 会话，将每一行 REPL 输入
作为 `lead_input` WebSocket 帧发送，并在短暂断连后从最后接收到的
序号处恢复流式输出。当某个工具需要确认时，
daemon 会在同一流上发出审批请求；`nano lead-chat`
会在终端中提示，并将决定发送回去，同时不结束当前回合。

### 2. Team-Lead TUI 模式

以 team-lead 模式启动 TUI：

```bash
nano --team beta --tui
```

或者使用 Bubble Tea TUI：

```bash
nano --team gamma --tea
```

TUI 将显示：
- 聊天界面中来自 teammate 的消息
- 团队协调状态
- 并行任务执行情况

### 3. 带团队会话的 Daemon 模式

启动 daemon：

```bash
nano daemon start
```

通过 API 创建 team-lead 会话：

```bash
curl -X POST http://localhost:4380/api/v1/teams/sessions \
  -H "Content-Type: application/json" \
  -d '{"team_name": "delta"}'
```

在团队会话中执行命令：

```bash
curl -X POST http://localhost:4380/api/v1/teams/sessions/{session_id}/execute \
  -H "Content-Type: application/json" \
  -d '{"command": "analyze the codebase and find potential bugs"}'
```

列出所有团队会话：

```bash
curl http://localhost:4380/api/v1/teams/sessions
```

## Daemon API 端点

### 创建团队会话
```
POST /api/v1/teams/sessions
Content-Type: application/json

{
  "session_id": "optional-custom-id",
  "team_name": "team-alpha",
  "interactive_confirm": true
}

Response:
{
  "session_id": "abc123",
  "team_name": "team-alpha",
  "created_at": "2026-04-25T07:00:00Z",
  "last_active_at": "2026-04-25T07:00:00Z"
}
```

### 列出团队会话
```
GET /api/v1/teams/sessions

Response:
[
  {
    "session_id": "abc123",
    "team_name": "team-alpha",
    "created_at": "2026-04-25T07:00:00Z",
    "last_active_at": "2026-04-25T07:05:00Z"
  },
  ...
]
```

### 获取团队会话
```
GET /api/v1/teams/sessions/{session_id}

Response:
{
  "session_id": "abc123",
  "team_name": "team-alpha",
  "created_at": "2026-04-25T07:00:00Z",
  "last_active_at": "2026-04-25T07:05:00Z"
}
```

### 在团队会话中执行
```
POST /api/v1/teams/sessions/{session_id}/execute
Content-Type: application/json

{
  "command": "analyze code for security vulnerabilities"
}

Response:
{
  "success": true,
  "events": [
    {"type": "content", "content": "Starting analysis..."},
    ...
  ]
}
```

### 流式订阅团队会话
```
GET /api/v1/teams/sessions/{session_id}/stream
Upgrade: websocket
```

支持的客户端帧：

```json
{"type":"subscribe","since_seq":42}
{"type":"lead_input","command":"analyze code","task_id":"optional-task-id","since_seq":42}
{"type":"tool_approval","call_id":"tool-call-id","approved":true}
{"type":"cancel"}
{"type":"ping"}
```

服务端帧包括：
- `session_start`：用于回放/订阅连接
- `lead_input_ack`：在接受 REPL 输入后发送
- `waiting_for_user`：当工具需要确认时，带有元数据 `kind=tool_approval_request`
- `tool_approval_ack`：在接受审批决定后发送
- 常规的 `StreamEvent` 帧，如 `content`、`stream_content`、`tool_call` 和 `task_completion`
- `completion`：带有 `last_seq`、`status` 和 `success`
- `error`：用于无效帧或被拒绝的输入

使用 `last_seq`/`since_seq` 可以在恢复时避免重放已渲染的事件。

### 删除团队会话
```
DELETE /api/v1/teams/sessions/{session_id}

Response:
{
  "success": true,
  "message": "Session deleted"
}
```

## 团队模式下可用的工具

### Team-Lead Agent 可用

#### spawn_teammate
派生一个新的 teammate agent 来处理子任务：

```json
{
  "tool": "spawn_teammate",
  "parameters": {
    "name": "code-analyzer",
    "task": "Analyze the authentication module for security issues"
  }
}
```

该工具会在 teammate 完成其任务时返回。

### Teammate Agent 可用

#### send_message
向 team-lead 发送状态更新或结果：

```json
{
  "tool": "send_message",
  "parameters": {
    "to": "team-lead",
    "text": "Found 3 potential SQL injection vulnerabilities",
    "topic": "finding"
  }
}
```

## Mailbox 系统

Mailbox 系统实现了 team-lead 与 teammate agent 之间的异步通信。

### 消息流程

1. **Teammate → Team-Lead**：teammate 使用 `send_message` 工具发送更新
2. **自动注入**：消息会在 team-lead 每个回合开始时自动注入
3. **消息格式**：消息包含发送者 ID、时间戳和结构化正文
4. **消息清理**：消息根据 TTL 过期，并可由消费方清除

### 消息结构

参见 `docs/features/MAILBOX.md` 获取规范的消息结构定义。

### Mailbox 存储

- **文件系统后端**：消息存储在 `~/.nano/teams/<team>/mailbox/` 下
- **原子操作**：锁文件确保并发安全
- **持久化**：消息在进程重启后仍然保留

## 配置

### 环境变量

- `NANO_DISABLE_TEAM_SESSIONS`：设置为 `true` 可在 daemon 模式下禁用 team-lead 会话

### 配置文件选项

```yaml
# Enable swarm functionality (default: true)
enable_swarm: true

# Mailbox configuration
mailbox:
  enabled: true
  backend: "memory"  # or "file" for daemon mode
  root_dir: "~/.nano/teams/<team>/mailbox"  # optional override for file backend
  max_per_agent: 1000
  ttl_days: 7
  injection_limit: 5
  injection_max_kb: 4
```

## 使用场景

### 1. 并行代码分析

```
Team-Lead: "Analyze the entire codebase for security issues"
  ├─ Teammate 1: Analyze authentication module
  ├─ Teammate 2: Analyze API endpoints
  ├─ Teammate 3: Analyze database queries
  └─ Team-Lead: Aggregate findings and create report
```

### 2. 多组件开发

```
Team-Lead: "Build a REST API with authentication"
  ├─ Teammate 1: Create database schema
  ├─ Teammate 2: Implement authentication middleware
  ├─ Teammate 3: Create API endpoints
  └─ Team-Lead: Integration and testing
```

### 3. 调研与文档编写

```
Team-Lead: "Research best practices and write documentation"
  ├─ Teammate 1: Research security best practices
  ├─ Teammate 2: Research performance optimization
  ├─ Teammate 3: Research testing strategies
  └─ Team-Lead: Synthesize findings and write comprehensive guide
```

## 会话管理

### 自动清理

团队会话在闲置 30 分钟后会自动清理（可通过 `TeamLeadRegistry` 配置）。

### 手动清理

删除一个团队会话：
```bash
curl -X DELETE http://localhost:4380/api/v1/teams/sessions/{session_id}
```

或者通过 daemon API 以编程方式删除。

## 限制与最佳实践

### 当前限制

1. **子进程通信**：teammate 作为独立进程运行，存在一定开销
2. **不支持嵌套团队**：teammate 不能派生自己的子 teammate
3. **文件系统 Mailbox**：目前仅支持基于文件系统的 mailbox
4. **Token 限制**：每个 teammate 拥有独立的 token 限制

### 最佳实践

1. **任务分解**：将任务拆分为清晰、独立的子任务交给 teammate
2. **消息频率**：teammate 应在逻辑检查点处发送进度更新
3. **错误处理**：team-lead 应优雅地处理 teammate 的失败
4. **资源管理**：限制并发 teammate 数量以避免资源耗尽
5. **干净关闭**：完成后务必清理团队会话

## 故障排查

### 问题：Teammate 收不到 mailbox 消息

**解决方案**：检查 mailbox 权限，并确保 mailbox 路径存在：
```bash
ls -la ~/.nano/teams/<team-name>/mailbox/
```

### 问题：团队会话闲置超时太短

**解决方案**：在 daemon 初始化时调整超时时间：
```go
registry := NewTeamLeadRegistry(60 * time.Minute) // 60 minutes
```

### 问题：并发 teammate 过多

**解决方案**：在 team-lead 逻辑中实现 teammate 池化或顺序执行

## 未来增强

计划在后续版本中改进：

1. **远程 Mailbox 后端**：支持 Redis、PostgreSQL，用于分布式团队
2. **团队层级**：允许 teammate 派生子 teammate
3. **团队可观测性**：用于监控团队活动的仪表盘
4. **动态团队扩缩**：根据工作负载自动扩缩 teammate
5. **团队间通信**：支持多个团队协同工作

## 迁移指南

有关从单 agent 模式迁移到 swarm 模式的详细说明，请参阅 [MIGRATION.md](./MIGRATION.md)。

## 贡献指南

在贡献 swarm 相关功能时：

1. 在 `e2e/team_session_test.go` 和 `e2e/swarm_cli_test.go` 中添加测试
2. 更新本文档
3. 遵循 `pkg/engine/`、`pkg/daemon/` 和 `pkg/swarm/` 中的现有模式
4. 确保与非 swarm 模式的向后兼容

## 另请参阅

- [README.md](./README.md) - nano-agent 通用文档
- [MIGRATION.md](./MIGRATION.md) - swarm 采用迁移指南
- `pkg/swarm/` - Swarm 实现源代码
- `pkg/daemon/team_session.go` - 团队会话管理
- `e2e/team_session_test.go` - E2E 测试示例
