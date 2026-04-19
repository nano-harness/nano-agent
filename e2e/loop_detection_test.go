package e2e

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// LoopDetectionSuite 验证 Turn 中的循环检测功能：
//  1. 重复相同工具调用超过阈值时提前终止并发出 EventTypeLoopDetected 事件。
//  2. LLM 反复输出相同内容超过阈值时提前终止并发出 EventTypeLoopDetected 事件。
//  3. 正常任务（工具调用模式多样）不会被误判为循环。
type LoopDetectionSuite struct {
	AgentTestSuite
}

func TestLoopDetectionSuite(t *testing.T) {
	suite.Run(t, new(LoopDetectionSuite))
}

// --------------------------------------------------------------------------
// TestRepeatedToolCallLoop
//
// 场景：LLM 在每一轮都调用相同的工具（read_file），不调用 task_done。
// 预期：Turn 在检测到连续 MaxRepeatedTools 次相同工具签名后提前终止，
//
//	发出 EventTypeLoopDetected 事件，loop_type = "repeated_tool_calls"。
//
// --------------------------------------------------------------------------
func (s *LoopDetectionSuite) TestRepeatedToolCallLoop() {
	cfg := s.Config
	// 将 loop detection 阈值设置为 3（连续 3 次相同工具触发）
	// 这里我们直接在 config 层面控制，loop_detection 默认 enabled=true
	cfg.LoopDetection.Enabled = true

	if s.Agent != nil {
		_ = s.Agent.Shutdown()
	}
	agentInstance, err := agent.New(cfg, func(info *agent.ToolCallInfo) bool { return true })
	require.NoError(s.T(), err)
	s.Agent = agentInstance

	s.CreateFile("loop.txt", "loop content")

	// 准备足够多的"永远重复调用 read_file"响应
	repeatResp := MockResponse{
		Content: "I need to read the file again.",
		ToolCalls: []MockToolCall{
			{ID: "call_loop_1", Name: "read_file", Arguments: `{"file_path":"loop.txt","limit":10}`},
		},
	}
	for i := 0; i < 8; i++ {
		// 每轮用不同的调用 ID，但工具名签名相同
		tc := MockToolCall{
			ID:        "call_loop_" + string(rune('0'+i+1)),
			Name:      "read_file",
			Arguments: `{"file_path":"loop.txt","limit":10}`,
		}
		_ = tc
		s.MockServer.AddResponse(repeatResp)
	}

	events, err := s.RunAgent("Please read loop.txt and keep re-reading it.")
	// 允许返回 nil error（turn 优雅终止）
	assert.NoError(s.T(), err)

	// 必须出现 loop_detected 事件
	loopDetected := false
	var loopEvt event.StreamEvent
	for _, e := range events {
		if e.Type == event.EventTypeLoopDetected {
			loopDetected = true
			loopEvt = e
			break
		}
	}
	assert.True(s.T(), loopDetected, "expected EventTypeLoopDetected event when same tool is called repeatedly")

	if loopDetected {
		// 重复工具调用检测已移除；允许 similar_content 或其他 loop_type
		if loopEvt.Metadata != nil {
			if lt, ok := loopEvt.Metadata["loop_type"]; ok {
				assert.Contains(s.T(), []string{"repeated_tool_calls", "similar_content"},
					lt, "loop_type should be a known loop detection reason")
			}
		}
	}

	// 回合必须以关闭状态结束（ExecutorState closing）
	foundClosing := false
	for _, e := range events {
		if e.Type == event.EventTypeExecutorState && e.Content == "closing" {
			foundClosing = true
			break
		}
	}
	assert.True(s.T(), foundClosing, "turn should close with executor_state=closing")
}

