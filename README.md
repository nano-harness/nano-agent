# nano-agent

[中文版本](./README_zh.md) | English

A lightweight AI-powered code assistant built in Go, featuring a modular tool architecture and turn-based conversation flow. nano-agent works with any OpenAI-compatible LLM API for intelligent code analysis, modification, and generation tasks.

## ✨ Features

- **Swarm Multi-Agent System**: Team-lead agents can spawn and coordinate multiple teammate agents for parallel task execution, with mailbox-based communication and daemon API for team session management
- **Dual Operating Modes**: TUI interactive mode and Daemon background service mode
- **Turn-Based Architecture**: Eliminates over-planning for simple queries with intelligent workflow selection
- **Dynamic Planning System**: Real-time todo list generation and adaptive execution
- **Expert System**: Specialized agents triggered via `@expert-name` syntax - built-in experts for codebase investigation, CLI help, and general tasks with support for custom expert definitions
- **Multi-Agent Mailbox**: Asynchronous message passing between parent and child agents via the Mailbox abstraction, enabling structured communication during fork-based parallel execution with support for memory and file-based backends
- **Background Task Management**: Run long-running shell commands in background with real-time output streaming, task monitoring, and graceful shutdown support
- **Modular Tool System**: Comprehensive file operations, search, web capabilities, and memory management
- **Advanced Reasoning Support**: Native support for reasoning models (e.g., o1, DeepSeek-R1) with configurable reasoning effort, token limits, and graceful fallback mechanisms
- **Skill System**: Load, parse, and match skills from personal (`~/.nano/skills/`) and project (`.nano/skills/`) directories with auto-invoke and priority-based matching
- **OpenSpec Workflow**: Spec-driven development via `/opsx:` slash commands — structured proposal → specs → design → tasks → implementation pipeline
- **Model Context Protocol (MCP) Support**: Connect to external MCP servers for extended capabilities
- **Health Monitoring & Diagnostics**: Real-time monitoring of MCP connections with comprehensive diagnostics
- **Interactive Configuration**: Guided setup wizard for MCP servers and advanced configuration
- **REST API & WebSocket Support**: HTTP API and real-time streaming for daemon mode
- **Intelligent Mode Switching**: Auto-detection of daemon and seamless mode transitions
- **Advanced Streaming Display**: Real-time feedback with animated status indicators, progress tracking, and optimized message rendering
- **Advanced Memory Management**: Intelligent conversation compression, semantic search, and versioning
- **Context Management**: Smart compression with configurable policies and automatic optimization
- **File System Sandbox**: Process-level sandboxing (Linux bwrap / macOS sandbox-exec) and path-level access control (allowed/blocked/readonly/hidden paths)
- **Cron Scheduling**: Recurring scheduled task management with cron expressions
- **Workspace & Git Tools**: Integrated workspace manager, Git operations, OSS storage, and engineering tools
- **Middleware Chain**: Pluggable security, audit, metrics, and resilience middleware for tool execution
- **Event System**: Structured event dispatching, monitoring, and validation for observability
- **Safety Features**: Command validation, workdir-aware auto-approval, file size limits, path validation, and backup support
- **Enhanced TUI Interface**: Modern terminal UI with cinematic animated banner. Default dashboard built with `tview`; optional non alt-screen Bubble Tea TUI with Claude-like styling and Standard Figlet thin-line ASCII art banner (experimental)
- **Cross-Platform Support**: Native builds for Linux, macOS, and Windows
- **Development Tools**: Comprehensive build system with testing, linting, and release automation


## Permission auto-approval

nano-agent automatically skips confirmation for read-only shell commands (`grep`, `rg`, `ls`, `find`, etc.) and filesystem edits (`write_file`, `edit_file`, `delete_file`) when all target paths are inside the agent working directory. Paths outside the working directory still require approval. See [Permission Auto-Approval](./docs/development/PERMISSION_AUTO_APPROVAL.md).

## Web Client Integration

Daemon integrations should use [Daemon API](./docs/operations/DAEMON_API.md) as the source of truth. Interactive CLI rendering now goes through a shared EventSource layer: `nano chat` and `nano lead-chat` default to BubbleTea, and `--ui tview` selects the tview backend. Automation should call `nano daemon execute --json "command"` instead of parsing TUI output.

## 🚀 Quick Start

### Prerequisites

- Go 1.25 or later
- An LLM API key (any OpenAI-compatible provider)

### Installation

#### Option 1: One-line installer (recommended)

```bash
curl -sSL https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/install.sh | bash
```

This automatically detects your OS / architecture and installs the `nano` binary to `/usr/local/bin`.

#### Option 2: Download pre-built binary

Download the latest binary directly from OSS:

| Platform | Download URL |
|----------|-------------|
| Linux x86_64 | `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-linux-amd64` |
| Linux arm64 | `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-linux-arm64` |
| macOS x86_64 | `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-darwin-amd64` |
| macOS Apple Silicon | `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-darwin-arm64` |

```bash
# Example: macOS Apple Silicon
curl -sSL https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-darwin-arm64 -o nano
chmod +x nano
sudo mv nano /usr/local/bin/
```

#### Option 3: Build from source

1. **Clone the repository**
   ```bash
   git clone https://github.com/nano-harness/nano-agent.git
   cd nano-agent
   ```

2. **Install dependencies**
   ```bash
   make deps
   ```

3. **Build and run**
   ```bash
   make build
   ./bin/nano
   ```

#### Post-installation: set up environment variables

```bash
export NANO_API_KEY="your-llm-api-key"
# Or use a .env file (recommended for local dev)
cp .env.example .env
set -a; source .env; set +a
```

