# 配置参考

[English](./config-reference.md)

`.nano.yaml` 所有配置选项的完整参考。

## 文件位置

配置文件按以下优先级顺序加载：

1. `--config <path>` - 命令行参数（最高优先级）
2. `.nano.yaml` - 项目根目录
3. `~/.config/nano/config.yaml` - 用户全局配置
4. 内置默认值 - 兜底值

以 `NANO_` 为前缀的环境变量会覆盖配置文件中的值。

## 核心 LLM 配置

### 旧版 Schema（仍受支持）

```yaml
api_key: "sk-..."           # NANO_API_KEY
base_url: "https://..."     # NANO_BASE_URL
model: "deepseek-chat"      # NANO_MODEL
verbose: false              # NANO_VERBOSE
```

### 多 Provider Schema（推荐）

```yaml
# Primary model with provider prefix
model: "deepseek/deepseek-chat"

# Fallback chain
fallbacks:
  - "openai/gpt-4.1"
  - "anthropic/claude-sonnet-4.6"
  - "moonshot/kimi-k2"

# Provider configurations
providers:
  deepseek:
    api_key_env: NANO_DEEPSEEK_API_KEY
    base_url: "https://api.deepseek.com/v1"  # optional override

  openai:
    api_key_env: OPENAI_API_KEY

  anthropic:
    api_key_env: ANTHROPIC_API_KEY

  moonshot:
    api_key_env: MOONSHOT_API_KEY
    base_url: "https://api.moonshot.cn/v1"
```

**模式：** 所有模式
**环境变量：** `NANO_API_KEY`、`NANO_BASE_URL`、`NANO_MODEL`，以及各 provider 专属变量

## 系统限制

```yaml
max_file_size: 10485760     # Max file size in bytes (10MB)
response_timeout: 120s      # LLM response timeout
http_timeout: 60s           # HTTP client timeout
```

**模式：** 所有模式
**环境变量：** `NANO_MAX_FILE_SIZE`、`NANO_RESPONSE_TIMEOUT`、`NANO_HTTP_TIMEOUT`

## 上下文管理

```yaml
context:
  max_tokens: 80000               # Context window limit
  compression_ratio: 0.25         # Target compression ratio
  preserve_recent_turns: 6        # Always keep N recent turns
  enable_compression: true        # Enable auto-compression
```

**模式：** 所有模式
**环境变量：** `NANO_CONTEXT_MAX_TOKENS`、`NANO_CONTEXT_COMPRESSION_RATIO` 等

## 推理配置（适用于 o1/R1 模型）

```yaml
reasoning:
  enabled: false              # Enable reasoning mode
  effort: "medium"            # low, medium, high
  max_tokens: 0              # 0 = model default
  exclude: false             # Exclude reasoning from response
```

**模式：** 所有模式
**环境变量：** `NANO_REASONING_ENABLED`、`NANO_REASONING_EFFORT`、`NANO_REASONING_MAX_TOKENS`

## 高级弹性配置

```yaml
advanced:
  circuit_breaker:
    max_retries: 3
    base_delay_ms: 2000
    max_delay_ms: 60000
    open_timeout_ms: 60000
    exclude_non_failback: true
    truncation_detection: true
```

**模式：** 所有模式

## 记忆系统（Mem0）

```yaml
memory:
  api_key: "mem0-key"         # Get from https://mem0.ai
  base_url: "https://api.mem0.ai"
  user_id: "nano-agent-user"
  org_id: ""                  # Optional
  project_id: ""              # Optional
  agent_id: ""                # Auto-generated if empty
```

**模式：** 所有模式（在 TUI/Daemon 中最有用）
**环境变量：** `NANO_MEMORY_API_KEY`、`NANO_MEMORY_USER_ID` 等

## 安全设置

```yaml
confirm_destructive: false

# Additional sensitive paths requiring confirmation
sensitive_read_paths:
  - "/tmp/secret"
  - "~/.claude"

# Additional exec patterns requiring confirmation
arbitrary_exec_commands:
  - "python:-m"
  - "node:--print"
```

**模式：** 所有模式
**环境变量：** `NANO_CONFIRM_DESTRUCTIVE`、`NANO_SENSITIVE_READ_PATHS`、`NANO_ARBITRARY_EXEC_COMMANDS`

