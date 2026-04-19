# Nano Agent Daemon API 文档

## 概述

Nano Agent Daemon 提供了一个 RESTful API 和 WebSocket 接口，用于与 nano agent 进行交互。daemon 运行在后台，提供持久化的服务接口。

## 基础信息

- 基础URL: `http://{host}:{port}/api/v1`
- 默认端口: 8080
- 默认主机: 127.0.0.1
- 认证方式: API Key (可选)
- 内容类型: application/json

## 认证

如果配置了 API Key，除公开端点外均需要携带凭证：
```
X-API-Key: your-api-key
```
或：
```
Authorization: Bearer your-api-key
```

## API 端点

### 1. 健康检查

#### GET /health

检查 daemon 服务的健康状态。

**响应示例**:
```json
{
  "status": "healthy"
}
```

**响应字段**:
- `status`: 服务状态（"healthy"）

#### GET /health（v1）

同上，也可通过 v1 路径访问：

- `GET /api/v1/health`

### 2. 服务状态

#### GET /status

获取 agent 的详细状态信息。

**响应示例**:
```json
{
  "agent_status": "running",
  "mcp_enabled": true,
  "memory_size": 0,
  "active_tools": 15
}
```

**响应字段**:
- `agent_status`: Agent 状态
- `mcp_enabled`: MCP 是否启用
- `memory_size`: 内存使用大小
- `active_tools`: 活跃工具数量

### 3. 执行命令

执行入口已统一为 `POST /sessions/{id}/execute`（见第 4 节）。旧的 `POST /execute` 已移除，不再兼容 task 语义。

### 4. 会话执行（HTTP）

#### POST /sessions/{id}/execute

在指定 session 内执行一条命令。支持两种模式：
- 同步模式（默认）：阻塞等待完成并返回聚合结果
- 异步模式：仅启动执行并立即返回（用于后台执行），结果通过 WebSocket 流式获取

**请求体**:
```json
{
  "command": "生成一份项目审计报告并保存到文件",
  "timeout": 3600,
  "include_steps": false,
  "async": false
}
```

**请求字段**:
- `command`: 要执行的命令（必需）
- `timeout`: 超时时间（秒，可选，默认 3600，最大 86400）
- `include_steps`: 是否在 HTTP 响应中附带 steps（仅同步模式有效）
- `async`: 是否异步启动（true=后台执行；false=同步等待）
- `images`: 多模态图片数组（可选），结构与 WebSocket 一致

**响应示例（异步）**:
```json
{
  "success": true,
  "session_id": "sess_1234567890abcdef",
  "message": "Session execution started",
  "status": "running"
}
```

**响应示例（同步）**:
```json
{
  "success": true,
  "session_id": "sess_1234567890abcdef",
  "status": "completed",
  "completed": true,
  "result": "（聚合输出）",
  "token_stats": { "input_tokens": 1, "output_tokens": 2, "total_tokens": 3 }
}
```

### 5. 流式执行 (WebSocket)

#### GET /stream

通过 WebSocket 进行流式命令执行。

**连接信息**:
- WebSocket URL: `ws://localhost:8080/api/v1/stream`
- 认证方式:
  - URL参数: `?api_key=your-api-key`（也支持 `apikey` / `apiKey` / `key`）
  - 或请求头: `X-API-Key: your-api-key` 或 `Authorization: Bearer your-api-key` 或 `Authorization: ApiKey your-api-key`

**输入格式**:

客户端发送给服务器的消息格式为JSON对象：

**基础命令格式**:
```json
{
  "command": "要执行的命令",
  "session_id": "sess_1234567890abcdef",
  "timeout": 30
}
```

**订阅/断线续传（推荐用于长任务）**:

当客户端断线重连或需要“只订阅执行轨迹”时，可发送 `type=subscribe`。服务端会先下发 `session_start`（包含 `last_seq`），再发送快照（Planner/Executor/Worker 当前态），最后回放 `since_seq` 之后的增量事件并进入实时流。

```json
{
  "type": "subscribe",
  "session_id": "sess_1234567890abcdef",
  "run_id": "run_abcdef1234",
  "since_seq": 120,
  "streams": ["planner", "executor", "worker", "tool", "content"]
}
```

字段说明：
- `run_id`：可选；用于校验订阅的执行实例（不匹配会返回 warning error）。
- `since_seq`：可选；客户端已接收的最后一个事件序号，服务端将补发更大的 `seq`。
- `streams`：可选；事件分流过滤。支持：`planner`、`executor`、`worker`、`tool`、`content`。不传表示不过滤。

