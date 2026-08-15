# Mailbox 系统文档

[English](./MAILBOX.md)

## 概述

Mailbox 系统在 nano-agent 基于 fork 的并行执行模型中，为父 agent 与子 agent 之间提供结构化的异步消息传递。它使子 agent 能够在不阻塞执行的情况下，向父 agent 汇报进度、发现和请求。

## 核心特性

- **异步通信**：子 agent 发送消息，父 agent 在其下一轮次（turn）接收
- **简单的投递语义**：消息按收件人存储，由父 agent 在需要时提取（drain）
- **注入限制**：可配置每轮次的消息数量和大小上限
- **后端灵活性**：支持 Memory（单进程）或 File（daemon/多进程）后端
- **可观测性**：提供 EventTypeMailboxSent 和 EventTypeMailboxReceived 事件

## 配置

### 基本配置

添加到 `.nano.yaml` 或 `~/.config/nano/config.yaml`：

```yaml
mailbox:
  enabled: true
  backend: "memory"  # or "file" for daemon mode
  max_per_agent: 1000
  max_body_kb: 16
  ttl_days: 7
  injection_limit: 5
  injection_max_kb: 4
  guidance_prompt_enabled: true
  janitor_interval_sec: 60  # 1 minute; 0 to disable
```

### 配置字段

| 字段 | 默认值 | 说明 |
|-------|---------|-------------|
| `enabled` | `false` | 启用 mailbox 系统 |
| `backend` | `"memory"` | 后端类型：`"memory"`（CLI）或 `"file"`（daemon） |
| `root_dir` | `~/.nano/teams/<team>/mailbox` | file 后端的根目录 |
| `max_per_agent` | `1000` | 每个 agent 收件箱的最大消息数 |
| `max_body_kb` | `16` | 单条消息正文的最大大小（KB） |
| `ttl_days` | `7` | 消息在 janitor 清理前的存活时间（TTL） |
| `injection_limit` | `5` | 每轮次注入的最大消息数 |
| `injection_max_kb` | `4` | 每轮次注入消息的总大小上限（KB） |
| `guidance_prompt_enabled` | `true` | 在注入内容中包含引导文本 |
| `janitor_interval_sec` | `60` | janitor 清理间隔（秒）（0 = 禁用） |

### 后端选择

**Memory 后端**（CLI 推荐）：
- 单进程、内存存储
- 快速，无文件 I/O
- 进程重启后数据丢失
- 适用于：交互式 CLI 会话、测试

**File 后端**（Daemon 必需）：
- 持久化存储于 `~/.nano/teams/<team>/mailbox/`
- 进程重启后数据保留
- 使用 flock 实现并发访问
- 适用于：daemon 模式、多进程场景、崩溃恢复

## 使用场景

### 场景 1：子 agent 进度汇报

**父 agent fork 一个 investigator 子 agent：**

```
Parent: "Investigate the authentication bug"
  ↓ fork(task="Investigate auth bug", agent_type="investigate")

Child (investigator):
  - Reads codebase
  - Finds potential issue
  - send_message(to="parent", topic="finding", body={
      "file": "pkg/auth/jwt.go",
      "line": 142,
      "issue": "JWT token expiry not validated"
    })
  - Continues investigation

Parent (next turn):
  System prompt includes:
  """
  # Mailbox Messages

  ## Message from investigator-child-1
  **Topic:** finding

  **Content:**
  ```json
  {
    "file": "pkg/auth/jwt.go",
    "line": 142,
    "issue": "JWT token expiry not validated"
  }
  ```
  """

  Parent: "Thanks for the finding. I'll fix the JWT validation issue."
```

### 场景 2：任务修正请求

**子 agent 向父 agent 请求澄清：**

```
Child: "The task is ambiguous. Which authentication method should I investigate?"
  send_message(to="parent", topic="amend_task", body={
    "question": "Multiple auth methods found: JWT, OAuth2, API Key. Which one?",
    "options": ["JWT", "OAuth2", "API Key"]
  })

Parent (next turn):
  System prompt: "Child agent requests clarification..."

  Parent: "Focus on JWT authentication only."
  ↓ Re-fork child with updated task
```

### 场景 3：并发子 agent 汇报

**父 agent fork 多个子 agent，各自发送进度：**

```
Parent forks 3 children: investigate-auth, investigate-api, investigate-db

Each child independently:
  - Performs investigation
  - Sends findings via send_message

Parent (next turn):
  - Drains mailbox: receives 3 messages
  - Messages sorted by priority (high first)
  - InjectionLimit=5 allows all 3
  - Parent synthesizes findings and decides next steps
```

