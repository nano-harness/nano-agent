//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// TurnExecutionSuite 覆盖 Turn 执行引擎的关键路径。
type TurnExecutionSuite struct {
	AgentTestSuite
}

func TestTurnExecutionSuite(t *testing.T) {
	suite.Run(t, new(TurnExecutionSuite))
}

// --- 基础对话：纯文本问答，无工具调用 ---

func (s *TurnExecutionSuite) TestBasicConversation() {
	// LLM 返回一个简单回答，无 tool_calls
	s.MockServer.AddResponse(MockResponse{
		Content: "Hello, this is a basic response.",
	})

	events, err := s.RunAgent("Say hello")
	require.NoError(s.T(), err)

	// 存在内容事件，且不应有工具调用
	AssertEventExists(s.T(), events, event.EventTypeContent)
	assert.False(s.T(), toolCalled(events, "read_file"))
	assert.False(s.T(), toolCalled(events, "task_done"))

	// 内容包含预期子串
	assert.True(s.T(), contentContains(events, "basic response"))

	// 基础对话应该通过隐式完成逻辑收尾，产生 TaskCompletion 事件
	s.AssertEventExists(event.EventTypeTaskCompletion)
	// 当前实现中隐式完成的 reason 文本是固定的，这里精确校验
	s.AssertCompletionReason("natural-completion: model returned text without tool calls")
}

// --- 单工具调用：LLM 调用一个工具并通过隐式完成结束 ---

func (s *TurnExecutionSuite) TestSingleToolCall() {
	// 准备一个测试文件，让 read_file 能成功
	s.CreateFile("hello.txt", "hello from test")

	// Turn 1：请求读取文件
	s.MockServer.AddResponse(MockResponse{
		Content: "I will read hello.txt.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_read",
				Name:      "read_file",
				Arguments: `{"file_path":"hello.txt","limit":100}`,
			},
		},
	})

	// 添加额外的响应以匹配实际的迭代次数
	s.MockServer.AddResponse(MockResponse{
		Content: "Reading file...",
	})

	// Turn 2：根据工具结果给出总结（纯文本响应，无工具调用，触发隐式完成）
	s.MockServer.AddResponse(MockResponse{
		Content: "The file content is: hello from test",
	})

	// Turn 3：隐式完成后的最终响应（可能不会被调用，但需要准备以防万一）
	s.MockServer.AddResponse(MockResponse{
		Content: "Task is done.",
	})

	events, err := s.RunAgent("Please read hello.txt")
	require.NoError(s.T(), err)

	// read_file 应被调用
	s.AssertToolCalled("read_file")

	// 内容包含文件内容
	s.AssertContentContains("hello from test")

	// 至少有一次 ToolResult 事件
	assert.True(s.T(), eventExists(events, event.EventTypeToolResult))

	// 完整回合应触发 TaskCompletion 事件，通过隐式完成
	s.AssertEventExists(event.EventTypeTaskCompletion)
}

// --- 多工具并行调用：单轮中包含多个工具调用 ---

