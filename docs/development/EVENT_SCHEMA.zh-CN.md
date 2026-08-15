# 事件 Schema

[English](./EVENT_SCHEMA.md)

本文档汇总公开的事件、回放（replay）与审计（audit）schema 约定。字段级示例维护在 `docs/EXTENSION_EVENT_SCHEMA.md` 中。

## 公开流事件

公开事件的消费方应使用稳定的流事件字段：

- `type`
- `session_id`
- `run_id`
- `seq`
- `metadata`
- `payload`

CLI、TUI 与 daemon 客户端应消费统一的公开事件结构，而不是 agent 的私有状态。

## 回放

回放使用序列号（sequence number）：

- REST 团队回放：`GET /api/v1/teams/sessions/{id}/events?since_seq=N`
- WebSocket 实时订阅：`subscribe`
- WebSocket 仅回放请求：`replay`

客户端应持久化最后已处理的 `seq`，并在重新连接后从该序列号继续。

## 审批事件

工具审批请求以 `waiting_for_user` 事件表示，其内容为：

```json
{
  "kind": "tool_approval_request"
}
```

Daemon 客户端可以使用 `tool_approval`、`approve` 或 `reject` 帧进行响应。

## 审计 JSONL

审计条目包含 `schema_version`、经过脱敏处理的工具参数、执行结果、耗时，以及可选的安全决策字段。审计日志在持久化之前必须对敏感值进行脱敏。