## 轮次执行控制

```yaml
turn:
  max_duration: 30m    # Maximum duration per turn
```

**模式：** TUI、Daemon
**环境变量：** `NANO_TURN_MAX_DURATION`

## Ralph 循环（Stop Hook 续跑）

```yaml
hooks:
  ralph:
    enabled: true              # Allow Stop hooks to restart turns
    max_iterations: 10         # Soft limit per session
    hard_max_iterations: 50    # Hard safety cap
```

**模式：** 所有模式

## 子 Agent（专家系统）

```yaml
sub_agents:
  - agent_name: "coder"
    system_prompt: "You are a senior software engineer..."
    when_to_use: "Use for coding tasks, debugging, code reviews"
    model: "gpt-4o"
    allowed_tools: ["web_search", "write_file", "read_file", "mcp_*"]
    auto_save_memory: true
    enabled: true

  - agent_name: "writer"
    system_prompt: "You are a professional writer..."
    when_to_use: "Use for writing, documentation, content creation"
    model: "gpt-4o-mini"
    allowed_tools: ["web_search", "write_file", "read_file"]
    auto_save_memory: false
    enabled: true
```

**模式：** TUI、Daemon（子 agent 按需生成）

## 工具配置

```yaml
enabled_tools: []     # Whitelist (empty = all enabled)
disabled_tools: []    # Blacklist specific tools

# Examples:
# disabled_tools: ["run_shell_command", "web_search"]
# enabled_tools: ["read_file", "write_file"]  # Only these
```

**模式：** 所有模式
**环境变量：** `NANO_ENABLED_TOOLS`、`NANO_DISABLED_TOOLS`

## Web 搜索 API Key

```yaml
web_search_api_keys:
  serper: ""          # Serper API key
  tavily: ""          # Tavily API key
  duckduckgo: ""      # DuckDuckGo (usually doesn't need key)
```

**模式：** 所有模式（使用 web_search 工具时）
**环境变量：** `SERPER_API_KEY`、`TAVILY_API_KEY`

## 图像生成器

```yaml
image_generator:
  providers:
    - provider: "openrouter"
      model: "black-forest-labs/flux-schnell"
      api_key: ""
      base_url: ""
      enabled: true

    - provider: "seedream"
      model: "sdxl"
      api_key: ""
      base_url: ""
      enabled: true
```

**模式：** 所有模式
**环境变量：** `IMAGE_API_KEY`、`OPENROUTER_IMAGE_MODEL`、`SEEDREAM_API_KEY`

## MCP（Model Context Protocol）

```yaml
enable_mcp: false
mcp:
  enable_client: true
  default_transport: "stdio"
  timeout: 30s
  max_retries: 3
  enable_auth: false
  auth_tokens: {}

  tls:
    enabled: false
    cert_file: ""
    key_file: ""
    ca_file: ""
    skip_verify: false

  servers:
    - name: "filesystem"
      command: ["npx", "@modelcontextprotocol/server-filesystem", "/path"]
      transport: "stdio"

    - name: "git"
      command: ["npx", "@modelcontextprotocol/server-git", "--repository", "."]
      transport: "stdio"

    - name: "web-service"
      url: "https://api.example.com/mcp"
      transport: "streamable"
      headers:
        Authorization: "Bearer token"
```

**模式：** 所有模式（在 TUI/Daemon 中最有用）
**环境变量：** `NANO_ENABLE_MCP`、`NANO_MCP_DEFAULT_TRANSPORT`、`NANO_MCP_TIMEOUT`

## Daemon 配置

```yaml
daemon:
  port: 8080                          # HTTP port
  host: "127.0.0.1"                   # Bind address (0.0.0.0 for remote)
  pid_file: ""                        # Auto: ~/.nano/daemon.pid
  log_file: ""                        # Auto: ~/.nano/daemon.log

  # CORS
  enable_cors: true

  # Authentication
  api_key: ""                         # Optional API key

  # TLS
  tls_cert_file: ""
  tls_key_file: ""

  # Permission policy
  confirm_policy: "allow"             # allow, block, allowlist
  allowlisted_tools: []
```

**模式：** 仅 Daemon
**环境变量：** `NANO_DAEMON_PORT`、`NANO_DAEMON_HOST`、`NANO_DAEMON_API_KEY` 等

