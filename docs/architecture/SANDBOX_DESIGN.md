# 沙箱设计方案

本文补充 nano-agent 的沙箱设计方向。当前实现已经具备基础沙箱能力：Linux 通过 `bwrap` 包装 Shell 命令，macOS 通过 `sandbox-exec` 包装 Shell 命令，Go 进程内文件工具通过 `PathChecker` 执行路径级访问控制，并支持 `AllowedPaths`、`BlockedPaths`、`ReadOnlyPaths`、`ExtraReadOnlyPaths`、`ExtraWritablePaths` 等配置。后续设计目标是将这些能力从“Shell 命令包装器”升级为统一的 Sandbox Runtime，并把 Docker 作为优先级更高、隔离性更强的执行后端。

## 1. 设计目标

nano-agent 的沙箱不应只解决“命令是否允许执行”，还需要解决以下问题：

- 限制 Agent 对宿主机文件系统的访问范围。
- 限制 Shell 命令、Hook、MCP Server、子 Agent 的副作用。
- 隔离网络、环境变量、进程、临时文件、凭据。
- 支持 YOLO/acceptEdits 等高权限模式下的安全兜底。
- 支持可审计、可回滚、可复现的任务执行环境。
- 支持本地开发、Daemon、CI、SWE-bench、团队模式等不同场景。

核心原则是：

> 权限系统决定“能不能做”，沙箱系统决定“就算做了也只能影响受控环境”。

## 2. 沙箱分层模型

建议采用三层沙箱模型。

### 2.1 Policy Sandbox

Policy Sandbox 负责“是否允许执行”。它不提供真正的进程隔离，而是做策略决策。

适用对象：

- `run_shell_command`
- 文件工具
- Web 工具
- MCP 工具
- Hook
- Slash Command prelude
- Subagent 工具调用

能力范围：

- 权限模式判断。
- allow / deny rule。
- Session allowlist。
- CommandGuard。
- Hook 决策。
- 用户确认。
- 审计日志。

### 2.2 Path Sandbox

Path Sandbox 负责“Agent 能访问哪些宿主机路径”，继续由当前 `PathChecker` 演进。

适用对象：

- `read_file`
- `write_file`
- `edit_file`
- `delete_file`
- workspace 工具
- Go 进程内执行的文件操作

能力范围：

- 工作目录内默认可访问。
- 敏感路径默认禁止。
- 可配置只读路径。
- 可配置额外可写路径。
- 解析 symlink，防止路径穿越。
- 对不存在的新文件，解析最近存在的父目录。

### 2.3 Execution Sandbox

Execution Sandbox 负责“真正隔离子进程”。

适用对象：

- Shell 命令
- Hook 脚本
- MCP stdio server
- Slash Command prelude
- 构建、测试、安装依赖等长任务
- Subagent 执行环境

推荐后端：

- `none`
- `native`
  - Linux: `bwrap`
  - macOS: `sandbox-exec`
- `docker`

未来可选后端：

- `podman`
- `firecracker`
- `nsjail`

其中 Docker 应作为推荐后端, 尤其适合 YOLO、Daemon、CI、SWE-bench 和远程任务场景。

## 3. Docker 沙箱设计

### 3.1 为什么 Docker 是更好的选择

相比 `bwrap` / `sandbox-exec`，Docker 的优势是：

- 跨平台一致性更好。
- 隔离边界更清晰。
- 可限制 CPU、内存、进程数、网络、文件系统。
- 适合长任务、CI、Daemon、SWE-bench。
- 可以构建可复现的开发环境。
- 更容易做快照、缓存、清理和审计。
- 对 YOLO 模式更安全：即使自动执行危险命令，也限制在容器内。
- 可以将 Agent 的执行环境和用户宿主机隔离，减少误删、泄密、污染系统环境的风险。

推荐后端优先级：

1. 显式配置的 backend。
2. Daemon / CI / YOLO 场景优先 Docker。
3. 本地轻量交互可使用 native。
4. 无可用后端时才 fallback 到 none，并给出明显警告。

### 3.2 Docker 执行模式

Docker 后端建议支持三种执行模式。

#### 一次性容器模式

每次 Shell 命令启动一个临时容器。

优点：

- 隔离最强。
- 每个命令干净执行。
- 易清理。

缺点：

- 启动开销较大。
- 依赖安装、构建缓存不友好。

适用：

- 高风险命令。
- Hook。
- 未信任 MCP。
- YOLO 模式下的危险命令。
- CI 审计任务。

#### 会话级持久容器

