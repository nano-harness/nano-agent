---
name: nano-startup-modes
description: Use when configuring nano-agent startup modes (TUI, binary, ACP, daemon), selecting which mode to use, troubleshooting mode-specific issues, or setting up required and advanced configuration options for any nano-agent mode
---

# Nano-Agent 启动模式配置指南

[English](./SKILL.md)

## 概述

nano-agent 支持四种运行模式，每种模式针对不同的使用场景进行了优化：

- **TUI 模式**（默认）：面向人类用户的交互式终端界面，提供 bubbletea/milktea/tview 三种 UI 变体，支持会话管理
- **Binary 模式**：非交互式一次性执行，适用于 CI/CD、脚本和 SWE-bench 评测，输出结构化结果
- **ACP 模式**：Agent Client Protocol 服务器，通过 stdio JSON-RPC 为编辑器（Zed、JetBrains、Neovim）提供集成
- **Daemon 模式**：后台 HTTP 服务器，提供 REST API + WebSocket，面向 Web 客户端和自动化工作流

## 模式选择指南

| 使用场景 | 推荐模式 | 原因 |
|----------|------------------|-----|
| 本地开发、交互式编码 | TUI | 实时反馈、会话连续性、可视化进度 |
| CI/CD 流水线、自动化测试 | Binary | 确定性输出、退出码、无需 TTY |
| 编辑器集成（Zed、VSCode 等） | ACP | 标准协议、原生 IDE 体验 |
| Web 界面、多客户端 | Daemon | 多会话支持、REST/WebSocket API、持久化 |
| SWE-bench 评测 | Binary (swebench) | 补丁生成、执行轨迹日志、基准兼容 |
| 后台自动化任务 | Daemon | 长时运行、计划任务、cron 支持 |

## 通用配置

所有模式共享以下核心 LLM 配置项（通过 `.nano.yaml` 或环境变量设置）：

```yaml
# Legacy schema (still supported)
api_key: "your-llm-api-key"       # NANO_API_KEY
base_url: "https://api.deepseek.com/v1"  # NANO_BASE_URL
model: "deepseek-chat"            # NANO_MODEL

# Multi-provider routing (recommended)
model: "deepseek/deepseek-chat"
fallbacks:
  - "openai/gpt-4.1"
  - "anthropic/claude-sonnet-4.6"
providers:
  deepseek:
    api_key_env: NANO_DEEPSEEK_API_KEY
  openai:
    api_key_env: OPENAI_API_KEY
  anthropic:
    api_key_env: ANTHROPIC_API_KEY

# Context management
context:
  max_tokens: 80000
  compression_ratio: 0.25
  preserve_recent_turns: 6
  enable_compression: true
```

**配置优先级**（从高到低）：
1. 命令行参数
2. 环境变量（`NANO_*`）
3. 项目配置（`.nano.yaml`）
4. 全局配置（`~/.config/nano/config.yaml`）

## TUI 模式

### 启动 TUI 模式

```bash
# Default mode - auto-launches TUI if no daemon running
nano

# Force TUI even if daemon is running
nano --tui

# UI variants
nano --tea          # Inline Bubble Tea (non alt-screen)
nano --milktea      # Fullscreen TUI (alt-screen mode)
nano --ui tview     # Classic tview dashboard (default)

# With initial prompt
nano "fix the bug in main.go"
```

### 会话管理

```bash
# Resume most recent session in current project
nano --continue

# Use specific session ID
nano --session my-session-id

# Start in team-lead mode (requires mailbox)
nano --team my-team-name
```

### 必需配置

TUI 模式的最小 `.nano.yaml` 配置：
```yaml
api_key: "sk-..."
model: "deepseek-chat"
```

### TUI 高级配置

```yaml
# Permission control
permission_mode: "default"  # Options: default, acceptEdits, yolo
dangerously_skip_permissions: false

# Session storage (default: project-scoped)
# Sessions stored in .nano/sessions/<session_id>/

# Banner customization
# --no-banner flag disables startup animation

# Turn execution control
turn:
  max_duration: 30m

# Ralph-loop continuation from Stop hooks
hooks:
  ralph:
    enabled: true
    max_iterations: 10
```

### TUI 斜杠命令

TUI 支持在会话中使用斜杠命令进行控制：
- `/clear` - 开始新会话
- `/help` - 显示帮助
- `/model` - 切换模型
- `/context` - 查看上下文状态
- `/think` - 启用推理模式
- `/allow` - 添加权限规则
- `/disallow` - 移除权限规则

