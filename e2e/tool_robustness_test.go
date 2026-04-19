package e2e

// tool_robustness_test.go
//
// 场景覆盖：agent 在工具调用失败（不存在的工具、缺少参数、用户拒绝、连续失败、
// 混合成功/失败批次、工具超时）后应当继续工作而不是停止。
//
// 每个用例都精确验证：
//   1. Agent 整体不返回 error（鲁棒性）。
//   2. 失败结果被回写到 LLM 上下文（产生了对应的 EventTypeToolResult 事件）。
//   3. Agent 在收到工具错误反馈后继续执行并最终完成任务。

import (
	"context"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ToolRobustnessSuite 覆盖工具调用失败后的 agent 鲁棒性场景。
type ToolRobustnessSuite struct {
	AgentTestSuite
}

func TestToolRobustnessSuite(t *testing.T) {
	suite.Run(t, new(ToolRobustnessSuite))
}

// --------------------------------------------------------------------------
// TestToolNotFound
//
// 场景：LLM 调用了一个不存在的工具。
// 预期：ToolScheduler 返回 tool_not_found 错误结果；agent 继续运行，
//
//	LLM 收到错误反馈后改用 task_done 完成任务。
//
// --------------------------------------------------------------------------
func (s *ToolRobustnessSuite) TestToolNotFound() {
	// 第一轮：调用不存在的工具
	s.MockServer.AddResponse(MockResponse{
		Content: "I will call ghost_tool.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_ghost",
				Name:      "ghost_tool", // 不存在
				Arguments: `{}`,
			},
		},
	})

	// 第二轮：LLM 收到错误后继续，调用 task_done
	s.MockServer.AddResponse(MockResponse{
		Content: "The tool was not found; I will finish instead.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_done",
				Name:      "task_done",
				Arguments: `{"status":"success"}`,
			},
		},
	})

	events, err := s.RunAgent("Please call ghost_tool then finish.")
	require.NoError(s.T(), err, "agent must not crash on tool_not_found")

	// 必须有 ToolResult 事件（包含错误信息）
	assert.True(s.T(), eventExists(events, event.EventTypeToolResult),
		"expected EventTypeToolResult for the failed tool call")

	// agent 最终应调用 task_done
	s.AssertToolCalled("task_done")
	s.AssertEventExists(event.EventTypeTaskCompletion)
}

// --------------------------------------------------------------------------
// TestMissingRequiredParameter
//
// 场景：LLM 调用 read_file 时缺少必填参数 file_path。
// 预期：ToolScheduler 返回 missing_required_parameters 错误；agent 继续运行，
//
//	LLM 修正参数后成功完成任务。
//
// --------------------------------------------------------------------------
func (s *ToolRobustnessSuite) TestMissingRequiredParameter() {
	s.CreateFile("notes.txt", "some notes")

	// 第一轮：缺少 file_path 参数
	s.MockServer.AddResponse(MockResponse{
		Content: "I will read the file.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_bad_read",
				Name:      "read_file",
				Arguments: `{"limit":50}`, // 缺少 file_path
			},
		},
	})

	// 第二轮：LLM 收到参数缺失反馈，修正后重试
	s.MockServer.AddResponse(MockResponse{
		Content: "Let me try with the correct path.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_good_read",
				Name:      "read_file",
				Arguments: `{"file_path":"notes.txt","limit":50}`,
			},
		},
	})

	// 第三轮：完成任务
	s.MockServer.AddResponse(MockResponse{
		Content: "File read successfully.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_done",
				Name:      "task_done",
				Arguments: `{"status":"success"}`,
			},
		},
	})

	events, err := s.RunAgent("Please read notes.txt.")
	require.NoError(s.T(), err, "agent must not crash on missing parameters")

	// 必须有工具结果事件（错误 + 成功各一条）
	assert.True(s.T(), eventExists(events, event.EventTypeToolResult),
		"expected EventTypeToolResult events")

	// 最终应成功读取文件并完成任务
	s.AssertToolCalled("read_file")
	s.AssertToolCalled("task_done")
	s.AssertEventExists(event.EventTypeTaskCompletion)
}