每个 Agent session 创建一个持久容器，所有命令在同一容器内执行。

优点：

- 性能更好。
- 保留依赖安装结果。
- 适合交互式开发。
- 适合长任务和多轮修改。

缺点：

- 需要生命周期管理。
- 容器内状态可能累积污染。

适用：

- TUI / Daemon 长会话。
- 多轮编码任务。
- 构建、测试、修复循环。
- SWE-bench。

#### 任务级隔离容器

每个 Turn 或每个 Task 创建一个容器。

优点：

- 比一次性命令更高效。
- 比会话级容器更可控。
- 方便做任务级回滚和审计。

适用：

- Subagent。
- Team mode。
- 批量任务。
- OpenSpec implementation task。

推荐默认：

- 本地普通模式：会话级容器。
- YOLO 模式：任务级或一次性容器。
- Daemon 模式：会话级容器 + 强资源限制。
- CI 模式：一次性或任务级容器。

## 4. Sandbox Runtime 抽象

建议引入统一抽象，避免不同执行场景直接依赖具体后端。

### 4.1 SandboxRuntime

`SandboxRuntime` 负责：

- 创建执行环境。
- 包装命令。
- 挂载工作区。
- 注入环境变量。
- 控制网络。
- 设置资源限制。
- 收集日志。
- 清理容器或临时目录。

当前实现状态：

- `pkg/sandbox` 已定义 `Runtime`、`SandboxRequest`、`SandboxEnvironment`、`SandboxResult`、`Backend`、`NetworkPolicy`、`Mount` 和 `ResourceLimits`。
- `NewRuntime` 将现有 `none`、Linux `bwrap`、macOS `sandbox-exec` 后端接入统一 adapter，不改变现有执行行为。
- `NewRuntime` 已支持显式 `docker` backend 的一次性容器模式，并通过 `sandbox.backend`、`sandbox.docker_image`、`NANO_SANDBOX_BACKEND`、`NANO_SANDBOX_DOCKER_IMAGE` 配置。
- 当 CLI permission mode 为 `yolo` 且未显式配置 sandbox backend 时，默认启用 Docker backend；如果需要复现性更强的容器镜像，可将 `sandbox.docker_image` 配置为 digest 形式。
- `run_shell_command` 已通过 `SandboxRuntime.PrepareCommand` 包装实际命令，并把 backend、network、mount、resource limit、fallback 信息写入 tool result metadata，同时发布 sandbox command lifecycle audit events。
- `hookservice` 可通过 `Options.SandboxRuntime` 将 hook shell 命令接入统一沙箱入口。
- MCP stdio server 启动命令已通过 Toolbox 注入的 `SandboxRuntime` 包装。
- Slash Command 的 `!prelude` 命令已可通过 `slash.CommandRuntime` 进入统一沙箱入口。
- Team/Subagent 元数据已记录 sandbox policy，并已用于会话级/任务级 Docker 容器生命周期管理。
- `SandboxRuntime` 可发布 sandbox audit events，Daemon `TaskEventStore` 已可保存并重放 sandbox events。
- Go 进程内文件工具继续使用 `PathChecker` 做路径级沙箱检查。

### 4.2 SandboxSession

`SandboxSession` 表示一次 Agent 会话的沙箱实例，负责保存：

- session id。
- container id。
- workspace mount。
- 网络策略。
- 资源配置。
- 生命周期状态。

Session 可短生命周期，也可复用。

### 4.3 SandboxExecutor

`SandboxExecutor` 负责在沙箱内执行单个命令，需要支持：

- stdout / stderr 流式输出。
- 取消。
- 超时。
- 后台任务。
- exit code。
- 审计记录。

## 5. Docker 挂载策略

默认挂载：

- 工作目录：读写挂载到 `/workspace`。
- 临时目录：容器内独立 `/tmp`。
- 用户 home：默认不挂载。
- `.git`：默认挂载，支持 git diff / status。
- SSH、GPG、AWS、Kube、Docker socket：默认不挂载。
- Docker socket：默认禁止，除非显式开启。

建议挂载规则：

- `workspace`: read-write。
- `extra_read_only_paths`: read-only。
- `extra_writable_paths`: read-write。
- `blocked_paths`: 不挂载。
- `secrets`: 只通过显式 secret mount 或环境变量白名单注入。

强烈建议默认禁止挂载：

- `~/.ssh`
- `~/.gnupg`
- `~/.aws`
- `~/.kube`
- `~/.docker`
- `/var/run/docker.sock`
- `/etc`
- `/root`
- 用户 home 全目录

