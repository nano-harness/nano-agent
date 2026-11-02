# Daemon 模式 API 参考

[English](./daemon-api.md)

nano-agent daemon 模式 HTTP 端点与 WebSocket 流式传输的完整 API 文档。

## 基础配置

默认 daemon 端点：`http://127.0.0.1:8080`

在 `.nano.yaml` 中配置：
```yaml
daemon:
  port: 8080
  host: "127.0.0.1"
  api_key: "optional-secret-key"  # 用于身份认证
```

## 身份认证

如果配置了 `api_key`，请在请求中携带：

```bash
curl -H "Authorization: Bearer your-api-key" http://localhost:8080/health
```

## 健康与状态端点

### GET /health

健康检查端点。

**响应：**
```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### GET /status

Daemon 状态及版本信息。

**响应：**
```json
{
  "version": "0.1.0",
  "uptime_seconds": 3600,
  "active_sessions": 5
}
```

## 会话管理

### POST /execute

在新会话或已有会话中执行命令。

**请求：**
```json
{
  "command": "fix the bug in main.go",
  "session_id": "optional-session-id",
  "timeout": 300,
  "include_steps": false
}
```

**响应：**
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

在指定会话中执行命令。

**请求：**
```json
{
  "command": "continue refactoring",
  "timeout": 300
}
```

**响应：** 与 `/execute` 相同

### GET /session/:id

获取会话信息。

**响应：**
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

删除会话及其历史记录。

**响应：**
```json
{
  "success": true,
  "message": "Session deleted"
}
```

## 团队会话（Swarm）

### POST /team-session

创建用于多 agent 协作的新团队会话。

**请求：**
```json
{
  "team_name": "backend-dev-team",
  "lead_model": "deepseek/deepseek-chat",
  "teammate_models": ["gpt-4o-mini", "claude-3-haiku"]
}
```

**响应：**
```json
{
  "team_id": "team_xyz789",
  "lead_session_id": "session_leader",
  "created_at": "2024-01-15T10:00:00Z"
}
```

### GET /team-session/:id

获取团队会话状态。

**响应：**
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

向团队会话发送消息（路由至 lead agent）。

**请求：**
```json
{
  "message": "Implement user authentication module",
  "timeout": 600
}
```

**响应：**
```json
{
  "success": true,
  "result": "Task delegated to 2 teammates...",
  "team_id": "team_xyz789"
}
```

## 记忆端点

### POST /memory/search

搜索 agent 记忆（需启用 Mem0）。

**请求：**
```json
{
  "query": "authentication implementation",
  "limit": 10
}
```

**响应：**
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

向 agent 上下文添加记忆。

**请求：**
```json
{
  "content": "User prefers TypeScript for new features",
  "metadata": {
    "category": "preference"
  }
}
```

**响应：**
```json
{
  "success": true,
  "memory_id": "mem_abc123"
}
```

## 指标与事件

### GET /metrics

兼容 Prometheus 的指标端点。

**响应：**
```
# HELP nano_requests_total Total number of requests
# TYPE nano_requests_total counter
nano_requests_total{method="POST",endpoint="/execute"} 1234

# HELP nano_active_sessions Number of active sessions
# TYPE nano_active_sessions gauge
nano_active_sessions 5
```

### GET /events

用于实时监控的 Server-Sent Events 流。

**响应（SSE 流）：**
```
event: session_start
data: {"session_id":"session_abc123","timestamp":"2024-01-15T10:00:00Z"}

event: tool_call
data: {"tool":"write_file","session_id":"session_abc123"}

event: session_complete
data: {"session_id":"session_abc123","duration_ms":5000}
```

## MCP 工具管理

### GET /mcp/tools

列出可用的 MCP 工具。

**响应：**
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

直接执行 MCP 工具。

**请求：**
```json
{
  "tool_name": "filesystem_read",
  "parameters": {
    "path": "/path/to/file.txt"
  }
}
```

**响应：**
```json
{
  "success": true,
  "result": "File contents..."
}
```

## WebSocket 流式传输

### WS /ws

用于实时流式传输的 WebSocket 端点。

**客户端消息：**
```json
{
  "type": "execute",
  "command": "implement feature X",
  "session_id": "optional"
}
```

**服务端消息：**
```json
// 流式内容
{
  "type": "stream_content",
  "content": "I'll implement feature X..."
}

// 工具调用
{
  "type": "tool_use",
  "tool_name": "write_file",
  "parameters": {...}
}

// 工具结果
{
  "type": "tool_result",
  "content": "File written successfully"
}

// 完成
{
  "type": "done",
  "token_stats": {...}
}
```

### WS /session/:id/stream

特定会话的 WebSocket 端点。

消息格式与 `/ws` 相同，但绑定到指定会话。

## 错误响应

所有端点均以下述格式返回错误：

```json
{
  "error": "Error message",
  "code": "ERROR_CODE",
  "details": {
    "additional": "context"
  }
}
```

常见错误码：
- `UNAUTHORIZED` - API key 无效或缺失
- `SESSION_NOT_FOUND` - 会话 ID 不存在
- `TIMEOUT` - 命令执行超出超时时间
- `INVALID_REQUEST` - 请求体格式错误
- `INTERNAL_ERROR` - 服务端错误

## 使用示例

### Python 客户端

```python
import requests

BASE_URL = "http://localhost:8080"
API_KEY = "your-api-key"

headers = {"Authorization": f"Bearer {API_KEY}"}

# 执行命令
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

### JavaScript 客户端

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

### WebSocket 流式传输（JavaScript）

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

## 速率限制

Daemon 模式对每个会话强制执行速率限制：
- 每个会话的最大并发执行数：1
- 每个会话的队列深度：10
- 全局并发会话数：100（可配置）

在 `.nano.yaml` 中配置：
```yaml
daemon:
  max_concurrent_sessions: 100
  max_queue_depth: 10
```

## CORS 配置

对于 Web 客户端，可启用 CORS：

```yaml
daemon:
  enable_cors: true
  # 可选：指定允许的 origin
  cors_origins:
    - "https://myapp.com"
    - "http://localhost:3000"
```

当 `enable_cors: true` 时，默认允许所有 origin。

## TLS/HTTPS

启用 TLS 以实现安全通信：

```yaml
daemon:
  port: 443
  host: "0.0.0.0"
  tls_cert_file: "/path/to/cert.pem"
  tls_key_file: "/path/to/key.pem"
```

通过 `https://your-host:443` 访问

## 高级配置

### 自定义默认超时

```yaml
daemon:
  default_timeout: 300  # 秒
  max_timeout: 3600     # 上限为 1 小时
```

### 会话持久化

会话会自动持久化到磁盘：
- 位置：`~/.nano/daemon-sessions/`
- daemon 重启后仍然保留
- 可配置 TTL 用于自动清理

```yaml
daemon:
  session_ttl_days: 30  # 30 天后自动清理
```
