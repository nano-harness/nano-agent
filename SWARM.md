# Swarm Multi-Agent System

The Swarm system enables nano-agent to operate as a coordinated team of AI agents, where a team-lead agent can spawn and coordinate multiple teammate agents to work on different aspects of a complex task in parallel.

## Overview

The Swarm architecture consists of three main components:

1. **Team-Lead Agent**: The primary agent that coordinates the team and delegates tasks
2. **Teammate Agents**: Subprocess agents spawned by the team-lead to handle specific subtasks
3. **Mailbox System**: Message-passing infrastructure for inter-agent communication

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Team-Lead Agent                       │
│  - Coordinates overall task execution                        │
│  - Spawns teammates via spawn_teammate tool                  │
│  - Receives status updates via mailbox                       │
│  - Aggregates results from all teammates                     │
└──────────────┬──────────────────────────────┬───────────────┘
               │                               │
        ┌──────┴──────┐               ┌───────┴────────┐
        │ Mailbox     │               │  Mailbox       │
        │ (Team Lead) │               │  (Team Lead)   │
        └──────┬──────┘               └───────┬────────┘
               │                               │
    ┌──────────▼──────────┐         ┌─────────▼──────────┐
    │  Teammate Agent 1   │         │  Teammate Agent 2   │
    │  - Executes subtask │         │  - Executes subtask │
    │  - Sends updates    │         │  - Sends updates    │
    │  - Uses send_message│         │  - Uses send_message│
    └─────────────────────┘         └────────────────────┘
```

## Usage

### 1. Team-Lead REPL Mode

Start an interactive team-lead session:

```bash
nano chat --team alpha
```

This launches a REPL where the agent acts as a team-lead with mailbox support. The agent can:
- Spawn teammates using the `spawn_teammate` tool
- Receive messages from teammates automatically at each turn
- Coordinate complex multi-step tasks

For daemon-backed long sessions, start the daemon and use the WebSocket REPL client:

```bash
nano daemon start
nano lead-chat --team alpha
```

`nano lead-chat` creates or resumes a daemon team-lead session, sends each REPL line
as a `lead_input` WebSocket frame, and resumes streaming from the last received
sequence number after transient disconnects. When a tool requires confirmation,
the daemon emits an approval request over the same stream; `nano lead-chat`
prompts in the terminal and sends the decision back without ending the turn.

### 2. Team-Lead TUI Mode

Start the TUI in team-lead mode:

```bash
nano --team beta --tui
```

Or with Bubble Tea TUI:

```bash
nano --team gamma --tea
```

The TUI will display:
- Messages from teammates in the chat interface
- Team coordination status
- Parallel task execution

### 3. Daemon Mode with Team Sessions

Start the daemon:

```bash
nano daemon start
```

Create a team-lead session via API:

```bash
curl -X POST http://localhost:4380/api/v1/teams/sessions \
  -H "Content-Type: application/json" \
  -d '{"team_name": "delta"}'
```

Execute commands in the team session:

```bash
curl -X POST http://localhost:4380/api/v1/teams/sessions/{session_id}/execute \
  -H "Content-Type: application/json" \
  -d '{"command": "analyze the codebase and find potential bugs"}'
```

List all team sessions:

```bash
curl http://localhost:4380/api/v1/teams/sessions
```

## Daemon API Endpoints

### Create Team Session
```
POST /api/v1/teams/sessions
Content-Type: application/json

{
  "session_id": "optional-custom-id",
  "team_name": "team-alpha",
  "interactive_confirm": true
}

Response:
{
  "session_id": "abc123",
  "team_name": "team-alpha",
  "created_at": "2026-04-25T07:00:00Z",
  "last_active_at": "2026-04-25T07:00:00Z"
}
```

### List Team Sessions
```
GET /api/v1/teams/sessions

Response:
[
  {
    "session_id": "abc123",
    "team_name": "team-alpha",
    "created_at": "2026-04-25T07:00:00Z",
    "last_active_at": "2026-04-25T07:05:00Z"
  },
  ...
]
```

### Get Team Session
```
GET /api/v1/teams/sessions/{session_id}

Response:
{
  "session_id": "abc123",
  "team_name": "team-alpha",
  "created_at": "2026-04-25T07:00:00Z",
  "last_active_at": "2026-04-25T07:05:00Z"
}
```

### Execute in Team Session
```
POST /api/v1/teams/sessions/{session_id}/execute
Content-Type: application/json

{
  "command": "analyze code for security vulnerabilities"
}

Response:
{
  "success": true,
  "events": [
    {"type": "content", "content": "Starting analysis..."},
    ...
  ]
}
```

### Stream Team Session
```
GET /api/v1/teams/sessions/{session_id}/stream
Upgrade: websocket
```

Supported client frames:

```json
{"type":"subscribe","since_seq":42}
{"type":"lead_input","command":"analyze code","task_id":"optional-task-id","since_seq":42}
{"type":"tool_approval","call_id":"tool-call-id","approved":true}
{"type":"cancel"}
{"type":"ping"}
```

Server frames include:
- `session_start` for replay/subscribe attachment
- `lead_input_ack` after accepting a REPL input
- `waiting_for_user` with metadata `kind=tool_approval_request` when a tool needs confirmation
- `tool_approval_ack` after accepting an approval decision
- regular `StreamEvent` frames such as `content`, `stream_content`, `tool_call`, and `task_completion`
- `completion` with `last_seq`, `status`, and `success`
- `error` for invalid frames or rejected input

Use `last_seq`/`since_seq` to resume without replaying already-rendered events.

### Delete Team Session
```
DELETE /api/v1/teams/sessions/{session_id}