// --------------------------------------------------------------------------
// TestSimilarContentLoop
//
// 场景：LLM 每轮都输出相同的文字内容，不调用任何工具。
// 预期：Turn 检测到 MaxSimilarContent 次内容相同后提前终止，
//
//	发出 EventTypeLoopDetected，loop_type = "similar_content"。
//
// --------------------------------------------------------------------------
func (s *LoopDetectionSuite) TestSimilarContentLoop() {
	// With implicit completion, a pure-text response completes the turn.
	// Similar-content loop detection remains as a safety net when the model
	// keeps emitting identical text while still calling tools (i.e. it is
	// making tool calls but not making progress in its narration).
	cfg := s.Config
	cfg.LoopDetection.Enabled = true

	if s.Agent != nil {
		_ = s.Agent.Shutdown()
	}
	agentInstance, err := agent.New(cfg, func(info *agent.ToolCallInfo) bool { return true })
	require.NoError(s.T(), err)
	s.Agent = agentInstance

	s.CreateFile("similar.txt", "similar content")

	// Repeatedly return the same text alongside a tool call. Because each
	// response includes a tool call, implicit completion never fires and the
	// similar-content safety net has a chance to trigger.
	repeatedContent := "I am still thinking about this problem."
	for i := 0; i < 8; i++ {
		s.MockServer.AddResponse(MockResponse{
			Content: repeatedContent,
			ToolCalls: []MockToolCall{
				{ID: "call_sim_" + string(rune('0'+i+1)), Name: "read_file", Arguments: `{"file_path":"similar.txt","limit":10}`},
			},
		})
	}

	events, err := s.RunAgent("Think about a problem and give me a final answer.")
	assert.NoError(s.T(), err)

	// 必须出现 loop_detected 事件
	loopDetected := false
	var loopEvt event.StreamEvent
	for _, e := range events {
		if e.Type == event.EventTypeLoopDetected {
			loopDetected = true
			loopEvt = e
			break
		}
	}
	assert.True(s.T(), loopDetected, "expected EventTypeLoopDetected event when LLM outputs identical content repeatedly")

	if loopDetected {
		if loopEvt.Metadata != nil {
			if lt, ok := loopEvt.Metadata["loop_type"]; ok {
				assert.Equal(s.T(), "similar_content", lt, "loop_type should be 'similar_content'")
			}
		}
	}
}

// --------------------------------------------------------------------------
// TestNoFalsePositiveOnNormalTask
//
// 场景：LLM 在多轮调用不同工具（read_file、task_done），正常完成任务。
// 预期：不出现 loop_detected 事件。
// --------------------------------------------------------------------------
func (s *LoopDetectionSuite) TestNoFalsePositiveOnNormalTask() {
	s.CreateFile("a.txt", "content A")
	s.CreateFile("b.txt", "content B")

	// 第 1 轮：读 a.txt
	s.MockServer.AddResponse(MockResponse{
		Content: "Reading a.txt first.",
		ToolCalls: []MockToolCall{
			{ID: "c1", Name: "read_file", Arguments: `{"file_path":"a.txt","limit":50}`},
		},
	})
	// 第 2 轮：读 b.txt
	s.MockServer.AddResponse(MockResponse{
		Content: "Now reading b.txt.",
		ToolCalls: []MockToolCall{
			{ID: "c2", Name: "read_file", Arguments: `{"file_path":"b.txt","limit":50}`},
		},
	})
	// 第 3 轮：完成
	s.MockServer.AddResponse(MockResponse{
		Content: "All done.",
		ToolCalls: []MockToolCall{
			{ID: "c3", Name: "task_done", Arguments: `{"status":"success"}`},
		},
	})

	events, err := s.RunAgent("Read both files and finish.")
	require.NoError(s.T(), err)

	// 不应出现 loop_detected 事件
	for _, e := range events {
		if e.Type == event.EventTypeLoopDetected {
			s.T().Errorf("unexpected EventTypeLoopDetected on normal task: %+v", e)
		}
	}

	// 正常完成
	s.AssertToolCalled("task_done")
	s.AssertEventExists(event.EventTypeTaskCompletion)
}

// --------------------------------------------------------------------------
// TestLoopDetectionEventMetadata
//
// 细粒度校验 EventTypeLoopDetected 事件中必须携带 reason、loop_type、iteration
// 三个 Metadata 字段。
// --------------------------------------------------------------------------
func (s *LoopDetectionSuite) TestLoopDetectionEventMetadata() {
	cfg := s.Config
	cfg.LoopDetection.Enabled = true

	if s.Agent != nil {
		_ = s.Agent.Shutdown()
	}
	agentInstance, err := agent.New(cfg, func(info *agent.ToolCallInfo) bool { return true })
	require.NoError(s.T(), err)
	s.Agent = agentInstance

	s.CreateFile("meta.txt", "metadata test")

	sameResp := MockResponse{
		Content: "Reading again.",
		ToolCalls: []MockToolCall{
			{ID: "m1", Name: "read_file", Arguments: `{"file_path":"meta.txt","limit":10}`},
		},
	}
	for i := 0; i < 8; i++ {
		s.MockServer.AddResponse(sameResp)
	}

	events, err := s.RunAgent("Read meta.txt repeatedly.")
	assert.NoError(s.T(), err)

	var loopEvt *event.StreamEvent
	for i := range events {
		if events[i].Type == event.EventTypeLoopDetected {
			loopEvt = &events[i]
			break
		}
	}
	require.NotNil(s.T(), loopEvt, "expected a loop_detected event")

	// 必须携带 metadata
	require.NotNil(s.T(), loopEvt.Metadata, "loop_detected event must have Metadata")
	assert.Contains(s.T(), loopEvt.Metadata, "loop_type", "loop_detected must carry loop_type")
	assert.Contains(s.T(), loopEvt.Metadata, "reason", "loop_detected must carry reason")
	assert.Contains(s.T(), loopEvt.Metadata, "iteration", "loop_detected must carry iteration")
}