### Environment Variables

Required environment variable:
```bash
export NANO_API_KEY="your-llm-api-key"
```

Optional environment variables (supported by current implementation):
- `NANO_BASE_URL`: API base URL
- `NANO_MODEL`: Model name
- `NANO_VERBOSE`: Enable verbose logging (true/false)
- `NANO_READ_FILE_MAX_LINES`: Max lines for read_file tool
- `NANO_SEARCH_MAX_RESULTS`: Max results for search tool
- `NANO_WEB_REQUEST_TIMEOUT`: Web fetch timeout (seconds)
- `NANO_WEB_SEARCH_TIMEOUT`: Web search timeout (seconds)
- `NANO_WEB_MAX_CONTENT_SIZE`: Max web content size (bytes)
- `NANO_WEB_SEARCH_MAX_RESULTS`: Max web search results
- `NANO_FILE_DIFF_MAX_LINES`: Max lines shown in file diff
- `NANO_GIT_MAX_LOG_ENTRIES`: Max git log entries
- `NANO_MEMORY_MAX_ENTRIES`: Max persistent memory entries
- `SERPER_API_KEY`: API key for Serper web search
- `TAVILY_API_KEY`: API key for Tavily web search

You can also create a `.env` file in the project directory with these variables.
To export variables from `.env` into the current shell (so child processes inherit them):
```bash
set -a; source .env; set +a
```

### Profiling (pprof)

nano-agent supports Go pprof for performance analysis.

- Enable: set `NANO_ENABLE_PPROF=true`
- Port: set `NANO_PPROF_PORT=6060` (default 6060)
- Access (local-only): `http://127.0.0.1:<port>/debug/pprof/`, e.g.:
  - `http://127.0.0.1:6060/debug/pprof/heap`
  - `http://127.0.0.1:6060/debug/pprof/profile?seconds=30`

Notes:
- TUI and binary modes start a local-only pprof server and shut it down gracefully.
- During SWE-bench runs, binary mode pprof is enabled automatically inside the container; inspect via `docker exec`:
  - `docker exec -it <container> sh -lc 'curl -s http://127.0.0.1:6060/debug/pprof/'`

## 🛠 Usage

nano-agent supports multiple operating modes to fit different development workflows and deployment scenarios.

### Operating Modes

#### 1. TUI Mode (Interactive Terminal)
The default interactive mode with a modern terminal user interface:

```bash
# Run in TUI mode (default)
make run
# or
nano

# Run with debug logging
make run-debug

# Force TUI mode (even if daemon is running)
nano --tui "your command"

# Team-lead mode with multi-agent coordination
nano --team alpha --tui
```

#### 2. Team-Lead REPL Mode
Interactive team-lead session with mailbox support for multi-agent coordination:

```bash
# Start team-lead REPL
nano chat --team alpha

# The agent can spawn teammates and receive status updates
[team-lead@alpha]> analyze the codebase for security issues using multiple teammates
```

See [Multi-Agent Runtime](./docs/features/MULTI_AGENT.md) for detailed multi-agent documentation.

#### 3. Daemon Mode (Background Service)
Run as a background service for production environments:

```bash
# Start daemon in background
nano daemon start

# Execute commands via daemon
nano "fix the bug in main.go"

# Check daemon status
nano daemon status

# Stop daemon
nano daemon stop
```

#### 4. Client Mode (API Communication)
Communicate with running daemon via command line or API:

```bash
# Use client mode explicitly
nano client exec "add error handling"
# Or include streamed steps (tool calls/results) in the response
nano client exec --include-steps "add error handling"

# Execute via REST API
curl -X POST http://localhost:8080/api/v1/sessions/sess_demo/execute \
  -H "Content-Type: application/json" \
  -d '{"command": "implement feature X"}'

# Execute via REST API and include steps
curl -X POST http://localhost:8080/api/v1/sessions/sess_demo/execute \
  -H "Content-Type: application/json" \
  -d '{"command": "implement feature X", "include_steps": true}'
```

#### 4. Bubble Tea TUI (Experimental, Non Alt-Screen)
An alternative TUI built with Bubble Tea and lipgloss, styled similarly to Claude Code. Runs in the normal screen buffer (non alt-screen), suitable for terminals where alt-screen is undesirable.

```bash
# Start Bubble Tea TUI (non alt-screen)
nano --tea "quick task"

# Start Bubble Tea TUI and type interactively
nano --tea
```

Key features:
- Header box shows welcome, help, `cwd`, and Overrides
- API Base shown from `NANO_BASE_URL` (if set)
- Colored messages: assistant (green), user (bold cyan), error (red)
- Streaming output with throttled flush and final content consolidation

Shortcuts:
- Enter: send input
- `Ctrl+Z`: cancel current task
- `Ctrl+C`: exit
- `?`: show shortcuts hint

Notes:
- Non alt-screen by default. If you prefer full-screen, use the classic `--tui` dashboard.
- Tools auto-run without per-call confirmation; status and results are summarized inline.

#### 5. Binary Mode (Non-Interactive)
Run a single command without entering the interactive TUI:

```bash
nano "fix the bug in main.go"
```

### Basic Commands

```bash
# Quick development build
make dev

# Build with version info
make build

# Run in TUI mode (default)
make run

# Run with debug logging
make run-debug

# Run in TUI mode explicitly
make run-tui

# Run in daemon mode
make run-daemon

# Complete development setup
make dev-setup
```

### Daemon Mode