Response:
{
  "success": true,
  "message": "Session deleted"
}
```

## Tools Available in Team Mode

### For Team-Lead Agents

#### spawn_teammate
Spawns a new teammate agent to handle a subtask:

```json
{
  "tool": "spawn_teammate",
  "parameters": {
    "name": "code-analyzer",
    "task": "Analyze the authentication module for security issues"
  }
}
```

The tool returns when the teammate completes its task.

### For Teammate Agents

#### send_message
Sends a status update or result to the team-lead:

```json
{
  "tool": "send_message",
  "parameters": {
    "message": "Found 3 potential SQL injection vulnerabilities",
    "priority": "high"
  }
}
```

Priority levels: `low`, `medium`, `high`, `urgent`

## Mailbox System

The mailbox system enables asynchronous communication between team-lead and teammate agents.

### Message Flow

1. **Teammate → Team-Lead**: Teammates use `send_message` tool to send updates
2. **Automatic Injection**: Messages are automatically injected at the start of each team-lead turn
3. **Message Formatting**: Messages include sender ID, timestamp, and priority
4. **Message Cleanup**: Messages are marked as read after being delivered

### Message Structure

```go
type Message struct {
    ID        string
    From      string
    Content   string
    Priority  string
    Timestamp time.Time
    Metadata  map[string]interface{}
}
```

### Mailbox Storage

- **Filesystem Backend**: Messages stored under `~/.nano/teams/<team>/mailbox/`
- **Atomic Operations**: Lock files ensure concurrent safety
- **Persistence**: Messages survive process restarts

## Configuration

### Environment Variables

- `NANO_DISABLE_TEAM_SESSIONS`: Set to `true` to disable team-lead sessions in daemon mode

### Config File Options

```yaml
# Enable swarm functionality (default: true)
enable_swarm: true

# Mailbox configuration
mailbox:
  backend: "filesystem"  # Currently only filesystem is supported
  base_path: "~/.nano/teams"  # team mailboxes resolve to ~/.nano/teams/<team>/mailbox
  max_messages: 1000
  retention_days: 7
```

## Use Cases

### 1. Parallel Code Analysis

```
Team-Lead: "Analyze the entire codebase for security issues"
  ├─ Teammate 1: Analyze authentication module
  ├─ Teammate 2: Analyze API endpoints
  ├─ Teammate 3: Analyze database queries
  └─ Team-Lead: Aggregate findings and create report
```

### 2. Multi-Component Development

```
Team-Lead: "Build a REST API with authentication"
  ├─ Teammate 1: Create database schema
  ├─ Teammate 2: Implement authentication middleware
  ├─ Teammate 3: Create API endpoints
  └─ Team-Lead: Integration and testing
```

### 3. Research and Documentation

```
Team-Lead: "Research best practices and write documentation"
  ├─ Teammate 1: Research security best practices
  ├─ Teammate 2: Research performance optimization
  ├─ Teammate 3: Research testing strategies
  └─ Team-Lead: Synthesize findings and write comprehensive guide
```

## Session Management

### Automatic Cleanup

Team sessions are automatically cleaned up after 30 minutes of inactivity (configurable via `TeamLeadRegistry`).

### Manual Cleanup

Delete a team session:
```bash
curl -X DELETE http://localhost:4380/api/v1/teams/sessions/{session_id}
```

Or programmatically via the daemon API.

## Limitations and Best Practices

### Current Limitations

1. **Subprocess Communication**: Teammates run as separate processes, which has overhead
2. **No Nested Teams**: Teammates cannot spawn their own sub-teammates
3. **Filesystem Mailbox**: Currently only filesystem-based mailbox is supported
4. **Token Limits**: Each teammate has independent token limits

### Best Practices

1. **Task Decomposition**: Break tasks into clear, independent subtasks for teammates
2. **Message Frequency**: Teammates should send progress updates at logical checkpoints
3. **Error Handling**: Team-lead should handle teammate failures gracefully
4. **Resource Management**: Limit concurrent teammates to avoid resource exhaustion
5. **Clean Shutdown**: Always clean up team sessions when done

## Troubleshooting

### Issue: Teammate not receiving mailbox messages

**Solution**: Check mailbox permissions and ensure mailbox path exists:
```bash
ls -la ~/.nano/teams/<team-name>/mailbox/
```

### Issue: Team session idle timeout too short

**Solution**: Adjust timeout in daemon initialization:
```go
registry := NewTeamLeadRegistry(60 * time.Minute) // 60 minutes
```

### Issue: Too many concurrent teammates

**Solution**: Implement teammate pooling or sequential execution in team-lead logic

## Future Enhancements

Planned improvements for future releases:

1. **Remote Mailbox Backends**: Redis, PostgreSQL support for distributed teams
2. **Team Hierarchies**: Allow teammates to spawn sub-teammates
3. **Team Observability**: Dashboard for monitoring team activity
4. **Dynamic Team Scaling**: Auto-scale teammates based on workload
5. **Inter-Team Communication**: Enable multiple teams to collaborate

## Migration Guide

See [MIGRATION.md](./MIGRATION.md) for detailed migration instructions from single-agent to swarm mode.

## Contributing

When contributing swarm-related features:

1. Add tests to `e2e/team_session_test.go` and `e2e/swarm_cli_test.go`
2. Update this documentation
3. Follow the existing patterns in `pkg/engine/`, `pkg/daemon/`, and `pkg/swarm/`
4. Ensure backward compatibility with non-swarm mode

## See Also

- [README.md](./README.md) - General nano-agent documentation
- [MIGRATION.md](./MIGRATION.md) - Migration guide for swarm adoption
- `pkg/swarm/` - Swarm implementation source code
- `pkg/daemon/team_session.go` - Team session management
- `e2e/team_session_test.go` - E2E test examples
