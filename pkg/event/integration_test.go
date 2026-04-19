package event

import (
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/tools"
)

// TestEventStreamIntegration 测试事件流的完整集成
func TestEventStreamIntegration(t *testing.T) {
	// 创建监控器和验证器
	monitor := NewEventMonitor(50)
	validator := NewEventValidator()

	// 模拟一个完整的事件流序列
	events := []StreamEvent{
		// 1. 开始思考
		NewStreamEvent(EventTypeThinking, "agent_turn").WithContent("Starting to process user request..."),

		// 2. 流式内容
		NewStreamEvent(EventTypeStreamContent, "llm_client").WithContent("I need to"),
		NewStreamEvent(EventTypeStreamContent, "llm_client").WithContent(" analyze"),
		NewStreamEvent(EventTypeStreamContent, "llm_client").WithContent(" the request"),

		// 3. 工具调用
		StreamEvent{
			Type:      EventTypeToolCall,
			Timestamp: time.Now().Unix(),
			Source:    "llm_client",
			ToolCalls: []*tools.ToolCall{
				{
					ID:        "call_123",
					Name:      "search_files",
					Arguments: map[string]interface{}{"query": "test"},
				},
			},
		},

		// 4. 工具结果
		StreamEvent{
			Type:      EventTypeToolResult,
			Timestamp: time.Now().Unix(),
			Source:    "tool_executor",
			ToolResult: &tools.ToolResult{
				ID:      "call_123",
				Content: "Found 5 files matching query",
			},
		},

		// 5. 最终内容
		NewStreamEvent(EventTypeContent, "llm_client").WithContent("Based on the search results, I found 5 relevant files."),

		// 6. Token统计
		StreamEvent{
			Type:      EventTypeTokenStats,
			Timestamp: time.Now().Unix(),
			Source:    "llm_client",
			TokenStats: &TokenStats{
				InputTokens:  100,
				OutputTokens: 50,
				TotalTokens:  150,
			},
		},

		// 7. 完成
		NewStreamEvent(EventTypeDone, "agent_turn").WithMetadata("completed", true),
	}

	// 处理每个事件
	for i, event := range events {
		// 验证事件
		validationResult := validator.ValidateEvent(event)
		if !validationResult.Valid {
			t.Errorf("Event %d validation failed: %v", i, validationResult.Errors)
		}

		// 记录到监控器
		monitor.RecordEvent(event)

		// 模拟处理延迟
		time.Sleep(10 * time.Millisecond)
	}

	// 验证事件序列
	sequenceResult := validator.ValidateEventSequence(events)
	if !sequenceResult.Valid {
		t.Errorf("Event sequence validation failed: %v", sequenceResult.Errors)
	}

	// 检查监控器统计
	stats := monitor.GetStats()
	if totalEvents, ok := stats["total_events"].(int64); !ok || totalEvents != int64(len(events)) {
		t.Errorf("Expected %d events, got %v", len(events), stats["total_events"])
	}

	// 检查健康状况
	health := monitor.CheckHealth()
	if status, ok := health["status"].(string); !ok || status != "healthy" {
		t.Errorf("Expected healthy status, got %v", health["status"])
	}

	// 获取最近事件
	recentEvents := monitor.GetRecentEvents(3)
	if len(recentEvents) != 3 {
		t.Errorf("Expected 3 recent events, got %d", len(recentEvents))
	}
}

// TestEventValidation 测试事件验证功能
func TestEventValidation(t *testing.T) {
	validator := NewEventValidator()

	// 测试有效事件
	validEvent := NewStreamEvent(EventTypeContent, "test_source").WithContent("Test content")
	result := validator.ValidateEvent(validEvent)
	if !result.Valid {
		t.Errorf("Valid event failed validation: %v", result.Errors)
	}

	// 测试无效事件 - 缺少必需字段
	invalidEvent := StreamEvent{
		Type:   EventTypeContent,
		Source: "test_source",
		// Content missing
	}
	result = validator.ValidateEvent(invalidEvent)
	if result.Valid {
		t.Error("Invalid event passed validation")
	}

	// 测试警告情况
	warningEvent := NewStreamEvent(EventTypeContent, "").WithContent("Test content")
	result = validator.ValidateEvent(warningEvent)
	if len(result.Warnings) == 0 {
		t.Error("Expected warnings for empty source")
	}
}