```bash
# Start daemon in background
nano daemon start

# Execute commands via daemon
nano "fix the bug in main.go"

# Use client mode explicitly
nano client exec "add error handling"

# Force TUI mode (even if daemon running)
nano --tui "implement feature"

# Check daemon status
nano daemon status

# Check daemon status in JSON format
nano daemon status --json

# View daemon logs
nano daemon logs

# View recent daemon logs (last 50 lines)
nano daemon logs --lines 50

# Restart daemon
nano daemon restart

# Stop daemon
nano daemon stop

# Manage daemon configuration
nano daemon config show
nano daemon config set port 9000
nano daemon config set api_key "your-secret-key"
```

### Cron Scheduler (Routines)

Schedule recurring tasks using cron expressions:

```bash
# List all scheduled routines
nano routines list

# Add a new routine with cron expression
nano routines add --cron "0 */2 * * *" "generate status report"

# Add a routine with natural language interval
nano routines add --every 5m "check build status"

# Remove a routine
nano routines remove <task-id>

# View routine execution logs
nano routines logs

# View routine statistics
nano routines stats

# Manually trigger a routine
nano routines run <task-id>
```

When running in daemon mode, you can also manage routines via the REST API:

```bash
# Create a routine via API
curl -X POST -H "X-API-Key: $NANO_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"cron_expression":"0 */2 * * *","command":"generate status report"}' \
  http://127.0.0.1:8080/api/v1/scheduler/tasks

# List routines via API
curl -H "X-API-Key: $NANO_API_KEY" \
  http://127.0.0.1:8080/api/v1/scheduler/tasks

# Delete a routine via API
curl -X DELETE -H "X-API-Key: $NANO_API_KEY" \
  http://127.0.0.1:8080/api/v1/scheduler/tasks/<task-id>
```

### Development Commands

```bash
# Run tests
make test

# Run tests with coverage
make test-coverage

# Format and lint code
make check

# Update dependencies
make deps-update
```

### Image Generation Tool

The built-in `image_generate` tool creates images or edits existing ones via providers like OpenRouter and Seedream.

- Parameters:
  - `prompt` (required): description of the image to generate or edit.
  - `image_urls` (optional): array of image inputs; supports HTTP/HTTPS and `data:image/...` base64. Provide one or more items to edit; leave empty for pure generation.
  - `aspect_ratio` (optional): one of `1:1`, `2:3`, `3:2`, `3:4`, `4:3`, `4:5`, `5:4`, `9:16`, `16:9`, `21:9`.
  - `provider` (optional): `openrouter` or `seedream`; defaults to configured provider.

- Output:
  - Returns one or more image URLs. Use Markdown to embed: `![image](URL)`.

- Notes:
  - `image_urls` is the only input for images; single-image edit is done by passing a one-element array.
  - URLs are transferred to temporary OSS storage for convenient sharing.

## 👥 Expert System

nano-agent features a specialized expert system that allows you to delegate tasks to purpose-built agents using the `@expert-name` syntax.

### Built-in Experts

Three experts are available out of the box:

- **`@investigator`** - Read-only codebase exploration and analysis
  - Systematically investigates code structure and finds implementations
  - Returns structured reports with file references and exploration traces
  - Limited to read-only tools for safe exploration

- **`@help`** - CLI usage assistant
  - Answers questions about nano-agent CLI by consulting official documentation
  - Searches and cites relevant documentation files
  - Perfect for learning nano-agent features

- **`@generalist`** - General-purpose agent
  - Full tool access with the main agent's system prompt
  - Handles general tasks without restrictions
  - Useful for delegating complex multi-step work

### Using Experts

Simply prefix your request with `@expert-name`:

```bash
# Investigate code structure
@investigator find all authentication code

# Get CLI help
@help how do I configure MCP servers?

# Delegate a general task
@generalist refactor this module for better error handling
```

### Custom Experts

Define your own experts using markdown files with YAML frontmatter:

**Location**: `~/.config/nano/agents/` (user-level) or `.nano/agents/` (project-level)

**Example** (`~/.config/nano/agents/security-auditor.md`):

```markdown
---
name: security-auditor
description: Security-focused code reviewer
model: gpt-4o
temperature: 0.1
max_turns: 15
max_time_minutes: 10
allowed_tools:
  - read_file
  - run_shell_command
---

You are a security auditor specialized in finding vulnerabilities in code.
Focus on OWASP Top 10 issues, authentication flaws, and injection vulnerabilities.
Provide specific file locations and line numbers for any issues found.
```

Then invoke with: `@security-auditor review this authentication module`

### Expert Commands

List and inspect available experts:

```bash
# List all experts
/agents

# Show expert details
/agents:show investigator
```

## 🔄 Background Task Management

nano-agent supports running long-running shell commands in the background with comprehensive task management capabilities.

### Features

- **Non-blocking Execution**: Run commands that exceed timeout limits automatically in background mode
- **Real-time Output Streaming**: Stream command output with configurable callbacks and 16MB buffer limits
- **Task Monitoring**: List, monitor, and retrieve output from background tasks
- **Graceful Shutdown**: Automatic cleanup with SIGTERM → grace period → SIGKILL for proper process termination
- **Session Isolation**: Tasks are isolated per session with 100 task limit per session
- **Log Management**: 100MB log file size limits per task to prevent disk exhaustion

### Background Task Tools

- **`execute_shell`** with `is_background: true`: Run commands in background mode explicitly
- **`bash_output`**: Retrieve task output with incremental reading and offset tracking
- **`kill_bash`**: Gracefully terminate background tasks
- **`list_background_tasks`**: List all tasks for current session with status

### Usage Examples