**多模态命令格式（支持图片）**:
```json
{
  "command": "分析这张图片",
  "session_id": "sess_1234567890abcdef",
  "timeout": 30,
  "images": [
    {
      "url": "https://example.com/image.jpg",
      "mime_type": "image/jpeg"
    },
    {
      "base64": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
      "mime_type": "image/png"
    }
  ]
}
```

**字段说明**:
- `command`: 字符串，要执行的命令内容（必需）
- `session_id`: 字符串，会话 ID（可选）。同一连接内不传时会使用连接级隔离 session_id
- `timeout`: 数字，命令执行的超时时间，单位为秒（可选）
- `images`: 数组，多模态图片数据（可选），支持以下格式：
  - `url`: 图片URL地址（与base64二选一）
  - `base64`: 图片的base64编码数据（与url二选一）
  - `mime_type`: 图片MIME类型（必需），支持 `image/jpeg`, `image/png`, `image/webp`, `image/gif`

**输出格式**:

服务器在执行过程中会发送以下类型的消息：

1. **会话开始消息**（首先发送，用于确认已开始/已附着）：

```json
{
  "type": "session_start",
  "session_id": "sess_1234567890abcdef",
  "run_id": "run_abcdef1234",
  "command": "创建一个简单的Python脚本",
  "status": "running",
  "since_seq": 0,
  "last_seq": 120
}
```

2. **流式事件消息**（执行过程中发送）：

服务器会发送 StreamEvent（完整事件对象）。事件会包含 `session_id` 用于隔离/续聊；不再使用 `task_id`。

3. **完成消息**（始终在最后发送）：

```json
{
  "type": "completion",
  "session_id": "sess_1234567890abcdef",
  "success": true,
  "token_stats": {
    "input_tokens": 123,
    "output_tokens": 456,
    "total_tokens": 579,
    "tokens_per_second": 60.0,
    "peak_tokens_per_second": 140.0,
    "request_size_bytes": 2048,
    "response_size_bytes": 8192,
    "start_time": 1703123400,
    "end_time": 1703123405,
    "duration_ms": 5000,
    "session_input_tokens": 1000,
    "session_output_tokens": 800,
    "session_total_tokens": 1800,
    "update_count": 5,
    "is_streaming": false
  }
}
```

**字段说明**：
- `type`: 消息类型（"session_start", "completion" 或具体的事件类型）
- `success`: 布尔值，表示执行是否成功（仅在 completion 消息中）
- `token_stats`: 最终一次的 Token 统计信息。仅在服务器能够统计到 token 信息时返回；若本次执行未产生相关统计，该字段可能为 null 或缺省
- `session_id`: 会话 ID

说明：WebSocket 渠道不会发送 `token_stats` 事件，但会在最终的 completion 消息中附带最后一次 `token_stats` 聚合结果。

**大消息分片（Chunking）**：

当单条 WebSocket 消息序列化后超过 64KB 时，服务端会自动分片发送。分片消息格式：

```json
{
  "id": "20250916083152.123456",
  "index": 0,
  "total": 3,
  "data": "分片数据内容（JSON 字符串片段）",
  "is_chunk": true,
  "complete": false
}
```

客户端应将同一 `id` 的 `data` 按 `index` 拼接为完整 JSON 字符串后再解析。

1. 事件消息（StreamEvent，对象字段随事件类型不同而不同，例如）：
```json
{
  "type": "stream_content",
  "session_id": "sess_1234567890abcdef",
  "content": "流式文本片段"
}
```

```json
{
  "type": "tool_call",
  "session_id": "sess_1234567890abcdef",
  "tool_calls": [
    { "id": "call_123", "name": "write_to_file", "arguments": "{\"file_path\":\"/tmp/script.py\",\"content\":\"print('Hello, World!')\"}" }
  ]
}
```

```json
{
  "type": "tool_result",
  "session_id": "sess_1234567890abcdef",
  "tool_result": { "id": "call_123", "content": "文件已成功创建", "error": "" }
}
```

```json
{
  "type": "error",
  "session_id": "sess_1234567890abcdef",
  "error": "错误信息",
  "severity": "warning"
}
```

```json
{
  "type": "thinking",
  "session_id": "sess_1234567890abcdef",
  "content": "正在分析用户需求与上下文..."
}
```

```json
{
  "type": "compression",
  "session_id": "sess_1234567890abcdef",
  "content": "已压缩历史上下文为若干条关键要点"
}
```


