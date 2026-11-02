package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func TestCancelSessionHandler_PersistsSnapshot(t *testing.T) {
	testAgent := createTestAgent()

	tmpDir := t.TempDir()
	testAgent.GetSessionManager().SetStorage(agent.NewLocalSessionStorage(tmpDir))

	sessionID := "sess_cancel_persist"
	testAgent.GetSessionManager().GetOrCreateSession(sessionID)

	server := NewServer(testAgent, &config.DaemonConfig{
		Port: 8080,
		Host: "127.0.0.1",
	})

	_, cancel := context.WithCancel(context.Background())
	server.tasksMutex.Lock()
	server.activeTasks[sessionID] = &ActiveTask{
		ID:          "run_test",
		SessionID:   sessionID,
		Command:     "hello",
		Type:        "unified",
		StartTime:   time.Now(),
		Cancel:      cancel,
		Status:      "running",
		Broadcaster: NewEventBroadcaster(),
		Store:       NewTaskEventStore(100),
	}
	server.tasksMutex.Unlock()

	req := httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/cancel", nil)
	req = mux.SetURLVars(req, map[string]string{"id": sessionID})
	w := httptest.NewRecorder()

	server.cancelSessionHandler(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	snapshotPath := filepath.Join(tmpDir, sessionID+".json")
	data, err := os.ReadFile(snapshotPath)
	require.NoError(t, err)
	require.Contains(t, string(data), sessionID)
}