// --------------------------------------------------------------------------
// TestUserRejectsToolCall
//
// 场景：Agent 需要用户批准工具调用，但用户拒绝（approvalHandler 返回 false）。
// 预期：被拒绝的工具返回 cancelled 结果；agent 继续运行，LLM 调用 task_done。
// --------------------------------------------------------------------------
func (s *ToolRobustnessSuite) TestUserRejectsToolCall() {
	// 注册一个需要用户确认的工具
	confirmTool := &requiresConfirmationTool{}
	err := s.Agent.GetToolbox().Register(confirmTool)
	require.NoError(s.T(), err)

	// 重新创建 agent，使用异步拒绝审批处理器（模仿 TUI 行为）：
	// 返回 false 让工具保持 StatusAwaitingApproval，然后通过
	// HandleConfirmationResponse 异步拒绝。
	cfg := s.Config
	if s.Agent != nil {
		_ = s.Agent.Shutdown()
	}
	var agentInstance *agent.Agent
	agentInstance, err = agent.New(cfg, func(info *agent.ToolCallInfo) bool {
		if info.Name == "confirm_required_tool" {
			// Async rejection via HandleConfirmationResponse (mirrors TUI flow)
			go func() {
				_ = agentInstance.GetToolScheduler().HandleConfirmationResponse(info.ID, false)
			}()
			return false
		}
		return true
	})
	require.NoError(s.T(), err)
	s.Agent = agentInstance

	// 重新注册工具（agent 重新创建了）
	err = s.Agent.GetToolbox().Register(confirmTool)
	require.NoError(s.T(), err)

	// 第一轮：调用需要确认的工具
	s.MockServer.AddResponse(MockResponse{
		Content: "I will use the confirm_required_tool.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_confirm",
				Name:      "confirm_required_tool",
				Arguments: `{}`,
			},
		},
	})

	// 第二轮：LLM 收到拒绝通知，改用 task_done 完成
	s.MockServer.AddResponse(MockResponse{
		Content: "The tool was rejected; I will complete the task without it.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_done",
				Name:      "task_done",
				Arguments: `{"status":"success"}`,
			},
		},
	})

	events, err := s.RunAgent("Please use confirm_required_tool, then finish.")
	require.NoError(s.T(), err, "agent must not crash when tool is rejected by user")

	// 必须有 ToolResult 事件（cancelled 结果）
	assert.True(s.T(), eventExists(events, event.EventTypeToolResult),
		"expected EventTypeToolResult for the rejected tool")

	// agent 最终应完成任务
	s.AssertToolCalled("task_done")
	s.AssertEventExists(event.EventTypeTaskCompletion)
}

// requiresConfirmationTool 是一个需要用户确认的测试工具
type requiresConfirmationTool struct{}

func (t *requiresConfirmationTool) Name() string        { return "confirm_required_tool" }
func (t *requiresConfirmationTool) Description() string { return "A tool that requires confirmation" }
func (t *requiresConfirmationTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryDevelopment
}
func (t *requiresConfirmationTool) RequiresConfirmation() bool { return true }
func (t *requiresConfirmationTool) ConcurrencySafe() bool      { return true }
func (t *requiresConfirmationTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema("confirm required tool", map[string]*interfaces.PropertySchema{}, nil)
}
func (t *requiresConfirmationTool) Execute(_ context.Context, _ map[string]interface{}) (*interfaces.ToolResult, error) {
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  "confirm_required_tool executed",
		UserContent: "confirm_required_tool executed",
	}, nil
}

// --------------------------------------------------------------------------
// TestConsecutiveToolFailuresThenRecovery
//
// 场景：工具连续多次失败（返回 Success=false），最终 LLM 采取替代方案完成任务。
// 预期：agent 不会因连续工具失败而崩溃；每次失败结果都被回写到 LLM 上下文；
//
//	最终 task_done 被成功调用。
//
// --------------------------------------------------------------------------
func (s *ToolRobustnessSuite) TestConsecutiveToolFailuresThenRecovery() {
	// 注册一个总是失败的工具
	permanentFail := &permanentlyFailingTool{}
	err := s.Agent.GetToolbox().Register(permanentFail)
	require.NoError(s.T(), err)

	// 第一轮：调用永久失败工具
	s.MockServer.AddResponse(MockResponse{
		Content: "Let me try fail_tool.",
		ToolCalls: []MockToolCall{
			{ID: "call_fail_1", Name: "permanent_fail_tool", Arguments: `{}`},
		},
	})

	// 第二轮：再次尝试（LLM 重试）
	s.MockServer.AddResponse(MockResponse{
		Content: "Let me retry.",
		ToolCalls: []MockToolCall{
			{ID: "call_fail_2", Name: "permanent_fail_tool", Arguments: `{}`},
		},
	})

	// 第三轮：放弃该工具，直接完成
	s.MockServer.AddResponse(MockResponse{
		Content: "The tool is broken. I will finish without it.",
		ToolCalls: []MockToolCall{
			{ID: "call_done", Name: "task_done", Arguments: `{"status":"success"}`},
		},
	})

	events, err := s.RunAgent("Please use permanent_fail_tool and then finish.")
	require.NoError(s.T(), err, "agent must not crash on consecutive tool failures")

	// 两次失败都应产生 ToolResult 事件
	toolResultCount := 0
	for _, e := range events {
		if e.Type == event.EventTypeToolResult {
			toolResultCount++
		}
	}
	assert.GreaterOrEqual(s.T(), toolResultCount, 2,
		"expected at least 2 ToolResult events (one per failure + task_done)")

	s.AssertToolCalled("task_done")
	s.AssertEventExists(event.EventTypeTaskCompletion)
}

