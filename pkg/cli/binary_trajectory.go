package cli

import "github.com/nano-harness/nano-agent/pkg/event"

// trajectoryEvent captures a simplified event for trajectory logging
type trajectoryEvent struct {
	Type       string                   `json:"type"`
	Content    string                   `json:"content,omitempty"`
	ToolCalls  []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolResult map[string]interface{}   `json:"tool_result,omitempty"`
	TokenStats map[string]interface{}   `json:"token_stats,omitempty"`
	Error      string                   `json:"error,omitempty"`
	Meta       map[string]interface{}   `json:"meta,omitempty"`
	Timestamp  int64                    `json:"timestamp,omitempty"`
}

// trajectoryOptimizer handles trajectory compression and optimization
type trajectoryOptimizer struct {
	compressionEnabled bool
}

// newTrajectoryOptimizer creates a new trajectory optimizer
func newTrajectoryOptimizer() *trajectoryOptimizer {
	return &trajectoryOptimizer{
		compressionEnabled: true,
	}
}

// shouldRecordEvent determines if an event should be recorded in trajectory
func (to *trajectoryOptimizer) shouldRecordEvent(eventType event.EventType) bool {
	if !to.compressionEnabled {
		return true
	}

	// 只记录关键事件，移除流内容相关事件
	switch eventType {
	case event.EventTypeStreamContent:
		return false // 完全移除流内容记录
	case event.EventTypeContent:
		return true // 保留最终内容
	case event.EventTypeToolCall, event.EventTypeToolResult:
		return true // 保留工具调用和结果
	case event.EventTypeError:
		return true // 保留错误信息
	case event.EventTypeCompression:
		return true // 保留压缩信息
	case event.EventTypeDebug:
		return true // 保留上下文统计信息
	case event.EventTypeTokenStats:
		return false // 去除中间过程的令牌统计事件，最终一次在完成时收敛输出
	case event.EventTypeFinalSummary:
		return true // 保留最终摘要
	default:
		return false // 其他事件类型默认不记录
	}
}
