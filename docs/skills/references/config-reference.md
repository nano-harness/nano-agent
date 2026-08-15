# Configuration Reference

[中文](./config-reference.zh-CN.md)

Complete reference for all `.nano.yaml` configuration options.

## File Locations

Configuration files are loaded in priority order:

1. `--config <path>` - Command-line flag (highest priority)
2. `.nano.yaml` - Project root
3. `~/.config/nano/config.yaml` - User global config
4. Built-in defaults - Fallback values

Environment variables prefixed with `NANO_` override file values.

## Core LLM Configuration

### Legacy Schema (Still Supported)

```yaml
api_key: "sk-..."           # NANO_API_KEY
base_url: "https://..."     # NANO_BASE_URL
model: "deepseek-chat"      # NANO_MODEL
verbose: false              # NANO_VERBOSE
```

### Multi-Provider Schema (Recommended)

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

**Modes:** All modes
**Environment:** `NANO_API_KEY`, `NANO_BASE_URL`, `NANO_MODEL`, provider-specific vars

## System Limits

```yaml
max_file_size: 10485760     # Max file size in bytes (10MB)
response_timeout: 120s      # LLM response timeout
http_timeout: 60s           # HTTP client timeout
```

**Modes:** All modes
**Environment:** `NANO_MAX_FILE_SIZE`, `NANO_RESPONSE_TIMEOUT`, `NANO_HTTP_TIMEOUT`

## Context Management

```yaml
context:
  max_tokens: 80000               # Context window limit
  compression_ratio: 0.25         # Target compression ratio
  preserve_recent_turns: 6        # Always keep N recent turns
  enable_compression: true        # Enable auto-compression
```

**Modes:** All modes
**Environment:** `NANO_CONTEXT_MAX_TOKENS`, `NANO_CONTEXT_COMPRESSION_RATIO`, etc.

## Reasoning Configuration (for o1/R1 models)

```yaml
reasoning:
  enabled: false              # Enable reasoning mode
  effort: "medium"            # low, medium, high
  max_tokens: 0              # 0 = model default
  exclude: false             # Exclude reasoning from response
```

**Modes:** All modes
**Environment:** `NANO_REASONING_ENABLED`, `NANO_REASONING_EFFORT`, `NANO_REASONING_MAX_TOKENS`

## Advanced Resilience

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

**Modes:** All modes

## Memory System (Mem0)

```yaml
memory:
  api_key: "mem0-key"         # Get from https://mem0.ai
  base_url: "https://api.mem0.ai"
  user_id: "nano-agent-user"
  org_id: ""                  # Optional
  project_id: ""              # Optional
  agent_id: ""                # Auto-generated if empty
```

**Modes:** All modes (most useful in TUI/Daemon)
**Environment:** `NANO_MEMORY_API_KEY`, `NANO_MEMORY_USER_ID`, etc.

## Safety Settings

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

**Modes:** All modes
**Environment:** `NANO_CONFIRM_DESTRUCTIVE`, `NANO_SENSITIVE_READ_PATHS`, `NANO_ARBITRARY_EXEC_COMMANDS`

## Turn Execution Control

```yaml
turn:
  max_duration: 30m    # Maximum duration per turn
```

**Modes:** TUI, Daemon
**Environment:** `NANO_TURN_MAX_DURATION`

## Ralph Loop (Stop Hook Continuation)

```yaml
hooks:
  ralph:
    enabled: true              # Allow Stop hooks to restart turns
    max_iterations: 10         # Soft limit per session
    hard_max_iterations: 50    # Hard safety cap
```

**Modes:** All modes

## Sub-Agents (Expert System)

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

**Modes:** TUI, Daemon (sub-agents spawned on demand)

## Tool Configuration

```yaml
enabled_tools: []     # Whitelist (empty = all enabled)
disabled_tools: []    # Blacklist specific tools

# Examples:
# disabled_tools: ["run_shell_command", "web_search"]
# enabled_tools: ["read_file", "write_file"]  # Only these
```

**Modes:** All modes
**Environment:** `NANO_ENABLED_TOOLS`, `NANO_DISABLED_TOOLS`

## Web Search API Keys

```yaml
web_search_api_keys:
  serper: ""          # Serper API key
  tavily: ""          # Tavily API key
  duckduckgo: ""      # DuckDuckGo (usually doesn't need key)
```

**Modes:** All modes (when web_search tool used)
**Environment:** `SERPER_API_KEY`, `TAVILY_API_KEY`

## Image Generator

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

**Modes:** All modes
**Environment:** `IMAGE_API_KEY`, `OPENROUTER_IMAGE_MODEL`, `SEEDREAM_API_KEY`

## MCP (Model Context Protocol)

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

**Modes:** All modes (most useful in TUI/Daemon)
**Environment:** `NANO_ENABLE_MCP`, `NANO_MCP_DEFAULT_TRANSPORT`, `NANO_MCP_TIMEOUT`

## Daemon Configuration

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

