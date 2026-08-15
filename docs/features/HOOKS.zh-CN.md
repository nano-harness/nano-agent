# Hooks

[English](./HOOKS.md)

Hook 是通过 `pkg/hookservice` 执行的生命周期扩展点。

## 兼容性

`pkg/middleware` 为现有的 hook 调用方保留了兼容包装器。新代码在可能的情况下应直接依赖 `pkg/hookservice.Service`。

## Hook 执行契约

Hook 会接收关于生命周期事件的标准化上下文，并可返回：

- 允许/继续（allow/continue）；
- 要求确认（require confirmation）；
- 附带原因的阻止（block with a reason）；
- 用于审计的执行元数据。

Hook 会收到旧的 `NANO_TOOL_NAME` 和 `NANO_TOOL_INPUT` 变量，以及
结构化的 `NANO_HOOK_INPUT` JSON，其中包含事件、工具名称、参数、
工作目录、环境变量白名单、沙箱标志和超时摘要。

Hook 可以向 stdout 写入结构化 JSON：

```json
{
  "action": "block",
  "reason": "dangerous command",
  "warnings": ["use a read-only command"],
  "audit_metadata": {"risk": "high"}
}
```

支持的 action 包括 `allow`、`confirm`、`block`、`emit_warning`、
`add_context`、`modify_params`、`redact_output` 和 `request_sandbox`。
退出码保持兼容：`0` 表示允许，`1` 表示要求确认，`2` 表示阻止。

`modify_params` 可以返回 `modified_params` 作为浅层参数覆盖。
对于 shell 工具，改写后的 `command` 会先由 `CommandGuard` 重新分析，
然后才能执行：

```json
{
  "action": "modify_params",
  "modified_params": {
    "command": "git status"
  },
  "audit_metadata": {"rewrite": "safe-status"}
}
```

Hook 失败和超时必须遵循配置的失败策略，而不是 panic 或绕过策略。
支持的失败策略有 `confirm`（默认）、`block`、`allow` 和
`ignore_but_audit`。

## Hook 类型（M2-3）

Hook 条目可以通过 `type` 字段声明四种执行后端之一：

| 类型      | 作用                                                                          |
|-----------|------------------------------------------------------------------------------|
| `command` | 默认。启动一个 shell 进程，并传入规范的 `NANO_HOOK_INPUT` 环境变量。            |
| `http`    | 将 JSON 信封 POST 到配置的 URL，并解析 JSON 响应。                               |
| `prompt`  | 将模板化的 prompt 发送给 LLM，并解析 `{ok,reason}` 或 `{action,...}`。          |
| `agent`   | 将决策委托给一个具名的 subagent 配置（profile）。                                |

**状态**：自本 PR 起，全部四种 hook 类型已完成接线并可正常使用。`type` 字段及子配置（`http`、`prompt`、`agent`）现在可以从 YAML 配置正确传递到运行时 hook 引擎。

HTTP hook 会强制执行主机白名单（`url_allowlist`），拒绝配置的 header 中出现 CR/LF，
绝不自动跟随重定向，并限制响应体大小
（默认 64 KB，可用 `max_response_kb` 覆盖）。

## M1F 中新接入的事件

Hook 引擎现在会触发此前仅为占位的事件：

- `pre_compact` / `post_compact`：围绕上下文压缩（context compaction）触发。
- `stop` / `stop_failure`：当一个回合（turn）成功结束或中止时触发。
- `subagent_start` / `subagent_stop` / `teammate_idle`：针对派生的队友（spawned teammates）触发。

## 安全预期

- Hook 的环境变量应使用白名单控制。
- Hook 命令应在显式超时限制下运行。
- Hook 的决策应可审计。
- Hook 的输出不应被视为可信代码。
- 高风险 hook 不应静默扩大工具权限。
- HTTP hook 的 URL 必须包含在 `url_allowlist` 中；部署时策略应在生产环境中
  拒绝空白名单。

## 相关文档

- [Permission Policy](../development/PERMISSION_POLICY.md)
- [Extension Event Schema](../development/EXTENSION_EVENT_SCHEMA.md)
- [Configuration Guide](../development/CONFIGURATION.md)
