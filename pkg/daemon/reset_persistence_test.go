package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/stretchr/testify/require"
)

func TestResetSessionHandler_ClearsHistoryMetadataAndPersists(t *testing.T) {
	testAgent := createTestAgent()

	tmpDir := t.TempDir()
	testAgent.GetSessionManager().SetStorage(agent.NewLocalSessionStorage(tmpDir))

	sessionID := "sess_reset_persist"
	session := testAgent.GetSessionManager().GetOrCreateSession(sessionID)

	// Seed: history + metadata + stats
	session.AppendMessage(llm.Message{Role: "user", Content: "hello"})
	session.AppendMessage(llm.Message{Role: "assistant", Content: "hi there"})
	session.Metadata["title"] = "Old Title"
	session.Metadata["custom"] = "should-be-cleared"
	session.UpdateStats(1234, 5.0)

	server := NewServer(testAgent, &config.DaemonConfig{
		Port: 8080,
		Host: "127.0.0.1",
	})

	body, _ := json.Marshal(map[string]string{"session_id": sessionID})
	req := httptest.NewRequest("POST", "/api/v1/sessions/reset", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.resetSessionHandler(w, req)

	// Assertion 1: HTTP 200 + success body
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, true, resp["success"])
	require.Equal(t, sessionID, resp["session_id"])
	require.Equal(t, "reset", resp["status"])

	// Assertion 2: in-memory session has cleared state
	inMem, ok := testAgent.GetSessionManager().GetSession(sessionID)
	require.True(t, ok)
	require.Empty(t, inMem.GetConversationHistory(), "history should be cleared")
	require.Empty(t, inMem.GetMetadataCopy(), "metadata should be cleared")
	require.Equal(t, 0, inMem.TotalTokens, "stats should be reset")
	require.Equal(t, sessionID, inMem.ID, "ID must be preserved")

	// Assertion 3: snapshot persisted to disk reflects cleared state
	snapshotPath := filepath.Join(tmpDir, sessionID+".json")
	data, err := os.ReadFile(snapshotPath)
	require.NoError(t, err)
	require.Contains(t, string(data), sessionID)
	require.NotContains(t, string(data), "Old Title", "title must not persist")
	require.NotContains(t, string(data), "should-be-cleared", "custom metadata must not persist")
}

func TestResetSessionHandler_RejectsEmptyAndUnknown(t *testing.T) {
	testAgent := createTestAgent()
	server := NewServer(testAgent, &config.DaemonConfig{Port: 8080, Host: "127.0.0.1"})

	// Empty session_id → 400
	body, _ := json.Marshal(map[string]string{"session_id": ""})
	req := httptest.NewRequest("POST", "/api/v1/sessions/reset", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.resetSessionHandler(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Unknown session_id → 404
	body, _ = json.Marshal(map[string]string{"session_id": "sess_does_not_exist"})
	req = httptest.NewRequest("POST", "/api/v1/sessions/reset", bytes.NewReader(body))
	w = httptest.NewRecorder()
	server.resetSessionHandler(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}
