# 配置与迁移指南

[English](./CONFIGURATION.md)

本指南记录了架构重构的配置兼容性规则。本次重构是增量式的：除非在下文中明确列出，否则现有的 CLI 标志、`.nano.yaml` 字段、daemon API、会话（session）、技能（skill）、命令以及团队数据均保持受支持。

## 配置来源与优先级

nano-agent 从以下来源读取配置，按优先级从低到高排列：

1. 内置默认值。
2. `~/.nano` 下的用户级配置与运行时状态。
3. 项目配置，如 `.nano.yaml` 以及项目本地的 `.nano/*` 目录。
4. 环境变量，如 `NANO_WORKING_DIR`。
5. CLI 标志与进程内覆盖。

运行时状态仍然集中在 `~/.nano` 之下。自定义命令、技能以及 agent 配置文件（profile）等项目级声明存放在项目工作目录中。

## 无需迁移的变更

以下重构特性无需迁移：

- 工具元数据已迁移至 `pkg/toolruntime` 之后，现有 `pkg/tools` 描述符 API 仍作为兼容别名保留。
- Hook 执行已迁移至 `pkg/hookservice` 之后，中间件 hook 入口点仍作为兼容包装保留。
- 轮次编排（turn orchestration）已迁移至 `turnExecutor` 之后，公开的 agent 处理 API 保持不变。
- 会话生命周期获得了显式状态和增量式 JSONL 恢复（resume）支持，且未改变现有会话 ID。
- 事件回放使用现有的公开 `event.StreamEvent` 信封与序列字段。

## 斜杠命令

自定义斜杠命令仍可从项目命令及兼容的 `.claude/commands` 位置发现。新增的 frontmatter 字段均为可选：

```yaml
allowed-tools: [run_shell_command]
permission-profile: acceptEdits
prelude_timeout: 30
prelude_on_error: abort
prelude_output: summary
```

迁移规则：

- 不含这些字段的现有命令继续正常工作。
- 添加 `allowed-tools` 以收窄命令可用的工具范围。
- 仅在命令需要临时权限模式时才添加 `permission-profile`。
- 以 `!shell command` 开头的前奏（prelude）行由 `slash.CommandRuntime` 通过 `SandboxRuntime` 执行。
- `prelude_timeout`、`prelude_on_error` 和 `prelude_output` 均为可选，且仅影响前奏执行。

Daemon 的 `/api/v1/commands` 以 `allowedTools` 和 `permissionProfile` 的形式暴露这些字段。

## Hook

现有 hook 声明保持兼容。Hook 可以选择启用更严格的环境继承和显式的失败处理：

```yaml
security:
  hooks:
    - name: deny-dangerous-shell
      event: pre_tool_use
      pattern: run_shell_command:*
      command: ./hooks/deny-dangerous-shell.sh
      enabled: true
      failure_policy: confirm
      env_whitelist: [PATH]
```

`failure_policy` 支持 `confirm`（默认）、`block`、`allow` 和 `ignore_but_audit`。Hook 仍会接收 `NANO_TOOL_NAME` 和 `NANO_TOOL_INPUT`；新的 hook 可以读取结构化的 `NANO_HOOK_INPUT`，并在 stdout 上返回结构化 JSON。

## 模型路由与思考（thinking）

主模型设置与现有顶层字段保持兼容：

```yaml
model: deepseek-chat
base_url: https://api.deepseek.com/v1
```

命令层可以通过以下方式管理这些字段：

```bash
nano model list
nano model use deepseek-r1 --provider deepseek
nano model status
nano think on --effort high
nano think status
```

回退（fallback）路由元数据单独存储，在调用方显式启用模型路由之前，不会改变默认的主路由行为：

```yaml
model_routing:
  fallbacks:
    - name: fast
      model: gpt-4.1
      base_url: https://api.openai.com/v1
```

通过以下命令管理回退路由元数据：

```bash
nano model fallback list
nano model fallback add gpt-4.1 --name fast --provider openai
nano model fallback clear
```

## 沙箱配置

沙箱配置保持增量式且向后兼容。不含 sandbox 小节的现有配置保持原有行为。

```yaml
sandbox:
  enabled: true
  backend: docker # "", none, native, docker
  docker_image: ubuntu:24.04
  network_access: false
```

迁移规则：

