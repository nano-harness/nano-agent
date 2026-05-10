package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/memory"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
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

func TestMemoryHandlers_KeyValueLifecycle(t *testing.T) {
	testAgent := createTestAgent()
	memoryManager := memory.NewManager(t.TempDir(), t.TempDir(), true)
	defer memoryManager.Close()
	testAgent.SetMemoryManager(memoryManager)
	server := NewServer(testAgent, &config.DaemonConfig{
		Port: 8080,
		Host: "127.0.0.1",
	})

	saveBody := bytes.NewBufferString(`{"key":"release-note","content":"Prefer daemon WebSocket for UI","tags":["daemon"]}`)
	saveReq := httptest.NewRequest("POST", "/api/v1/memory", saveBody)
	saveReq.Header.Set("Content-Type", "application/json")
	saveRecorder := httptest.NewRecorder()
	server.memoryHandler(saveRecorder, saveReq)
	assert.Equal(t, http.StatusOK, saveRecorder.Code)

	getReq := httptest.NewRequest("GET", "/api/v1/memory/release-note", nil)
	getReq = mux.SetURLVars(getReq, map[string]string{"key": "release-note"})
	getRecorder := httptest.NewRecorder()
	server.memoryItemHandler(getRecorder, getReq)
	assert.Equal(t, http.StatusOK, getRecorder.Code)
	var getResponse map[string]interface{}
	assert.NoError(t, json.Unmarshal(getRecorder.Body.Bytes(), &getResponse))
	assert.Equal(t, true, getResponse["found"])
	assert.Equal(t, "Prefer daemon WebSocket for UI", getResponse["content"])

	deleteReq := httptest.NewRequest("DELETE", "/api/v1/memory/release-note", nil)
	deleteReq = mux.SetURLVars(deleteReq, map[string]string{"key": "release-note"})
	deleteRecorder := httptest.NewRecorder()
	server.memoryItemHandler(deleteRecorder, deleteReq)
	assert.Equal(t, http.StatusOK, deleteRecorder.Code)
	var deleteResponse map[string]interface{}
	assert.NoError(t, json.Unmarshal(deleteRecorder.Body.Bytes(), &deleteResponse))
	assert.Equal(t, true, deleteResponse["success"])

	missingReq := httptest.NewRequest("GET", "/api/v1/memory/release-note", nil)
	missingReq = mux.SetURLVars(missingReq, map[string]string{"key": "release-note"})
	missingRecorder := httptest.NewRecorder()
	server.memoryItemHandler(missingRecorder, missingReq)
	assert.Equal(t, http.StatusOK, missingRecorder.Code)
	var missingResponse map[string]interface{}
	assert.NoError(t, json.Unmarshal(missingRecorder.Body.Bytes(), &missingResponse))
	assert.Equal(t, false, missingResponse["found"])
}

func TestMemoryRequestTagsSanitizesCommaDelimitedValues(t *testing.T) {
	tags := memoryRequestTags([]string{" daemon,ui ", "\n"}, "Docs,Internal", "high\rpriority")
	assert.Equal(t, []string{"daemon ui", "category=Docs Internal", "priority=high priority"}, tags)
}

func TestSessionContextStatusHandler(t *testing.T) {
	testAgent := createTestAgent()
	sessionID := "ctx_status"
	session := testAgent.GetSessionManager().GetOrCreateSession(sessionID)
	session.AppendMessage(llm.Message{Role: "user", Content: "hello"})
	session.AppendMessage(llm.Message{Role: "assistant", Content: "world"})
	server := NewServer(testAgent, &config.DaemonConfig{
		Port: 8080,
		Host: "127.0.0.1",
	})

	req := httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID+"/context/status", nil)
	req = mux.SetURLVars(req, map[string]string{"id": sessionID})
	w := httptest.NewRecorder()

	server.sessionContextStatusHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, sessionID, response["session_id"])
	assert.Contains(t, response, "context")
}

func TestRuntimeSessionDirForIDRejectsPathTraversal(t *testing.T) {
	for _, id := range []string{"../secret", "foo/../bar", "/tmp/session", `foo\bar`, "bad.id"} {
		if _, err := runtimeSessionDirForID(id); err == nil {
			t.Fatalf("runtimeSessionDirForID(%q) returned nil error", id)
		}
	}
	if _, err := runtimeSessionDirForID("sess_valid-123"); err != nil {
		t.Fatalf("valid session id rejected: %v", err)
	}
}