## 6. Docker 网络策略

网络策略应按任务风险配置。

建议支持：

- `network: none`
  - 完全无网络。
  - 适合代码分析、测试、格式化。
- `network: restricted`
  - 只允许配置白名单域名或代理。
  - 适合依赖下载。
- `network: bridge`
  - 默认 Docker 网络。
  - 适合普通开发任务，但需要审计。
- `network: host`
  - 默认不建议。
  - 仅开发者显式启用。

推荐默认：

- read-only / analysis：`none`。
- build / test：`none` 或 `restricted`。
- dependency install：`restricted`。
- web / MCP：按工具权限单独开放。
- YOLO：默认 `none`，需要网络时显式确认。

## 7. Docker 资源限制

必须支持资源限制，避免 Agent 生成命令拖垮宿主机。

建议配置项：

- CPU 限制。
- 内存限制。
- pids 限制。
- 磁盘写入上限。
- 单命令超时。
- 容器最大生命周期。
- 最大后台任务数。
- 最大输出大小。
- 最大文件数。
- 最大网络流量，后续可选。

推荐默认：

- CPU：2 核。
- 内存：2GB 或 4GB。
- PIDs：256。
- 命令超时：120 秒。
- 长任务自动转后台。
- 输出上限沿用当前 shell output 限制。

## 8. Docker 镜像策略

### 8.1 默认基础镜像

可提供官方推荐镜像，例如：

- `nano-agent/sandbox:go`
- `nano-agent/sandbox:node`
- `nano-agent/sandbox:python`
- `nano-agent/sandbox:full`

### 8.2 项目自定义镜像

项目可在 `.nano.yaml` 中配置：

- image。
- dockerfile。
- build context。
- build args。
- init command。

### 8.3 自动检测镜像

可根据项目文件自动建议镜像：

- `go.mod` → Go 镜像。
- `package.json` → Node 镜像。
- `pyproject.toml` / `requirements.txt` → Python 镜像。
- `Cargo.toml` → Rust 镜像。
- `pom.xml` → Maven 镜像。

自动检测只作为建议，不应静默拉取未知镜像。

### 8.4 镜像安全

- 不自动使用不可信镜像。
- 首次使用镜像需提示来源。
- 可配置 trusted registries。
- 可记录镜像 digest。
- Daemon 模式建议固定 digest。

## 9. 配置设计

建议扩展 `sandbox` 配置。以下字段是设计目标，不要求一次性实现：

```yaml
sandbox:
  enabled: true
  backend: docker        # none | native | docker
  mode: session          # command | task | session
  network: none          # none | restricted | bridge | host
  image: nano-agent/sandbox:go
  workdir: /workspace

  mounts:
    workspace:
      host_path: "."
      container_path: "/workspace"
      mode: rw
    extra_read_only:
      - "/usr/include"
    extra_writable: []

  blocked_paths:
    - "/etc"
    - "/root"
    - "~/.ssh"
    - "~/.aws"
    - "~/.kube"
    - "~/.docker"

  env:
    allow:
      - "PATH"
      - "HOME"
      - "LANG"
      - "GOPROXY"
      - "NPM_CONFIG_REGISTRY"
    block:
      - "NANO_API_KEY"
      - "OPENAI_API_KEY"
      - "ANTHROPIC_API_KEY"
      - "AWS_SECRET_ACCESS_KEY"

  docker:
    socket: ""           # default disabled
    pull_policy: missing # never | missing | always
    auto_remove: true
    reuse_session: true
    user: "1000:1000"
    read_only_rootfs: false
    cap_drop:
      - "ALL"
    security_opt:
      - "no-new-privileges:true"
    memory: "4g"
    cpus: "2"
    pids_limit: 256
```

## 10. 与权限系统的关系

Docker 沙箱不是权限系统的替代，而是权限系统的兜底。

推荐规则：

- `default` 模式：
  - 危险命令仍需确认。
  - 确认后在沙箱内执行。
- `acceptEdits`：
  - 文件编辑允许，但只作用于挂载的 workspace。
- `yolo`：
  - 自动执行，但必须强制启用 Docker 或 native sandbox。
  - 如果无沙箱后端，应阻止进入 YOLO 或强警告二次确认。
- `daemon`：
  - 默认要求 Docker backend。
  - 非 Docker backend 需要显式配置。
- `ci`：
  - Docker backend 默认开启。
  - 网络默认关闭。

## 11. 与 Hook 的关系