## Mailbox（多 Agent 通信）

```yaml
mailbox:
  enabled: false
  backend: "memory"                   # memory, file
  root_dir: ""                        # Auto: ~/.nano-agent/mailbox
  ttl_days: 7                         # Message expiration
  max_per_agent: 1000                 # Per-agent inbox limit
  max_body_kb: 16                     # Message size limit
  ack_timeout_sec: 10
  injection_limit: 5                  # Messages injected per turn
  injection_max_kb: 4
  guidance_prompt_enabled: true
  janitor_interval_sec: 60            # Cleanup interval
```

**模式：** Daemon、TUI（配合 --team 参数）

## Watcher（事件监控）

```yaml
watcher:
  enabled: false
  state_dir: ""                       # Auto: ~/.nano

  rules:
    - id: check-build-status
      source: shell
      shell_command: "./scripts/check-build.sh"
      command: "Build status: {{.OUTPUT}}"
      interval: 5m
      timeout: 10m

    # Aone MR monitoring (requires Aone CLI)
    - id: review-mrs
      source: aone
      event: new_mr
      filter: "repo:aone/a1 state:opened"
      command: "Review MR {{.MR_URL}}: {{.MR_TITLE}}"
      interval: 5m
      timeout: 30m
```

**模式：** 仅 Daemon

## Sandbox（文件系统访问控制）

```yaml
sandbox:
  enabled: false                      # Daemon: filesystem protection

  allowed_paths:                      # Readable/writable
    - "/home/user/workspace"
    - "/tmp"

  blocked_paths:                      # Completely forbidden
    - "/etc/passwd"
    - "/etc/shadow"
    - "/home/user/.ssh"
    - "/root"

  readonly_paths:                     # Read-only
    - "/usr/share/doc"

  hidden_paths:                       # Hidden from agent
    - "/home/user/.cache"

  max_file_size: 52428800             # 50MB
```

**模式：** 仅 Daemon（对所有会话强制执行）
**环境变量：** `NANO_SANDBOX_ENABLED`、`NANO_SANDBOX_ALLOWED_PATHS` 等

## 权限模式

```yaml
permission_mode: "default"            # default, acceptEdits, yolo
```

**选项：**
- `default` - 对有风险的操作请求确认
- `acceptEdits` - 自动批准文件写入，shell 命令仍需确认
- `yolo` - 跳过所有确认（危险！）

**模式：** 所有模式
**参数：** `--permission-mode`、`--dangerously-skip-permissions`

## Spinner 动词

```yaml
spinner_verbs:
  enabled: true
  mode: "append"                      # append, replace
  verbs:
    - "Brewing"
    - "Pondering"
    - "Crafting"
    - "Weaving"
```

**模式：** 仅 TUI

## 持久化允许列表

```yaml
allowed_rules:
  - "read_file"                       # Allow all file reads
  - "Bash(git *)"                     # Allow all git commands
  - "write_file(*.go)"                # Allow writing Go files
  - "Bash(npm run *)"                 # Allow npm scripts
  - "file_*"                          # Allow all file_* tools
```

**模式：** 所有模式（会话级生效）

## OSS 存储（阿里云 OSS）

```yaml
oss:
  enabled: true
  access_key_id: ""
  access_key_secret: ""
  endpoint: "oss-cn-shenzhen.aliyuncs.com"
  default_bucket: "nano-agent"
  region: "cn-shenzhen"
  timeout: 60
```

**模式：** 所有模式（用于产物存储）
**环境变量：** `OSS_ACCESS_KEY_ID`、`OSS_ACCESS_KEY_SECRET`、`OSS_ENDPOINT`

## 内置 Pprof

```yaml
enable_pprof: true
pprof_port: 6060                      # Local-only profiling server
```

**模式：** TUI、Daemon
**访问地址：** `http://127.0.0.1:6060/debug/pprof/`

## Goal 系统

```yaml
goal:
  max_turns: 10                       # Max evaluation turns
  timeout: 30m                        # Per-goal timeout
```

**模式：** Binary 模式（配合 --goal 参数）
**参数：** `--goal "condition"`、`--goal-max-turns N`

## 脱敏（日志清洗）