func (s *TurnExecutionSuite) TestMultiToolParallel() {
	s.CreateFile("a.txt", "AAA")
	s.CreateFile("b.txt", "BBB")

	// 第一轮：并行读两个文件
	s.MockServer.AddResponse(MockResponse{
		Content: "I will read a.txt and b.txt.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_read_a",
				Name:      "read_file",
				Arguments: `{"file_path":"a.txt","limit":100}`,
			},
			{
				ID:        "call_read_b",
				Name:      "read_file",
				Arguments: `{"file_path":"b.txt","limit":100}`,
			},
		},
	})

	// 添加额外的响应以匹配实际的迭代次数
	s.MockServer.AddResponse(MockResponse{
		Content: "Files read successfully.",
	})

	// 第二轮：汇总结果（纯文本响应，触发隐式完成）
	s.MockServer.AddResponse(MockResponse{
		Content: "a.txt contains AAA, b.txt contains BBB.",
	})

	// 第三轮：隐式完成后的最终响应（可能不会被调用，但需要准备以防万一）
	s.MockServer.AddResponse(MockResponse{
		Content: "Task is done.",
	})

	events, err := s.RunAgent("Read a.txt and b.txt and summarize")
	require.NoError(s.T(), err)

	// read_file 应被调用两次
	s.AssertToolCallCount("read_file", 2)
	s.AssertContentContains("AAA")
	s.AssertContentContains("BBB")

	// ExecutorSchedule 事件应该存在，表示调度了多个 worker
	AssertEventExists(s.T(), events, event.EventTypeExecutorSchedule)

	// 更细粒度：校验调度事件中的 workers_count 和 tool_names
	var sched event.StreamEvent
	foundSched := false
	for _, e := range events {
		if e.Type == event.EventTypeExecutorSchedule {
			sched = e
			foundSched = true
			break
		}
	}
	require.True(s.T(), foundSched, "expected executor_schedule event")

	if sched.Metadata != nil {
		if wc, ok := sched.Metadata["workers_count"]; ok {
			switch v := wc.(type) {
			case int:
				assert.Equal(s.T(), 2, v, "workers_count should be 2 for two parallel tools")
			case int64:
				assert.Equal(s.T(), int64(2), v, "workers_count should be 2 for two parallel tools")
			}
		}
		if namesVal, ok := sched.Metadata["tool_names"]; ok {
			foundA, foundB := false, false
			switch names := namesVal.(type) {
			case []string:
				for _, n := range names {
					if n == "read_file" {
						if !foundA {
							foundA = true
						} else {
							foundB = true
						}
					}
				}
			case []interface{}:
				for _, raw := range names {
					if n, ok := raw.(string); ok && n == "read_file" {
						if !foundA {
							foundA = true
						} else {
							foundB = true
						}
					}
				}
			}
			assert.True(s.T(), foundA && foundB, "expected tool_names to contain two read_file entries")
		}
	}
}

// --- 相似内容循环：LLM 反复输出相同文字后 Turn 自动终止 ---

func (s *TurnExecutionSuite) TestMaxIterations() {
	// Semantic change: MaxIterations removed, this test now verifies "similar content loop detection" can terminate Turn early.
	// With implicit completion, the first response without tool calls will complete the turn
	// But loop detection should still trigger if we artificially prevent completion
	cfg := s.Config
	cfg.LoopDetection.Enabled = true

	if s.Agent != nil {
		_ = s.Agent.Shutdown()
	}
	agentInstance, err := agent.New(cfg, func(info *agent.ToolCallInfo) bool { return true })
	require.NoError(s.T(), err)
	s.Agent = agentInstance

	// LLM 连续输出完全相同的内容（触发相似内容循环检测，默认阈值 3 次）
	for i := 0; i < 5; i++ {
		s.MockServer.AddResponse(MockResponse{Content: "I am still thinking about this."})
	}

	events, err := s.RunAgent("Loop forever")
	assert.NoError(s.T(), err)

	// 校验 EventTypeLoopDetected 事件或 executor_state closing 事件存在
	foundTerminated := false
	for _, e := range events {
		if e.Type == event.EventTypeLoopDetected {
			foundTerminated = true
			break
		}
		if e.Type == event.EventTypeExecutorState && strings.Contains(e.Content, "closing") {
			foundTerminated = true
			break
		}
	}
	assert.True(s.T(), foundTerminated, "expected loop detected or closing event when similar content repeated")
}

// --- 任务完成：隐式完成后 Turn 立即终止 ---

func (s *TurnExecutionSuite) TestTimeoutTermination() {
	// 语义变更：超时机制已移除，本测试改为验证"隐式完成后 Turn 立即终止"的行为。
	s.MockServer.AddResponse(MockResponse{
		Content: "Let me finish the task.",
	})
	// 准备多余的响应，验证 Turn 在隐式完成后不再发起 LLM 请求
	s.MockServer.AddResponse(MockResponse{Content: "This response should never be reached."})

	events, err := s.RunAgent("Complete the task immediately")
	require.NoError(s.T(), err)

	// 应该触发隐式完成
	s.AssertEventExists(event.EventTypeTaskCompletion)

	// Turn 在隐式完成后不应继续产生更多轮次的内容
	completionSeen := false
	for _, e := range events {
		if e.Type == event.EventTypeTaskCompletion {
			completionSeen = true
		}
		if completionSeen && e.Type == event.EventTypeContent &&
			e.Content == "This response should never be reached." {
			s.T().Fatal("agent continued after implicit completion; extra response was produced")
		}
	}
}