**事件类型详解**:

- `content`: 主要文本内容（注：WebSocket 渠道中为避免与 `stream_content` 重复，`content` 类型会被过滤，不发送）
- `stream_content`: 用于实时渲染的流式文本
- `tool_call`: 工具调用事件。字段：`tool_calls`（数组，元素包含 `id`, `name`, `arguments`（字符串，可能为 JSON 文本））
- `tool_result`: 工具调用结果事件。字段：`tool_result`（对象，包含 `id`, `content`, `error`）
- `tool_use`: 工具使用过程事件。字段：`tool_use`（对象，包含 `id`, `tool_name`, `parameters`, `status`, `result?`）
- `error`: 错误事件（含 `error` 与可选 `severity`）
- `done`: 完成事件（内部使用）
- `waiting_for_user`: 等待用户输入事件
- `todo_list_update` / `todo_update`: 待办事项相关事件
- `final_summary`: 最终总结
- `task_start` / `task_progress` / `task_cancel` / `task_completion`: 执行生命周期事件（事件 type 名称保留，但以 session_id 作为主标识）
- `retry` / `warning` / `debug`: 重试 / 警告 / 调试事件
- `thinking`: 思考阶段事件，可包含 `content` 或元数据，用于展示 Agent 的推理/规划状态。支持通过 `metadata` 字段区分实时和最终thinking事件：
  - 实时thinking事件：`metadata.thinking_type = "realtime"`, `metadata.is_streaming = true`
  - 最终thinking事件：`metadata.thinking_type = "final"`, `metadata.is_complete = true`
- `compression`: 上下文压缩事件，必须包含 `content` 字段，用于描述压缩摘要或策略变更
- `token_stats`: Token 用量统计事件（WebSocket 渠道不发送该中间事件）
- `satisfaction_eval`: 满意度评估等内部事件（WebSocket 渠道不发送）

**被过滤的事件类型（WebSocket）**:

以下事件类型不会发送给 WebSocket 客户端：
- `token_stats`、`debug`、`satisfaction_eval`
- `content`（为避免与 `stream_content` 的重复，WebSocket 渠道只发送 `stream_content`）

**使用示例**:

客户端发送命令：
```json
{
  "command": "创建一个简单的Python脚本",
  "timeout": 30
}
```

服务器响应流：
```json
{"type":"session_start","session_id":"sess_1234567890abcdef","command":"创建一个简单的Python脚本","status":"running"}
{"type":"task_start","session_id":"sess_1234567890abcdef","content":"Session started: 创建一个简单的Python脚本"}
{"type":"thinking","session_id":"sess_1234567890abcdef","content":"用户想要一个简单的Python脚本，我需要分析需求并选择合适的实现方式","metadata":{"thinking_type":"realtime","is_streaming":true}}
{"type":"stream_content","session_id":"sess_1234567890abcdef","content":"我将为您创建一个简单的Python脚本。"}
{"type":"tool_call","session_id":"sess_1234567890abcdef","tool_calls":[{"id":"call_123","name":"write_to_file","arguments":"{\"file_path\":\"/tmp/script.py\",\"content\":\"print('Hello, World!')\"}"}]}
{"type":"tool_result","session_id":"sess_1234567890abcdef","tool_result":{"id":"call_123","content":"文件已成功创建"}}
{"type":"thinking","session_id":"sess_1234567890abcdef","content":"我已经成功创建了一个打印Hello World的Python脚本，这是一个经典的入门示例，满足了用户的需求","metadata":{"thinking_type":"final","is_complete":true}}
{"type":"stream_content","session_id":"sess_1234567890abcdef","content":"我已经创建了一个简单的Python脚本，它会打印'Hello, World!'。"}
{"type":"completion","session_id":"sess_1234567890abcdef","success":true,"status":"completed","session_done":true}
```

**多模态使用示例（图片分析）**:

客户端发送多模态命令：
```json
{
  "command": "请分析这张图片的内容",
  "timeout": 60,
  "images": [
    {
      "url": "https://example.com/sample-image.jpg",
      "mime_type": "image/jpeg"
    }
  ]
}
```