## Binary 模式

### 启动 Binary 模式

```bash
# General-purpose one-shot execution
nano binary exec "implement user authentication"

# SWE-bench compatible mode
nano binary swebench "fix issue #123"

# With output directory for artifacts
nano binary exec --output-dir ./results "generate tests"

# Streaming NDJSON output
nano binary exec --stream "analyze codebase"

# JSON structured output
nano binary exec --format json "refactor module"
```

### Stdin 支持

Binary 模式会自动检测管道输入：
```bash
echo "fix typo in README" | nano
cat task.txt | nano binary exec
```

### 必需配置

与 TUI 模式相同——需要 LLM 凭据。

### Binary 高级配置

```bash
# Sandbox control (for embedded execution)
nano binary exec --sandbox auto "task"      # default: auto-enable in embedded contexts
nano binary exec --sandbox on "task"        # force enable
nano binary exec --sandbox off "task"       # disable

# Goal-driven execution
nano binary exec --goal "all tests pass" --goal-max-turns 10 "fix tests"

# Exit hook for orchestration
nano binary exec --on-exit-cmd 'echo "Status: $NANO_RESULT_STATUS"' "task"

# Output formats
--format plain    # Raw text output (default)
--format json     # Single JSON document
--format jsonl    # Streaming NDJSON lines

# Quiet mode (suppress metadata)
nano binary exec -q "task"
```

### 退出码

Binary 模式返回具有语义化的退出码：
- `0` - 成功
- `10` - 需要重试（限流、临时性错误）
- `20` - 已放弃（永久性失败）
- `30` - 超时
- `1` - 未分类错误

### 输出产物

指定 `--output-dir` 时，Binary 模式会生成：
- `solution.patch` - 变更的 Git diff
- `trajectory.json` - 包含工具调用和事件的执行轨迹
- `sessions/` - 会话历史（如启用）

## ACP 模式

### 启动 ACP 模式

```bash
# Start ACP server (stdio mode for editors)
nano acp serve

# With custom log file (required - stdout/stderr reserved for JSON-RPC)
nano acp serve --log-file ~/.nano/acp-server.log

# With custom config
nano acp serve --config /path/to/.nano.yaml --log-level debug
```

### 编辑器集成

ACP 服务器通过 stdin/stdout 使用 JSON-RPC 2.0 通信。编辑器将 nano 作为子进程启动：

```json
// Example editor config (Zed)
{
  "command": "nano",
  "args": ["acp", "serve", "--log-file", "/tmp/nano-acp.log"]
}
```

### 必需配置

最小配置：
```yaml
api_key: "sk-..."
model: "deepseek-chat"
```

**重要**：ACP 模式必须指定 `--log-file` 参数，因为 stdout/stderr 被 JSON-RPC 协议占用。

### ACP 高级配置

```yaml
# Filesystem mode
--fs-mode auto    # Auto-detect (default)
--fs-mode acp     # Use ACP protocol file operations
--fs-mode local   # Use local filesystem directly

# Swarm/multi-agent support
--enable-swarm    # Enable team-lead coordination

# Working directory
--workdir /path/to/project
```

默认日志位置：`~/.nano/acp-server.log`

## Daemon 模式

### 启动 Daemon 模式

```bash
# Start daemon in background
nano daemon start

# Stop daemon
nano daemon stop

# Restart daemon
nano daemon restart

# Check status
nano daemon status

# View logs
nano daemon logs
nano daemon logs -f  # Follow mode
```

### 必需配置

`.nano.yaml` 中最小的 `daemon` 配置段：
```yaml
daemon:
  port: 8080
  host: "127.0.0.1"
```

或通过环境变量：
```bash
NANO_DAEMON_PORT=8080
NANO_DAEMON_HOST=127.0.0.1
```

### Daemon 高级配置

```yaml
daemon:
  port: 8080
  host: "127.0.0.1"                      # Use 0.0.0.0 for remote access
  pid_file: "~/.nano/daemon.pid"         # Auto-default
  log_file: "~/.nano/daemon.log"         # Auto-default

  # CORS (for web clients)
  enable_cors: true

  # Authentication
  api_key: "secret-api-key"              # Optional API key

  # TLS (HTTPS)
  tls_cert_file: "/path/to/cert.pem"
  tls_key_file: "/path/to/key.pem"

  # Permission policy when no approval handler
  confirm_policy: "allow"                # Options: allow, block, allowlist
  allowlisted_tools: []                  # When confirm_policy=allowlist
```

