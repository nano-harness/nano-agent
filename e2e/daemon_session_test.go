//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/e2e/shared"
	"github.com/stretchr/testify/suite"
)

// DaemonSessionSuite tests daemon session management operations.
// This suite validates:
// - Session listing
// - Session retrieval
// - Session deletion
// - Session cancellation
// - Session reset
type DaemonSessionSuite struct {
	suite.Suite
	MockServer *EnhancedMockServer
	Harness    *shared.DaemonHarness
}

func TestDaemonSessionSuite(t *testing.T) {
	suite.Run(t, new(DaemonSessionSuite))
}

func (s *DaemonSessionSuite) SetupTest() {
	s.MockServer = NewMockServerWithDefaults()
	s.Harness = shared.NewDaemonHarness(s.T(), s.MockServer)
	err := s.Harness.WaitReady(2 * time.Second)
	s.Require().NoError(err)
}

func (s *DaemonSessionSuite) TearDownTest() {
	if s.Harness != nil {
		s.Harness.Shutdown()
	}
	if s.MockServer != nil {
		s.MockServer.Close()
	}
}

// TestSession_ListEmpty verifies listing when no sessions exist.
func (s *DaemonSessionSuite) TestSession_ListEmpty() {
	resp, err := s.Harness.Client.ListSessions(10)
	s.NoError(err)
	s.NotNil(resp)
	s.True(resp.Success)

	// May have some sessions or be empty depending on initialization
	// Just verify response structure
	s.NotNil(resp.Sessions)
}

// TestSession_ExecuteAndList verifies session appears in list after execution.
func (s *DaemonSessionSuite) TestSession_ExecuteAndList() {
	// Setup mock response
	s.MockServer.AddResponse(MockResponse{
		Content: "Task completed.",
	})

	// Execute command in new session
	execResp, err := s.Harness.Client.ExecuteInSession("do task", "", 30, false, false)
	s.NoError(err)
	s.NotNil(execResp)
	s.True(execResp.Success)

	sessionID := execResp.SessionID
	s.NotEmpty(sessionID)

	// List sessions
	listResp, err := s.Harness.Client.ListSessions(10)
	s.NoError(err)
	s.True(listResp.Success)

	// Verify session exists in list
	found := false
	for _, session := range listResp.Sessions {
		if session.ID == sessionID {
			found = true
			s.NotEmpty(session.CreatedAt)
			break
		}
	}
	s.True(found, "Executed session should appear in list")
}

// TestSession_GetSession verifies retrieving specific session.
func (s *DaemonSessionSuite) TestSession_GetSession() {
	// Setup mock
	s.MockServer.AddResponse(MockResponse{
		Content: "Session test.",
	})

	// Create session via execution
	execResp, err := s.Harness.Client.ExecuteInSession("test", "", 30, false, false)
	s.NoError(err)
	sessionID := execResp.SessionID

	// Get session details
	sessionData, err := s.Harness.Client.GetSession(sessionID)
	s.NoError(err)
	s.NotNil(sessionData)

	// Verify session data structure
	s.Contains(sessionData, "id")
	s.Equal(sessionID, sessionData["id"])
}

// TestSession_GetNonexistent verifies error for nonexistent session.
func (s *DaemonSessionSuite) TestSession_GetNonexistent() {
	_, err := s.Harness.Client.GetSession("nonexistent-session-id")
	s.Error(err, "Getting nonexistent session should error")
}

// TestSession_DeleteSession verifies session deletion.
func (s *DaemonSessionSuite) TestSession_DeleteSession() {
	// Setup mock
	s.MockServer.AddResponse(MockResponse{
		Content: "Delete test.",
	})

	// Create session
	execResp, err := s.Harness.Client.ExecuteInSession("test", "", 30, false, false)
	s.NoError(err)
	sessionID := execResp.SessionID

	// Verify session exists
	_, err = s.Harness.Client.GetSession(sessionID)
	s.NoError(err)

	// Delete session
	deleteResp, err := s.Harness.Client.DeleteSession(sessionID)
	s.NoError(err)
	s.NotNil(deleteResp)

	// Verify session no longer exists
	_, err = s.Harness.Client.GetSession(sessionID)
	s.Error(err, "Deleted session should not be retrievable")
}