服务器响应流：
```json
{"type":"session_start","session_id":"sess_abcdef1234567890","command":"请分析这张图片的内容","status":"running"}
{"type":"task_start","session_id":"sess_abcdef1234567890","content":"Session started: 请分析这张图片的内容"}
{"type":"thinking","session_id":"sess_abcdef1234567890","content":"用户想要分析一张图片，我需要仔细观察图片内容并提供详细描述","metadata":{"thinking_type":"realtime","is_streaming":true}}
{"type":"stream_content","session_id":"sess_abcdef1234567890","content":"我来为您分析这张图片。"}
{"type":"stream_content","session_id":"sess_abcdef1234567890","content":"从图片中我可以看到..."}
{"type":"thinking","session_id":"sess_abcdef1234567890","content":"我已经完成了对图片的分析，提供了详细的描述和见解","metadata":{"thinking_type":"final","is_complete":true}}
{"type":"completion","session_id":"sess_abcdef1234567890","success":true,"status":"completed","session_done":true}
```

**错误处理**:

如果在执行过程中发生错误，服务器会发送一个带有错误信息的事件消息，然后发送一个失败的完成消息：
```json
{"type":"error","error":"执行命令时出错：权限被拒绝"}
{"type":"completion","success":false,"error":"执行命令失败：权限被拒绝"}
```

**注意事项**:
1. 客户端应该准备好处理任何类型的事件消息，包括未在此文档中列出的新事件类型
2. 事件消息可能会以任何顺序到达，客户端应该根据事件类型适当处理
3. WebSocket连接可能会因为超时或服务器重启而断开，客户端应该实现重连逻辑
4. 完成消息总是最后一个消息，表示命令执行已结束
5. 执行期间，服务端会将客户端发送的文本消息排队，并在当前执行完成后按发送顺序依次处理（同一连接内串行执行）

### 6. MCP 管理

#### GET /mcp/status

获取 MCP 服务状态。

**响应示例**:
```json
{
  "enabled": true,
  "servers": 3,
  "tools": 25,
  "resources": 12,
  "prompts": 4,
  "connections": []
}
```

#### GET /mcp/tools

获取可用的 MCP 工具列表。

**响应示例**:
```json
{
  "tools": [
    {
      "name": "read_file",
      "description": "读取文件内容",
      "server": "filesystem",
      "connected": true
    }
  ]
}
```

#### GET /mcp/diagnostics

获取 MCP 诊断信息。

**响应示例**:
```json
{
  "status": "healthy",
  "servers": [
    {
      "name": "filesystem",
      "status": "connected",
      "last_ping": 1703123456
    }
  ],
  "metrics": {
    "total_requests": 1234,
    "failed_requests": 5,
    "avg_response_time": 150
  }
}
```

### 7. 内存管理

#### GET /memory

获取内存条目列表。

**响应示例**:
```json
{
  "entries": [
    {
      "message": "..."
    }
  ],
  "count": 1
}
```

#### POST /memory

保存内存条目。

**请求体**:
```json
{
  "key": "user_preference",
  "content": "用户偏好使用 TypeScript",
  "category": "General",
  "tags": ["preference", "typescript"],
  "priority": "medium"
}
```

**响应示例**:
```json
{
  "success": true,
  "key": "user_preference",
  "message": "Memory entry saved successfully"
}
```

#### GET /memory/{key}

获取特定的内存条目。

**响应示例**:
```json
{
  "key": "user_preference",
  "content": "用户偏好使用 TypeScript",
  "found": true
}
```

#### DELETE /memory/{key}

删除内存条目。

**响应示例**:
```json
{
  "success": false,
  "key": "user_preference",
  "error": "Delete operation not yet implemented in Memory Manager"
}
```

### 8. 监控指标

#### GET /metrics

获取当前系统指标。

**响应示例**:
```json
{
  "system": {
    "cpu_usage": 45.2,
    "memory_usage": 67.8,
    "disk_usage": 23.1,
    "load_average": [1.2, 1.5, 1.8]
  },
  "performance": {
    "requests_per_second": 12.5,
    "avg_response_time": 150,
    "error_rate": 0.02
  },
  "timestamp": 1703123456
}
```

#### GET /metrics/history

获取历史指标数据。

**查询参数**:
- `limit`: 返回的记录数量（默认 100）

**响应示例**:
```json
{
  "system_history": [
    {
      "timestamp": 1703123456,
      "cpu_usage": 45.2,
      "memory_usage": 67.8
    }
  ],
  "performance_history": [
    {
      "timestamp": 1703123456,
      "requests_per_second": 12.5,
      "avg_response_time": 150
    }
  ],
  "count": 100,
  "timestamp": 1703123456
}
```

#### GET /system/health

获取系统健康状态。