Hook 也必须运行在沙箱里，不能绕过工具权限。

建议：

- PreToolUse / PostToolUse Hook 默认在 Docker 或 native sandbox 中执行。
- Hook 只接收结构化 JSON 输入。
- Hook 默认不继承 secrets。
- Hook 超时短于普通 shell 命令。
- Hook 失败策略可配置为 `allow`、`confirm`、`block`。
- Hook 的 stdout / stderr 进入审计日志。
- Hook 不允许默认访问 Docker socket。

## 12. 与 MCP 的关系

MCP Server 是高风险扩展，应纳入沙箱。

建议：

- stdio MCP server 可运行在 Docker sandbox 内。
- 每个 MCP server 可配置独立 backend、image、mount、network。
- 默认不允许 MCP server 访问宿主机 home。
- MCP server 的权限要进入 Extension Manifest。
- 未信任 MCP server 默认只读、无网络或 restricted network。
- MCP tool 调用也要经过 Policy Pipeline。

## 13. 与多 Agent / Swarm 的关系

不同 Agent 可以有不同沙箱。

建议：

- Team Lead 使用主 session sandbox。
- Subagent 默认创建 task-level sandbox。
- Investigator 只读 sandbox。
- Coder 使用 workspace rw sandbox。
- Researcher 可使用 network restricted sandbox。
- Untrusted Agent 使用一次性 Docker 容器。
- 每个 Agent 的 sandbox id、container id、mount、网络策略进入事件流和审计日志。

## 14. 与后台任务的关系

当前 shell 支持后台任务，Docker 后端需要明确长任务生命周期。

建议：

- 后台任务绑定到 `SandboxSession`。
- 如果 session 容器退出，后台任务自动取消。
- `bash_output` 从容器执行日志中读取。
- `kill_bash` 映射为容器内进程 kill，而不是直接操作宿主机进程。
- 对会话级容器，后台任务可跨 turn 保持。
- 对任务级容器，turn 结束时默认清理。

## 15. 与审计和回滚的关系

Docker 后端应提供更强审计能力。

建议记录：

- image。
- image digest。
- container id。
- sandbox mode。
- mounts。
- network mode。
- env allowlist。
- resource limits。
- command。
- exit code。
- duration。
- stdout / stderr 摘要。
- changed files 摘要。
- git diff 摘要。

可选增强：

- 执行前创建 git checkpoint。
- 执行后生成 diff。
- 高风险命令自动保存 patch。
- 支持 `/sandbox diff`、`/sandbox reset`、`/sandbox logs`、`/sandbox status`。

## 16. 推荐实现阶段

### 第一阶段：抽象 Sandbox Runtime

- 保留现有 bwrap / sandbox-exec 实现。
- 引入 `backend` 概念。
- 将当前 `Sandbox` 接口扩展为统一 runtime。
- 将 `PathChecker` 保持为独立路径层。
- 增加事件与审计字段。

### 第二阶段：实现 Docker command 模式

- 支持每次命令用 `docker run --rm` 执行。
- 挂载 workspace 到 `/workspace`。
- 支持 network none / bridge。
- 支持 env allowlist。
- 支持 memory / cpu / pids 限制。
- 支持 stdout / stderr 流式输出。

### 第三阶段：实现 Docker session 模式

- 每个 Agent session 创建一个持久容器。
- Shell 命令通过 `docker exec` 执行。
- 支持后台任务。
- 支持取消。
- 支持 session 结束清理。
- 支持 daemon 重启后的容器恢复或清理。

### 第四阶段：将 Hook / MCP / Subagent 纳入 Docker 沙箱

- Hook 默认沙箱执行。
- stdio MCP 可运行在容器内。
- Subagent 可绑定独立 sandbox profile。
- Extension manifest 增加 sandbox 权限声明。

### 第五阶段：默认策略调整

- Daemon 默认推荐 Docker。
- YOLO 默认要求 Docker 或 native sandbox。
- CI 默认 Docker + network none。
- 无沙箱时高风险命令强提示。

## 17. 推荐结论

- 当前 bwrap / sandbox-exec 适合作为轻量 native backend。
- Docker 应作为更推荐的强隔离 backend。
- YOLO、Daemon、CI、多 Agent、MCP、Hook 等高风险场景应优先使用 Docker。
- 沙箱要从“Shell 包装器”升级为“统一执行环境管理器”。
- 权限系统与沙箱系统必须分离但联动。
- Docker 沙箱应支持 command / task / session 三种生命周期。
- 所有沙箱行为必须进入事件流与审计日志。
