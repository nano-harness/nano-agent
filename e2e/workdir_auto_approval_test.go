//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type WorkdirAutoApprovalSuite struct {
	AgentTestSuite
}

func TestWorkdirAutoApprovalSuite(t *testing.T) {
	suite.Run(t, new(WorkdirAutoApprovalSuite))
}

func (s *WorkdirAutoApprovalSuite) TestShellGrepInWorkdir_NoApproval() {
	require.NoError(s.T(), os.MkdirAll(filepath.Join(s.WorkDir, "src"), 0o755))
	s.CreateFile("src/a.txt", "foo\n")

	approvalCalls := 0
	s.Agent.SetApprovalHandlerV2(func(_ *agent.ToolCallInfo) agent.ApprovalDecision {
		approvalCalls++
		return agent.ApprovalApproveOnce
	})

	s.MockServer.AddResponse(MockResponse{
		Content: "I will grep in src.",
		ToolCalls: []MockToolCall{{
			ID:        "call_grep_src",
			Name:      "run_shell_command",
			Arguments: `{"command":"grep foo src/"}`,
		}},
	})
	s.MockServer.AddResponse(MockResponse{
		Content: "Done.",
		ToolCalls: []MockToolCall{{
			ID:        "call_done",
			Name:      "task_done",
			Arguments: `{"status":"success"}`,
		}},
	})

	_, err := s.RunAgent("grep foo in src")
	require.NoError(s.T(), err)
	require.Equal(s.T(), 0, approvalCalls)
}

func (s *WorkdirAutoApprovalSuite) TestShellGrepOutsideWorkdir_RequiresApproval() {
	approvalCalls := 0
	s.Agent.SetApprovalHandlerV2(func(_ *agent.ToolCallInfo) agent.ApprovalDecision {
		approvalCalls++
		return agent.ApprovalApproveOnce
	})

	s.MockServer.AddResponse(MockResponse{
		Content: "I will grep outside.",
		ToolCalls: []MockToolCall{{
			ID:        "call_grep_outside",
			Name:      "run_shell_command",
			Arguments: `{"command":"grep foo /etc/hosts"}`,
		}},
	})
	s.MockServer.AddResponse(MockResponse{
		Content: "Done.",
		ToolCalls: []MockToolCall{{
			ID:        "call_done",
			Name:      "task_done",
			Arguments: `{"status":"success"}`,
		}},
	})

	_, err := s.RunAgent("grep foo outside workdir")
	require.NoError(s.T(), err)
	require.Equal(s.T(), 1, approvalCalls)
}

func (s *WorkdirAutoApprovalSuite) TestWriteFileInWorkdir_NoApproval() {
	approvalCalls := 0
	s.Agent.SetApprovalHandlerV2(func(_ *agent.ToolCallInfo) agent.ApprovalDecision {
		approvalCalls++
		return agent.ApprovalApproveOnce
	})

	s.MockServer.AddResponse(MockResponse{
		Content: "I will write a file.",
		ToolCalls: []MockToolCall{{
			ID:        "call_write_inside",
			Name:      "write_file",
			Arguments: `{"file_path":"notes.md","content":"hello"}`,
		}},
	})
	s.MockServer.AddResponse(MockResponse{
		Content: "Done.",
		ToolCalls: []MockToolCall{{
			ID:        "call_done",
			Name:      "task_done",
			Arguments: `{"status":"success"}`,
		}},
	})

	_, err := s.RunAgent("write notes")
	require.NoError(s.T(), err)
	require.Equal(s.T(), 0, approvalCalls)
}

func (s *WorkdirAutoApprovalSuite) TestWriteFileOutsideWorkdir_RequiresApproval() {
	approvalCalls := 0
	s.Agent.SetApprovalHandlerV2(func(_ *agent.ToolCallInfo) agent.ApprovalDecision {
		approvalCalls++
		return agent.ApprovalApproveOnce
	})

	s.MockServer.AddResponse(MockResponse{
		Content: "I will write outside.",
		ToolCalls: []MockToolCall{{
			ID:        "call_write_outside",
			Name:      "write_file",
			Arguments: `{"file_path":"/tmp/nano-agent-workdir-auto-approval.txt","content":"hello"}`,
		}},
	})
	s.MockServer.AddResponse(MockResponse{
		Content: "Done.",
		ToolCalls: []MockToolCall{{
			ID:        "call_done",
			Name:      "task_done",
			Arguments: `{"status":"success"}`,
		}},
	})

	_, err := s.RunAgent("write outside")
	require.NoError(s.T(), err)
	require.Equal(s.T(), 1, approvalCalls)
	_ = os.Remove("/tmp/nano-agent-workdir-auto-approval.txt")
}
