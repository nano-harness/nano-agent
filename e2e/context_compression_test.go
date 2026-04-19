package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ContextCompressionSuite 覆盖 Turn 的上下文压缩逻辑（集成 LLM + CompressionStrategy）。
type ContextCompressionSuite struct {
	AgentTestSuite
}

func TestContextCompressionSuite(t *testing.T) {
	suite.Run(t, new(ContextCompressionSuite))
}

// TestCompressionTriggered 模拟多轮对话超过 token 阈值后触发压缩，
// 验证：
//   - 请求中包含 COMPRESSED CONTEXT 标记，或
//   - 事件流中出现 EventTypeCompression 事件。
func (s *ContextCompressionSuite) TestCompressionTriggered() {
	// 配置激进的压缩阈值，方便在测试中触发
	s.Config.ContextConfig.MaxTokens = 50
	s.Config.ContextConfig.CompressionRatio = 0.5
	s.Config.ContextConfig.PreserveRecentTurns = 2
	s.Config.ContextConfig.EnableCompression = true

	// 更新全局配置并重新初始化 Agent
	cfg := s.Config
	config.SetGlobalConfig(cfg)

	if s.Agent != nil {
		_ = s.Agent.Shutdown()
	}
	agentInstance, err := agent.New(cfg, func(info *agent.ToolCallInfo) bool { return true })
	require.NoError(s.T(), err)
	s.Agent = agentInstance

	// Turn 1-3：常规对话（每轮都调用 task_done）
	s.MockServer.AddResponse(MockResponse{
		Content: "Response 1: Hello!",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_task_done_1",
				Name:      "task_done",
				Arguments: `{"status": "success"}`,
			},
		},
	})

	s.MockServer.AddResponse(MockResponse{
		Content: "Response 2: World!",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_task_done_2",
				Name:      "task_done",
				Arguments: `{"status": "success"}`,
			},
		},
	})

	s.MockServer.AddResponse(MockResponse{
		Content: "Response 3: Compressed?",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_task_done_3",
				Name:      "task_done",
				Arguments: `{"status": "success"}`,
			},
		},
	})

	ctx := context.Background()
	sessionID := "compression_session"

	// 构造一个很大的 payload，确保 token 数量明显增长
	largePayload := strings.Repeat("This is a lot of text to force context compression. ", 100)

	// Turn 1
	var events1 []event.StreamEvent
	err = s.Agent.ProcessStreamWithMultimodalAndSession(ctx, sessionID, "Message 1: "+largePayload, nil, func(e event.StreamEvent) {
		events1 = append(events1, e)
	})
	require.NoError(s.T(), err)

	// Turn 2
	var events2 []event.StreamEvent
	err = s.Agent.ProcessStreamWithMultimodalAndSession(ctx, sessionID, "Message 2: "+largePayload, nil, func(e event.StreamEvent) {
		events2 = append(events2, e)
	})
	require.NoError(s.T(), err)

	// 给异步操作一点时间
	time.Sleep(100 * time.Millisecond)

	// CompressionStrategy 在需要生成 summary 时会额外调用 LLM 一次，
	// 因此这里预置一条 summary 响应
	s.MockServer.AddResponse(MockResponse{
		Content: "This is a summarized context of previous turns.",
	})

	// Turn 3 的实际响应
	s.MockServer.AddResponse(MockResponse{
		Content: "Response 3: Yes, compressed.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_task_done_4",
				Name:      "task_done",
				Arguments: `{"status": "success"}`,
			},
		},
	})

	var events3 []event.StreamEvent
	err = s.Agent.ProcessStreamWithMultimodalAndSession(ctx, sessionID, "Message 3", nil, func(e event.StreamEvent) {
		events3 = append(events3, e)
	})
	require.NoError(s.T(), err)

	// 验证压缩是否发生：
	// 1) 请求 messages 中包含 "COMPRESSED CONTEXT" 标记，或
	// 2) 事件流中出现 EventTypeCompression 事件。

	// 防御：确保确实有请求发给 MockServer
	if len(s.MockServer.Requests) == 0 {
		s.T().Fatal("no requests were sent to mock server")
	}

	foundCompressedMarker := false
	for i, req := range s.MockServer.Requests {
		rawMsgs, ok := req["messages"].([]interface{})
		if !ok {
			continue
		}
		for j, msg := range rawMsgs {
			m, ok := msg.(map[string]interface{})
			if !ok {
				continue
			}
			content, _ := m["content"].(string)
			if content != "" && strings.Contains(content, "COMPRESSED CONTEXT") {
				s.T().Logf("Found COMPRESSED CONTEXT in request %d message %d", i, j)
				foundCompressedMarker = true
				break
			}
		}
		if foundCompressedMarker {
			break
		}
	}

	foundCompressionEvent := eventExists(events3, event.EventTypeCompression)

	if !foundCompressedMarker && !foundCompressionEvent {
		// 打印帮助信息便于调试
		for i, req := range s.MockServer.Requests {
			rawMsgs, ok := req["messages"].([]interface{})
			if !ok {
				continue
			}
			for j, msg := range rawMsgs {
				m, ok := msg.(map[string]interface{})
				if !ok {
					continue
				}
				content, _ := m["content"].(string)
				if len(content) > 80 {
					content = content[:80] + "..."
				}
				s.T().Logf("Req %d Msg %d [%v]: %v", i, j, m["role"], content)
			}
		}
		s.T().Fatal("expected context compression to occur on turn 3, but found neither a COMPRESSED CONTEXT marker nor a compression event")
	}
}