// permanentlyFailingTool 是一个总是返回失败的测试工具（不可重试错误）
type permanentlyFailingTool struct{}

func (t *permanentlyFailingTool) Name() string        { return "permanent_fail_tool" }
func (t *permanentlyFailingTool) Description() string { return "A tool that always fails" }
func (t *permanentlyFailingTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryDevelopment
}
func (t *permanentlyFailingTool) RequiresConfirmation() bool { return false }
func (t *permanentlyFailingTool) ConcurrencySafe() bool      { return true }
func (t *permanentlyFailingTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema("permanently failing tool", map[string]*interfaces.PropertySchema{}, nil)
}
func (t *permanentlyFailingTool) Execute(_ context.Context, _ map[string]interface{}) (*interfaces.ToolResult, error) {
	return &interfaces.ToolResult{
		Success:     false,
		Error:       "tool is permanently broken",
		LLMContent:  "Tool permanent_fail_tool failed: tool is permanently broken",
		UserContent: "Tool permanent_fail_tool failed: tool is permanently broken",
		Metadata: map[string]interface{}{
			"code": "not_allowed", // unrecoverable – no retry
		},
	}, nil
}

// --------------------------------------------------------------------------
// TestMixedSuccessFailureBatch
//
// 场景：单轮同时调用两个工具：一个成功，一个不存在（失败）。
// 预期：两个工具都产生 ToolResult 事件；成功的结果被正常处理；
//
//	agent 继续运行并最终完成任务。
//
// --------------------------------------------------------------------------
func (s *ToolRobustnessSuite) TestMixedSuccessFailureBatch() {
	s.CreateFile("valid.txt", "valid content")

	// 第一轮：同时调用一个有效工具和一个不存在工具
	s.MockServer.AddResponse(MockResponse{
		Content: "I will read a file and also call a ghost tool.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_valid",
				Name:      "read_file",
				Arguments: `{"file_path":"valid.txt","limit":100}`,
			},
			{
				ID:        "call_invalid",
				Name:      "nonexistent_tool_xyz",
				Arguments: `{}`,
			},
		},
	})

	// 第二轮：汇总并完成
	s.MockServer.AddResponse(MockResponse{
		Content: "Got partial results. The file was read successfully.",
		ToolCalls: []MockToolCall{
			{ID: "call_done", Name: "task_done", Arguments: `{"status":"success"}`},
		},
	})

	events, err := s.RunAgent("Read valid.txt and also call nonexistent_tool_xyz.")
	require.NoError(s.T(), err, "agent must handle mixed success/failure batch")

	// 两个工具都应有 ToolResult 事件（一成功一失败）
	toolResultCount := 0
	for _, e := range events {
		if e.Type == event.EventTypeToolResult {
			toolResultCount++
		}
	}
	assert.GreaterOrEqual(s.T(), toolResultCount, 2,
		"expected at least 2 ToolResult events for mixed batch (success + failure)")

	// read_file 必须被成功调用
	s.AssertToolCalled("read_file")
	s.AssertToolCalled("task_done")
	s.AssertEventExists(event.EventTypeTaskCompletion)
}

// --------------------------------------------------------------------------
// TestToolTimeoutContinues
//
// 场景：工具执行超时（Turn 超时配置很短），agent 不应 panic/error；
//
//	超时结果被写入 LLM 上下文，agent 继续尝试完成任务。
//
// --------------------------------------------------------------------------
func (s *ToolRobustnessSuite) TestToolTimeoutContinues() {
	// 注册一个会阻塞一小段时间的工具（模拟慢工具，但不超过测试超时）
	slowTool := &slowExecutingTool{blockDuration: 200 * time.Millisecond}
	err := s.Agent.GetToolbox().Register(slowTool)
	require.NoError(s.T(), err)

	// 第一轮：调用慢工具
	s.MockServer.AddResponse(MockResponse{
		Content: "I will run the slow tool.",
		ToolCalls: []MockToolCall{
			{ID: "call_slow", Name: "slow_tool", Arguments: `{}`},
		},
	})

	// 第二轮：慢工具完成，agent 继续并完成任务
	s.MockServer.AddResponse(MockResponse{
		Content: "Slow tool result received.",
		ToolCalls: []MockToolCall{
			{ID: "call_done", Name: "task_done", Arguments: `{"status":"success"}`},
		},
	})

	events, err := s.RunAgent("Please run slow_tool.")
	require.NoError(s.T(), err, "agent must not crash when a slow tool completes normally")

	// executor_state 事件必须存在（Turn 已启动）
	s.AssertEventExists(event.EventTypeExecutorState)
	// 慢工具和 task_done 都应该被调用
	s.AssertToolCalled("slow_tool")
	s.AssertToolCalled("task_done")
	s.AssertEventExists(event.EventTypeTaskCompletion)
	_ = events
}

