package e2e

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ForkSuite 覆盖 ForkTool 与子 Agent 的基础集成行为。
type ForkSuite struct {
	AgentTestSuite
}

func TestForkSuite(t *testing.T) {
	suite.Run(t, new(ForkSuite))
}

// TestBasicFork_ExecuteAgent
//
// 场景：
// - 顶层 Agent 通过 fork 工具创建一个 execute 类型的子 Agent
// - 子 Agent 执行简单任务并返回结果（通过 fork 工具的 ToolResult 体现）
//
// 这里不强制要求最终由 task_done 结束，只验证 fork 工具链路打通。
func (s *ForkSuite) TestBasicFork_ExecuteAgent() {
	// 父回合第 1 次 LLM 调用：请求使用 fork 工具
	s.MockServer.AddResponse(MockResponse{
		Content: "I will delegate this task to a child agent.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_fork",
				Name:      "fork",
				Arguments: `{"task":"Summarize work in a child agent","agent_type":"execute"}`,
			},
		},
	})

	// 子 Agent 的 LLM 调用：真实执行任务（这里我们直接返回一个结果字符串）
	s.MockServer.AddResponse(MockResponse{
		Content: "Child agent completed task: hello from child.",
	})

	// 父回合后续 LLM 调用：不再强制特定内容，由 Agent 自行根据上下文完成
	_, err := s.RunAgent("Use a child agent to help with part of this task.")
	require.NoError(s.T(), err)

	// fork 工具应被调用，证明子 Agent 路径被走通
	s.AssertToolCalled("fork")
}

// TestFork_AgentTypesSmoke
//
// 这里不走完整 LLM 流程，只验证 fork 管理器对不同 AgentType 能正常创建子 Agent
// 并返回对应的类型信息（更细致的系统提示/工具权限在 pkg/agent 单元测试中覆盖）。
func (s *ForkSuite) TestFork_AgentTypesSmoke() {
	fm := agent.NewForkManager(s.Agent)

	types := []agent.AgentType{agent.AgentTypeExplore, agent.AgentTypePlan, agent.AgentTypeExecute, agent.AgentTypeVerify}

	for _, tpe := range types {
		tpe := tpe
		s.T().Run(string(tpe), func(t *testing.T) {
			ctx := context.Background()
			res, err := fm.Fork(ctx, agent.ForkConfig{
				AgentType: tpe,
				Task:      "briefly introduce your role and capabilities",
			})
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Equal(t, tpe, res.AgentType)
		})
	}
}