// TestEventSanitization 测试事件清理功能
func TestEventSanitization(t *testing.T) {
	validator := NewEventValidator()

	// 创建需要清理的事件
	dirtyEvent := StreamEvent{
		Type:       EventTypeContent,
		Content:    "  Test content with spaces  ",
		Error:      "  Error message  ",
		Severity:   "  HIGH  ",
		Source:     "  TEST_SOURCE  ",
		Progress:   -0.5, // 无效进度
		RetryCount: -1,   // 无效重试次数
	}

	// 清理事件
	cleanEvent := validator.SanitizeEvent(dirtyEvent)

	// 验证清理结果
	if cleanEvent.Content != "Test content with spaces" {
		t.Errorf("Content not trimmed properly: '%s'", cleanEvent.Content)
	}

	if cleanEvent.Error != "Error message" {
		t.Errorf("Error not trimmed properly: '%s'", cleanEvent.Error)
	}

	if cleanEvent.Severity != "high" {
		t.Errorf("Severity not normalized properly: '%s'", cleanEvent.Severity)
	}

	if cleanEvent.Source != "test_source" {
		t.Errorf("Source not normalized properly: '%s'", cleanEvent.Source)
	}

	if cleanEvent.Progress != 0 {
		t.Errorf("Progress not corrected properly: %f", cleanEvent.Progress)
	}

	if cleanEvent.RetryCount != 0 {
		t.Errorf("RetryCount not corrected properly: %d", cleanEvent.RetryCount)
	}

	if cleanEvent.Timestamp == 0 {
		t.Error("Timestamp not set during sanitization")
	}
}

// TestEventMonitor 测试事件监控功能
func TestEventMonitor(t *testing.T) {
	monitor := NewEventMonitor(5) // 小历史记录用于测试

	// 添加一些事件
	for i := 0; i < 10; i++ {
		event := NewStreamEvent(EventTypeContent, "test_source")
		event = event.WithContent("Test content")
		monitor.RecordEvent(event)
		time.Sleep(1 * time.Millisecond)
	}

	// 检查统计
	stats := monitor.GetStats()
	if totalEvents, ok := stats["total_events"].(int64); !ok || totalEvents != 10 {
		t.Errorf("Expected 10 events, got %v", stats["total_events"])
	}

	// 检查历史记录限制
	recentEvents := monitor.GetRecentEvents(10)
	if len(recentEvents) != 5 { // 应该被限制为maxHistory
		t.Errorf("Expected 5 recent events (maxHistory), got %d", len(recentEvents))
	}

	// 添加错误事件
	errorEvent := NewStreamEvent(EventTypeError, "test_source")
	errorEvent = errorEvent.WithError("Test error", "high")
	monitor.RecordEvent(errorEvent)

	// 检查健康状况
	health := monitor.CheckHealth()
	if status, ok := health["status"].(string); !ok {
		t.Error("Health status not returned")
	} else if status == "healthy" {
		// 可能仍然健康，因为错误率不高
		t.Logf("Health status: %s", status)
	}

	// 重置监控器
	monitor.Reset()
	stats = monitor.GetStats()
	if totalEvents, ok := stats["total_events"].(int64); !ok || totalEvents != 0 {
		t.Errorf("Expected 0 events after reset, got %v", stats["total_events"])
	}
}

// BenchmarkEventProcessing 性能基准测试
func BenchmarkEventProcessing(b *testing.B) {
	monitor := NewEventMonitor(1000)
	validator := NewEventValidator()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := NewStreamEvent(EventTypeContent, "benchmark_source")
		event = event.WithContent("Benchmark content")

		// 验证事件
		validator.ValidateEvent(event)

		// 记录事件
		monitor.RecordEvent(event)
	}
}
