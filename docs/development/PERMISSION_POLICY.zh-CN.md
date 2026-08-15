# 权限策略

[English](./PERMISSION_POLICY.md)

本文档记录了重构时期的权限与审批模型。

## 权限来源

权限来源于：

- 全局配置和 CLI 标志；
- 工具元数据和工具类别；
- 命令 frontmatter，例如 `allowed-tools` 和 `permission-profile`；
- AgentProfile 字段，例如 `permission_mode` 和 `allowed_tools`；
- hook 决策；
- 沙箱的路径/网络/进程检查；
- daemon 或 TUI 的用户审批决策。

更具体的作用域应当收窄更宽泛的作用域，而不是静默地扩大它。

## 决策流程

1. 对请求的工具操作和参数进行规范化。
2. 应用命令级或 teammate 级的工具限制。
3. 运行静态安全检查和已配置的允许/拒绝规则。
4. 通过 `pkg/hookservice` 运行 hooks。
5. 在需要时请求用户审批。
6. 在启用审计中间件时，将最终决策记录到审计 JSONL 中。

决策动作包括：

- `allow`：立即执行。
- `confirm`：等待用户明确批准。
- `block`：拒绝执行。

## 作用域工具限制

Slash 命令可以声明：

```yaml
allowed-tools: [run_shell_command]
permission-profile: acceptEdits
```

Agent profile 可以声明：

```yaml
permission_mode: acceptEdits
allowed_tools: [read_file, run_shell_command]
```

对于 teammate，`permission_mode` 和 `allowed_tools` 独立于父 agent 应用。teammate profile 不得通过修改共享配置来扩大父进程的权限。

## 审批兼容性

Daemon 团队会话支持以下审批帧：

- `tool_approval`
- `approve`
- `reject`

审批请求还会以 `waiting_for_user` 事件的形式可见，其 `metadata.kind=tool_approval_request`。

## 审计字段

审计条目包括：

- schema 版本；
- 工具名称；
- 脱敏后的参数；
- 成功/错误状态；
- 持续时间；
- 可选的 `security_decision`。

诸如 token、密码、secret 和 API key 等敏感键在写入审计 JSONL 之前会被脱敏。