**响应示例**:
```json
{
  "overall_status": "healthy",
  "components": {
    "cpu": "healthy",
    "memory": "warning",
    "disk": "healthy",
    "network": "healthy"
  },
  "alerts": [
    {
      "level": "warning",
      "component": "memory",
      "message": "内存使用率超过 80%"
    }
  ],
  "timestamp": 1703123456
}
```

### 9. Commands

#### GET /commands

列出 daemon 可用的命令描述（基于工作目录的 command definitions）。

**响应示例**:
```json
{
  "commands": [
    {
      "name": "serve",
      "namespace": "subagent",
      "description": "启动子代理服务",
      "source": "builtin",
      "allowedTools": ["search_codebase", "read_file"]
    }
  ]
}
```

### 10. 会话（统一历史）

daemon 将所有对话/执行统一到 “session” 概念（不再区分 task）。

#### GET /sessions

返回统一历史列表。

**查询参数**:
- `limit`: 返回条数（默认 200，最大 2000）

**响应示例**:
```json
{
  "success": true,
  "sessions": [
    {
      "id": "sess_1234567890abcdef",
      "type": "session",
      "title": "Chat abcdef",
      "created_at": "2026-01-01T12:00:00Z",
      "last_active_at": "2026-01-01T12:10:00Z",
      "stored": true,
      "active": true
    }
  ]
}
```

#### GET /sessions/{id}

获取会话详情（含 metadata 与 history）。

**响应示例（chat）**:
```json
{
  "success": true,
  "session": {
    "id": "sess_1234567890abcdef",
    "created_at": "2026-01-01T12:00:00Z",
    "last_active_at": "2026-01-01T12:10:00Z",
    "metadata": {
      "type": "chat"
    },
    "history": [
      { "role": "user", "content": "你好" },
      { "role": "assistant", "content": "我能帮你做什么？" }
    ]
  },
  "history": [
    { "role": "user", "content": "你好" },
    { "role": "assistant", "content": "我能帮你做什么？" }
  ]
}
```

#### DELETE /sessions/{id}

删除会话（若仍在运行会先尝试取消）。成功时返回 JSON；找不到时返回 404。

**响应示例**:
```json
{
  "success": true,
  "message": "session deleted",
  "id": "sess_1234567890abcdef"
}
```

#### POST /sessions/{id}/cancel

取消正在运行的会话执行。

**响应示例**:
```json
{
  "success": true,
  "session_id": "sess_1234567890abcdef",
  "status": "cancelled"
}
```

#### POST /sessions/reset

重置会话历史。支持重置默认会话，或通过 `session_id` 精确指定。

**请求方式**:
- Query: `POST /sessions/reset?session_id=sess_...`
- JSON body: `{ "session_id": "sess_..." }`

**响应示例**:
```json
{
  "success": true,
  "session_id": "sess_1234567890abcdef",
  "message": "Session sess_1234567890abcdef history has been reset",
  "timestamp": 1703123456
}
```

## 错误处理

所有 API 端点都使用标准的 HTTP 状态码：

- `200 OK`: 请求成功
- `400 Bad Request`: 请求参数错误
- `401 Unauthorized`: 认证失败
- `404 Not Found`: 资源不存在
- `500 Internal Server Error`: 服务器内部错误

说明：
- 部分参数校验错误使用 `http.Error` 返回纯文本错误信息
- 部分业务错误返回 JSON（例如 `{ "success": false, "error": "..." }`）

## CORS 支持

如果启用了 CORS，daemon 会设置以下响应头：
- `Access-Control-Allow-Origin: *`
- `Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS`
- `Access-Control-Allow-Headers: Content-Type, Authorization, X-API-Key`

## 配置

Daemon 配置结构：
```json
{
  "port": 8080,
  "host": "127.0.0.1",
  "pid_file": "/path/to/daemon.pid",
  "log_file": "/path/to/daemon.log",
  "enable_cors": true,
  "api_key": "your-api-key",
  "tls_cert_file": "/path/to/cert.pem",
  "tls_key_file": "/path/to/key.pem"
}
```

## 使用示例

### cURL 示例

```bash
# 健康检查
curl -X GET http://localhost:8080/api/v1/health

# 执行命令（仅结果）
curl -X POST http://localhost:8080/api/v1/sessions/sess_demo/execute \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"command": "创建一个 README 文件"}'

# 执行命令（包含步骤）
curl -X POST http://localhost:8080/api/v1/sessions/sess_demo/execute \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"command": "创建一个 README 文件", "include_steps": true}'

# 获取内存条目
curl -X GET http://localhost:8080/api/v1/memory/project_info \
  -H "X-API-Key: your-api-key"

# 重置会话历史
curl -X POST http://localhost:8080/api/v1/sessions/reset \
  -H "X-API-Key: your-api-key"
```

