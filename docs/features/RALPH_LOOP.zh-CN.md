# Ralph-loop

[English](./RALPH_LOOP.md)

Ralph-loop 允许 Stop hook 通过返回 block 决策来请求再运行一个 agent 轮次，例如：

```json
{"decision":"block","reason":"Continue with the remaining checklist"}
```

启用后，nano-agent 会将 hook 的 reason 作为下一次用户输入，并在保留之前对话历史的情况下开始新的一轮。

## 配置

```yaml
hooks:
  ralph:
    enabled: true
    max_iterations: 10
    hard_max_iterations: 50 # values above 50 are clamped to the built-in safety cap
```

将 `hooks.ralph.enabled: false` 设为 false 可以阻止 Stop hook 的 block 决策重新启动轮次。

## Stop hook 载荷

Stop hook 通过 `NANO_HOOK_INPUT` 接收与 Claude Code 兼容的字段：

- `hook_event_name`
- `session_id`
- `transcript_path`
- `cwd`
- `stop_hook_active`
- `iteration`

`stop_hook_active=true` 表示当前轮次已经由 ralph-loop 启动。Hook 在此状态下不应再返回 block；nano-agent 也会忽略此类 block，以防止递归。

## 会话记录（Transcript）

每个会话都会向以下文件追加 JSONL 记录：

```text
~/.nano-agent/sessions/<session_id>/transcript.jsonl
```

长会话可能会变得很大。删除会话会同时移除其 transcript 目录。

## 高级 hook 字段

Hook 输出支持：

- `decision`：`block`、`approve` 或 `continue`；优先级高于 `action`
- `systemMessage`：以警告/状态事件形式呈现的状态文本
- `continue: false`：显式允许本轮结束
- `suppressOutput`：记录为 hook 元数据

Hook 配置支持：

- `once: true`：每个服务生命周期内只运行一次 hook
- `async: true`：在后台运行命令型 hook
- `async_rewake: true`：后台命令以退出码 `2` 结束时发送 mailbox 唤醒消息
- `status_message`：用于 hook 集成的静态状态文本元数据