```yaml
redaction:
  sensitive_keys:
    - password
    - secret
    - api_key
    - token
    - authorization

  additional:
    - name: "github_token"
      regex: "ghp_[A-Za-z0-9]{36,}"
      replacement: "[REDACTED_GH_TOKEN]"

    - name: "aws_access_key"
      regex: "AKIA[0-9A-Z]{16}"
      replacement: "[REDACTED_AWS_AK]"
```

**模式：** 所有模式
**环境变量：** `NANO_REDACTION_SENSITIVE_KEYS`、`NANO_REDACTION_ADDITIONAL`

## 环境变量映射

环境变量的完整列表：

### 核心
- `NANO_API_KEY` → `api_key`
- `NANO_BASE_URL` → `base_url`
- `NANO_MODEL` → `model`
- `NANO_VERBOSE` → `verbose`

### Provider 密钥
- `NANO_DEEPSEEK_API_KEY`
- `NANO_OPENAI_API_KEY` / `OPENAI_API_KEY`
- `NANO_ANTHROPIC_API_KEY` / `ANTHROPIC_API_KEY`
- `NANO_MOONSHOT_API_KEY`
- `NANO_GEMINI_API_KEY`

### 推理
- `NANO_REASONING_ENABLED` → `reasoning.enabled`
- `NANO_REASONING_EFFORT` → `reasoning.effort`
- `NANO_REASONING_MAX_TOKENS` → `reasoning.max_tokens`

### 上下文
- `NANO_CONTEXT_MAX_TOKENS` → `context.max_tokens`
- `NANO_CONTEXT_COMPRESSION_RATIO` → `context.compression_ratio`
- `NANO_CONTEXT_PRESERVE_RECENT_TURNS` → `context.preserve_recent_turns`

### 记忆
- `NANO_MEMORY_API_KEY` → `memory.api_key`
- `NANO_MEMORY_USER_ID` → `memory.user_id`
- `NANO_MEMORY_ORG_ID` → `memory.org_id`

### Daemon
- `NANO_DAEMON_PORT` → `daemon.port`
- `NANO_DAEMON_HOST` → `daemon.host`
- `NANO_DAEMON_API_KEY` → `daemon.api_key`
- `NANO_DAEMON_LOG_FILE` → `daemon.log_file`
- `NANO_DAEMON_ENABLE_CORS` → `daemon.enable_cors`

### Sandbox
- `NANO_SANDBOX_ENABLED` → `sandbox.enabled`
- `NANO_SANDBOX_ALLOWED_PATHS` → `sandbox.allowed_paths`
- `NANO_SANDBOX_BLOCKED_PATHS` → `sandbox.blocked_paths`

### Web 搜索
- `SERPER_API_KEY` → `web_search_api_keys.serper`
- `TAVILY_API_KEY` → `web_search_api_keys.tavily`

### MCP
- `NANO_ENABLE_MCP` → `enable_mcp`
- `NANO_MCP_DEFAULT_TRANSPORT` → `mcp.default_transport`
- `NANO_MCP_TIMEOUT` → `mcp.timeout`
- `NANO_MCP_GITHUB_TOKEN` - 用于 GitHub MCP server

### Binary 模式
- `NANO_BASE_COMMIT` - 用于生成补丁的基准 commit
- `NANO_CACHE_KEY` - 用于 prompt 去重的缓存键
- `NANO_ON_EXIT` - 退出钩子命令
- `NANO_BINARY_RESULT_FORMAT` - 输出格式（plain、json、both）

## 各模式的默认行为

| 配置项 | TUI | Binary | ACP | Daemon |
|---------------|-----|--------|-----|--------|
| `enable_mcp` | ✓ | （内置） | ✓ | ✓ |
| `sandbox.enabled` | ✗ | （自动） | ✗ | （推荐） |
| `confirm_destructive` | ✓ | ✗ | ✓ | （按策略） |
| `mailbox.enabled` | (--team) | ✗ | ✗ | ✓ |
| `watcher.enabled` | ✗ | ✗ | ✗ | ✓ |

图例：
- ✓ = 通常启用
- ✗ = 通常禁用
- （自动） = 在嵌入式场景中自动启用
- （按策略） = 由 `confirm_policy` 控制
