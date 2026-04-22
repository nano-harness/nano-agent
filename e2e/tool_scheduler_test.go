//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ToolSchedulerSuite 覆盖 ToolScheduler 的关键调度与集成行为。
type ToolSchedulerSuite struct {
	AgentTestSuite
}

func TestToolSchedulerSuite(t *testing.T) {
	suite.Run(t, new(ToolSchedulerSuite))
}

// --- 工具不存在处理：调用不存在的工具 ---

func (s *ToolSchedulerSuite) TestToolNotFound() {
	ts := s.Agent.GetToolScheduler()
	require.NotNil(s.T(), ts)

	// 捕获事件，便于断言 ToolResult / ToolUse
	ts.SetEventHandler(func(ev event.StreamEvent) {
		s.AppendEvents(ev)
	})

	calls := []agent.ToolToExecute{
		{
			ID:   "missing_tool_call",
			Name: "non_existing_tool", // 工具箱中不存在
		},
	}

	ctx := context.Background()
	results, err := ts.ExecuteParallel(ctx, calls)
	// 对于单个调用失败，ExecuteParallel 可能仍返回 nil error，通过结果和事件表达错误
	if err != nil {
		s.T().Logf("ExecuteParallel returned error for missing tool (acceptable): %v", err)
	}

	// 结果中应该有对应条目，且 Success=false，错误包含 tool not found
	res, ok := results["missing_tool_call"]
	assert.True(s.T(), ok, "expected result entry for missing tool call")
	if ok && res != nil {
		assert.False(s.T(), res.Success)
		assert.Contains(s.T(), res.Error, "tool not found")
	}

	// 事件层面应当有 ToolResult 和 ToolUse 事件
	s.AssertEventExists(event.EventTypeToolResult)
	s.AssertEventExists(event.EventTypeToolUse)
}

// --- 工具白名单：SetAllowedTools 限制可用工具 ---

func (s *ToolSchedulerSuite) TestAllowedToolsWhitelist() {
	ts := s.Agent.GetToolScheduler()
	require.NotNil(s.T(), ts)

	ts.SetEventHandler(func(ev event.StreamEvent) {
		s.AppendEvents(ev)
	})

	// 只允许 read_file，禁止 write_file
	ts.SetAllowedTools([]string{"read_file"})

	calls := []agent.ToolToExecute{
		{ID: "read_ok", Name: "read_file", Parameters: map[string]interface{}{"file_path": "README.md", "limit": 10}},
		{ID: "write_blocked", Name: "write_file", Parameters: map[string]interface{}{"file_path": "blocked.txt", "content": "x"}},
	}

	ctx := context.Background()
	results, err := ts.ExecuteParallel(ctx, calls)
	// ExecuteParallel 在单个调用失败时仍会返回结果 map，但也可能返回聚合错误
	if err != nil {
		s.T().Logf("ExecuteParallel returned error (allowed for mixed success/failure): %v", err)
	}

	// read_file 结果应存在
	if res, ok := results["read_ok"]; ok && res != nil {
		// 不强制要求 Success=true（比如 README 不存在），但至少不应该是 "tool not allowed" 错误
		assert.NotContains(s.T(), res.Error, "tool not allowed")
	}

	// write_file 应被白名单拒绝，返回 not allowed 错误
	res, ok := results["write_blocked"]
	assert.True(s.T(), ok, "expected result for blocked tool call")
	if ok && res != nil {
		assert.False(s.T(), res.Success)
		assert.Contains(s.T(), res.Error, "not allowed")
	}

	// 事件中也应有对应 ToolResult/ToolUse
	s.AssertEventExists(event.EventTypeToolResult)
	s.AssertEventExists(event.EventTypeToolUse)
}

// --- 工具模式匹配：通配符匹配工具名（如 read_*） ---

