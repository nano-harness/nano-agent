package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/gorilla/websocket"
)

// WriteMessage 表示要写入WebSocket的消息
type WriteMessage struct {
	Data     interface{}
	Response chan error // 用于返回写入结果
}

// 消息分片配置
const (
	// 最大消息大小 (64KB)，超过此大小将进行分片
	MaxMessageSize = 64 * 1024
	// 分片大小 (32KB)，确保每个分片都在安全范围内
	ChunkSize = 32 * 1024
)

// ChunkedMessage 表示分片消息
type ChunkedMessage struct {
	ID       string      `json:"id"`       // 消息唯一标识
	Index    int         `json:"index"`    // 分片索引（从0开始）
	Total    int         `json:"total"`    // 总分片数
	Data     interface{} `json:"data"`     // 分片数据
	IsChunk  bool        `json:"is_chunk"` // 标识这是一个分片消息
	Complete bool        `json:"complete"` // 标识是否为最后一个分片
}

// ConnectionManager 符合gorilla/websocket最佳实践的连接管理器
// 所有写操作都在单一goroutine中执行
type ConnectionManager struct {
	conn       *websocket.Conn
	writeChan  chan WriteMessage
	pingTicker *time.Ticker
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
}

// NewConnectionManager 创建新的连接管理器
func NewConnectionManager(conn *websocket.Conn) *ConnectionManager {
	ctx, cancel := context.WithCancel(context.Background())
	cm := &ConnectionManager{
		conn:       conn,
		writeChan:  make(chan WriteMessage, 100), // 缓冲通道避免阻塞
		pingTicker: time.NewTicker(30 * time.Second),
		ctx:        ctx,
		cancel:     cancel,
		done:       make(chan struct{}),
	}

	// 启动写入goroutine（唯一的写入goroutine）
	go cm.writeLoop()

	return cm
}

// writeLoop 在单一goroutine中处理所有写入操作
func (cm *ConnectionManager) writeLoop() {
	defer close(cm.done)
	defer cm.pingTicker.Stop()

	// 设置pong处理器
	cm.conn.SetPongHandler(func(string) error {
		cm.conn.SetReadDeadline(time.Now().Add(60 * time.Second)) //nolint:errcheck
		return nil
	})

	for {
		select {
		case <-cm.ctx.Done():
			return

		case writeMsg := <-cm.writeChan:
			// 设置写入超时
			cm.conn.SetWriteDeadline(time.Now().Add(30 * time.Second)) //nolint:errcheck

			err := cm.writeJSONWithChunking(writeMsg.Data)
			if err != nil && cm.isConnectionError(err) {
				logger.Infof("Client disconnected gracefully: %v", err)
				// 通知所有等待的写入操作
				if writeMsg.Response != nil {
					writeMsg.Response <- err
				}
				// 清空剩余的写入请求
				cm.drainWriteChannel()
				return
			}

			// 返回写入结果
			if writeMsg.Response != nil {
				writeMsg.Response <- err
			}

		case <-cm.pingTicker.C:
			// 发送ping消息
			cm.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck
			if err := cm.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				if cm.isConnectionError(err) {
					logger.Infof("Ping failed, client disconnected: %v", err)
				} else {
					logger.Errorf("Ping failed with unexpected error: %v", err)
				}
				return
			}
		}
	}
}

// SafeWriteJSON 安全地写入JSON数据（同步版本）
func (cm *ConnectionManager) SafeWriteJSON(data interface{}) error {
	response := make(chan error, 1)

	select {
	case cm.writeChan <- WriteMessage{Data: data, Response: response}:
		// 等待写入结果
		select {
		case err := <-response:
			return err
		case <-cm.ctx.Done():
			return context.Canceled
		case <-time.After(35 * time.Second): // 比写入超时稍长
			return context.DeadlineExceeded
		}
	case <-cm.ctx.Done():
		return context.Canceled
	case <-time.After(5 * time.Second): // 写入通道满了
		return context.DeadlineExceeded
	}
}

// SafeWriteJSONAsync 安全地写入JSON数据（异步版本）
func (cm *ConnectionManager) SafeWriteJSONAsync(data interface{}) error {
	select {
	case cm.writeChan <- WriteMessage{Data: data, Response: nil}:
		return nil
	case <-cm.ctx.Done():
		return context.Canceled
	default:
		// 通道满了，丢弃消息
		logger.Warnf("Write channel full, dropping message")
		return context.DeadlineExceeded
	}
}

// IsConnectionAlive 检查连接是否仍然活跃
func (cm *ConnectionManager) IsConnectionAlive() bool {
	select {
	case <-cm.ctx.Done():
		return false
	default:
		return true
	}
}

// Close 关闭连接管理器
func (cm *ConnectionManager) Close() {
	cm.cancel()
	// 等待写入goroutine完成
	select {
	case <-cm.done:
	case <-time.After(5 * time.Second):
		logger.Warnf("Connection manager close timeout")
	}
}

// drainWriteChannel 清空写入通道中的剩余消息
func (cm *ConnectionManager) drainWriteChannel() {
	for {
		select {
		case writeMsg := <-cm.writeChan:
			if writeMsg.Response != nil {
				writeMsg.Response <- websocket.ErrCloseSent
			}
		default:
			return
		}
	}
}

// writeJSONWithChunking 写入JSON数据，如果消息过大则进行分片
func (cm *ConnectionManager) writeJSONWithChunking(data interface{}) error {
	// 首先序列化数据以检查大小
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// 如果消息大小在限制内，直接发送
	if len(jsonData) <= MaxMessageSize {
		return cm.conn.WriteJSON(data)
	}

	// 消息过大，需要分片
	logger.Infof("Message size %d bytes exceeds limit, chunking into smaller pieces", len(jsonData))

	// 生成唯一的消息ID
	messageID := cm.generateMessageID()

	// 计算需要的分片数量
	totalChunks := (len(jsonData) + ChunkSize - 1) / ChunkSize

	// 发送分片
	for i := 0; i < totalChunks; i++ {
		start := i * ChunkSize
		end := start + ChunkSize
		if end > len(jsonData) {
			end = len(jsonData)
		}

		// 创建分片数据
		chunkData := string(jsonData[start:end])

		chunkedMsg := ChunkedMessage{
			ID:       messageID,
			Index:    i,
			Total:    totalChunks,
			Data:     chunkData,
			IsChunk:  true,
			Complete: i == totalChunks-1,
		}

		// 发送分片
		if err := cm.conn.WriteJSON(chunkedMsg); err != nil {
			return err
		}

		// 在分片之间添加小延迟，避免过快发送导致缓冲区问题
		if i < totalChunks-1 {
			time.Sleep(1 * time.Millisecond)
		}
	}

	logger.Infof("Successfully sent message in %d chunks", totalChunks)
	return nil
}

// generateMessageID 生成消息唯一标识
func (cm *ConnectionManager) generateMessageID() string {
	return time.Now().Format("20060102150405.000000")
}

// isConnectionError 检查是否是连接相关的错误
func (cm *ConnectionManager) isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	return strings.Contains(errStr, "connection reset by peer") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "write: connection refused") ||
		strings.Contains(errStr, "message too big") ||
		strings.Contains(errStr, "too big for the output buffer") ||
		websocket.IsCloseError(err,
			websocket.CloseGoingAway,
			websocket.CloseAbnormalClosure,
			websocket.CloseNormalClosure,
			websocket.CloseNoStatusReceived,
			websocket.CloseMessageTooBig)
}