### Daemon API 端点

完整 API 文档见 `references/daemon-api.md`。关键端点：

```bash
# Health check
GET /health

# Execute command
POST /execute
POST /session/:id/execute

# Team sessions (swarm)
POST /team-session
GET /team-session/:id
POST /team-session/:id/message

# WebSocket streaming
WS /ws
WS /session/:id/stream
```

### Daemon 客户端命令

```bash
# Execute via daemon (when daemon is running, nano auto-uses it)
nano "implement feature X"

# Explicit daemon execution
nano --daemon "task"

# With session ID
nano --session-id abc123 --daemon "continue task"

# Using client subcommand
nano client exec "task"
nano client status
```

### 沙箱配置（仅 Daemon）

Daemon 模式支持文件系统沙箱：

```yaml
sandbox:
  enabled: true
  allowed_paths:
    - "/home/user/workspace"
    - "/tmp"
  blocked_paths:
    - "/etc/passwd"
    - "/home/user/.ssh"
  readonly_paths:
    - "/usr/share/doc"
  hidden_paths:
    - "/home/user/.cache"
  max_file_size: 52428800  # 50MB
```

### Mailbox 配置（Daemon + Team 模式）

```yaml
mailbox:
  enabled: true
  backend: "file"                       # Options: memory, file
  root_dir: "~/.nano-agent/mailbox"    # File backend storage
  ttl_days: 7                          # Message expiration
  max_per_agent: 1000                  # Inbox capacity
  injection_limit: 5                   # Messages per turn
```

### Watcher 配置（仅 Daemon）

面向自动化工作流的事件驱动监控：

```yaml
watcher:
  enabled: true
  state_dir: "~/.nano"
  rules:
    - id: check-build-status
      source: shell
      shell_command: "./scripts/check-build.sh"
      command: "Build status check: {{.OUTPUT}}"
      interval: 5m
      timeout: 10m
```

## 配置优先级

配置按以下顺序加载和合并（后加载的来源覆盖先前的）：

1. **内置默认值** - 硬编码的合理默认值
2. **全局配置** - `~/.config/nano/config.yaml`
3. **项目配置** - 项目根目录下的 `.nano.yaml`
4. **环境变量** - `NANO_*` 前缀
5. **命令行参数** - 最高优先级

优先级链示例：
```bash
# This command uses:
# - api_key from .nano.yaml (project)
# - base_url from NANO_BASE_URL env var (overrides project)
# - model from --config flag (if provided, overrides all)

NANO_BASE_URL=https://custom.api.com nano --config custom.yaml
```

## 常见错误与解决方案

| 问题 | 解决方案 |
|-------|----------|
| **TUI 启动成了 daemon 客户端** | 使用 `nano --tui` 强制 TUI 模式，或先停止 daemon |
| **Binary 模式挂起** | 以参数形式提供提示词：`nano binary exec "task"`，或通过 stdin 传入 |
| **ACP 报错：没有日志文件** | 始终指定 `--log-file ~/.nano/acp-server.log`（stdout 被 JSON-RPC 占用） |
| **Daemon 端口冲突** | 设置 `NANO_DAEMON_PORT=8081`，或在 `.nano.yaml` 中配置 `daemon.port` |
| **无法远程连接 daemon** | 设置 `daemon.host: "0.0.0.0"`，并启用 `api_key` 和 CORS |
| **配置未初始化** | 运行 `nano config init`，或设置 `NANO_API_KEY` 和 `NANO_MODEL` 环境变量 |
| **权限被拒绝** | 使用 `--permission-mode acceptEdits`，或在 `.nano.yaml` 中配置 |
| **沙箱阻止了路径访问** | 在配置中将所需目录加入 `sandbox.allowed_paths` |
| **Binary 退出码不符合预期** | 使用 `--goal "condition"` 获得语义化退出码（0/10/20/30） |
| **MCP 服务器无法连接** | 检查 `enable_mcp: true`，核对配置中的服务器命令，并查看 `nano daemon logs` |

---

详细 API 文档见 `references/daemon-api.md`。
完整配置参考见 `references/config-reference.md`。
