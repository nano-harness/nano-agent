# Daemon Mode API Reference

Complete API documentation for nano-agent daemon mode HTTP endpoints and WebSocket streaming.

## Base Configuration

Default daemon endpoint: `http://127.0.0.1:8080`

Configure in `.nano.yaml`:
```yaml
daemon:
  port: 8080
  host: "127.0.0.1"
  api_key: "optional-secret-key"  # For authentication
```

## Authentication

If `api_key` is configured, include it in requests:

```bash
curl -H "Authorization: Bearer your-api-key" http://localhost:8080/health
```

## Health & Status Endpoints

### GET /health

Health check endpoint.

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### GET /status

Daemon status with version info.

**Response:**
```json
{
  "version": "0.1.0",
  "uptime_seconds": 3600,
  "active_sessions": 5
}
```

## Session Management

### POST /execute

Execute a command in a new or existing session.

**Request:**
```json
{
  "command": "fix the bug in main.go",
  "session_id": "optional-session-id",
  "timeout": 300,
  "include_steps": false
}
```

**Response:**
```json
{
  "success": true,
  "result": "Fixed null pointer in line 42...",
  "session_id": "session_abc123",
  "token_stats": {
    "input_tokens": 1234,
    "output_tokens": 567,
    "total_tokens": 1801
  }
}
```

### POST /session/:id/execute

Execute command in specific session.

**Request:**
```json
{
  "command": "continue refactoring",
  "timeout": 300
}
```

**Response:** Same as `/execute`

### GET /session/:id

Get session information.

**Response:**
```json
{
  "id": "session_abc123",
  "created_at": "2024-01-15T10:00:00Z",
  "last_active": "2024-01-15T10:30:00Z",
  "message_count": 15,
  "total_tokens": 45000
}
```

### DELETE /session/:id

Delete a session and its history.

**Response:**
```json
{
  "success": true,
  "message": "Session deleted"
}
```

## Team Sessions (Swarm)

### POST /team-session

Create a new team session for multi-agent coordination.

**Request:**
```json
{
  "team_name": "backend-dev-team",
  "lead_model": "deepseek/deepseek-chat",
  "teammate_models": ["gpt-4o-mini", "claude-3-haiku"]
}
```

**Response:**
```json
{
  "team_id": "team_xyz789",
  "lead_session_id": "session_leader",
  "created_at": "2024-01-15T10:00:00Z"
}
```

### GET /team-session/:id

Get team session status.

**Response:**
```json
{
  "team_id": "team_xyz789",
  "team_name": "backend-dev-team",
  "lead_session_id": "session_leader",
  "teammate_sessions": [
    {
      "agent_id": "teammate_001",
      "session_id": "session_t001",
      "status": "active"
    }
  ],
  "mailbox_enabled": true
}
```

### POST /team-session/:id/message

Send message to team session (routes to lead agent).

**Request:**
```json
{
  "message": "Implement user authentication module",
  "timeout": 600
}
```

**Response:**
```json
{
  "success": true,
  "result": "Task delegated to 2 teammates...",
  "team_id": "team_xyz789"
}
```

## Memory Endpoints

### POST /memory/search

Search agent memory (if Mem0 enabled).

**Request:**
```json
{
  "query": "authentication implementation",
  "limit": 10
}
```

**Response:**
```json
{
  "results": [
    {
      "content": "Implemented JWT authentication...",
      "score": 0.95,
      "timestamp": "2024-01-15T09:00:00Z"
    }
  ]
}
```

### POST /memory/add

Add memory to agent context.

**Request:**
```json
{
  "content": "User prefers TypeScript for new features",
  "metadata": {
    "category": "preference"
  }
}
```

**Response:**
```json
{
  "success": true,
  "memory_id": "mem_abc123"
}
```

## Metrics & Events

### GET /metrics

Prometheus-compatible metrics endpoint.

**Response:**
```
# HELP nano_requests_total Total number of requests
# TYPE nano_requests_total counter
nano_requests_total{method="POST",endpoint="/execute"} 1234

# HELP nano_active_sessions Number of active sessions
# TYPE nano_active_sessions gauge
nano_active_sessions 5
```

### GET /events

Server-Sent Events stream for real-time monitoring.

**Response (SSE stream):**
```
event: session_start
data: {"session_id":"session_abc123","timestamp":"2024-01-15T10:00:00Z"}

event: tool_call
data: {"tool":"write_file","session_id":"session_abc123"}

event: session_complete
data: {"session_id":"session_abc123","duration_ms":5000}
```

