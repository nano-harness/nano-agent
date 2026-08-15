# 迁移指南

[English](./MIGRATION_GUIDE.md)

本指南涵盖 nano-agent 的所有迁移场景，包括架构重构与功能升级。

## 目录

1. [架构重构迁移](#architecture-refactor-migration)
2. [单 Agent 到 Swarm 模式迁移](#single-agent-to-swarm-mode-migration)
3. [特定版本迁移](#version-specific-migration)

---

## 架构重构迁移

本节将架构重构计划映射为保持兼容性的迁移步骤。

### 迁移原则

- 在引入新接缝（seam）的同时，保持旧的公开入口点可用。
- 每次只迁移一条调用链。
- 在删除旧代码之前，优先使用适配器与兼容别名。
- 在修改内部实现之前，先为公开行为补充测试。
- 为新行为发出稳定的公开事件与审计记录。

### 已完成的迁移接缝

- 工具元数据与执行已迁移到 `pkg/toolruntime` 之后。
- Hook 执行已迁移到 `pkg/hookservice` 之后。
- Turn 执行获得了内部执行器接缝。
- Session 获得了显式的生命周期状态与增量 JSONL 恢复支持。
- 扩展清单统一了 skill、MCP、tool、agent 与 command 的元数据。
- 斜杠命令获得了作用域化的 tool/permission 元数据。
- AgentProfiles 增加了项目本地的可配置队友（teammate）。
- Daemon 团队 session 获得了回放（replay）与审批（approval）兼容帧。
- 配置迁移指南位于 `docs/development/CONFIGURATION.md`。

### 配置迁移

现有配置无需迁移。新增字段与目录均为增量添加：

- 用于斜杠命令的 `.nano/commands` 以及兼容的 `.claude/commands`。
- 用于 AgentProfiles 的 `.nano/agents`。
- 命令 frontmatter 中的 `allowed-tools` 与 `permission-profile`。
- AgentProfile 字段 `permission_mode` 与 `allowed_tools`。

### 验证清单

在合并重构变更之前：

- 运行 lint 与单元测试。
- 针对变更涉及的包运行聚焦测试。
- 当 daemon/session/tool 相关接口发生变化时，运行完整的 e2e 测试。
- 针对权限、hook、扩展、沙箱与审计变更运行安全验证。

---

## 单 Agent 到 Swarm 模式迁移

本指南帮助你从单 agent 的 nano-agent 用法迁移到新的 Swarm 多 agent 系统。

### v3 → v4：EventSource TUI 与 Daemon 客户端迁移

#### 已移除的命令及替代方案

- `nano lead-chat` 不再使用 readline/纯文本渲染；默认启动 BubbleTea TUI。
- 使用 `nano lead-chat --ui tview` 选择 tview 后端。
- 脚本与 CI 必须使用 `nano daemon execute --json "your command"`，而不是解析交互式流输出。

#### 适配器接口迁移

旧的 UI 适配器实现 `Run`、`SendEvent`、`SubmitChannel` 与 `CancelChannel`。新的适配器只实现 `Run(ctx, EventSource) error` 与 `Stop`。请将执行、取消、审批、重置以及 session 列表等行为放入 `eventsource.EventSource` 实现中。

#### Daemon 渲染迁移

不要在 CLI 路径中用 `fmt.Print` 渲染 daemon WebSocket 帧。应在 daemon WebSocket 之上构建一个 EventSource，将入站帧喂给 BubbleTea/tview，并将用户操作作为 submit/cancel/approval/control 出站消息发送回去。

### 概览

Swarm 模式引入了基于团队的 agent 协调机制，允许 team-lead agent 派生并协调多个 teammate agent。本指南涵盖：

1. 理解差异
2. 更新你的工作流
3. 迁移现有代码
4. 新能力

### 变更内容

#### 阶段 1：运行时层（已完成）

**Mailbox 系统**：
- 用于 agent 间通信的新 mailbox 基础设施
- 基于文件系统的消息存储，位于 `~/.nano/teams/<team>/mailbox/`（旧的 `~/.nano-agent/` 路径会在首次使用时自动迁移）
- 工具：供 teammate 使用的 `send_message`

**影响**：极小 —— Mailbox 是可选的，不影响单 agent 用法

#### 阶段 2：Agent 工具层（已完成）

**新工具**：
- `main_agent`：使用 main agent 能力执行任务
- `spawn_teammate`：team-lead 可以派生 teammate agent（未来）
- `send_message`：teammate 可以向 team-lead 发送消息

**变更**：
- `task` 工具已移除（由 `main_agent` 替代）
- `fork` 工具已移除（由 spawn_teammate 取代）

**影响**：中等 —— 如果你使用了 `task` 或 `fork` 工具，需要进行更新

#### 阶段 3：Daemon 与 TUI 集成（当前）

**Daemon 增强**：
- 通过 HTTP API 进行 team-lead session 管理
- 支持 mailbox 的长时运行团队 session
- 通过 `/api/v1/teams/sessions/{id}/stream` 提供 WebSocket 团队 REPL 流
- 使用 `lead_input` 帧的 `nano lead-chat` daemon 后端 REPL 客户端
- 使用 `waiting_for_user` 与 `tool_approval` 帧的 REPL 驱动 daemon 工具确认
- 空闲超时后自动清理 session

**TUI 增强**：
- TUI 模式的 `--team` 标志
- 用于 team-lead REPL 的 `nano chat --team <name>`
- 用于 daemon 后端可恢复 team-lead REPL 的 `nano lead-chat --team <name>`
- 用于子进程执行的 `nano teammate` 命令

**影响**：低 —— 新功能是增量添加的，现有功能不变

#### 阶段 4：运行时主目录统一（破坏性变更）

**运行时主目录**：
- 此前存储在 `~/.nano-agent/` 下的所有运行时状态现在集中到 `~/.nano/`
- 团队状态位于 `~/.nano/teams/<team-name>/`
- 团队 mailbox 位于 `~/.nano/teams/<team-name>/mailbox/`
- Daemon session 位于 `~/.nano/sessions/`

**自动迁移**：
- 首次使用时，`~/.nano-agent/` 中的内容（`README.md` 除外）会被移动到 `~/.nano/`
- `~/.nano-agent/` 中会留下一个指向新位置的 `README.md` 占位文件
- 迁移是幂等的，每个用户主目录只执行一次（由 `sync.Once` 保护）
- 实现参见 `pkg/runtime/paths.go::MigrateLegacyPaths`

**需要的操作**：
- 将任何硬编码 `~/.nano-agent/` 路径的工具更新为使用 `~/.nano/`
- 将 mailbox 的 `root_dir` 配置项更新为指向 `~/.nano/teams/<team>/mailbox`（或留空以使用默认值）
- 确认 `~/.nano/` 已包含迁移后的状态后，删除 `~/.nano-agent/`

**影响**：中等 —— 文件路径发生变化，但状态会在首次使用时自动迁移

### 迁移路径

#### 路径 1：继续使用单 Agent 模式（无需变更）

如果你不需要多 agent 协调，**无需任何迁移**。单 agent 模式与之前完全一样：

```bash
# These commands work unchanged
nano "fix the bug"
nano --tui
nano daemon start
```

新的 swarm 功能是可选启用的，不影响现有工作流。

#### 路径 2：采用 Team-Lead 模式（渐进式迁移）

通过在复杂任务中使用 team-lead 模式来渐进迁移：

**之前（单 Agent）**：
```bash
nano "analyze the entire codebase for security issues"
```

单个 agent 按顺序处理所有事情。

**之后（Team-Lead）**：
```bash
nano --team security-team --tui
```

然后在 TUI 中：
```
analyze the codebase for security issues - spawn teammates to check different modules
```

team-lead 可以将任务委派给 teammate 进行并行处理。

#### 路径 3：完全采用 Swarm（高级）

使用 daemon API 进行编程化的团队管理：

```python
import requests

# Create team session
response = requests.post('http://localhost:4380/api/v1/teams/sessions', json={
    'team_name': 'security-team'
})
session_id = response.json()['session_id']

# Execute with team coordination
requests.post(f'http://localhost:4380/api/v1/teams/sessions/{session_id}/execute', json={
    'command': 'comprehensive security audit with parallel module analysis'
})
```

### 具体迁移场景

#### 场景 1：你使用了 `task` 工具

**之前（阶段 1）**：
```json
{
  "tool": "task",
  "parameters": {
    "instruction": "analyze code quality",
    "context": "focus on main.go"
  }
}
```

**之后（阶段 2+）**：
```json
{
  "tool": "main_agent",
  "parameters": {
    "task": "analyze code quality in main.go"
  }
}
```

**变更原因**：`main_agent` 提供了更清晰的语义，并更好地契合 team-lead/teammate 架构。

#### 场景 2：你使用了 `fork` 工具

**之前（阶段 1）**：
```json
{
  "tool": "fork",
  "parameters": {
    "session_id": "parallel-task-1",
    "command": "run tests"
  }
}
```

**之后（阶段 3+）**：
在 team-lead 模式中使用 `spawn_teammate`：
```json
{
  "tool": "spawn_teammate",
  "parameters": {
    "name": "test-runner",
    "task": "run all unit tests and report results"
  }
}
```

**变更原因**：teammate 提供了更好的生命周期管理、自动 mailbox 集成以及更清晰的语义。

### 破坏性变更

#### 阶段 2 破坏性变更

1. **已移除的工具**：
   - `task` → 改用 `main_agent`
   - `fork` → 在 team-lead 模式中使用 `spawn_teammate`

2. **工具注册**：
   - `RegisterAgentTools()` 现在只注册 `main_agent` 和 `send_message`
   - Swarm 工具（spawn_teammate 等）在 team-lead 模式下单独注册

#### API 兼容性

daemon API 保持向后兼容。新端点为增量添加：

**现有 API（不变）**：
```
POST /api/v1/sessions/execute
GET  /api/v1/sessions
GET  /api/v1/sessions/{id}
DELETE /api/v1/sessions/{id}
```

**新 API（可选启用）**：
```
POST /api/v1/teams/sessions
GET  /api/v1/teams/sessions
GET  /api/v1/teams/sessions/{id}
POST /api/v1/teams/sessions/{id}/execute
DELETE /api/v1/teams/sessions/{id}
```

### 配置更新

#### 无需配置变更

Swarm 功能可配合现有配置工作。可选的增强配置：

```yaml
# Optional: Disable team sessions in daemon
# Set environment variable NANO_DISABLE_TEAM_SESSIONS=true

# Optional: Configure mailbox (defaults shown)
mailbox:
  enabled: true
  backend: "file"  # or "memory" for CLI sessions
  root_dir: "~/.nano/teams/<team>/mailbox"  # optional override
  max_per_agent: 1000
```

### 测试你的迁移

#### 1. 测试单 Agent 模式仍然可用

```bash
# Should work unchanged
nano "simple task"
nano --tui "interactive task"
```

#### 2. 测试 Team-Lead 模式

```bash
# Test chat command
nano chat --team test-team
> exit

# Test TUI with team flag
nano --team test-team --help
```

#### 3. 测试 Daemon 团队 Session

```bash
# Start daemon
nano daemon start

# Test team session API
curl -X POST http://localhost:4380/api/v1/teams/sessions \
  -H "Content-Type: application/json" \
  -d '{"team_name": "test"}'
```

#### 4. 运行测试

```bash
# Run E2E tests
go test -tags=e2e ./e2e -run "TeamSession|Swarm" -v
```

### 回滚方案

如果你在使用 swarm 功能时遇到问题：

#### 方案 1：在 Daemon 中禁用团队 Session

```bash
NANO_DISABLE_TEAM_SESSIONS=true nano daemon start
```

#### 方案 2：使用之前的版本

```bash
# Checkout specific version before swarm
git checkout <commit-before-swarm>
go build ./cmd/nano
```

#### 方案 3：避免使用团队功能

只需不使用：
- `--team` 标志
- `nano chat` 命令
- 团队 session API 端点

单 agent 模式不受影响。

### 常见问题与解决方案

#### 问题 1："main_agent tool not found"

**原因**：在阶段 1 的代码上使用了阶段 2+ 的预期行为

**解决方案**：确保你的 agent 正确注册了工具：
```go
import "github.com/nano-harness/nano-agent/pkg/tools/agent"

// Register agent tools
agent.RegisterAgentTools(registry, cfg, mainAgent)
```

#### 问题 2：Mailbox 权限错误

**原因**：`~/.nano/teams/<team>/mailbox/` 目录权限不正确

**解决方案**：
```bash
chmod -R 755 ~/.nano/teams/
```

#### 问题 3：团队 session 未出现在列表中

**原因**：session 可能因空闲超时已被清理

**解决方案**：检查超时配置，或更频繁地执行命令

#### 问题 4：Teammate 命令执行失败

**原因**：缺少必需的标志

**解决方案**：确保提供了所有必需的标志：
```bash
nano teammate --team alpha --name worker-1 \
  --session sess_123 --initial-prompt-file /tmp/prompt.txt
```

---

## 特定版本迁移

### 获取帮助

- **文档**：详细的 swarm 文档参见 [Multi-Agent Runtime](../features/MULTI_AGENT.md)
- **问题反馈**：在 https://github.com/nano-harness/nano-agent/issues 报告问题
- **示例**：可运行的示例见 `e2e/team_session_test.go`

### 总结

- **无强制迁移**：单 agent 模式照常工作
- **可选启用的功能**：团队功能是增量添加的
- **渐进采用**：需要协调时使用 `--team` 标志
- **向后兼容**：现有 API 与命令照常工作
- **新能力**：并行执行、专业化 teammate、状态更新

Swarm 系统在不干扰现有工作流的前提下增强了 nano-agent。随着需求增长逐步采用即可。
