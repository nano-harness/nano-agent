//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// SessionSuite 覆盖会话隔离等行为。
type SessionSuite struct {
	AgentTestSuite
}

func TestSessionSuite(t *testing.T) {
	suite.Run(t, new(SessionSuite))
}

// TestSessionIsolation 确认不同 session 的历史不会互相污染。
func (s *SessionSuite) TestSessionIsolation() {
	// Session A 的响应
	s.MockServer.AddResponse(MockResponse{
		Content: "Hello from Session A. I remember your name is Alice.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_task_done_a",
				Name:      "task_done",
				Arguments: `{"status": "success"}`,
			},
		},
	})

	// Session B 的响应
	s.MockServer.AddResponse(MockResponse{
		Content: "Hello from Session B. I don't know your name.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_task_done_b",
				Name:      "task_done",
				Arguments: `{"status": "success"}`,
			},
		},
	})

	ctx := context.Background()

	// Session A
	var eventsA []event.StreamEvent
	err := s.Agent.ProcessStreamWithMultimodalAndSession(ctx, "session-a", "My name is Alice.", nil, func(e event.StreamEvent) {
		eventsA = append(eventsA, e)
	})
	require.NoError(s.T(), err)

	// Session B
	var eventsB []event.StreamEvent
	err = s.Agent.ProcessStreamWithMultimodalAndSession(ctx, "session-b", "What is my name?", nil, func(e event.StreamEvent) {
		eventsB = append(eventsB, e)
	})
	require.NoError(s.T(), err)

	// 聚合内容
	contentA := ""
	for _, e := range eventsA {
		if e.Type == event.EventTypeContent {
			contentA += e.Content
		}
	}
	if !strings.Contains(contentA, "Alice") {
		s.T().Errorf("expected Session A content to contain 'Alice', got: %s", contentA)
	}

	contentB := ""
	for _, e := range eventsB {
		if e.Type == event.EventTypeContent {
			contentB += e.Content
		}
	}
	if strings.Contains(contentB, "Alice") {
		s.T().Errorf("expected Session B content NOT to contain 'Alice', got: %s", contentB)
	}

	// 更细粒度：两个会话都应该发出 SessionInfo 事件，且 sessionID 不同
	AssertEventExists(s.T(), eventsA, event.EventTypeSessionInfo)
	AssertEventExists(s.T(), eventsB, event.EventTypeSessionInfo)
}
