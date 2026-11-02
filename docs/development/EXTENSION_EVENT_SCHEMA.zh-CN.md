# 扩展与事件模式

[English](./EXTENSION_EVENT_SCHEMA.md)

本文档记录了架构重构所引入的稳定公开模式（schema）。

## 扩展清单（Extension manifest）

`pkg/extension.Manifest` 是由 `manage_extension` 以及内部扩展注册表返回的规范化视图。

```json
{
  "schema_version": "1",
  "id": "agent:reviewer",
  "name": "reviewer",
  "kind": "agent",
  "description": "Review code changes",
  "source": "/repo/.nano/agents/reviewer.yaml",
  "enabled": true,
  "installed": true,
  "permissions": [
    { "type": "agent_spawn", "scope": "in_process" },
    { "type": "permission_profile", "scope": "acceptEdits" },
    { "type": "tool_execution", "scope": "read_file" }
  ],
  "trust": {
    "trusted": true,
    "level": "local",
    "reason": "loaded from local configuration or filesystem"
  },
  "health": {
    "status": "healthy",
    "message": "agent profile discovered"
  },
  "metadata": {
    "permission_mode": "acceptEdits",
    "allowed_tools": ["read_file"]
  }
}
```

支持的 `kind` 取值：

- `skill`
- `mcp`
- `tool`
- `agent`
- `command`

Agent 清单同时包含运行时 agent 工具和 `.nano/agents` 配置文件（profile）。Command 清单来自 `.nano/commands` 以及兼容的 `.claude/commands`。

信任级别（Trust levels）：

- `runtime`：在当前进程中注册。
- `local` / `configured`：从本地配置或文件系统加载。
- `remote`：HTTPS 远程来源，在显式确认之前不被信任。
- `remote_insecure`：明文 HTTP 远程来源，不被信任，应升级为 HTTPS。

当没有可用的确认处理器（confirmation handler）时，`manage_extension` 会拒绝远程安装/更新操作，因此不受信任的远程扩展无法被静默添加。

## AgentProfile 配置

项目级 agent 配置文件位于 `.nano/agents` 目录下，可以是 YAML、JSON，或带有 YAML frontmatter 的 Markdown。

```yaml
name: reviewer
description: Review code changes
initial_prompt: Review the requested patch and report risks.
permission_mode: acceptEdits
allowed_tools: [read_file, run_shell_command]
model: gpt-4.1
kind: in_process
color: "#00ff00"
```

现有配置无需迁移。`.nano/agents` 是增量式的，旧的静态子 agent 配置仍然受支持。当配置文件声明了 `allowed_tools` 时，这些工具将成为被派生出的 teammate 独立的启用工具集。

## 斜杠命令元数据

自定义斜杠命令支持：

```yaml
allowed-tools: [run_shell_command]
permission-profile: acceptEdits
```

daemon 的 `/api/v1/commands` 响应将这些字段暴露为 `allowedTools` 和 `permissionProfile`。

## 审计 JSONL 模式

由 `pkg/middleware.AuditMiddleware` 写入的审计条目包含一个模式版本号：

```json
{
  "schema_version": "1",
  "ts": "2026-04-30T12:00:00Z",
  "tool": "run_shell_command",
  "params": { "command": "git status" },
  "success": true,
  "duration_ms": 12,
  "session_id": "session_abc",
  "security_decision": {
    "action": "allow",
    "reason": "session allowlist",
    "rule": "run_shell_command(git status)",
    "layer": 2,
    "confidence": 1,
    "suggestions": [],
    "auto_whitelist": false
  }
}
```

代码中的模式描述符是 `middleware.AuditSchema()`。

## 事件回放与审批

Team-lead 流支持：

- `subscribe`：用于回放加实时事件。
- `replay`：用于仅回放的使用场景。
- `tool_approval`：携带 `approved`。
- `approve` / `reject`：用于显式 UI 操作的别名。

回放状态基于序号（`seq` / `since_seq`），并且 CLI、TUI 与 daemon 消费方使用相同的公开 `event.StreamEvent` 信封（envelope）。