### JavaScript 示例

```javascript
// 使用 fetch API（包含步骤）
const response = await fetch('http://localhost:8080/api/v1/sessions/sess_demo/execute', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'X-API-Key': 'your-api-key'
  },
  body: JSON.stringify({
    command: '分析项目结构',
    timeout: 30,
    include_steps: true,
    async: false
  })
});

const result = await response.json();
console.log(result.steps); // 包含过滤后的 StreamEvent 列表
```

### WebSocket 示例

```javascript
const ws = new WebSocket('ws://localhost:8080/api/v1/stream');

const uiState = {
  textBuffer: '',
  thinking: '',
  thinkingSummary: '',
  compression: '',
  toolActivities: [],
  errors: []
};

ws.onopen = () => {
  ws.send(JSON.stringify({ command: '创建一个新的组件', timeout: 30 }));
};

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  switch (data.type) {
    case 'stream_content': {
      uiState.textBuffer += data.content || '';
      // 例如：实时渲染到界面
      console.log('[stream]', data.content);
      break;
    }
    case 'thinking': {
      // 根据 metadata.thinking_type 区分实时和最终thinking事件
      const thinkingType = data.metadata?.thinking_type;
      if (thinkingType === 'realtime') {
        // 实时推理过程，适合显示动态思考状态
        uiState.thinking = data.content || '正在思考...';
        console.log('[thinking:realtime]', uiState.thinking);
        // 例如：在状态栏显示实时思考过程
      } else if (thinkingType === 'final') {
        // 最终推理结果，适合显示完整的思考总结
        uiState.thinkingSummary = data.content || '';
        console.log('[thinking:final]', uiState.thinkingSummary);
        // 例如：在侧边栏显示完整的推理总结
      } else {
        // 兼容旧版本或未指定类型的thinking事件
        uiState.thinking = data.content || '正在思考...';
        console.log('[thinking]', uiState.thinking);
      }
      break;
    }
    case 'compression': {
      // 压缩事件包含 content（压缩摘要/策略说明），可用于侧边状态栏或提示
      uiState.compression = data.content || '';
      console.log('[compression]', uiState.compression);
      break;
    }
    case 'tool_call':
    case 'tool_result':
    case 'tool_use': {
      uiState.toolActivities.push(data);
      console.log('[tool]', data);
      break;
    }
    case 'error': {
      uiState.errors.push(data.error);
      console.warn('[error]', data.error);
      break;
    }
    case 'completion': {
      // 最后一条完成消息，可读取 token_stats 聚合结果
      console.log('[completion]', data.success, data.token_stats || null);
      // 在此处结束加载动画、落盘结果等
      break;
    }
    default: {
      // 为向前兼容，建议记录未识别的新事件类型
      console.debug('[event]', data.type, data);
    }
  }
};

ws.onerror = (e) => console.error('WebSocket error:', e);
ws.onclose = () => console.log('WebSocket closed');
```

**最佳实践**
- 使用 `stream_content` 作为主要渲染源；`content` 事件在 WebSocket 渠道中被过滤以避免重复。
- 监听 `thinking` 事件以展示"思考/规划中"的状态，避免与 `stream_content` 竞争主区域渲染；适合放在状态栏或顶部提示。可通过 `metadata.thinking_type` 区分：
  - `"realtime"`: 实时推理过程，适合显示动态思考状态
  - `"final"`: 最终推理结果，适合显示完整的思考总结
- 监听 `compression` 事件以显示上下文压缩摘要或策略变更，帮助用户理解为什么上下文被精简。
- `token_stats` 为中间统计事件，WebSocket 渠道不发送，但会在最终 `completion` 消息中附带一次聚合结果。
- 客户端可将 `token_stats` 可视化为 `in/out/total (+ peak rate)`：
  - `input_tokens`/`output_tokens`/`total_tokens` 对应 `in/out/total`
  - `peak_tokens_per_second` 可用于显示峰值速率（例如 `120 t/s`）
  - 数值格式由客户端决定（例如 `1.2k`/`45.6`），API 返回原始数值
- 事件可能乱序到达，应基于 `type` 分发处理并保持幂等更新。
- 实现断线重连与超时处理，避免因网络中断导致的任务状态不一致。