## MCP Tool Management

### GET /mcp/tools

List available MCP tools.

**Response:**
```json
{
  "tools": [
    {
      "name": "filesystem_read",
      "server": "filesystem",
      "description": "Read file contents"
    }
  ]
}
```

### POST /mcp/execute

Execute MCP tool directly.

**Request:**
```json
{
  "tool_name": "filesystem_read",
  "parameters": {
    "path": "/path/to/file.txt"
  }
}
```

**Response:**
```json
{
  "success": true,
  "result": "File contents..."
}
```

## WebSocket Streaming

### WS /ws

WebSocket endpoint for real-time streaming.

**Client message:**
```json
{
  "type": "execute",
  "command": "implement feature X",
  "session_id": "optional"
}
```

**Server messages:**
```json
// Stream content
{
  "type": "stream_content",
  "content": "I'll implement feature X..."
}

// Tool use
{
  "type": "tool_use",
  "tool_name": "write_file",
  "parameters": {...}
}

// Tool result
{
  "type": "tool_result",
  "content": "File written successfully"
}

// Completion
{
  "type": "done",
  "token_stats": {...}
}
```

### WS /session/:id/stream

Session-specific WebSocket endpoint.

Same message format as `/ws`, but tied to specific session.

## Error Responses

All endpoints return errors in this format:

```json
{
  "error": "Error message",
  "code": "ERROR_CODE",
  "details": {
    "additional": "context"
  }
}
```

Common error codes:
- `UNAUTHORIZED` - Invalid or missing API key
- `SESSION_NOT_FOUND` - Session ID doesn't exist
- `TIMEOUT` - Command execution exceeded timeout
- `INVALID_REQUEST` - Malformed request body
- `INTERNAL_ERROR` - Server-side error

## Example Usage

### Python Client

```python
import requests

BASE_URL = "http://localhost:8080"
API_KEY = "your-api-key"

headers = {"Authorization": f"Bearer {API_KEY}"}

# Execute command
response = requests.post(
    f"{BASE_URL}/execute",
    json={
        "command": "analyze the codebase structure",
        "timeout": 300
    },
    headers=headers
)

result = response.json()
print(result["result"])
print(f"Tokens used: {result['token_stats']['total_tokens']}")
```

### JavaScript Client

```javascript
const BASE_URL = 'http://localhost:8080';
const API_KEY = 'your-api-key';

async function executeCommand(command) {
  const response = await fetch(`${BASE_URL}/execute`, {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${API_KEY}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      command,
      timeout: 300
    })
  });

  const result = await response.json();
  console.log(result.result);
}

executeCommand('refactor the authentication module');
```

### WebSocket Streaming (JavaScript)

```javascript
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onopen = () => {
  ws.send(JSON.stringify({
    type: 'execute',
    command: 'implement user profile page'
  }));
};

ws.onmessage = (event) => {
  const message = JSON.parse(event.data);

  switch(message.type) {
    case 'stream_content':
      console.log(message.content);
      break;
    case 'tool_use':
      console.log(`Using tool: ${message.tool_name}`);
      break;
    case 'done':
      console.log('Execution complete');
      ws.close();
      break;
  }
};
```

## Rate Limiting

Daemon mode enforces per-session rate limits:
- Max concurrent executions per session: 1
- Queue depth per session: 10
- Global concurrent sessions: 100 (configurable)

Configure in `.nano.yaml`:
```yaml
daemon:
  max_concurrent_sessions: 100
  max_queue_depth: 10
```

## CORS Configuration

For web clients, enable CORS:

```yaml
daemon:
  enable_cors: true
  # Optionally specify allowed origins
  cors_origins:
    - "https://myapp.com"
    - "http://localhost:3000"
```

Default allows all origins when `enable_cors: true`.

## TLS/HTTPS

Enable TLS for secure communication:

```yaml
daemon:
  port: 443
  host: "0.0.0.0"
  tls_cert_file: "/path/to/cert.pem"
  tls_key_file: "/path/to/key.pem"
```

Access via `https://your-host:443`

## Advanced Configuration

### Custom Timeout Defaults

```yaml
daemon:
  default_timeout: 300  # seconds
  max_timeout: 3600     # cap at 1 hour
```

### Session Persistence

Sessions are automatically persisted to disk:
- Location: `~/.nano/daemon-sessions/`
- Survives daemon restarts
- Configurable TTL for cleanup

```yaml
daemon:
  session_ttl_days: 30  # Auto-cleanup after 30 days
```
