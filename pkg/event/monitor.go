package event

import (
	"fmt"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// EventMonitor 监控事件流的健康状况和性能
type EventMonitor struct { //nolint:revive
	mu           sync.RWMutex
	eventCounts  map[EventType]int64
	errorCounts  map[string]int64
	lastEvents   []StreamEvent
	maxHistory   int
	startTime    time.Time
	lastActivity time.Time
}

// NewEventMonitor 创建新的事件监控器
func NewEventMonitor(maxHistory int) *EventMonitor {
	if maxHistory <= 0 {
		maxHistory = 100
	}
	return &EventMonitor{
		eventCounts:  make(map[EventType]int64),
		errorCounts:  make(map[string]int64),
		lastEvents:   make([]StreamEvent, 0, maxHistory),
		maxHistory:   maxHistory,
		startTime:    time.Now(),
		lastActivity: time.Now(),
	}
}

// RecordEvent 记录事件
func (m *EventMonitor) RecordEvent(event StreamEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 更新计数
	m.eventCounts[event.Type]++
	m.lastActivity = time.Now()

	// 记录错误
	if event.Error != "" {
		m.errorCounts[event.Error]++
	}

	// 添加到历史记录
	if len(m.lastEvents) >= m.maxHistory {
		// 移除最旧的事件
		m.lastEvents = m.lastEvents[1:]
	}
	m.lastEvents = append(m.lastEvents, event)
}

// GetStats 获取统计信息
func (m *EventMonitor) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalEvents := int64(0)
	for _, count := range m.eventCounts {
		totalEvents += count
	}

	return map[string]interface{}{
		"total_events":   totalEvents,
		"event_counts":   m.eventCounts,
		"error_counts":   m.errorCounts,
		"uptime_seconds": time.Since(m.startTime).Seconds(),
		"last_activity":  m.lastActivity,
		"events_per_sec": float64(totalEvents) / time.Since(m.startTime).Seconds(),
		"recent_events":  len(m.lastEvents),
	}
}

// GetRecentEvents 获取最近的事件
func (m *EventMonitor) GetRecentEvents(limit int) []StreamEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 || limit > len(m.lastEvents) {
		limit = len(m.lastEvents)
	}

	start := len(m.lastEvents) - limit
	if start < 0 {
		start = 0
	}

	return m.lastEvents[start:]
}

// CheckHealth 检查事件流健康状况
func (m *EventMonitor) CheckHealth() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := map[string]interface{}{
		"status": "healthy",
		"issues": []string{},
	}

	issues := []string{}

	// 检查是否有活动
	if time.Since(m.lastActivity) > 5*time.Minute {
		issues = append(issues, "No recent activity detected")
	}

	// 检查错误率
	totalEvents := int64(0)
	totalErrors := int64(0)
	for _, count := range m.eventCounts {
		totalEvents += count
	}
	for _, count := range m.errorCounts {
		totalErrors += count
	}

	if totalEvents > 0 {
		errorRate := float64(totalErrors) / float64(totalEvents)
		if errorRate > 0.1 { // 超过10%错误率
			issues = append(issues, fmt.Sprintf("High error rate: %.2f%%", errorRate*100))
		}
	}

	// 检查事件类型分布
	if m.eventCounts[EventTypeError] > m.eventCounts[EventTypeContent] {
		issues = append(issues, "More errors than content events")
	}

	if len(issues) > 0 {
		health["status"] = "warning"
		health["issues"] = issues
	}

	return health
}

// Reset 重置监控器
func (m *EventMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.eventCounts = make(map[EventType]int64)
	m.errorCounts = make(map[string]int64)
	m.lastEvents = make([]StreamEvent, 0, m.maxHistory)
	m.startTime = time.Now()
	m.lastActivity = time.Now()
}

// PrintDiagnostics 打印诊断信息
func (m *EventMonitor) PrintDiagnostics() {
	stats := m.GetStats()
	health := m.CheckHealth()

	fmt.Println("=== Event Stream Diagnostics ===")
	logger.Infof("Status: %s", health["status"])
	logger.Infof("Total Events: %v", stats["total_events"])
	logger.Infof("Events/sec: %.2f", stats["events_per_sec"])
	logger.Infof("Uptime: %.2f seconds", stats["uptime_seconds"])
	logger.Infof("Last Activity: %v", stats["last_activity"])

	if issues, ok := health["issues"].([]string); ok && len(issues) > 0 {
		fmt.Println("\nIssues:")
		for _, issue := range issues {
			logger.Infof("  - %s", issue)
		}
	}

	fmt.Println("\nEvent Counts:")
	if eventCounts, ok := stats["event_counts"].(map[EventType]int64); ok {
		for eventType, count := range eventCounts {
			logger.Infof("  %s: %d", eventType, count)
		}
	}

	if errorCounts, ok := stats["error_counts"].(map[string]int64); ok && len(errorCounts) > 0 {
		fmt.Println("\nError Counts:")
		for error, count := range errorCounts {
			logger.Infof("  %s: %d", error, count)
		}
	}

	fmt.Println("================================")
}