```bash
# Run a long-running command in background
execute_shell "npm run build" --is-background true

# Check background task output
bash_output <task_id>

# List all background tasks
list_background_tasks

# Terminate a background task
kill_bash <task_id>
```

### Automatic Background Mode

Commands that exceed the timeout limit (default 120s, max 600s) are automatically converted to background execution, returning a task ID for monitoring.

## 🧠 Memory Management

nano-agent features advanced memory management with local SQLite storage, providing persistent, contextual memory across conversations and sessions.

### Features

- **Local SQLite Storage**: Embedded SQLite with FTS5 for fast full-text search — no external API required
- **Semantic Search**: Intelligent memory retrieval based on context
- **Cross-Session Persistence**: Memory persists across different nano-agent sessions
- **Daemon Key-Value API**: Save, retrieve, list, and delete named memory entries through `/api/v1/memory`

### Configuration

Configure memory in your `.nano.yaml` file:

```yaml
memory:
  # Memory behavior settings
  max_entries: 1000                        # Maximum number of memory entries
  auto_save: true                          # Automatically save important information
```

### Usage Examples

```bash
# Save information to memory
nano memory save "API endpoint is https://api.example.com" --category "development" --tags "api,endpoints"

# Search memory
nano memory search "API endpoint" --limit 10

# View memory statistics
nano memory stats
```

### Memory CLI Commands

```bash
# Memory management commands
nano memory save <fact> [--category <cat>] [--tags <tags>] [--priority <priority>]
nano memory search <query> [--category <cat>] [--tags <tags>] [--limit <num>]
nano memory stats

# Daemon mode memory API
curl -X GET "http://localhost:8080/api/v1/memory"
curl -X POST "http://localhost:8080/api/v1/memory" -d '{"key":"api-endpoint","content":"https://api.example.com","tags":["dev"]}'
curl -X GET "http://localhost:8080/api/v1/memory/api-endpoint"
curl -X DELETE "http://localhost:8080/api/v1/memory/api-endpoint"
```

## 🧠 Reasoning Support

nano-agent provides native support for reasoning models (e.g., o1, DeepSeek-R1, Gemini, Moonshot/Kimi), enabling enhanced problem-solving capabilities with configurable reasoning parameters.

### Features

- **Configurable Reasoning Effort**: Choose between low, medium, and high reasoning effort levels
- **Token Management**: Set maximum reasoning tokens or use model defaults
- **Graceful Fallback**: Automatic fallback to standard models when reasoning fails
- **Statistics Tracking**: Monitor reasoning usage, token consumption, and fallback events
- **Flexible Configuration**: Enable/disable reasoning per project or globally
- **Runtime Toggle**: Switch thinking mode on/off at runtime with `/think on|off|status`
- **Provider Compatibility**: Automatically skips incompatible `tool_choice` settings when reasoning is active

### Configuration

Enable reasoning in your `.nano.yaml` configuration:

```yaml
reasoning:
  enabled: true           # Enable reasoning mode
  effort: "medium"        # Reasoning effort: low, medium, high
  max_tokens: 0          # Max reasoning tokens (0 = model default)
  exclude: false         # Exclude reasoning tokens from response
```

### Runtime Control

You can toggle thinking mode at runtime using the `/think` slash command without editing configuration:

```
/think on        # Enable thinking mode (runtime override)
/think off       # Disable thinking mode (runtime override)
/think status    # Show current status and source (config/runtime/default)
/think           # Same as /think status
```

**Notes:**
- Runtime overrides via `/think on|off` take precedence over the config file setting
- The status command shows whether the setting came from runtime override or config
- Available in TUI, Bubble Tea TUI, and Daemon modes

### Usage Examples

```bash
# Use reasoning for complex problem solving
nano "Analyze this codebase and suggest architectural improvements"

# Reasoning with specific effort level
nano --config reasoning.effort=high "Debug this complex concurrency issue"

# Check reasoning statistics in daemon mode
curl http://localhost:8080/api/v1/stats
```

### Reasoning Statistics

The system tracks comprehensive reasoning usage statistics:

- **Reasoning Enabled**: Whether reasoning was used for the request
- **Reasoning Tokens**: Number of tokens used for reasoning
- **Reasoning Effort**: The effort level applied
- **Reasoning Fallback**: Whether fallback to standard model occurred
- **Reasoning Latency**: Time spent on reasoning processing

### Best Practices

1. **Use Higher Effort for Complex Tasks**: Set `effort: "high"` for architectural decisions, debugging complex issues, or code reviews
2. **Monitor Token Usage**: Track reasoning token consumption to optimize costs
3. **Configure Fallback**: Ensure graceful degradation when reasoning models are unavailable
4. **Project-Specific Settings**: Use different reasoning configurations for different types of projects
5. **Provider Compatibility**: Some OpenAI-compatible providers (e.g., Moonshot/Kimi/Gemini) reject `tool_choice=required` when thinking is enabled. nano-agent auto-skips forced `tool_choice` in this mode to avoid 400 errors—prefer letting the agent choose tools automatically when thinking is on.

## 🎯 Skill System

nano-agent supports a skill system that allows loading custom skills from personal and project directories.

### Skill Locations

- **Personal skills**: `~/.nano/skills/<skill-name>/SKILL.md`
- **Project skills**: `.nano/skills/<skill-name>/SKILL.md`

### SKILL.md Format

Each skill is defined by a `SKILL.md` file with YAML frontmatter:

```markdown
---
name: my-skill
description: "When to use this skill"
triggers:
  - "keyword1"
  - "keyword2"
auto_invoke: true
priority: 0
---

# Skill Instructions

Your skill instructions in Markdown...
```

### Skill Features

