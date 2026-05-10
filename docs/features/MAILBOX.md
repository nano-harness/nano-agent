# Mailbox System Documentation

## Overview

The Mailbox system provides structured, asynchronous message passing between parent and child agents in nano-agent's fork-based parallel execution model. It enables child agents to report progress, findings, and requests back to their parent agents without blocking execution.

## Key Features

- **Async Communication**: Sub-agents send messages that parent agents receive on their next turn
- **Simple Delivery Semantics**: Messages are stored per-recipient and drained by the parent when needed
- **Injection Limits**: Configurable message count and size limits per turn
- **Backend Flexibility**: Memory (single-process) or File (daemon/multi-process) backends
- **Observability**: EventTypeMailboxSent and EventTypeMailboxReceived events

## Configuration

### Basic Configuration

Add to `.nano.yaml` or `~/.config/nano/config.yaml`:

```yaml
mailbox:
  enabled: true
  backend: "memory"  # or "file" for daemon mode
  max_per_agent: 1000
  max_body_kb: 16
  ttl_days: 7
  injection_limit: 5
  injection_max_kb: 4
  guidance_prompt_enabled: true
  janitor_interval_sec: 60  # 1 minute; 0 to disable
```

### Configuration Fields

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Enable mailbox system |
| `backend` | `"memory"` | Backend type: `"memory"` (CLI) or `"file"` (daemon) |
| `root_dir` | `~/.nano/teams/<team>/mailbox` | Root directory for file backend |
| `max_per_agent` | `1000` | Maximum messages per agent inbox |
| `max_body_kb` | `16` | Maximum message body size (KB) |
| `ttl_days` | `7` | Message time-to-live before janitor cleanup |
| `injection_limit` | `5` | Max messages injected per turn |
| `injection_max_kb` | `4` | Max total size of injected messages (KB) |
| `guidance_prompt_enabled` | `true` | Include guidance text in injection |
| `janitor_interval_sec` | `60` | Janitor cleanup interval in seconds (0 = disabled) |

### Backend Selection

**Memory Backend** (Recommended for CLI):
- Single-process, in-memory storage
- Fast, no file I/O
- Lost on process restart
- Use for: Interactive CLI sessions, testing

**File Backend** (Required for Daemon):
- Persistent storage in `~/.nano/teams/<team>/mailbox/`
- Survives process restart
- Uses flock for concurrent access
- Use for: Daemon mode, multi-process scenarios, crash recovery

## Usage Scenarios

### Scenario 1: Child Progress Reporting

**Parent agent forks investigator child:**

```
Parent: "Investigate the authentication bug"
  ↓ fork(task="Investigate auth bug", agent_type="investigate")

Child (investigator):
  - Reads codebase
  - Finds potential issue
  - send_message(to="parent", topic="finding", body={
      "file": "pkg/auth/jwt.go",
      "line": 142,
      "issue": "JWT token expiry not validated"
    })
  - Continues investigation

Parent (next turn):
  System prompt includes:
  """
  # Mailbox Messages

  ## Message from investigator-child-1
  **Topic:** finding

  **Content:**
  ```json
  {
    "file": "pkg/auth/jwt.go",
    "line": 142,
    "issue": "JWT token expiry not validated"
  }
  ```
  """

  Parent: "Thanks for the finding. I'll fix the JWT validation issue."
```

### Scenario 2: Task Amendment Request

**Child requests clarification from parent:**

```
Child: "The task is ambiguous. Which authentication method should I investigate?"
  send_message(to="parent", topic="amend_task", body={
    "question": "Multiple auth methods found: JWT, OAuth2, API Key. Which one?",
    "options": ["JWT", "OAuth2", "API Key"]
  })

Parent (next turn):
  System prompt: "Child agent requests clarification..."

  Parent: "Focus on JWT authentication only."
  ↓ Re-fork child with updated task
```

### Scenario 3: Concurrent Children Reporting

**Parent forks multiple children, each sends progress:**

```
Parent forks 3 children: investigate-auth, investigate-api, investigate-db

Each child independently:
  - Performs investigation
  - Sends findings via send_message

Parent (next turn):
  - Drains mailbox: receives 3 messages
  - Messages sorted by priority (high first)
  - InjectionLimit=5 allows all 3
  - Parent synthesizes findings and decides next steps
```

