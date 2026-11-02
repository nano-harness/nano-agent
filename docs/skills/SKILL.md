---
name: nano-startup-modes
description: Use when configuring nano-agent startup modes (TUI, binary, ACP, daemon), selecting which mode to use, troubleshooting mode-specific issues, or setting up required and advanced configuration options for any nano-agent mode
---

# Nano-Agent Startup Modes Configuration Guide

[中文](./SKILL.zh-CN.md)

## Overview

nano-agent supports four operational modes, each optimized for different use cases:

- **TUI Mode** (default): Interactive terminal UI for human users, featuring bubbletea/milktea/tview UI variants with session management
- **Binary Mode**: Non-interactive one-shot execution for CI/CD, scripts, and SWE-bench evaluation with structured output
- **ACP Mode**: Agent Client Protocol server for editor integrations (Zed, JetBrains, Neovim) via stdio JSON-RPC
- **Daemon Mode**: Background HTTP server with REST API + WebSocket for web clients and automated workflows

## Mode Selection Guide

| Use Case | Recommended Mode | Why |
|----------|------------------|-----|
| Local development, interactive coding | TUI | Real-time feedback, session continuity, visual progress |
| CI/CD pipelines, automated testing | Binary | Deterministic output, exit codes, no TTY required |
| Editor integration (Zed, VSCode, etc.) | ACP | Standard protocol, native IDE experience |
| Web interface, multiple clients | Daemon | Multi-session support, REST/WebSocket API, persistence |
| SWE-bench evaluation | Binary (swebench) | Patch generation, trajectory logging, benchmark compatibility |
| Background automation tasks | Daemon | Long-running, scheduled tasks, cron support |

## Common Configuration

All modes share these core LLM configuration options (via `.nano.yaml` or environment variables):

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

**Configuration priority** (highest to lowest):
1. Command-line flags
2. Environment variables (`NANO_*`)
3. Project config (`.nano.yaml`)
4. Global config (`~/.config/nano/config.yaml`)

## TUI Mode

### Starting TUI Mode

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

### Session Management

```bash
# Resume most recent session in current project
nano --continue

# Use specific session ID
nano --session my-session-id

# Start in team-lead mode (requires mailbox)
nano --team my-team-name
```

### Required Configuration

Minimum `.nano.yaml` for TUI:
```yaml
api_key: "sk-..."
model: "deepseek-chat"
```

### Advanced TUI Configuration

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

### TUI Slash Commands

TUI supports slash commands for in-session control:
- `/clear` - Start new session
- `/help` - Show help
- `/model` - Switch model
- `/context` - Check context status
- `/think` - Enable reasoning mode
- `/allow` - Add permission rule
- `/disallow` - Remove permission rule

## Binary Mode

### Starting Binary Mode

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

### Stdin Support

Binary mode auto-detects piped input:
```bash
echo "fix typo in README" | nano
cat task.txt | nano binary exec
```

### Required Configuration

Same as TUI mode - LLM credentials required.

### Advanced Binary Configuration

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

### Exit Codes

Binary mode returns semantic exit codes:
- `0` - Success
- `10` - Needs retry (rate limit, temporary error)
- `20` - Abandoned (permanent failure)
- `30` - Timeout
- `1` - Unclassified error

### Output Artifacts

When `--output-dir` specified, binary mode generates:
- `solution.patch` - Git diff of changes
- `trajectory.json` - Execution trace with tool calls and events
- `sessions/` - Session history (if enabled)

## ACP Mode

### Starting ACP Mode

```bash
# Start ACP server (stdio mode for editors)
nano acp serve

# With custom log file (required - stdout/stderr reserved for JSON-RPC)
nano acp serve --log-file ~/.nano/acp-server.log

# With custom config
nano acp serve --config /path/to/.nano.yaml --log-level debug
```

### Editor Integration

ACP server communicates via stdin/stdout using JSON-RPC 2.0. Editors spawn nano as subprocess:

```json
// Example editor config (Zed)
{
  "command": "nano",
  "args": ["acp", "serve", "--log-file", "/tmp/nano-acp.log"]
}
```

### Required Configuration

Minimum config:
```yaml
api_key: "sk-..."
model: "deepseek-chat"
```

**Important**: ACP mode REQUIRES `--log-file` parameter since stdout/stderr are used for JSON-RPC protocol.

### Advanced ACP Configuration

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

Default log location: `~/.nano/acp-server.log`

## Daemon Mode

### Starting Daemon Mode

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

### Required Configuration

Minimum `daemon` section in `.nano.yaml`:
```yaml
daemon:
  port: 8080
  host: "127.0.0.1"
```

Or via environment variables:
```bash
NANO_DAEMON_PORT=8080
NANO_DAEMON_HOST=127.0.0.1
```

### Advanced Daemon Configuration

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

### Daemon API Endpoints

See `references/daemon-api.md` for complete API documentation. Key endpoints:

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

### Daemon Client Commands

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

### Sandbox Configuration (Daemon Only)

Daemon mode supports filesystem sandboxing:

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

### Mailbox Configuration (Daemon + Team Mode)

```yaml
mailbox:
  enabled: true
  backend: "file"                       # Options: memory, file
  root_dir: "~/.nano-agent/mailbox"    # File backend storage
  ttl_days: 7                          # Message expiration
  max_per_agent: 1000                  # Inbox capacity
  injection_limit: 5                   # Messages per turn
```

### Watcher Configuration (Daemon Only)

Event-driven monitoring for automated workflows:

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

## Configuration Priority

Configuration is loaded and merged in this order (later sources override earlier):

1. **Built-in defaults** - Hardcoded sensible defaults
2. **Global config** - `~/.config/nano/config.yaml`
3. **Project config** - `.nano.yaml` in project root
4. **Environment variables** - `NANO_*` prefixed
5. **Command-line flags** - Highest priority

Example priority chain:
```bash
# This command uses:
# - api_key from .nano.yaml (project)
# - base_url from NANO_BASE_URL env var (overrides project)
# - model from --config flag (if provided, overrides all)

NANO_BASE_URL=https://custom.api.com nano --config custom.yaml
```

## Common Mistakes & Solutions

| Issue | Solution |
|-------|----------|
| **TUI launches daemon client instead** | Use `nano --tui` to force TUI mode, or stop daemon first |
| **Binary mode hangs** | Provide prompt as argument: `nano binary exec "task"` or via stdin |
| **ACP fails: no log file** | Always specify `--log-file ~/.nano/acp-server.log` (stdout reserved for JSON-RPC) |
| **Daemon port conflict** | Set `NANO_DAEMON_PORT=8081` or configure `daemon.port` in `.nano.yaml` |
| **Can't connect remotely to daemon** | Set `daemon.host: "0.0.0.0"` + enable `api_key` and CORS |
| **Configuration not initialized** | Run `nano config init` or set `NANO_API_KEY` and `NANO_MODEL` env vars |
| **Permission denied** | Use `--permission-mode acceptEdits` or configure in `.nano.yaml` |
| **Sandbox blocks paths** | Add needed directories to `sandbox.allowed_paths` in config |
| **Wrong binary exit code** | Use `--goal "condition"` for semantic exit codes (0/10/20/30) |
| **MCP servers not connecting** | Check `enable_mcp: true` and verify server commands in config, review `nano daemon logs` |

---

For detailed API documentation, see `references/daemon-api.md`.
For complete configuration reference, see `references/config-reference.md`.
