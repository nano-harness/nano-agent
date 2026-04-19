package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// CircuitBreakerSuite 覆盖 LLM Client 级别熔断器与 MockServer 的集成行为。
type CircuitBreakerSuite struct {
	AgentTestSuite
}

func TestCircuitBreakerSuite(t *testing.T) {
	suite.Run(t, new(CircuitBreakerSuite))
}

// TestCircuitBreaker_OpensAfterFailures
//
// 场景：
// - 连续多次请求均返回 500，可重试
// - CircuitBreaker 的连续失败计数达到阈值后进入 open 状态
// - 后续请求在未冷却前会立刻被拒绝，错误信息中包含 "circuit breaker is open"。
func (s *CircuitBreakerSuite) TestCircuitBreaker_OpensAfterFailures() {
	// 所有请求都失败（failurePattern 中 false 表示失败）
	s.MockServer.SetFailurePattern([]bool{false})

	// 使用 Agent 内部的 LLMClient 直接发起流式补全，避免额外的 Agent 层逻辑干扰
	llmClient := s.Agent.GetLLMClient()
	client, ok := llmClient.(*llm.Client)
	require.True(s.T(), ok, "expected underlying LLM client to be *llm.Client")

	messages := []llm.Message{{Role: "user", Content: "trigger circuit breaker"}}

	// 连续多次调用，直到看到熔断错误或达到安全上限
	ctx := context.Background()
	var lastErr error
	var events []event.StreamEvent
	opened := false

	for i := 0; i < 10 && !opened; i++ {
		events = nil
		err := client.StreamCompletion(ctx, messages, func(ev event.StreamEvent) {
			events = append(events, ev)
		})
		lastErr = err
		if err != nil && strings.Contains(err.Error(), "circuit breaker is open") {
			opened = true
			break
		}
	}

	require.True(s.T(), opened, "expected circuit breaker to open after repeated failures, last error: %v", lastErr)

	// 一旦熔断开启，错误事件中也应包含对应信息
	foundErrorEvent := false
	for _, e := range events {
		if e.Type == event.EventTypeError && strings.Contains(e.Error, "circuit breaker") {
			foundErrorEvent = true
			break
		}
	}
	require.True(s.T(), foundErrorEvent, "expected at least one error event mentioning circuit breaker")
}