## Message Topics

Standard topics for structured communication:

| Topic | Use Case |
|-------|----------|
| `progress` | Status updates ("30% complete") |
| `finding` | Discoveries, insights |
| `amend_task` | Request parent to clarify/update task |

## Ordering and Injection

Messages are stored per-recipient in the mailbox backend and delivered in FIFO order.
When a parent agent injects mailbox messages into the prompt, it may cap how many
messages (and total payload size) are injected per turn via `injection_limit` and
`injection_max_kb`.

## Operational Considerations

### Janitor Cleanup

The janitor process runs periodically to:
- Remove expired messages (older than `ttl_days`)
- Clean up stale lock files

**Configuration:**
```yaml
mailbox:
  janitor_interval_sec: 60  # Run every 1 minute (default)
```

**Disable janitor** (for testing):
```yaml
mailbox:
  janitor_interval_sec: 0
```

### Lock File Residue

If a process crashes mid-operation, lock files may remain:
```
~/.nano/teams/<team>/mailbox/<agent-id>.lock
```

**Safe cleanup** (when no agents running):
```bash
rm ~/.nano/teams/<team>/mailbox/*.lock
```

**Automatic cleanup**: Janitor opportunistically peeks mailboxes and relies on
backend TTL filtering; stale lock removal is best-effort and may require manual
cleanup if a process crashes.

### Disk Space Monitoring

For long-running daemons, monitor:
```bash
du -sh ~/.nano/teams/<team>/mailbox/
du -sh ~/.nano/teams/<team>/mailbox/archive/
```

**Recommended**: Set up log rotation or periodic archive cleanup for production deployments.

## Troubleshooting

### Messages Not Appearing in Parent

**Symptom**: Child sends message, but parent doesn't see it.

**Check**:
1. Mailbox enabled in config:
   ```yaml
   mailbox:
     enabled: true
   ```
2. Parent agent ID matches child's `to` field
3. Check event stream for `EventTypeMailboxSent` and `EventTypeMailboxReceived`
4. Verify backend backend (if file, check `~/.nano/teams/<team>/mailbox/<parent-id>.json`)

### High Disk Usage

**Symptom**: `~/.nano/teams/<team>/mailbox/` grows large

**Causes**:
1. High message volume
2. Large message bodies

**Solutions**:
- Enable janitor: `janitor_interval_sec: 300`
- Reduce `ttl_days` for faster expiry
- Reduce `max_body_kb` to limit message size

### File Backend Contention

**Symptom**: Slow mailbox operations in daemon mode

**Cause**: Multiple agents competing for file locks.

**Solutions**:
- Monitor lock acquisition latency
- Consider reducing `max_per_agent` to minimize file size
- Future: Migrate to Redis backend for better concurrency

## Event Observability

### EventTypeMailboxSent

Emitted when child successfully sends a message:

```json
{
  "type": "mailbox_sent",
  "source": "agent_tool",
  "metadata": {
    "message_id": "01HX...",
    "topic": "finding",
    "from": "child-agent-1",
    "to": "parent"
  },
  "content": "Message sent: child-agent-1 -> parent (topic: finding)"
}
```

### EventTypeMailboxReceived

Emitted when parent drains and injects messages:

```json
{
  "type": "mailbox_received",
  "source": "mailbox_inject",
  "metadata": {
    "count": 3,
    "from_agents": ["child-1", "child-2"],
    "topics": ["finding", "progress"]
  },
  "content": "Received 3 message(s) from mailbox"
}
```

## Best Practices

1. **Use Appropriate Topics**: `finding` for discoveries, `progress` for updates, `amend_task` for clarifications
2. **Keep Messages Concise**: Respect `max_body_kb` limits
3. **Enable Janitor**: Prevent disk space issues with periodic cleanup
4. **Monitor Events**: Use `EventTypeMailboxSent/Received` for debugging and observability
5. **Graceful Degradation**: Mailbox failures should not crash agents (send_message returns error, not exception)

## Future Enhancements

- **Redis Backend**: Distributed mailbox for multi-instance deployments
- **Message Filtering**: Allow parent to filter by topic/sender
- **Broadcast Messages**: Send to multiple agents
- **Message Expiry Per-Message**: Override global TTL for time-sensitive messages
