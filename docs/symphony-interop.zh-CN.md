# nano-agent 二进制执行契约

[English](./symphony-interop.md)

> 本文档固化了 `nano-agent` 二进制执行（`nano binary exec`、`nano binary swebench`）的稳定接口契约。
> `nano-symphony` 等编排器（orchestrator）依赖这些不变量。
> 以下条目的任何破坏性变更**必须**与调用方协调进行。

---

## 1. 退出码语义

| 退出码 | 状态           | 含义                                              |
|--------|----------------|---------------------------------------------------|
| `0`    | `success`      | 任务正常完成                                      |
| `1`    | （未分类）      | 意外 / 未处理的失败                                |
| `10`   | `needs_retry`  | 瞬时失败（限流、可恢复的超时）                     |
| `20`   | `abandoned`    | 永久失败（分类器锁定、panic 等）                   |
| `30`   | `timeout`      | 超过硬性截止时间                                   |

来源：`pkg/cli/binary.go` 中的常量 `binaryExitSuccess`、`binaryExitRetry`、
`binaryExitAbandoned`、`binaryExitTimeout`、`binaryExitUnclassified`。

编排器读取退出码来决定重试 / 放弃 / 成功的处置方式。

---

## 2. stdout 协议（最后一行 JSON）

`nano binary exec` 和 `nano binary swebench` **始终**向 stdout 输出恰好一个紧凑的 JSON 对象——即**二进制结果摘要**。
它是 stdout 输出的**最后一行**。调用方应将最后一个非空行解析为 JSON。

### Schema：`binaryResultSummary`

```jsonc
{
  "status": "success" | "needs_retry" | "abandoned" | "timeout",
  "reason": "human-readable explanation (optional)",
  "termination_cause": "natural_completion" | "llm_failure" | "goal_max_turns" | "classifier_lockout" | "error_threshold" | "",
  "blocker_fingerprint": "stable identifier for dedup (optional)",
  "blocked_commands_sample": ["cmd1", "cmd2"],
  "tool_calls": 42,
  "duration_ms": 12345,
  "tokens": {
    "input": 10000,
    "output": 2000
  },
  "goal_state": { /* nullable, GoalState object */ },
  "cache_key": "sanitized_key (optional)"
}
```

**关键不变量**：即使在 panic、超时或出错退出的情况下，进程也**必须**向 stdout 输出一行合法的 JSON，至少包含：

```json
{"status":"abandoned","reason":"<error description>","duration_ms":0,"tool_calls":0,"tokens":{"input":0,"output":0}}
```

如果 stdout 的最后一行不是合法 JSON，编排器可以回退到 `no_result_payload` 分类，并应用其自身的重试逻辑。

### 结果文件镜像

相同的 JSON 会逐字节一致地写入 `<output-dir>/result.json`。

---

## 3. 文件产物

所有产物都写入 `--output-dir` 目录下（由编排器调用时通常为 `<workspace>/.nano-out/`）。

| 文件                | 格式               | 生命周期                                      |
|---------------------|--------------------|-----------------------------------------------|
| `result.json`       | 紧凑 JSON          | 始终写入（与 stdout 逐字节一致）               |
| `solution.patch`    | 统一 diff 格式     | 仅当工作区存在变更时写入                       |
| `trajectory.json`   | JSON 数组          | 在成功或部分完成时写入                         |
| `sessions/`         | 内部会话数据库     | 设置了 --output-dir 时写入                     |

### `solution.patch`

- 由 `pkg/patch.Generator.GenerateGitDiff()` 生成。
- diff 基于 `NANO_BASE_COMMIT` 环境变量（未设置时默认为 HEAD）。
- 编排器可将此文件作为主要交付物读取。
- 如果该文件**不**存在，则本次运行未产生任何代码变更。

### `trajectory.json`

- 经过美化打印的 `trajectoryEvent` 对象 JSON 数组。
- 用于调试和 SWE-bench 评估；不属于主要契约的一部分。

---

## 4. 标志 / 环境变量优先级

### 4.1 权限模式

解析顺序（优先级从高到低）：

| 优先级 | 来源                                | 示例                                 |
|--------|-------------------------------------|--------------------------------------|
| 1      | `--dangerously-skip-permissions`    | 强制使用 `yolo`                      |
| 2      | `--permission-mode=<mode>`          | `--permission-mode=acceptEdits`      |
| 3      | `NANO_PERMISSION_MODE` 环境变量     | `NANO_PERMISSION_MODE=auto`          |
| 4      | 配置文件 `permission_mode:`         | 位于 `~/.config/nano/config.yaml`    |
| 5      | 默认值                              | `default`                            |

有效模式：`default`、`acceptEdits`、`plan`、`auto`、`yolo`

来源：`pkg/cli/permission_resolver.go` → `ResolvePermission()`

### 4.2 Sandbox 配置

解析顺序：

