package e2e

import (
	"context"
	"net/http"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ErrorRecoverySuite 覆盖 LLM API 重试与 ToolRecoveryStrategy 的集成行为。
type ErrorRecoverySuite struct {
	AgentTestSuite
}

func TestErrorRecoverySuite(t *testing.T) {
	suite.Run(t, new(ErrorRecoverySuite))
}

// --- LLM 层错误恢复：500/429 等错误的重试 ---

// TestLLMRetryOnServerError 模拟连续 500 错误后成功的场景，验证发生了多次 LLM 请求。
func (s *ErrorRecoverySuite) TestLLMRetryOnServerError() {
	// 第一次请求返回 500，第二次返回成功内容
	s.MockServer.SetFailurePattern([]bool{false, true})
	s.MockServer.AddResponse(MockResponse{ // 用于成功重试
		Content: "Recovered after server error.",
	})

	// 为避免额外的标题生成请求，预先创建 session 并设置 title
	sessionID := "llm_retry_500"
	session := s.Agent.GetSessionManager().GetOrCreateSession(sessionID)
	session.SetMetadata("title", "Retry 500 Test")

	ctx := context.Background()
	err := s.Agent.ProcessStreamWithMultimodalAndSession(ctx, sessionID, "Please handle a transient error.", nil, func(event.StreamEvent) {})

	// 即使中间有 500，也应最终成功返回
	require.NoError(s.T(), err)

	// 至少应有两次对 Mock LLM 的请求（一次失败 + 一次重试成功）
	assert.GreaterOrEqual(s.T(), len(s.MockServer.Requests), 2, "expected at least two LLM requests due to retry on 500")
}

// TestLLMRetryOnRateLimit 模拟 429 限流错误的重试，通过请求次数验证行为。
func (s *ErrorRecoverySuite) TestLLMRetryOnRateLimit() {
	// 第一条响应返回 429，第二条正常；不使用 failurePattern，仅依赖 resp.Error
	s.MockServer.AddResponse(MockResponse{Error: http.StatusTooManyRequests})
	s.MockServer.AddResponse(MockResponse{Content: "Recovered after rate limit."})

	sessionID := "llm_retry_429"
	session := s.Agent.GetSessionManager().GetOrCreateSession(sessionID)
	session.SetMetadata("title", "Retry 429 Test")

	ctx := context.Background()
	err := s.Agent.ProcessStreamWithMultimodalAndSession(ctx, sessionID, "Please handle rate limiting.", nil, func(event.StreamEvent) {})

	require.NoError(s.T(), err)

	// 同样检查至少发生了两次对 Mock LLM 的请求
	assert.GreaterOrEqual(s.T(), len(s.MockServer.Requests), 2, "expected at least two LLM requests due to retry on 429")
}

// --- 工具执行重试：ToolRecoveryStrategy ---

// flakyTool 是一个用于测试的工具：前两次返回可恢复错误，第三次成功。
type flakyTool struct {
	attempts int
}

func (t *flakyTool) Name() string        { return "flaky_tool" }
func (t *flakyTool) Description() string { return "A tool that fails twice before succeeding" }
func (t *flakyTool) Category() interfaces.ToolCategory {
	// 使用一个通用的开发类 Category，避免依赖不存在的 System 分类
	return interfaces.CategoryDevelopment
}
func (t *flakyTool) RequiresConfirmation() bool { return false }
func (t *flakyTool) ConcurrencySafe() bool      { return true }
func (t *flakyTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema("flaky tool", map[string]*interfaces.PropertySchema{}, nil)
}

func (t *flakyTool) Execute(_ context.Context, _ map[string]interface{}) (*interfaces.ToolResult, error) {
	t.attempts++
	if t.attempts < 3 {
		// 返回一个可恢复错误：错误码包含 "temporary"
		return &interfaces.ToolResult{
			Success: false,
			Error:   "temporary network error",
			Metadata: map[string]interface{}{
				"code": "temporary",
			},
		}, nil
	}
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  "flaky tool succeeded",
		UserContent: "flaky tool succeeded",
	}, nil
}

// TestToolRecovery_RetrySuccess 验证 ToolRecoveryStrategy 在可恢复错误下会重试并最终成功。
// 通过工具内部 attempts 计数以及整体任务成功，精确验证至少发生了两次失败 + 一次成功。
func (s *ErrorRecoverySuite) TestToolRecovery_RetrySuccess() {
	// 注册测试用工具
	toolbox := s.Agent.GetToolbox()
	ft := &flakyTool{}
	err := toolbox.Register(ft)
	require.NoError(s.T(), err)

	// 第一轮：LLM 调用 flaky_tool
	s.MockServer.AddResponse(MockResponse{
		Content: "I will run the flaky tool.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_flaky",
				Name:      "flaky_tool",
				Arguments: `{}`,
			},
		},
	})

	// 第二轮：在工具最终成功后调用 task_done
	s.MockServer.AddResponse(MockResponse{
		Content: "Flaky tool eventually succeeded.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_task_done",
				Name:      "task_done",
				Arguments: `{"status":"success"}`,
			},
		},
	})

	_, err = s.RunAgent("Please use flaky_tool and then finish.")
	require.NoError(s.T(), err)

	// flaky_tool 最终应成功被调用，且整个回合结束
	s.AssertToolCalled("flaky_tool")
	s.AssertToolCalled("task_done")

	// 更细粒度：flaky_tool 内部应正好尝试 3 次（前两次失败、第三次成功）
	assert.Equal(s.T(), 3, ft.attempts, "expected flaky_tool to be executed 3 times (2 failures + 1 success)")
}
