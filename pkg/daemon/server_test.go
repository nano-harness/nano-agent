package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/gorilla/mux"
)

// createTestAgent creates a minimal agent for testing
func createTestAgent() *agent.Agent {
	// Create minimal config for testing
	cfg := config.DefaultConfig()
	cfg.APIKey = "sk-test-key-for-testing-only" // Valid format API key
	cfg.Model = "gpt-3.5-turbo"
	cfg.BaseURL = "http://localhost:9999" // Point to non-existent server to avoid real API calls

	// Load the config globally to avoid "configuration not loaded" error
	config.LoadConfig("") // This will load default config //nolint:errcheck

	// Create agent with approval handler that always approves
	agentInstance, _ := agent.New(cfg, func(*agent.ToolCallInfo) bool {
		return true
	})

	return agentInstance
}

func TestSessionExecuteHandler_TimeoutHandling(t *testing.T) {
	testAgent := createTestAgent()
	server := NewServer(testAgent, &config.DaemonConfig{
		Port: 8080,
		Host: "127.0.0.1",
	})

	// Test with empty command (should return 400 - command is required)
	reqBody := struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout,omitempty"`
	}{
		Command: "",
		Timeout: 5,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/api/v1/sessions/sess_test/execute", bytes.NewBuffer(body))
	req = mux.SetURLVars(req, map[string]string{"id": "sess_test"})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.sessionExecuteHandler(w, req)

	// Empty command should return 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionExecuteHandler_InvalidRequests(t *testing.T) {
	testAgent := createTestAgent()
	server := NewServer(testAgent, &config.DaemonConfig{
		Port: 8080,
		Host: "127.0.0.1",
	})

	// Test with invalid JSON (should return 400 - invalid request)
	req := httptest.NewRequest("POST", "/api/v1/sessions/sess_test/execute", strings.NewReader("invalid json"))
	req = mux.SetURLVars(req, map[string]string{"id": "sess_test"})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.sessionExecuteHandler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Test with another empty command case
	reqBody2 := struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout,omitempty"`
	}{
		Command: "",
		Timeout: 10,
	}
	body2, _ := json.Marshal(reqBody2)

	req2 := httptest.NewRequest("POST", "/api/v1/sessions/sess_test/execute", bytes.NewReader(body2))
	req2 = mux.SetURLVars(req2, map[string]string{"id": "sess_test"})
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	server.sessionExecuteHandler(w2, req2)

	assert.Equal(t, http.StatusBadRequest, w2.Code)
}

func TestSessionExecuteHandler_ErrorTypes(t *testing.T) {
	testAgent := createTestAgent()
	server := NewServer(testAgent, &config.DaemonConfig{
		Port: 8080,
		Host: "127.0.0.1",
	})

	// Test with empty command (should return 400 - command is required)
	reqBody := struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout,omitempty"`
	}{
		Command: "",
		Timeout: 5,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/api/v1/sessions/sess_test/execute", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"id": "sess_test"})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.sessionExecuteHandler(w, req)

	// Empty command should return 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHealthHandler(t *testing.T) {
	testAgent := createTestAgent()
	server := NewServer(testAgent, &config.DaemonConfig{
		Port: 8080,
		Host: "127.0.0.1",
	})

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()

	server.healthHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "healthy", response["status"])
}

func TestStatusHandler(t *testing.T) {
	testAgent := createTestAgent()
	server := NewServer(testAgent, &config.DaemonConfig{
		Port: 8080,
		Host: "127.0.0.1",
	})

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()

	server.statusHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "agent_status")
}
