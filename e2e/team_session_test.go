//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/e2e/shared"
	"github.com/stretchr/testify/suite"
)

// TeamSessionSuite tests team-lead session management via daemon API
type TeamSessionSuite struct {
	suite.Suite
	MockServer *EnhancedMockServer
	Harness    *shared.DaemonHarness
}

func TestTeamSessionSuite(t *testing.T) {
	suite.Run(t, new(TeamSessionSuite))
}

func (s *TeamSessionSuite) SetupTest() {
	s.MockServer = NewMockServerWithDefaults()
	s.Harness = shared.NewDaemonHarness(s.T(), s.MockServer)
	err := s.Harness.WaitReady(2 * time.Second)
	s.Require().NoError(err)
}

func (s *TeamSessionSuite) TearDownTest() {
	if s.Harness != nil {
		s.Harness.Shutdown()
	}
	if s.MockServer != nil {
		s.MockServer.Close()
	}
}

// TeamLeadSessionResponse represents the response from team session API
type TeamLeadSessionResponse struct {
	SessionID    string    `json:"session_id"`
	TeamName     string    `json:"team_name"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
}

type TeamLeadSessionListResponse struct {
	Sessions []TeamLeadSessionResponse `json:"sessions"`
	Count    int                       `json:"count"`
}

// CreateTeamLeadSessionRequest represents request to create team session
type CreateTeamLeadSessionRequest struct {
	SessionID string `json:"session_id,omitempty"`
	TeamName  string `json:"team_name"`
}

// TestTeamSession_CreateAndList verifies creating and listing team-lead sessions
func (s *TeamSessionSuite) TestTeamSession_CreateAndList() {
	// Create team-lead session
	reqBody := CreateTeamLeadSessionRequest{
		TeamName: "alpha",
	}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/api/v1/teams/sessions", s.Harness.BaseURL())
	req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	s.NoError(err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	s.NoError(err)
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)

	// Parse response
	var createResp TeamLeadSessionResponse
	respBody, _ := io.ReadAll(resp.Body)
	err = json.Unmarshal(respBody, &createResp)
	s.NoError(err)

	s.NotEmpty(createResp.SessionID)
	s.Equal("alpha", createResp.TeamName)
	s.False(createResp.CreatedAt.IsZero())

	// List team sessions
	listReq, _ := http.NewRequest("GET", url, nil)
	listResp, err := http.DefaultClient.Do(listReq)
	s.NoError(err)
	defer listResp.Body.Close()

	s.Equal(http.StatusOK, listResp.StatusCode)

	var list TeamLeadSessionListResponse
	listBody, _ := io.ReadAll(listResp.Body)
	err = json.Unmarshal(listBody, &list)
	s.NoError(err)

	// Verify our session is in the list
	found := false
	for _, sess := range list.Sessions {
		if sess.SessionID == createResp.SessionID {
			found = true
			s.Equal("alpha", sess.TeamName)
			break
		}
	}
	s.True(found, "Created team session should appear in list")
}

// TestTeamSession_GetSpecific verifies retrieving specific team session
func (s *TeamSessionSuite) TestTeamSession_GetSpecific() {
	// Create session first
	reqBody := CreateTeamLeadSessionRequest{
		SessionID: "test-team-123",
		TeamName:  "beta",
	}
	body, _ := json.Marshal(reqBody)

	createURL := fmt.Sprintf("%s/api/v1/teams/sessions", s.Harness.BaseURL())
	req, _ := http.NewRequest("POST", createURL, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	createResp, err := http.DefaultClient.Do(req)
	s.NoError(err)
	createResp.Body.Close()

	// Get the specific session
	getURL := fmt.Sprintf("%s/api/v1/teams/sessions/test-team-123", s.Harness.BaseURL())
	getReq, _ := http.NewRequest("GET", getURL, nil)
	getResp, err := http.DefaultClient.Do(getReq)
	s.NoError(err)
	defer getResp.Body.Close()

	s.Equal(http.StatusOK, getResp.StatusCode)

	var session TeamLeadSessionResponse
	getBody, _ := io.ReadAll(getResp.Body)
	err = json.Unmarshal(getBody, &session)
	s.NoError(err)

	s.Equal("test-team-123", session.SessionID)
	s.Equal("beta", session.TeamName)
}

// TestTeamSession_Delete verifies deleting team session
func (s *TeamSessionSuite) TestTeamSession_Delete() {
	// Create session
	reqBody := CreateTeamLeadSessionRequest{
		SessionID: "delete-me",
		TeamName:  "gamma",
	}
	body, _ := json.Marshal(reqBody)

	createURL := fmt.Sprintf("%s/api/v1/teams/sessions", s.Harness.BaseURL())
	req, _ := http.NewRequest("POST", createURL, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	createResp, err := http.DefaultClient.Do(req)
	s.NoError(err)
	createResp.Body.Close()

	// Delete the session
	deleteURL := fmt.Sprintf("%s/api/v1/teams/sessions/delete-me", s.Harness.BaseURL())
	delReq, _ := http.NewRequest("DELETE", deleteURL, nil)
	delResp, err := http.DefaultClient.Do(delReq)
	s.NoError(err)
	delResp.Body.Close()

	s.Equal(http.StatusOK, delResp.StatusCode)

	// Verify it's gone (GET should return 404 or error)
	getReq, _ := http.NewRequest("GET", deleteURL, nil)
	getResp, err := http.DefaultClient.Do(getReq)
	s.NoError(err)
	getResp.Body.Close()

	s.Equal(http.StatusNotFound, getResp.StatusCode)
}

// TestTeamSession_ExecuteCommand verifies executing commands in team session
func (s *TeamSessionSuite) TestTeamSession_ExecuteCommand() {
	// Setup mock response
	s.MockServer.AddResponse(MockResponse{
		Content: "Team command executed",
	})

	// Create team session
	reqBody := CreateTeamLeadSessionRequest{
		SessionID: "exec-test",
		TeamName:  "delta",
	}
	body, _ := json.Marshal(reqBody)

	createURL := fmt.Sprintf("%s/api/v1/teams/sessions", s.Harness.BaseURL())
	req, _ := http.NewRequest("POST", createURL, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	createResp, err := http.DefaultClient.Do(req)
	s.NoError(err)
	createResp.Body.Close()

	// Execute command in team session
	execBody := map[string]interface{}{
		"command": "analyze code",
	}
	execJSON, _ := json.Marshal(execBody)

	execURL := fmt.Sprintf("%s/api/v1/teams/sessions/exec-test/execute", s.Harness.BaseURL())
	execReq, _ := http.NewRequest("POST", execURL, strings.NewReader(string(execJSON)))
	execReq.Header.Set("Content-Type", "application/json")

	execResp, err := http.DefaultClient.Do(execReq)
	s.NoError(err)
	defer execResp.Body.Close()

	s.Equal(http.StatusOK, execResp.StatusCode)

	// Verify response contains events
	execRespBody, _ := io.ReadAll(execResp.Body)
	s.NotEmpty(execRespBody)

	var result map[string]interface{}
	err = json.Unmarshal(execRespBody, &result)
	s.NoError(err)

	// Check that events were collected
	events, ok := result["events"]
	s.True(ok, "Response should contain events")
	s.NotNil(events)
}

// TestTeamSession_GetOrCreate verifies idempotent session creation
func (s *TeamSessionSuite) TestTeamSession_GetOrCreate() {
	// Create session with specific ID
	reqBody := CreateTeamLeadSessionRequest{
		SessionID: "idempotent-test",
		TeamName:  "epsilon",
	}
	body, _ := json.Marshal(reqBody)

	createURL := fmt.Sprintf("%s/api/v1/teams/sessions", s.Harness.BaseURL())

	// First creation
	req1, _ := http.NewRequest("POST", createURL, strings.NewReader(string(body)))
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := http.DefaultClient.Do(req1)
	s.NoError(err)

	var session1 TeamLeadSessionResponse
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	json.Unmarshal(body1, &session1)

	s.Equal("idempotent-test", session1.SessionID)
	createdTime := session1.CreatedAt

	// Second creation with same ID - should return existing session
	time.Sleep(100 * time.Millisecond) // Small delay to ensure timestamp difference

	req2, _ := http.NewRequest("POST", createURL, strings.NewReader(string(body)))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	s.NoError(err)

	var session2 TeamLeadSessionResponse
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	json.Unmarshal(body2, &session2)

	s.Equal("idempotent-test", session2.SessionID)
	// CreatedAt should be the same (not updated)
	s.Equal(createdTime.Unix(), session2.CreatedAt.Unix())
}

// TestTeamSession_MultipleTeams verifies managing multiple team sessions
func (s *TeamSessionSuite) TestTeamSession_MultipleTeams() {
	teams := []string{"team-1", "team-2", "team-3"}
	createdIDs := make([]string, 0, len(teams))

	createURL := fmt.Sprintf("%s/api/v1/teams/sessions", s.Harness.BaseURL())

	// Create multiple team sessions
	for _, teamName := range teams {
		reqBody := CreateTeamLeadSessionRequest{
			TeamName: teamName,
		}
		body, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", createURL, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		s.NoError(err)

		var session TeamLeadSessionResponse
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		json.Unmarshal(respBody, &session)

		createdIDs = append(createdIDs, session.SessionID)
		s.Equal(teamName, session.TeamName)
	}

	// List all sessions
	listReq, _ := http.NewRequest("GET", createURL, nil)
	listResp, err := http.DefaultClient.Do(listReq)
	s.NoError(err)
	defer listResp.Body.Close()

	var list TeamLeadSessionListResponse
	listBody, _ := io.ReadAll(listResp.Body)
	json.Unmarshal(listBody, &list)

	// Verify all created sessions are in the list
	for _, id := range createdIDs {
		found := false
		for _, sess := range list.Sessions {
			if sess.SessionID == id {
				found = true
				break
			}
		}
		s.True(found, fmt.Sprintf("Session %s should be in list", id))
	}
}

// TestTeamSession_NotFoundErrors verifies proper 404 handling
func (s *TeamSessionSuite) TestTeamSession_NotFoundErrors() {
	baseURL := s.Harness.BaseURL()

	// GET non-existent session
	getReq, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/teams/sessions/does-not-exist", baseURL), nil)
	getResp, err := http.DefaultClient.Do(getReq)
	s.NoError(err)
	getResp.Body.Close()
	s.Equal(http.StatusNotFound, getResp.StatusCode)

	// DELETE non-existent session
	delReq, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/teams/sessions/does-not-exist", baseURL), nil)
	delResp, err := http.DefaultClient.Do(delReq)
	s.NoError(err)
	delResp.Body.Close()
	s.Equal(http.StatusNotFound, delResp.StatusCode)

	// EXECUTE in non-existent session
	execBody := map[string]interface{}{"command": "test"}
	execJSON, _ := json.Marshal(execBody)
	execReq, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/teams/sessions/does-not-exist/execute", baseURL), strings.NewReader(string(execJSON)))
	execReq.Header.Set("Content-Type", "application/json")
	execResp, err := http.DefaultClient.Do(execReq)
	s.NoError(err)
	execResp.Body.Close()
	s.Equal(http.StatusNotFound, execResp.StatusCode)
}