## 消息主题（Topic）

用于结构化通信的标准主题：

| 主题 | 使用场景 |
|-------|----------|
| `progress` | 状态更新（"已完成 30%"） |
| `finding` | 发现、洞察 |
| `amend_task` | 请求父 agent 澄清/更新任务 |

## 排序与注入

消息按收件人存储在 mailbox 后端中，并以 FIFO 顺序投递。
当父 agent 将 mailbox 消息注入 prompt 时，可以通过 `injection_limit` 和
`injection_max_kb` 限制每轮次注入的消息数量（以及总负载大小）。

## 运维注意事项

### Janitor 清理

janitor 进程定期运行，用于：
- 移除过期消息（早于 `ttl_days`）
- 清理残留的陈旧锁文件

**配置：**
```yaml
mailbox:
  janitor_interval_sec: 60  # Run every 1 minute (default)
```

**禁用 janitor**（用于测试）：
```yaml
mailbox:
  janitor_interval_sec: 0
```

### 锁文件残留

如果进程在操作中途崩溃，可能会残留锁文件：
```
~/.nano/teams/<team>/mailbox/<agent-id>.lock
```

**安全清理**（在没有 agent 运行时）：
```bash
rm ~/.nano/teams/<team>/mailbox/*.lock
```

**自动清理**：janitor 会择机查看（peek）mailbox，并依赖
后端的 TTL 过滤；陈旧锁文件的移除是尽力而为（best-effort）的，如果进程崩溃，
可能需要手动清理。

### 磁盘空间监控

对于长时间运行的 daemon，请监控：
```bash
du -sh ~/.nano/teams/<team>/mailbox/
du -sh ~/.nano/teams/<team>/mailbox/archive/
```

**建议**：在生产部署中设置日志轮转或定期归档清理。

## 故障排查

### 父 agent 中未出现消息

**症状**：子 agent 发送了消息，但父 agent 看不到。

**检查**：
1. 配置中已启用 mailbox：
   ```yaml
   mailbox:
     enabled: true
   ```
2. 父 agent 的 ID 与子 agent 消息的 `to` 字段匹配
3. 检查事件流中是否有 `EventTypeMailboxSent` 和 `EventTypeMailboxReceived`
4. 核实后端配置（如果是 file 后端，检查 `~/.nano/teams/<team>/mailbox/<parent-id>.json`）

### 磁盘占用过高

**症状**：`~/.nano/teams/<team>/mailbox/` 体积增长过大

**原因**：
1. 消息量过大
2. 消息正文过大

**解决方案**：
- 启用 janitor：`janitor_interval_sec: 300`
- 减小 `ttl_days` 以加快过期
- 减小 `max_body_kb` 以限制消息大小

### File 后端竞争

**症状**：daemon 模式下 mailbox 操作缓慢

**原因**：多个 agent 竞争文件锁。

**解决方案**：
- 监控锁获取延迟
- 考虑减小 `max_per_agent` 以缩小文件体积
- 未来规划：迁移到 Redis 后端以获得更好的并发性能

## 事件可观测性

### EventTypeMailboxSent

当子 agent 成功发送消息时触发：

```json
{
  "type": "mailbox_sent",
  "source": "agent_tool",
  "metadata": {
    "message_id": "01HX...",
    "topic": "finding",
    "from": "child-agent-1",
    "to": "parent"
  },
  "content": "Message sent: child-agent-1 -> parent (topic: finding)"
}
```

### EventTypeMailboxReceived

当父 agent 提取并注入消息时触发：

```json
{
  "type": "mailbox_received",
  "source": "mailbox_inject",
  "metadata": {
    "count": 3,
    "from_agents": ["child-1", "child-2"],
    "topics": ["finding", "progress"]
  },
  "content": "Received 3 message(s) from mailbox"
}
```

## 最佳实践

1. **使用合适的主题**：发现用 `finding`，更新用 `progress`，澄清用 `amend_task`
2. **保持消息简洁**：遵守 `max_body_kb` 限制
3. **启用 janitor**：通过定期清理防止磁盘空间问题
4. **监控事件**：使用 `EventTypeMailboxSent/Received` 进行调试和观测
5. **优雅降级**：mailbox 故障不应导致 agent 崩溃（send_message 返回错误而非抛出异常）

## 未来增强

- **Redis 后端**：面向多实例部署的分布式 mailbox
- **消息过滤**：允许父 agent 按主题/发送者过滤
- **广播消息**：向多个 agent 发送消息
- **单条消息过期**：为时效性消息覆盖全局 TTL
