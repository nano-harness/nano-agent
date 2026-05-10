# WebSocket 消息分片解决方案

## 问题描述

在透传 `/api/v1/stream` 响应时，当单个WebSocket消息过大时会出现以下错误：

```
WebSocket连接关闭: 状态: CloseStatus[code=1009, reason=The decoded text message was too big for the output buffer and the endpoint does not support partial messages]
```

错误码1009表示消息过大，超过了WebSocket的缓冲区限制。

## 解决方案

### 1. 服务端改进

在 `pkg/daemon/connection_manager.go` 中实现了消息分片机制：

#### 核心特性：
- **自动分片**: 当消息大小超过64KB时自动分片
- **分片大小**: 每个分片32KB，确保在安全范围内
- **消息重组**: 客户端可以根据分片信息重组完整消息
- **错误处理**: 增强了对消息过大错误的识别和处理

#### 分片消息格式：
```json
{
  "id": "20250916083152.123456",
  "index": 0,
  "total": 3,
  "data": "分片数据内容...",
  "is_chunk": true,
  "complete": false
}
```

#### 关键参数：
- `id`: 消息唯一标识符
- `index`: 分片索引（从0开始）
- `total`: 总分片数量
- `data`: 当前分片的数据内容
- `is_chunk`: 标识这是一个分片消息
- `complete`: 标识是否为最后一个分片

### 2. 客户端处理

提供了完整的HTML客户端示例 (`examples/websocket_chunked_client.html`)，演示如何：

- 检测分片消息
- 缓存和重组分片
- 处理重组后的完整消息
- 显示分片进度信息

#### 客户端核心逻辑：
```javascript
function handleChunkedMessage(chunk) {
    const { id, index, total, data: chunkData, complete } = chunk;

    // 初始化分片存储
    if (!messageChunks.has(id)) {
        messageChunks.set(id, {
            chunks: new Array(total),
            receivedCount: 0
        });
    }

    // 存储分片
    const messageData = messageChunks.get(id);
    messageData.chunks[index] = chunkData;
    messageData.receivedCount++;

    // 检查是否接收完所有分片
    if (complete && messageData.receivedCount === total) {
        // 重组完整消息
        const fullMessage = messageData.chunks.join('');
        const reconstructedData = JSON.parse(fullMessage);
        handleRegularMessage(reconstructedData);

        // 清理缓存
        messageChunks.delete(id);
    }
}
```

## 配置参数

可以通过修改 `pkg/daemon/connection_manager.go` 中的常量来调整分片行为：

```go
const (
    // 最大消息大小 (64KB)，超过此大小将进行分片
    MaxMessageSize = 64 * 1024
    // 分片大小 (32KB)，确保每个分片都在安全范围内
    ChunkSize = 32 * 1024
)
```

## 使用方法

### 1. 启动daemon服务
```bash
nano daemon start
```

### 2. 使用提供的客户端示例
打开 `examples/websocket_chunked_client.html` 在浏览器中测试。

### 3. 或者集成到现有客户端
参考示例代码，在现有WebSocket客户端中添加分片处理逻辑。

## 性能优化

### 服务端优化：
- 分片之间添加1ms延迟，避免过快发送导致缓冲区问题
- 使用高效的JSON序列化检查消息大小
- 保持连接管理器的单线程写入模式

### 客户端优化：
- 使用Map存储分片，提供O(1)查找性能
- 及时清理已处理的分片，避免内存泄漏
- 异步处理分片重组，不阻塞UI

## 兼容性

- **向后兼容**: 普通大小的消息仍然直接发送，不进行分片
- **客户端兼容**: 不支持分片的旧客户端仍然可以接收小消息
- **错误处理**: 增强了对各种WebSocket错误的识别和处理

## 监控和调试

### 日志输出：
```
Message size 98304 bytes exceeds limit, chunking into smaller pieces
Successfully sent message in 3 chunks
```

### 客户端调试：
- 分片接收进度显示
- 重组状态提示
- 详细的错误信息

## 故障排除

### 常见问题：

1. **分片丢失**: 检查网络连接稳定性
2. **重组失败**: 验证JSON格式是否正确
3. **内存泄漏**: 确保及时清理分片缓存

### 调试建议：
- 启用详细日志记录
- 监控分片接收状态
- 检查消息ID的唯一性

## 测试建议

1. **大消息测试**: 发送超过64KB的命令，验证分片功能
2. **网络中断测试**: 模拟网络不稳定情况
3. **并发测试**: 多个客户端同时连接测试
4. **长时间运行测试**: 验证内存使用情况

这个解决方案有效解决了WebSocket消息过大的问题，同时保持了良好的性能和兼容性。