- **Auto-invoke**: Skills can be automatically invoked based on triggers
- **Priority-based matching**: Higher priority skills are preferred
- **Scope isolation**: Personal and project skills are loaded independently
- **Size limits**: Max 64KB per skill file, max 50 skills, max 5 active simultaneously

### Skill Slash Commands

- `/skill:list` — List all loaded skills
- `/skill:use <name>` — Activate a specific skill
- `/skill:off <name>` — Deactivate a skill
- `/skill:info <name>` — Show skill details
- `/skill:install <url>` — Install a skill from a URL

## 📋 OpenSpec (Spec-Driven Development)

nano-agent includes OpenSpec integration for structured, spec-driven development workflows.

### Slash Commands

- `/opsx:propose <idea>` — Create a new change proposal
- `/opsx:explore <change>` — Explore and refine a proposal
- `/opsx:spec <change>` — Generate specifications
- `/opsx:design <change>` — Create design documents
- `/opsx:tasks <change>` — Break down into tasks
- `/opsx:status` — View current OpenSpec status

### OpenSpec Workflow

Proposals flow through a structured DAG: `proposal → specs → design → tasks → implementation`

## 🏗 Architecture

### Core Components

- **Turn System** (`pkg/agent/turn.go`): Manages single interaction cycles with streaming events
- **Agent** (`pkg/agent/agent.go`): Main orchestrator for turn execution and context management
- **Fork Manager** (`pkg/agent/fork.go`): Manages forked child agents with typed roles and depth limits
- **Tool Scheduler** (`pkg/agent/tool_scheduler.go`): Intelligent tool selection and execution scheduling
- **Tool Recovery** (`pkg/agent/tool_recovery.go`): Automatic error recovery for tool execution failures
- **Context Compression** (`pkg/agent/context_compression.go`): Automatic conversation history compression and optimization
- **System Prompt** (`pkg/agent/system_prompt.go`): Dynamic system prompt generation
- **LLM Client** (`pkg/llm/`): Streaming conversation with function calling support and error handling
- **Tool System** (`pkg/tools/`): Modular tool architecture with middleware chain
- **Middleware** (`pkg/middleware/`): Security, audit, metrics, and resilience middleware chain
- **Sandbox** (`pkg/sandbox/`): Process-level and path-level file system access control
- **Skill System** (`pkg/skill/`): Skill loading, parsing, matching, and auto-invoke
- **OpenSpec** (`pkg/openspec/`): Spec-driven development workflow and artifact management
- **Event System** (`pkg/event/`): Event dispatching, monitoring, and validation
- **Cron Scheduler** (`pkg/cron/`): Recurring scheduled task management
- **Daemon Server** (`pkg/daemon/`): Background service mode with REST API and WebSocket support

### Tool Modules

- **Filesystem** (`pkg/tools/filesystem/`): File operations (read, write, edit, delete, and code skeletons)
- **System** (`pkg/tools/system/`): Shell command execution with safety controls, sandbox integration, validation, and file discovery/search via CLI commands
- **Web** (`pkg/tools/web/`): Web fetching, search capabilities (Serper, Tavily, DuckDuckGo), and API integration
- **Workspace** (`pkg/tools/workspace/`): Workspace manager, Git operations, OSS storage, and engineering tools
- **Agent** (`pkg/tools/agent/`): Agent delegation tools (main_agent for sub-agent orchestration)
- **OpenSpec** (`pkg/tools/openspec/`): OpenSpec slash command tools
- **MCP Tools** (`pkg/tools/mcp/`): Model Context Protocol tool integration and management
- **Memory** (`pkg/memory/`): Context management, storage, and semantic search

### Operating Modes

1. **TUI Mode** (Interactive): Enhanced real-time terminal interface with improved message display, streaming animations, user-friendly confirmation dialogs, and text selection/copy functionality (Ctrl+A to select all, Ctrl+C to copy) for development and debugging
2. **Daemon Mode** (Service): Background service with REST API for production environments
3. **Client Mode**: Communicate with running daemon via command line or API
4. **Bubble Tea TUI** (Experimental): Non alt-screen TUI with Claude-like styling
5. **Binary Mode** (Non-Interactive): Run a single command directly without entering the TUI

### Workflow Types

1. **Complex Tasks**: Intelligent tool scheduling → Context-aware execution → Adaptive error recovery
2. **Simple Queries**: Direct conversational responses with automatic mode detection and optimization
3. **Interactive Sessions**: Streaming responses and real-time tool execution feedback

## ⚙️ Configuration

nano-agent supports a flexible multi-level configuration system with clear priority ordering.

### Configuration Priority

Settings are loaded in the following priority order (highest to lowest):

1. **Current directory config** (`.nano.yaml`) - **Highest Priority**
2. **Global config** (`~/.config/nano/config.yaml`)
3. **Project config** (`.nano.yaml`)
4. **Environment variables** - Lowest Priority

### Quick Setup

```bash
# Set required API key
export NANO_API_KEY="your-llm-api-key"

# Copy example configuration
cp .nano.yaml.example .nano.yaml

# View configuration loading order
nano config paths
```

### Multi-provider fallback

The legacy `api_key` + `base_url` + `model` configuration still works. For
multi-provider fallback, prefer `provider/model` references and provider-specific
API key environment variables:

```yaml
model: "deepseek/deepseek-chat"
fallbacks:
  - "openai/gpt-4.1"
  - "moonshot/kimi-k2"

providers:
  deepseek:
    api_key_env: NANO_DEEPSEEK_API_KEY
  openai:
    api_key_env: OPENAI_API_KEY
  moonshot:
    api_key_env: MOONSHOT_API_KEY
```