func TestModelsHandler(t *testing.T) {
	server := NewServer(nil, &config.DaemonConfig{
		Port: 8080,
		Host: "127.0.0.1",
	})

	req := httptest.NewRequest("GET", "/api/v1/models", nil)
	w := httptest.NewRecorder()

	server.modelsHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "providers")
	assert.NotEmpty(t, response["providers"])
}

func TestEventsAndAuditHandlersQueryStoredEvents(t *testing.T) {
	server := NewServer(nil, &config.DaemonConfig{
		Port: 8080,
		Host: "127.0.0.1",
	})
	store := NewTaskEventStore(10)
	store.Add(event.StreamEvent{Type: event.EventTypeContent, SessionID: "sess1", RunID: "run1", Content: "hello", Timestamp: time.Now().UnixMilli()})
	store.Add(event.StreamEvent{Type: event.EventType(sandbox.EventTypeSandboxDecisionCreated), SessionID: "sess1", RunID: "run1", Content: "sandbox", Timestamp: time.Now().UnixMilli()})
	server.activeTasks["run1"] = &ActiveTask{
		ID:        "run1",
		SessionID: "sess1",
		Store:     store,
	}

	req := httptest.NewRequest("GET", "/api/v1/events?session_id=sess1&since_seq=0", nil)
	w := httptest.NewRecorder()
	server.eventsHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var eventsResp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &eventsResp))
	assert.Equal(t, float64(2), eventsResp["count"])

	auditReq := httptest.NewRequest("GET", "/api/v1/audit?session_id=sess1&since_seq=0", nil)
	auditW := httptest.NewRecorder()
	server.auditHandler(auditW, auditReq)
	assert.Equal(t, http.StatusOK, auditW.Code)
	var auditResp map[string]interface{}
	assert.NoError(t, json.Unmarshal(auditW.Body.Bytes(), &auditResp))
	assert.Equal(t, float64(1), auditResp["count"])
	assert.Equal(t, true, auditResp["audit_only"])
}

func TestCommandsHandlerExposesPreludeMetadata(t *testing.T) {
	cwd := t.TempDir()
	cmdDir := filepath.Join(cwd, ".nano", "commands")
	assert.NoError(t, os.MkdirAll(cmdDir, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(cmdDir, "preflight.md"), []byte(`---
description: Run preflight
prelude_timeout: 9
prelude_on_error: abort
prelude_output: full
---
!echo ready
Do it
`), 0o644))

	cfg := config.DefaultConfig()
	cfg.APIKey = "sk-test-key-for-testing-only"
	cfg.Model = "gpt-3.5-turbo"
	cfg.BaseURL = "http://localhost:9999"
	cfg.WorkingDir = cwd
	agentInstance, err := agent.New(cfg, nil)
	assert.NoError(t, err)
	server := NewServer(agentInstance, &config.DaemonConfig{Port: 8080, Host: "127.0.0.1"})

	req := httptest.NewRequest("GET", "/api/v1/commands", nil)
	w := httptest.NewRecorder()
	server.commandsHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Commands []map[string]interface{} `json:"commands"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	var found map[string]interface{}
	for _, cmd := range resp.Commands {
		if cmd["name"] == "preflight" {
			found = cmd
			break
		}
	}
	if assert.NotNil(t, found) {
		assert.Equal(t, []interface{}{"echo ready"}, found["prelude"])
		assert.Equal(t, float64(9), found["preludeTimeoutSeconds"])
		assert.Equal(t, "abort", found["preludeOnError"])
		assert.Equal(t, "full", found["preludeOutput"])
	}
}

func TestModelDoctorHandler(t *testing.T) {
	testAgent := createTestAgent()
	testAgent.GetConfig().Model = "kimi-k2.5"
	testAgent.GetConfig().BaseURL = "https://api.moonshot.cn/v1"
	server := NewServer(testAgent, &config.DaemonConfig{
		Port: 8080,
		Host: "127.0.0.1",
	})

	req := httptest.NewRequest("GET", "/api/v1/models/doctor", nil)
	w := httptest.NewRecorder()

	server.modelDoctorHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "moonshot", response["provider"])
	assert.Equal(t, true, response["known"])
}
