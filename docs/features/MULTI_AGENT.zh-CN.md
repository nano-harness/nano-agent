# 多智能体运行时

[English](./MULTI_AGENT.md)

多智能体运行时支持 team-lead 会话、可配置的 teammate、mailbox 以及可回放的团队事件。

## Agent profile

项目本地的 profile 存放在 `.nano/agents` 目录下,支持 YAML、JSON 或带 YAML frontmatter 的 Markdown 格式。

示例:

```yaml
description: Review code changes
initial_prompt: Review the requested patch and report risks.
permission_mode: acceptEdits
allowed_tools: [read_file, run_shell_command]
kind: in_process
color: "#00ff00"
```

Profile 由 `pkg/agentprofile` 加载。

## 显式调用

用户输入可以通过斜杠命令显式指定某个 profile:

```text
/reviewer check pkg/agent for regressions
```

斜杠命令分发器会将其重写为一个带有 profile 默认值的 `spawn_teammate` 请求。`@` 前缀现在保留给文件引用使用。

## Teammate 权限

`permission_mode` 和 `allowed_tools` 是相互独立的 teammate 约束。它们应被复制到 teammate 的 identity/config 中,且不得修改父 agent 的配置。

Agent profile 和 `spawn_teammate` 还可以设置 `model`,为单个 teammate 指定模型覆盖。对于 in-process teammate,该覆盖会应用到复制出的子配置上;对于 subprocess teammate,则通过 `nano teammate --model` 传递。

Agent profile 和 `spawn_teammate` 可以设置 `context_providers` 来约束 teammate 的上下文来源。支持的 provider 名称有 `memory`、`skills` 和 `openspec`;省略的值会继承父级的上下文配置。

## 治理限制

`spawn_teammate` 仅限 team-lead 使用。Teammate 不能再嵌套生成 teammate,默认情况下这防止了 agent 深度无限制地增长。

团队还可以限制并发活跃 teammate 的数量:

```yaml
advanced:
  fork:
    max_concurrent: 3
    max_runtime_sec: 3600
```

当 `advanced.fork.max_concurrent` 大于零时,`spawn_teammate` 会统计目标团队中活跃成员的数量,达到上限后拒绝新的生成请求。值为 `0` 或省略该设置时,保留原有的无限制行为。

当 `advanced.fork.max_runtime_sec` 大于零时,生成的 teammate 会获得一个运行时截止时间。In-process teammate 在截止时间到期后会通过其 context 被取消并标记为不活跃;subprocess teammate 则通过隐藏的 `nano teammate --max-runtime-sec` 标志获得相同的限制。

## 团队事件与回放

Team-lead 会话会存储带序号的事件,并通过 daemon 的 REST/WebSocket API 提供回放能力。客户端应使用基于序号的回放,而不是直接读取 mailbox 或会话内部数据。

## 运行时路径

用户运行时状态集中存放在 `~/.nano` 下。团队 mailbox 位于:

```text
~/.nano/teams/<team>/mailbox
```