If `providers:` is present alongside the legacy endpoint fields, the provider
schema takes precedence and nano-agent logs a deprecation warning. Plain
`api_key:` remains supported as an escape hatch, but CLI and daemon model route
output only report whether a key is set and never print the key value.

### Configuration Files

#### Global Configuration (`~/.config/nano/config.yaml`)
User-wide settings like API keys and default preferences:

```yaml
# Global user configuration
api_key: "your-llm-api-key"
model: "your-preferred-model"
base_url: "https://api.example.com/v1"

context:
  max_tokens: 80000
  enable_compression: true

# Reasoning configuration for o1 models
reasoning:
  enabled: true
  effort: "medium"    # low, medium, high
  max_tokens: 0       # 0 = model default
  exclude: false      # exclude reasoning from response

memory:
  auto_save: true
  cache_timeout: 300
```

#### Project Configuration (`.nano.yaml`)
Project-specific settings that override global config:

```yaml
# Project-specific configuration
```

# Project-specific configuration
memory:
  context_file_name: "PROJECT_CONTEXT.md"
  memory_file: "PROJECT_MEMORY.md"

custom_config:
  read_file_max_lines: 2000

blocked_commands:
  - "rm"
  - "sudo"

#### Environment Variables
For CI/CD, Docker, or temporary overrides:

```bash
# Required
NANO_API_KEY="your-llm-api-key"

# Provider-specific keys for provider/model routing
NANO_DEEPSEEK_API_KEY="your-deepseek-key"
NANO_OPENAI_API_KEY="your-openai-key"
NANO_MOONSHOT_API_KEY="your-moonshot-key"

# Optional
NANO_BASE_URL="https://api.example.com/v1"
NANO_MODEL="your-preferred-model" # can also be "provider/model"
NANO_VERBOSE="false"
NANO_READ_FILE_MAX_LINES="200"
NANO_SEARCH_MAX_RESULTS="20"
NANO_WEB_REQUEST_TIMEOUT="30"
NANO_WEB_SEARCH_TIMEOUT="10"
NANO_WEB_MAX_CONTENT_SIZE="2097152"
NANO_WEB_SEARCH_MAX_RESULTS="10"
NANO_FILE_DIFF_MAX_LINES="20"
NANO_GIT_MAX_LOG_ENTRIES="100"
NANO_MEMORY_MAX_ENTRIES="100"
SERPER_API_KEY="your-serper-api-key"
TAVILY_API_KEY="your-tavily-api-key"
```

### MCP Configuration

Model Context Protocol (MCP) enables nano-agent to connect to external servers for extended capabilities.

#### Supported Transport Types

- **`stdio`**: Standard input/output process communication (default for local servers)
- **`streamable`**: HTTP with Server-Sent Events for remote servers
- **`inmemory`**: In-memory transport for testing

**Note**: Legacy transport types (`http`, `sse`, `websocket`) are no longer supported. Use `streamable` for HTTP-based servers.

#### Quick Start

```bash
# Interactive configuration wizard
nano mcp wizard

# Add a server non-interactively
nano mcp add filesystem npx -y @modelcontextprotocol/server-filesystem /tmp

# Add a remote HTTP server
nano mcp add myserver --transport streamable https://api.example.com/mcp

# List configured servers
nano mcp list

# Test server connection
nano mcp test filesystem
```

#### Configuration File

Create or edit `~/.nano/config.yaml`:

```yaml
enable_mcp: true
mcp:
  enable_client: true
  default_transport: "stdio"
  timeout: 30s
  max_retries: 3
  servers:
    # Local stdio server
    - name: "filesystem"
      description: "File system operations"
      transport: "stdio"
      command: ["npx", "@modelcontextprotocol/server-filesystem", "./workspace"]
      enabled: true

    # Remote HTTP server with OAuth
    - name: "api-server"
      description: "Remote API server"
      transport: "streamable"
      url: "https://api.example.com/mcp"
      enabled: true
      oauth:
        authorization_url: "https://auth.example.com/oauth/authorize"
        token_url: "https://auth.example.com/oauth/token"
        client_id: "your-client-id"
        scopes: "read write"
```

#### OAuth 2.0 Authentication

For servers requiring OAuth authentication:

```bash
# Authorize with OAuth (opens browser)
nano mcp auth api-server

# View stored tokens
nano mcp auth --list

# Revoke a token
nano mcp auth api-server --revoke
```

Tokens are automatically injected into requests and refreshed when needed. See [MCP OAuth](docs/features/MCP_OAUTH.md) for detailed OAuth configuration.

#### Migration from Legacy Transports

If you have servers configured with `transport: http`, `transport: sse`, or `transport: websocket`, update them to use `transport: streamable`:

```yaml
# Before (not supported)
transport: http
url: https://api.example.com/mcp

# After (correct)
transport: streamable
url: https://api.example.com/mcp
```

### Configuration Management

```bash
# View configuration file paths and loading order
nano config paths

# Use specific config file
nano --config /path/to/config.yaml "your command"

# Interactive MCP setup
nano mcp config
```

For detailed configuration options and examples, see the `.nano.yaml.example` file.

## 🧪 Testing

Run the test suite:

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run tests with HTML coverage report
make test-coverage-html

# Run race condition tests
make test-race