**Modes:** Daemon only
**Environment:** `NANO_DAEMON_PORT`, `NANO_DAEMON_HOST`, `NANO_DAEMON_API_KEY`, etc.

## Mailbox (Multi-Agent Communication)

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

**Modes:** Daemon, TUI (with --team flag)

## Watcher (Event Monitoring)

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

**Modes:** Daemon only

## Sandbox (File System Access Control)

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

**Modes:** Daemon only (enforced for all sessions)
**Environment:** `NANO_SANDBOX_ENABLED`, `NANO_SANDBOX_ALLOWED_PATHS`, etc.

## Permission Mode

```yaml
permission_mode: "default"            # default, acceptEdits, yolo
```

**Options:**
- `default` - Ask for confirmation on risky operations
- `acceptEdits` - Auto-approve file writes, still ask for shell
- `yolo` - Skip ALL confirmations (dangerous!)

**Modes:** All modes
**Flags:** `--permission-mode`, `--dangerously-skip-permissions`

## Spinner Verbs

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

**Modes:** TUI only

## Persistent Allowlist

```yaml
allowed_rules:
  - "read_file"                       # Allow all file reads
  - "Bash(git *)"                     # Allow all git commands
  - "write_file(*.go)"                # Allow writing Go files
  - "Bash(npm run *)"                 # Allow npm scripts
  - "file_*"                          # Allow all file_* tools
```

**Modes:** All modes (session-scoped)

## OSS Storage (Aliyun OSS)

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

**Modes:** All modes (for artifact storage)
**Environment:** `OSS_ACCESS_KEY_ID`, `OSS_ACCESS_KEY_SECRET`, `OSS_ENDPOINT`

## Built-in Pprof

```yaml
enable_pprof: true
pprof_port: 6060                      # Local-only profiling server
```

**Modes:** TUI, Daemon
**Access:** `http://127.0.0.1:6060/debug/pprof/`

## Goal System

```yaml
goal:
  max_turns: 10                       # Max evaluation turns
  timeout: 30m                        # Per-goal timeout
```

**Modes:** Binary mode (with --goal flag)
**Flags:** `--goal "condition"`, `--goal-max-turns N`

## Redaction (Log Sanitization)

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

**Modes:** All modes
**Environment:** `NANO_REDACTION_SENSITIVE_KEYS`, `NANO_REDACTION_ADDITIONAL`

## Environment Variable Mapping

Complete list of environment variables:

### Core
- `NANO_API_KEY` → `api_key`
- `NANO_BASE_URL` → `base_url`
- `NANO_MODEL` → `model`
- `NANO_VERBOSE` → `verbose`

### Provider Keys
- `NANO_DEEPSEEK_API_KEY`
- `NANO_OPENAI_API_KEY` / `OPENAI_API_KEY`
- `NANO_ANTHROPIC_API_KEY` / `ANTHROPIC_API_KEY`
- `NANO_MOONSHOT_API_KEY`
- `NANO_GEMINI_API_KEY`

### Reasoning
- `NANO_REASONING_ENABLED` → `reasoning.enabled`
- `NANO_REASONING_EFFORT` → `reasoning.effort`
- `NANO_REASONING_MAX_TOKENS` → `reasoning.max_tokens`

### Context
- `NANO_CONTEXT_MAX_TOKENS` → `context.max_tokens`
- `NANO_CONTEXT_COMPRESSION_RATIO` → `context.compression_ratio`
- `NANO_CONTEXT_PRESERVE_RECENT_TURNS` → `context.preserve_recent_turns`

### Memory
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

### Web Search
- `SERPER_API_KEY` → `web_search_api_keys.serper`
- `TAVILY_API_KEY` → `web_search_api_keys.tavily`

### MCP
- `NANO_ENABLE_MCP` → `enable_mcp`
- `NANO_MCP_DEFAULT_TRANSPORT` → `mcp.default_transport`
- `NANO_MCP_TIMEOUT` → `mcp.timeout`
- `NANO_MCP_GITHUB_TOKEN` - For GitHub MCP server

### Binary Mode
- `NANO_BASE_COMMIT` - Base commit for patch generation
- `NANO_CACHE_KEY` - Cache key for prompt deduplication
- `NANO_ON_EXIT` - Exit hook command
- `NANO_BINARY_RESULT_FORMAT` - Output format (plain, json, both)

## Mode-Specific Defaults

| Config Option | TUI | Binary | ACP | Daemon |
|---------------|-----|--------|-----|--------|
| `enable_mcp` | ✓ | (embedded) | ✓ | ✓ |
| `sandbox.enabled` | ✗ | (auto) | ✗ | (recommended) |
| `confirm_destructive` | ✓ | ✗ | ✓ | (policy) |
| `mailbox.enabled` | (--team) | ✗ | ✗ | ✓ |
| `watcher.enabled` | ✗ | ✗ | ✗ | ✓ |

Legend:
- ✓ = Typically enabled
- ✗ = Typically disabled
- (auto) = Auto-enabled in embedded contexts
- (policy) = Controlled by `confirm_policy`