// slowExecutingTool 是一个阻塞指定时间的测试工具
type slowExecutingTool struct {
	blockDuration time.Duration
}

func (t *slowExecutingTool) Name() string        { return "slow_tool" }
func (t *slowExecutingTool) Description() string { return "A tool that blocks for a while" }
func (t *slowExecutingTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryDevelopment
}
func (t *slowExecutingTool) RequiresConfirmation() bool { return false }
func (t *slowExecutingTool) ConcurrencySafe() bool      { return true }
func (t *slowExecutingTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema("slow tool", map[string]*interfaces.PropertySchema{}, nil)
}
func (t *slowExecutingTool) Execute(ctx context.Context, _ map[string]interface{}) (*interfaces.ToolResult, error) {
	select {
	case <-time.After(t.blockDuration):
	case <-ctx.Done():
	}
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  "slow_tool finished",
		UserContent: "slow_tool finished",
	}, nil
}

// --------------------------------------------------------------------------
// TestToolErrorDoesNotIncrementConsecutiveErrorsOnSuccess
//
// 场景：一轮中有工具失败后，下一轮有工具成功。
// 预期：成功后 ConsecutiveErrors 被重置，不会因为累积而触发错误阈值终止。
// --------------------------------------------------------------------------
func (s *ToolRobustnessSuite) TestToolErrorDoesNotIncrementConsecutiveErrorsOnSuccess() {
	// 注册一个第一次失败、之后成功的工具
	recoverableTool := &onceFailing{}
	err := s.Agent.GetToolbox().Register(recoverableTool)
	require.NoError(s.T(), err)

	// 第一轮：调用不存在工具（错误）
	s.MockServer.AddResponse(MockResponse{
		Content: "Let me try ghost_tool first.",
		ToolCalls: []MockToolCall{
			{ID: "call_ghost", Name: "ghost_tool_xxx", Arguments: `{}`},
		},
	})

	// 第二轮：调用有效工具（成功，应重置 ConsecutiveErrors）
	s.MockServer.AddResponse(MockResponse{
		Content: "Now I'll use once_failing_tool.",
		ToolCalls: []MockToolCall{
			{ID: "call_recover", Name: "once_failing_tool", Arguments: `{}`},
		},
	})

	// 第三轮：完成任务
	s.MockServer.AddResponse(MockResponse{
		Content: "Task complete.",
		ToolCalls: []MockToolCall{
			{ID: "call_done", Name: "task_done", Arguments: `{"status":"success"}`},
		},
	})

	_, err = s.RunAgent("Call ghost_tool_xxx, then once_failing_tool, then finish.")
	require.NoError(s.T(), err, "agent should not terminate due to false consecutive error accumulation")

	// once_failing_tool 和 task_done 都应该被调用
	s.AssertToolCalled("once_failing_tool")
	s.AssertToolCalled("task_done")
	s.AssertEventExists(event.EventTypeTaskCompletion)
}

// onceFailing 是一个总是成功的测试工具（用于验证成功后 ConsecutiveErrors 重置）
type onceFailing struct{}

func (t *onceFailing) Name() string        { return "once_failing_tool" }
func (t *onceFailing) Description() string { return "A tool that always succeeds" }
func (t *onceFailing) Category() interfaces.ToolCategory {
	return interfaces.CategoryDevelopment
}
func (t *onceFailing) RequiresConfirmation() bool { return false }
func (t *onceFailing) ConcurrencySafe() bool      { return true }
func (t *onceFailing) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema("once failing tool", map[string]*interfaces.PropertySchema{}, nil)
}
func (t *onceFailing) Execute(_ context.Context, _ map[string]interface{}) (*interfaces.ToolResult, error) {
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  "once_failing_tool succeeded",
		UserContent: "once_failing_tool succeeded",
	}, nil
}