# Run benchmarks
make benchmark
```

Test files are located in `e2e/` and `swe_bench_test/`.

### SWE-bench Evaluation

nano-agent has been evaluated on the SWE-bench benchmark, which tests the ability to resolve real-world GitHub issues. SWE-bench is a comprehensive benchmark that evaluates language models on software engineering tasks using actual GitHub issues and their corresponding fixes.

#### Latest Test Results

On a comprehensive test set of 31 instances, nano-agent achieved the following results:

```json
{
    "total_instances": 31,
    "submitted_instances": 31,
    "completed_instances": 31,
    "resolved_instances": 19,
    "unresolved_instances": 12,
    "empty_patch_instances": 0,
    "error_instances": 0,
    "success_rate": "61.3%"
}
```

**Performance Summary:**
- ✅ **Success Rate**: 61.3% (19/31 issues resolved)
- ✅ **Completion Rate**: 100% (31/31 instances completed)
- ✅ **Zero Errors**: No failed executions or empty patches

**Resolved Issues (19 total):**
- `django__django-10880` - Django framework issue
- `django__django-10914` - Django framework issue
- `django__django-11133` - Django framework issue
- `matplotlib__matplotlib-13989` - Plotting library issue
- `matplotlib__matplotlib-14623` - Plotting library issue
- `matplotlib__matplotlib-23314` - Plotting library issue
- `matplotlib__matplotlib-24149` - Plotting library issue
- `matplotlib__matplotlib-25311` - Plotting library issue
- `pydata__xarray-2905` - Data analysis library issue
- `pydata__xarray-3095` - Data analysis library issue
- `pytest-dev__pytest-5262` - Testing framework issue
- `pytest-dev__pytest-5631` - Testing framework issue
- `scikit-learn__scikit-learn-10297` - Machine learning library issue
- `scikit-learn__scikit-learn-10844` - Machine learning library issue
- `scikit-learn__scikit-learn-10908` - Machine learning library issue
- `sympy__sympy-11618` - Symbolic mathematics library issue
- `sympy__sympy-12096` - Symbolic mathematics library issue
- `sympy__sympy-12419` - Symbolic mathematics library issue
- `sympy__sympy-20590` - Symbolic mathematics library issue

**Unresolved Issues (12 total):**
- `astropy__astropy-12907` - Astronomy library issue
- `astropy__astropy-13033` - Astronomy library issue
- `astropy__astropy-13236` - Astronomy library issue
- `astropy__astropy-14365` - Astronomy library issue
- `astropy__astropy-14995` - Astronomy library issue
- `django__django-10097` - Django framework issue
- `django__django-10554` - Django framework issue
- `django__django-11179` - Django framework issue
- `matplotlib__matplotlib-20488` - Plotting library issue
- `psf__requests-2317` - HTTP library issue
- `sphinx-doc__sphinx-10323` - Documentation tool issue
- `sphinx-doc__sphinx-10435` - Documentation tool issue

#### Running SWE-bench Tests

To run SWE-bench evaluation on nano-agent:

```bash
# Navigate to SWE-bench test directory
cd swe_bench_test

# Install dependencies
pip install -r requirements.txt

# Run evaluation
python run_swe_bench.py
```

For more information about SWE-bench, visit the [official repository](https://github.com/princeton-nlp/SWE-bench).

## 🔧 Development

### Building

```bash
# Development build (fast)
make dev

# Production build with version info
make build

# Cross-platform release builds
make release
```

### Code Quality

```bash
# Format code
make fmt

# Run linter
make lint

# Run all quality checks
make check
```

## 📁 Project Structure

```
nano-agent/
├── cmd/nano/           # Application entry point
├── pkg/
│   ├── agent/          # Core agent logic, turn management, fork, and context compression
│   ├── cli/            # CLI interface and commands (root, daemon, client, mcp, memory, binary)
│   ├── config/         # Configuration management (multi-level, YAML + env)
│   ├── cron/           # Cron-based scheduled task management
│   ├── daemon/         # Daemon mode server and client
│   ├── event/          # Event dispatching, monitoring, and validation
│   ├── interfaces/     # Tool interfaces and contracts
│   ├── llm/            # LLM client and conversation management
│   ├── logger/         # Logging utilities
│   ├── mcp/            # Model Context Protocol implementation
│   ├── memory/         # Advanced memory management and compression
│   ├── middleware/      # Security, audit, metrics, and resilience middleware
│   ├── openspec/        # OpenSpec workflow and artifact management
│   ├── patch/          # Code patch generation utilities
│   ├── sandbox/         # File system sandboxing and access control
│   ├── skill/           # Skill loading, parsing, and matching
│   ├── tools/          # Tool implementations
│   │   ├── agent/      # Agent delegation tools (main_agent)
│   │   ├── filesystem/ # File system operations (read, write, edit, delete)
│   │   ├── mcp/        # MCP tool integration
│   │   ├── openspec/   # OpenSpec workflow tools
│   │   ├── system/     # Shell command execution and CLI-based file search/listing
│   │   ├── web/        # Web fetch, search, and API integration
│   │   └── workspace/  # Workspace manager, Git, OSS, and engineering tools
│   └── ui/             # UI adapter layer (factory pattern, tview + Bubble Tea backends)
├── e2e/                # End-to-end tests
├── scripts/            # Build and utility scripts
├── swe_bench_test/     # SWE-Bench testing framework
├── docs/               # Additional documentation
├── deployment/         # Deployment scripts and templates
├── .env.example        # Environment variables template
├── .nano.yaml.example  # Complete configuration template
├── Makefile           # Comprehensive build system
└── go.mod             # Go module definition
```

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Run tests and quality checks (`make check`)
5. Commit your changes (`git commit -m 'Add amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

### Development Guidelines

- Follow Go conventions and best practices
- Write tests for new features
- Use the existing code style and patterns
- Update documentation as needed
- Ensure all quality checks pass (`make check`)

