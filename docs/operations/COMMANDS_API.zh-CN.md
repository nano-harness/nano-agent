# nano CLI 命令与三端能力一致性矩阵

[English](./COMMANDS_API.md)

本文档追踪 `nano` 二进制对外暴露的公共、可脚本化接口，以及三个主要前端之间的能力一致性矩阵：

- **TUI (BubbleTea)** — 交互式单 agent / team-lead REPL。
- **TUI (TView)** — 备选交互式前端。
- **Daemon** — 长期运行的 HTTP/WebSocket 服务。
- **Binary** — 非交互式一次性命令（`nano binary ...`），面向 CI、脚本和基准测试驱动程序。

## 顶层命令

| 命令                            | 说明                                                              |
| ------------------------------ | ----------------------------------------------------------------- |
| `nano`（无参数）                | 默认的交互式单 agent REPL                                          |
| `nano chat --team <name>`      | 带 mailbox 的长期运行 team-lead REPL                              |
| `nano lead-chat`               | Lead chat（团队协调者）REPL                                       |
| `nano teammate ...`            | 内部使用的子进程 teammate 启动器                                   |
| `nano daemon ...`              | Daemon 生命周期：start/stop/status/logs                           |
| `nano session ...`             | 会话生命周期：list/load/prune                                     |
| `nano model[s] ...`            | 模型列表、切换与推理控制                                          |
| `nano routines ...`            | Cron 例行任务注册                                                 |
| `nano events` / `nano audit`   | daemon 事件/审计流的只读视图                                      |
| `nano doctor`                  | 健康检查                                                          |
| `nano mcp ...`                 | MCP server 管理（status/test/start/stop/restart）                 |
| `nano binary ...`              | 非交互式一次性操作（见下文）                                       |

## `nano binary` 子命令

元数据/列表类子命令支持 `--json` 输出机器可读结果。
`binary swebench` 保持 SWE-bench patch 输出兼容性。

| 子命令                    | 用途                                                       |
| ------------------------ | ---------------------------------------------------------- |
| `binary exec <prompt>`  | 运行单个 prompt 后退出                                     |
| `binary list-models`     | 列出已配置的 provider/model 预设                           |
| `binary list-tools`      | 列出内置工具描述符                                         |
| `binary list-slash`      | 列出内置斜杠命令                                           |
| `binary list-skills`     | 列出已安装的 skills（个人 + 项目）                         |
| `binary swebench <p>`    | 兼容 SWE-bench 的一次性评估                                |

## 三端能力一致性矩阵

以下矩阵追踪每项面向用户的能力在各前端是否可达。✅ = 支持，⚠️ = 部分支持 / 包装实现，❌ = 尚未接入。

| 能力                                    | BubbleTea | TView | Daemon | Binary |
| --------------------------------------- | :-------: | :---: | :----: | :----: |
| 一次性 prompt → 回答                    |    ✅    |  ✅  |   ✅   |   ✅   |
| 权限模式切换（`/yolo`、`/permission`、`/plan`） | ✅ | ✅ | ✅ | n/a |
| 会话白名单（`/allow`、`/disallow`、`/permissions`）   | ✅ | ✅ | ✅ | n/a |
| 列出 teammates（`/agents`、`/teammates`） | ✅ | ✅ | ✅ | ✅（`binary list-tools` 会列出 agent-spawn 工具） |
| 查看 teammate 详情                      |    ✅    |  ✅  |   ✅   |  ❌   |
| Checkpoints（`/checkpoint`、`/restore`） | ✅ 当 `checkpoint.enabled` 开启时 | ✅ 当 `checkpoint.enabled` 开启时 | ❌ | ❌ |
| 模型列表（`/models`、`binary list-models`） | ✅ | ✅ | ✅ | ✅   |
| 工具列表                                |    ❌    |  ❌  |   ✅   |   ✅   |
| 斜杠命令列表                            |    ✅    |  ✅  |   ✅   |   ✅   |
| Skills 列表（`/skill:list`、`binary list-skills`） | ✅ | ✅ | ✅ | ✅   |
| Routines 列表                           |    ✅    |  ✅  |   ✅   |  ❌   |
| OpenSpec 工作流（`/opsx:*`）            |    ✅    |  ✅  |   ✅   |  ❌   |
| `@file` 引用展开                        |    ✅    |  ❌  |   ❌   |  n/a   |
| 历史记录反向搜索（Ctrl+R）              |    ✅    |  ❌  |   n/a  |  n/a   |
| Daemon 日志尾随（`daemon logs --follow`） |   n/a   | n/a  |   ✅   |   ✅   |
| MCP server 探测 / 禁用                  |   n/a    | n/a  |   ✅   |   ✅   |
| 会话清理（`session prune --dry-run`）   | n/a    | n/a  |   ✅   |   ✅   |

Checkpoint 斜杠命令在 BubbleTea/TView 中由 `pkg/checkpoint` 提供支持，前提是 `checkpoint.enabled: true`。当该功能关闭时，命令仍会被识别，并返回友好的“未启用”提示，而不是被静默忽略。

## 迁移说明

- `nano binary swebench`（或 `nano binary exec`）是无头评估的入口。调用方式参见 `swe_bench_test/run_swe_bench.py`。
- `nano binary exec` 支持嵌入式 goal 循环：使用 `--goal`、`--goal-max-turns`，或在 prompt 首行写 `/goal <条件>` 指令。
- `nano mcp servers stop <name>` 现在会在当前配置中持久禁用该 server，而不是假装“停止一个进程”。使用 `nano mcp servers start <name>` 可重新探测并重新启用。