func (s *ToolSchedulerSuite) TestAllowedToolsWildcard() {
	ts := s.Agent.GetToolScheduler()
	require.NotNil(s.T(), ts)

	ts.SetEventHandler(func(ev event.StreamEvent) {
		s.AppendEvents(ev)
	})

	// 通过通配符仅允许 read_* 系列工具
	ts.SetAllowedTools([]string{"read_*"})

	calls := []agent.ToolToExecute{
		{ID: "read_file_ok", Name: "read_file", Parameters: map[string]interface{}{"file_path": "README.md", "limit": 10}},
		{ID: "write_file_blocked", Name: "write_file", Parameters: map[string]interface{}{"file_path": "test.txt", "content": "test"}},
	}

	ctx := context.Background()
	results, err := ts.ExecuteParallel(ctx, calls)
	if err != nil {
		s.T().Logf("ExecuteParallel returned error (allowed for mixed success/failure): %v", err)
	}

	// read_file 应不受通配符限制
	if res, ok := results["read_file_ok"]; ok && res != nil {
		assert.NotContains(s.T(), res.Error, "not allowed")
	}

	// write_file 应当被拒绝
	res, ok := results["write_file_blocked"]
	assert.True(s.T(), ok, "expected result for blocked write_file call")
	if ok && res != nil {
		assert.False(s.T(), res.Success)
		assert.Contains(s.T(), res.Error, "not allowed")
	}
}

// --- 工具执行状态事件：验证 WorkerStart → WorkerUpdate → WorkerEnd 序列 ---

func (s *ToolSchedulerSuite) TestToolExecutionStatusEvents() {
	ts := s.Agent.GetToolScheduler()
	require.NotNil(s.T(), ts)

	ts.SetEventHandler(func(ev event.StreamEvent) {
		s.AppendEvents(ev)
	})

	// 使用一个简单的 read_file 调用来触发完整生命周期
	s.CreateFile("status.txt", "status test")

	calls := []agent.ToolToExecute{
		{
			ID:   "status_call",
			Name: "read_file",
			Parameters: map[string]interface{}{
				"file_path": "status.txt",
				"limit":     50,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := ts.ExecuteParallel(ctx, calls)
	require.NoError(s.T(), err)

	// WorkerStart、WorkerUpdate 和 WorkerEnd 应按顺序出现
	s.AssertEventSequence(event.EventTypeWorkerStart, event.EventTypeWorkerUpdate, event.EventTypeWorkerEnd)
}

// --- 用户拒绝工具调用：RequiresConfirmation 工具被拒绝 ---

func (s *ToolSchedulerSuite) TestUserRejection() {
	// 将 Agent 的审批 handler 设为始终返回 false，模拟“等待用户确认”模式
	s.Agent.SetApprovalHandler(func(info *agent.ToolCallInfo) bool {
		// 所有需要确认的工具都先挂起，稍后通过 HandleConfirmationResponse 决定
		return false
	})

	// Turn 1: LLM 希望写入敏感文件
	s.MockServer.AddResponse(MockResponse{
		Content: "I will write a secret to .env.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_write",
				Name:      "write_file",
				Arguments: `{"file_path": ".env", "content": "secret content"}`,
			},
		},
	})

	// Turn 2: LLM 观察到用户拒绝后，通过 task_done 汇报失败
	s.MockServer.AddResponse(MockResponse{
		Content: "User rejected the write operation. I will stop.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_task_done_rejected",
				Name:      "task_done",
				Arguments: `{"status": "failure", "summary": "User rejected file writing operation."}`,
			},
		},
	})

	// 后台 goroutine 模拟“用户”通过 ToolScheduler 显式拒绝这次调用
	go func() {
		ts := s.Agent.GetToolScheduler()
		if ts == nil {
			return
		}
		// 轮询一段时间直到调用进入 awaiting_approval 状态
		for i := 0; i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
			if err := ts.HandleConfirmationResponse("call_write", false); err == nil {
				return
			}
		}
	}()

	events, err := s.RunAgent("Write a secret to .env")
	require.NoError(s.T(), err)

	// ToolResult 中应该有被用户取消的错误
	var toolError string
	for _, e := range events {
		if e.Type == event.EventTypeToolResult && e.ToolResult != nil {
			if e.ToolResult.Error != "" {
				toolError = e.ToolResult.Error
				break
			}
		}
	}

	assert.NotEmpty(s.T(), toolError, "expected a tool_result error due to user rejection")
	assert.True(s.T(),
		strings.Contains(toolError, "cancelled by user") ||
			strings.Contains(toolError, "rejected by user") ||
			strings.Contains(toolError, "user rejected"),
		"unexpected tool error for user rejection: %s", toolError,
	)

	// .env 不应被创建
	if _, err := s.ReadFile(".env"); err == nil {
		s.T().Fatal(".env file was created despite user rejection")
	}
}