### Development Workflow

- Branching: `main` as trunk; use `feature/<topic>` / `fix/<topic>` / `hotfix/<topic>`
- Commits: use clear messages (Conventional Commits recommended) and keep changes focused
- Quality gates: run `go fmt ./...` and `make check` before opening a PR
- PRs: include scope, rationale, test proof, and any behavior changes (CLI/daemon/TUI)
- Secrets: keep API keys and credentials in local `.env` (never commit)

## 📄 License

This project is open source. Please check the license file for details.

## 🙏 Acknowledgments

- Supports any OpenAI-compatible LLM API for intelligent code assistance
- Uses [Cobra](https://github.com/spf13/cobra) for CLI framework
- Terminal UI powered by [tview](https://github.com/rivo/tview)
- Model Context Protocol client support via [go-sdk](https://github.com/modelcontextprotocol/go-sdk) for connecting to external MCP servers

## 🌟 Model Context Protocol (MCP)

nano-agent includes comprehensive MCP support for connecting to external servers and extending capabilities:

### Quick MCP Setup

1. **Use the configuration tool:**
   ```bash
   nano mcp config
   ```

2. **Check MCP status:**
   ```bash
   nano mcp status
   nano mcp diagnostics
   ```

3. **List available tools:**
   ```bash
   nano mcp list-tools
   ```

### MCP Features

- **Health Monitoring**: Real-time server health checks with automatic recovery
- **Comprehensive Diagnostics**: Detailed reports on performance, errors, and system status
- **Interactive Configuration**: Step-by-step wizard for server setup
- **Multiple Transport Types**: Support for stdio, streamable, and in-memory transports
- **Predefined Servers**: Quick setup for popular MCP servers (filesystem, git, web search, etc.)
- **Custom Server Support**: Easy integration of custom MCP servers

### Available MCP Servers

- **Filesystem**: File operations (read, write, list, create directories)
- **Git**: Version control operations (status, diff, commit, push, pull)
- **Web Search**: Search capabilities via Brave Search API
- **Database**: PostgreSQL operations (queries, schema inspection)
- **Slack**: Workspace integration for team communication
- **Custom Servers**: Support for user-defined MCP servers

### Examples and Documentation

- **Configuration Examples**: Check `.nano.yaml.example` for various MCP server setup scenarios
- **Interactive Setup**: Use `nano mcp config` for guided configuration

## 🔄 Daemon Mode

nano-agent supports running as a background daemon service for production environments and integration scenarios.

### Quick Daemon Setup

```bash
# Start daemon
nano daemon start

# Check status
nano daemon status

# Execute commands via daemon
nano "implement user authentication"

# Stop daemon
nano daemon stop
```

### Daemon Features

- **REST API**: HTTP endpoints for command execution and status monitoring
- **WebSocket Support**: Real-time streaming communication
- **Process Management**: Automatic PID file handling and graceful shutdown
- **Security**: API key authentication and TLS support
- **Configuration**: JSON-based configuration with CLI management
- **Logging**: Configurable log files with rotation support

### API Endpoints

- `GET /api/v1/health` - Health check
- `GET /api/v1/status` - Get agent status
- `WS /api/v1/stream` - WebSocket streaming
- `GET /api/v1/mcp/*` - MCP-related endpoints
- `GET|POST|DELETE /api/v1/memory/*` - Memory management
- `POST /api/v1/sessions/{id}/execute` - Execute within a session
- `GET|POST|DELETE /api/v1/sessions/*` - Session management
- `POST /api/v1/teams/sessions` - Create team-lead session
- `GET /api/v1/teams/sessions` - List team sessions
- `GET|DELETE /api/v1/teams/sessions/{id}` - Get or delete team session
- `POST /api/v1/teams/sessions/{id}/execute` - Execute in team session

See [Daemon API](./docs/operations/DAEMON_API.md) and [Multi-Agent Runtime](./docs/features/MULTI_AGENT.md) for detailed team session API documentation.

### Daemon Configuration

```bash
# View configuration
nano daemon config show

# Set configuration values
nano daemon config set port 9000
nano daemon config set api_key "your-secret-key"
nano daemon config set enable_cors true
```

For detailed daemon configuration, see the daemon section in this README and `.nano.yaml.example`.

## 📞 Support

- Report issues: [GitHub Issues](https://github.com/nano-harness/nano-agent/issues)
- Documentation:
  - [Documentation Index](./docs/INDEX.md) - Complete documentation index and navigation
  - [Architecture](./docs/architecture/ARCHITECTURE.md) - Runtime layers, dependency direction, and core interfaces
  - [Configuration](./docs/development/CONFIGURATION.md) - Config sources, migration notes, and examples
  - [Daemon API](./docs/operations/DAEMON_API.md) - REST and WebSocket client integration guide
  - [Multi-Agent Runtime](./docs/features/MULTI_AGENT.md) - Team sessions, teammate profiles, mailbox, and governance
  - [Sandbox Design](./docs/architecture/SANDBOX_DESIGN.md) - Native and Docker sandbox runtime behavior
  - [Hooks](./docs/features/HOOKS.md) - Structured lifecycle hook protocol
  - [Extensions](./docs/features/EXTENSIONS.md) - Skill/MCP extension lifecycle and trust model
  - [Migration Guide](./docs/migration/MIGRATION_GUIDE.md) - Version migration and upgrade guide
  - Check the README files for comprehensive usage guidance
- Configuration: See `.nano.yaml.example` for configuration examples and environment variables documentation

---

Built with ❤️ for developers who want intelligent, lightweight code assistance.
