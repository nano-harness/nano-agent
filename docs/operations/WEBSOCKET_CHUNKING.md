# WebSocket Message Chunking Solution

[中文](./WEBSOCKET_CHUNKING.zh-CN.md)

## Problem description

When proxying `/api/v1/stream` responses, the following error occurs when a single WebSocket message is too large:

```
WebSocket connection closed: status: CloseStatus[code=1009, reason=The decoded text message was too big for the output buffer and the endpoint does not support partial messages]
```

Error code 1009 means the message is too large and exceeds the WebSocket buffer limit.

## Solution

### 1. Server-side improvements

A message chunking mechanism is implemented in `pkg/daemon/connection_manager.go`:

#### Core features:
- **Automatic chunking**: messages larger than 64 KB are chunked automatically
- **Chunk size**: 32 KB per chunk, keeping every chunk within a safe range
- **Message reassembly**: clients can reassemble the complete message from the chunk metadata
- **Error handling**: improved detection and handling of oversized-message errors

#### Chunked message format:
```json
{
  "id": "20250916083152.123456",
  "index": 0,
  "total": 3,
  "data": "chunk data content...",
  "is_chunk": true,
  "complete": false
}
```

#### Key fields:
- `id`: unique message identifier
- `index`: chunk index (starting from 0)
- `total`: total number of chunks
- `data`: data content of the current chunk
- `is_chunk`: marks this as a chunked message
- `complete`: marks whether this is the last chunk

### 2. Client-side handling

A complete HTML client example (`examples/websocket_chunked_client.html`) is provided, demonstrating how to:

- Detect chunked messages
- Buffer and reassemble chunks
- Handle the reassembled complete message
- Display chunking progress information

#### Core client logic:
```javascript
function handleChunkedMessage(chunk) {
    const { id, index, total, data: chunkData, complete } = chunk;

    // Initialize chunk storage
    if (!messageChunks.has(id)) {
        messageChunks.set(id, {
            chunks: new Array(total),
            receivedCount: 0
        });
    }

    // Store the chunk
    const messageData = messageChunks.get(id);
    messageData.chunks[index] = chunkData;
    messageData.receivedCount++;

    // Check whether all chunks have been received
    if (complete && messageData.receivedCount === total) {
        // Reassemble the complete message
        const fullMessage = messageData.chunks.join('');
        const reconstructedData = JSON.parse(fullMessage);
        handleRegularMessage(reconstructedData);

        // Clean up the buffer
        messageChunks.delete(id);
    }
}
```

## Configuration parameters

Chunking behavior can be tuned by modifying the constants in `pkg/daemon/connection_manager.go`:

```go
const (
    // Maximum message size (64KB); messages larger than this are chunked
    MaxMessageSize = 64 * 1024
    // Chunk size (32KB), keeping every chunk within a safe range
    ChunkSize = 32 * 1024
)
```

## Usage

### 1. Start the daemon service
```bash
nano daemon start
```

### 2. Use the provided client example
Open `examples/websocket_chunked_client.html` in a browser to test.

### 3. Or integrate into an existing client
Follow the example code to add chunk-handling logic to your existing WebSocket client.

## Performance optimizations

### Server-side optimizations:
- A 1 ms delay is added between chunks to avoid buffer issues caused by sending too fast
- Efficient JSON serialization is used to check message size
- The connection manager's single-writer model is preserved

### Client-side optimizations:
- Use a Map to store chunks, providing O(1) lookup performance
- Clean up processed chunks promptly to avoid memory leaks
- Reassemble chunks asynchronously without blocking the UI

## Compatibility

- **Backward compatible**: normal-sized messages are still sent directly without chunking
- **Client compatible**: older clients that do not support chunking can still receive small messages
- **Error handling**: improved detection and handling of various WebSocket errors

## Monitoring and debugging

### Log output:
```
Message size 98304 bytes exceeds limit, chunking into smaller pieces
Successfully sent message in 3 chunks
```

### Client-side debugging:
- Chunk receiving progress display
- Reassembly status indicators
- Detailed error messages

## Troubleshooting

### Common issues:

1. **Lost chunks**: check network connection stability
2. **Reassembly failure**: verify the JSON format is correct
3. **Memory leaks**: make sure the chunk buffer is cleaned up promptly

### Debugging tips:
- Enable verbose logging
- Monitor chunk receiving status
- Check message ID uniqueness

## Testing recommendations

1. **Large message test**: send commands larger than 64 KB to verify chunking
2. **Network interruption test**: simulate unstable network conditions
3. **Concurrency test**: test with multiple clients connected simultaneously
4. **Long-running test**: verify memory usage over time

This solution effectively solves the oversized WebSocket message problem while maintaining good performance and compatibility.