| 优先级 | 来源                              | 效果                                            |
|--------|-----------------------------------|-------------------------------------------------|
| 1      | `--sandbox=off`                   | 无论环境变量如何都禁用 sandbox                   |
| 2      | `--sandbox=on`                    | 使用原生后端启用 sandbox                         |
| 3      | `--sandbox=auto`（默认）           | 仅当由编排器启动时启用*                          |
| 4      | `NANO_SANDBOX_ENABLED` 环境变量   | 覆盖配置中的 `sandbox.enabled`                   |
| 5      | `NANO_SANDBOX_BACKEND` 环境变量   | 覆盖配置中的 `sandbox.backend`                   |
| 6      | `NANO_SANDBOX_NETWORK_ACCESS` 环境变量 | 覆盖配置中的 `sandbox.network_access`       |
| 7      | 配置文件 `sandbox:` 节            | 基础配置                                        |

*当设置了以下任一环境变量时，即判定为由编排器启动的执行：
`SYMPHONY_WORKSPACE`、`SYMPHONY_MCP_URL`、`NANO_ORCHESTRATOR_PROFILE`。
这些键是已文档化的 CLI 契约的一部分，不得更改。

**重要**：`--sandbox` 标志优先于 `NANO_SANDBOX_*` 环境变量。
传入 `--sandbox=off` 时，这些环境变量会被忽略。
传入 `--sandbox=on` 时，环境变量仍可能影响 `backend` 和 `network_access` 子设置
（通过 `pkg/config` 的环境变量覆盖层生效）。

来源：`pkg/cli/binary.go` → `applyBinarySandboxMode()`；`pkg/config/config.go`
→ 环境变量覆盖。

### 4.3 会话 ID

解析顺序：

| 优先级 | 来源                          | 示例                            |
|--------|-------------------------------|---------------------------------|
| 1      | `--session-id=<id>`           | 显式指定每次调用的 ID           |
| 2      | `NANO_SESSION_ID` 环境变量    | 在多次重试间保持稳定            |
| 3      | `SYMPHONY_ISSUE_ID` 环境变量  | 编排器回退值                    |
| 4      | 自动生成                      | `session_<hex>`                 |

来源：`pkg/cli/binary.go` → `resolveBinarySessionID()`

---

## 5. 编排器启动模式与独立二进制模式

| 行为                         | 独立运行（SWE-bench）    | 编排器启动                |
|------------------------------|--------------------------|---------------------------|
| MCP 自动连接                 | 禁用                     | 启用                      |
| Sandbox 自动启用             | 否                       | 是                        |
| Hook 服务                    | 来自配置                 | 来自配置                  |
| OSS 遥测                     | 禁用                     | 禁用                      |

检测方式：`isEmbeddedBinaryExecution()` 检查 `SYMPHONY_WORKSPACE`、
`SYMPHONY_MCP_URL` 或 `NANO_ORCHESTRATOR_PROFILE` 环境变量。

`nano-agent` **不**内嵌任何编排器专属的 skill 或 MCP server 配置档。
希望自动激活 skill 的编排器可以将 `NANO_ORCHESTRATOR_PROFILE`
设置为以逗号分隔的 skill 名称列表；这些名称会在配置加载时追加到
`skills.auto_activate`。MCP server 必须由编排器（例如通过 `--mcp-config`）
或用户配置文件来配置。

---

## 6. 错误恢复契约

当进程遇到不可恢复的错误时：

1. 在返回错误之前**始终**调用 `emitResult()`，确保即使在失败路径上 stdout 也包含合法 JSON。
2. 退出码由 `status` 字段经 `binaryExitCode()` 推导得出。
3. 编排器应将任何带有合法 JSON 的非零退出视为结构化失败（根据 status 决定重试 / 放弃）。
4. 编排器应将**没有**合法 JSON 最后一行的非零退出视为
   `no_result_payload`——这表明进程在 `emitResult` 能够执行之前就已崩溃。

### Panic 恢复

顶层 panic 恢复处理器会输出：

```json
{"status":"abandoned","reason":"panic: <message>","termination_cause":"panic","duration_ms":0,"tool_calls":0,"tokens":{"input":0,"output":0}}
```

该逻辑在 `pkg/cli/binary_cmd.go` 中通过 `emitPanicResult()` 实现。

---

## 7. MCP 工具命名约定

注册 MCP 工具时，本地工具名称为 `mcp_<server>_<tool>`，
例如名为 `symphony` 的 server 暴露的 `emit_result` 会命名为 `mcp_symphony_emit_result`。
工具的允许 / 拒绝匹配模式必须使用这种单下划线形式：
`mcp_<server>_*` 或 `mcp_<server>_<tool>`。

---

## 8. 版本兼容性

本契约适用于 nano-agent v0.7 及以上版本。对契约字段的变更
应当保持向后兼容（仅允许新增）。移除或重命名字段需要提升主版本号，
并协调编排器同步更新。

| 新增字段                  | 版本   | 备注                          |
|---------------------------|--------|-------------------------------|
| `cache_key`               | v0.7   | 可选，用于重试去重            |
| `goal_state`              | v0.7   | 可空，目标评估                |
| `termination_cause`       | v0.7   | 结构化枚举                    |
| `blocker_fingerprint`     | v0.7   | 稳定的去重键                  |
| `blocked_commands_sample` | v0.7   | 调试辅助                      |
