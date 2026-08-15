# 将 nano-agent 嵌入为执行引擎

[English](./EMBED_AS_ENGINE.md)

本文档定义了编排器（orchestrator）将 nano-agent 作为嵌入式执行引擎驱动时所支持的契约，同时保留独立的 CLI 用法。

## 调用模式

一次性任务使用 binary 模式：

```bash
nano binary exec "short prompt"
cat prompt.txt | nano binary exec
nano binary exec < prompt.txt
nano binary exec --goal "all Go package tests pass" --goal-max-turns 30 < prompt.txt
```

当编排器需要长驻进程、共享会话、WebSocket 事件或更低的冷启动开销时，使用 daemon 模式。

## 提示词输入

`nano binary exec` 接受提示词参数以保持向后兼容。当未提供提示词参数且 stdin 不是 TTY 时，它会从 stdin 读取完整提示词直至 EOF。对于大型提示词或包含敏感工作流上下文的提示词，建议优先使用 stdin，因为它可以避免 shell 引号问题、命令行长度限制以及 `ps` 暴露。

## 结构化结果契约

Binary 命令会先输出正常的人类可读/patch 内容，然后向 stdout 追加一行机器可读的 JSON：

```text
{"status":"success","tool_calls":12,"duration_ms":45000,"tokens":{"input":3200,"output":850}}
```

当目标（goal）处于激活状态时，JSON 会包含 `goal_state`，其中带有当前条件、评估计数器、token 消耗、最大轮数、最近一次判定（judge）理由，以及当判定标记目标完成时的 `achieved_at`。如果目标在达到 `max_turns` 时仍未完成，binary 模式会报告 `status=needs_retry`，以便编排器决定是重试还是放弃。

状态取值为 `success`、`needs_retry`、`abandoned` 和 `timeout`。

## 目标驱动的工作流

Binary 模式为嵌入式编排器支持目标驱动的内部循环：

- 传入 `--goal "<verifiable condition>"`；
- 可选地传入 `--goal-max-turns <n>` 以覆盖配置的轮数上限；
- 或者在提示词的第一行写上 `/goal <verifiable condition>`。该行会在其余提示词发送给 agent 之前被移除。

如果同时存在 `--goal` 和提示词中的 `/goal` 行，则以命令行标志为准，且提示词中的该行仍会被剥离。

退出码：

| 退出码 | 含义 |
|---:|---|
| 0 | success |
| 10 | needs_retry |
| 20 | abandoned |
| 30 | timeout |
| 1 | 未分类的失败 |

推荐的编排器优先级顺序为：若存在显式的 MCP 完成事件则优先使用，其次是 stdout 的 JSON 行，最后是退出码。

## MCP 与 skill 注册

`nano-agent` 不内置任何特定于编排器的 MCP server 配置或 skill。编排器可以通过 `--mcp-config` 或提供配置文件来注册自己的 MCP server，并可以通过 `NANO_ORCHESTRATOR_PROFILE` 请求 skill 自动激活。

### `NANO_ORCHESTRATOR_PROFILE`

将该环境变量设置为以逗号分隔的 skill 名称列表。在配置加载时，每个 skill 会被添加到 `skills.auto_activate` 中，并启用 skills 支持：

```bash
NANO_ORCHESTRATOR_PROFILE="nano-symphony" nano binary exec --output-dir ./out "your prompt"
```

这取代了先前硬编码的 "symphony" 编排器配置；所有 MCP server 配置现在都由编排器或用户配置提供。

### 手动 MCP 配置

```yaml
mcp:
  servers:
    - name: symphony
      transport: streamable
      url: "${env:SYMPHONY_MCP_URL}"
      headers:
        X-Symphony-Token: "${env:SYMPHONY_TOKEN}"
```

旧的 MCP transport 名称 `http` 仍被接受，作为 `streamable` 的已弃用别名。`sse` 和 `websocket` 仍然不受支持。

### 工具权限

注册 MCP 工具后，本地工具名称遵循 `mcp_<server>_<tool>` 的约定。允许/拒绝（allow/deny）模式必须使用这种形式：

```bash
nano binary exec \
  --mcp-config ./symphony.mcp.json \
  --allowedTools 'mcp_symphony_*' \
  --allowedTools 'ReadFile' \
  --output-dir ./out "prompt"
```

## 环境变量插值

配置文件支持在字符串值中使用 `${env:VAR_NAME}`。如果指定的环境变量缺失，加载将失败，这可以防止意外地使用字面占位符运行。对于不应写入磁盘的 token 和 URL，这是首选方式。

## 提示词缓存键元数据

对于重试频繁的编排器，可设置 `NANO_CACHE_KEY` 或 `SYMPHONY_ISSUE_ID`。Binary 模式会在 `~/.cache/nano/<key>/prompt-cache.json` 下记录提示词缓存元数据，使重复尝试可被观测，并为支持提示词缓存的 provider 提供一个稳定的执行键以进行协调。Anthropic 的提示词缓存继续使用系统提示词和工具 schema 中现有的 cache-control 边界。

## 沙箱模式

`nano binary exec` 和 `nano binary swebench` 支持：

```bash
--sandbox=auto|on|off
```

`auto` 是默认值。当检测到由编排器拉起的环境（例如 `SYMPHONY_WORKSPACE`、`SYMPHONY_MCP_URL` 或 `NANO_ORCHESTRATOR_PROFILE`）时，它会启用沙箱。在编排器拉起的模式下，写入被限制在当前项目路径以及运行时缓存/临时目录的例外范围内。使用 `--sandbox=off` 可恢复旧行为，或使用配置级别的沙箱路径来添加额外的允许列表。

## 故障排查

- 缺少 `${env:VAR}` 值会导致配置加载失败；请设置该变量或移除插值。
- 如果旧的 MCP 配置使用 `http`，请将其更新为 `streamable` 以消除弃用警告。
- 如果嵌入式写入被拒绝，请检查工作区路径和沙箱允许列表，或在调试期间临时使用 `--sandbox=off` 运行。
- 如果缺少 JSON 行，请检查进程日志，并回退到使用进程退出码。