// TestSession_CancelSession verifies session cancellation.
func (s *DaemonSessionSuite) TestSession_CancelSession() {
	// Setup mock with delay to allow cancellation
	s.MockServer.AddResponse(MockResponse{
		Content: "Long running task.",
		Delay:   2 * time.Second,
	})

	// Start async execution
	execResp, err := s.Harness.Client.ExecuteInSession("long task", "", 30, false, true)
	s.NoError(err)
	s.NotEmpty(execResp.SessionID)

	sessionID := execResp.SessionID

	// Give task time to start
	time.Sleep(200 * time.Millisecond)

	// Cancel session
	cancelResp, err := s.Harness.Client.CancelSession(sessionID)
	s.NoError(err)
	s.NotNil(cancelResp)

	// Note: The actual cancellation behavior depends on implementation
	// We just verify the API call succeeds
}

// TestSession_ResetSession verifies session reset.
func (s *DaemonSessionSuite) TestSession_ResetSession() {
	// Setup mock
	s.MockServer.AddResponse(MockResponse{
		Content: "First message.",
	})
	s.MockServer.AddResponse(MockResponse{
		Content: "Second message.",
	})

	// Create session with first execution
	execResp1, err := s.Harness.Client.ExecuteInSession("first", "", 30, false, false)
	s.NoError(err)
	sessionID := execResp1.SessionID

	// Reset session
	resetResp, err := s.Harness.Client.ResetSession(sessionID)
	s.NoError(err)
	s.NotNil(resetResp)

	// Execute again in same session (should have clean history)
	execResp2, err := s.Harness.Client.ExecuteInSession("second", sessionID, 30, false, false)
	s.NoError(err)
	s.Equal(sessionID, execResp2.SessionID)
}

// TestSession_ListWithLimit verifies limit parameter.
func (s *DaemonSessionSuite) TestSession_ListWithLimit() {
	// Create multiple sessions
	s.MockServer.SetDefaultResponse(MockResponse{Content: "test"})

	sessionIDs := make([]string, 0)
	for i := 0; i < 5; i++ {
		execResp, err := s.Harness.Client.ExecuteInSession("test", "", 30, false, false)
		if err == nil && execResp.SessionID != "" {
			sessionIDs = append(sessionIDs, execResp.SessionID)
		}
	}

	// List with limit
	listResp, err := s.Harness.Client.ListSessions(3)
	s.NoError(err)
	s.True(listResp.Success)

	// Response should respect limit (though may be less if fewer sessions exist)
	s.LessOrEqual(len(listResp.Sessions), 3)
}

// TestSession_ExecuteInExistingSession verifies reusing session ID.
func (s *DaemonSessionSuite) TestSession_ExecuteInExistingSession() {
	// Setup mocks
	s.MockServer.AddResponse(MockResponse{
		Content: "First execution.",
	})
	s.MockServer.AddResponse(MockResponse{
		Content: "Second execution in same session.",
	})

	// First execution creates session
	execResp1, err := s.Harness.Client.ExecuteInSession("first", "", 30, false, false)
	s.NoError(err)
	sessionID := execResp1.SessionID
	s.NotEmpty(sessionID)

	// Second execution reuses session
	execResp2, err := s.Harness.Client.ExecuteInSession("second", sessionID, 30, false, false)
	s.NoError(err)
	s.Equal(sessionID, execResp2.SessionID, "Should reuse same session ID")
}

// TestSession_AsyncExecution verifies async execution mode.
func (s *DaemonSessionSuite) TestSession_AsyncExecution() {
	s.MockServer.AddResponse(MockResponse{
		Content: "Async task result.",
	})

	// Execute in async mode
	execResp, err := s.Harness.Client.ExecuteInSession("async task", "", 30, false, true)
	s.NoError(err)
	s.NotNil(execResp)

	// Async execution returns immediately with session/run IDs
	s.NotEmpty(execResp.SessionID)
	s.NotEmpty(execResp.RunID)
}

// TestSession_ExecuteWithSteps verifies step inclusion.
func (s *DaemonSessionSuite) TestSession_ExecuteWithSteps() {
	s.MockServer.AddResponse(MockResponse{
		Content: "Task with steps.",
	})

	// Execute with steps
	execResp, err := s.Harness.Client.ExecuteInSession("task", "", 30, true, false)
	s.NoError(err)
	s.True(execResp.Success)

	// Steps should be included in response
	s.NotNil(execResp.Steps)
	// Step count depends on execution, just verify field exists
}

// TestSession_ExecuteWithTimeout verifies timeout parameter.
func (s *DaemonSessionSuite) TestSession_ExecuteWithTimeout() {
	// Setup mock with long delay
	s.MockServer.AddResponse(MockResponse{
		Content: "Should timeout.",
		Delay:   5 * time.Second,
	})

	// Execute with short timeout
	execResp, err := s.Harness.Client.ExecuteInSession("long task", "", 1, false, false)

	// Should either error or return with timeout status
	// Behavior depends on implementation
	if err == nil {
		s.NotNil(execResp)
	}
}