- `backend: ""` 保留平台原生的默认选择。
- `backend: docker` 使用一次性 Docker 容器执行命令。
- `docker_image` 可设置为按摘要固定（digest-pinned）的镜像，以获得更强的可复现性。
- 当 CLI 权限模式为 `yolo` 且未显式配置沙箱后端时，nano-agent 会默认将后端设为 Docker 并记录该选择。
- Docker 容器只会接收 `NANO_*` 环境变量；除非沙箱内命令确实需要，否则不要在 `NANO_*` 变量中存放机密信息。

## Agent 配置文件（Agent profile）

可配置的队友（teammate）在 `.nano/agents` 中声明：

```yaml
# .nano/agents/reviewer.yaml
description: Review code changes
initial_prompt: Review the requested patch and report risks.
permission_mode: acceptEdits
allowed_tools: [read_file, run_shell_command]
kind: in_process
color: "#00ff00"
```

迁移规则：

- 现有静态子 agent 配置继续受支持。
- 仅当项目需要显式的 `/agent-name` 斜杠命令调用或可复用的队友默认值时，才添加 `.nano/agents/<name>.yaml`。
- 当匹配的 AgentProfile 已提供 `initial_prompt` 时，`spawn_teammate` 中该参数为可选。
- `permission_mode`、`allowed_tools`、`model` 和 `context_providers` 会独立于父 agent 应用到生成的队友上。

可选的多 agent 治理限制：

```yaml
advanced:
  fork:
    max_depth: 1
    max_concurrent: 3
    max_runtime_sec: 3600
```

- 队友不允许再生成嵌套队友，默认防止 agent 深度失控。
- `max_concurrent` 设置为大于零的值时，限制每个团队中处于活跃状态的队友数量。
- `max_runtime_sec` 设置为大于零的值时，限制每个生成队友的运行时长。

支持的配置文件格式：

- `.yaml`
- `.yml`
- `.json`
- 带可选 YAML frontmatter 的 `.md`

## 扩展清单

`manage_extension` 提供以下内容的统一视图：

- 技能（skill）
- MCP 服务器
- 工具
- agent 工具及 `.nano/agents` 配置文件
- 斜杠命令

迁移规则：

- 使用 `manage_extension` 跨扩展类型进行状态/清单检查。
- 技能和 MCP 服务器仍可通过各自的专用工具管理。
- 远程扩展来源（`http://` 或 `https://`）在通过 `manage_extension` 安装/更新之前需要用户显式确认。
- 纯 HTTP 远程来源在信任元数据中会被标记为 `remote_insecure`。

## 事件与审计模式（schema）

事件消费方应使用公开的 `event.StreamEvent` 字段：

- `type`
- `session_id`
- `run_id`
- `seq`
- `metadata`
- `payload`

团队负责人（team-lead）回放支持：

- REST：`GET /api/v1/teams/sessions/{id}/events?since_seq=N`
- WebSocket 实时/回放：`subscribe`
- WebSocket 仅回放：`replay`

审计 JSONL 条目包含 `schema_version` 和可选的 `security_decision`。字段级模式详见 `docs/EXTENSION_EVENT_SCHEMA.md`。

本地审计 JSONL 轮转在 `middleware` 下配置：

```yaml
middleware:
  enable_audit: true
  audit_log_path: ~/.nano/audit.jsonl
  audit_max_size_mb: 100
  audit_max_backups: 3
  audit_max_age_days: 28
  audit_compress: true
```

默认路径仍为 `~/.nano/audit.jsonl`；轮转文件根据大小、备份数量、保留天数和压缩设置进行保留。

## 权限与安全迁移

现有权限模式继续有效。本次重构新增了更细粒度的可选控制：

- 命令级 `allowed-tools`
- 命令级 `permission-profile`
- AgentProfile 的 `permission_mode`
- AgentProfile 的 `allowed_tools`
- 扩展清单的信任/健康/权限元数据

推荐迁移顺序：

1. 保持现有配置不变。
2. 在命令行为已知的地方添加命令级 `allowed-tools`。
3. 将可复用的队友默认值迁移到 `.nano/agents`。
4. 使用 `manage_extension` 审计扩展的信任与权限。
5. 依据文档中的回放（replay）与审批（approval）帧校验 daemon 客户端。
