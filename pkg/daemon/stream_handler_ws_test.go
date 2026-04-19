package daemon

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestStreamHandler_RequiresSessionID(t *testing.T) {
	server := NewServer(nil, &config.DaemonConfig{
		Port:       0,
		Host:       "127.0.0.1",
		EnableCORS: true,
		APIKey:     "",
	})

	r := mux.NewRouter()
	r.HandleFunc("/api/v1/stream", server.streamHandler).Methods("GET")
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if !assert.NoError(t, err) {
		return
	}
	t.Cleanup(func() { _ = conn.Close() })

	err = conn.WriteJSON(map[string]interface{}{
		"command":    "",
		"session_id": "",
	})
	assert.NoError(t, err)

	_, msg, err := conn.ReadMessage()
	assert.NoError(t, err)

	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg, &resp))
	if resp["type"] != "error" {
		t.Logf("Unexpected response: %+v", resp)
	}
	assert.Equal(t, "error", resp["type"])
	assert.Equal(t, "session_id is required", resp["error"])
}

func TestStreamHandler_AttachRunningSession_IgnoresCommand(t *testing.T) {
	server := NewServer(nil, &config.DaemonConfig{
		Port:       0,
		Host:       "127.0.0.1",
		EnableCORS: true,
		APIKey:     "",
	})

	task := &ActiveTask{
		ID:          "run_1",
		SessionID:   "sess_1",
		Command:     "original",
		Status:      "running",
		Broadcaster: NewEventBroadcaster(),
	}

	server.tasksMutex.Lock()
	server.activeTasks["sess_1"] = task
	server.tasksMutex.Unlock()

	r := mux.NewRouter()
	r.HandleFunc("/api/v1/stream", server.streamHandler).Methods("GET")
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if !assert.NoError(t, err) {
		return
	}
	t.Cleanup(func() { _ = conn.Close() })

	err = conn.WriteJSON(map[string]interface{}{
		"session_id": "sess_1",
		"command":    "new command should be ignored",
	})
	assert.NoError(t, err)

	_, msg1, err := conn.ReadMessage()
	assert.NoError(t, err)
	var resp1 map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg1, &resp1))
	assert.Equal(t, "error", resp1["type"])
	assert.Equal(t, true, resp1["ignored_command"])
	assert.Equal(t, "sess_1", resp1["session_id"])

	_, msg2, err := conn.ReadMessage()
	assert.NoError(t, err)
	var resp2 map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg2, &resp2))
	assert.Equal(t, "session_start", resp2["type"])
	assert.Equal(t, "sess_1", resp2["session_id"])

	task.Broadcaster.Publish(event.StreamEvent{
		Type:      event.EventTypeTaskCompletion,
		SessionID: "sess_1",
		Timestamp: time.Now().Unix(),
	})

	_, msg3, err := conn.ReadMessage()
	assert.NoError(t, err)
	var resp3 map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg3, &resp3))
	assert.Equal(t, "task_completion", resp3["type"])
	assert.Equal(t, "sess_1", resp3["session_id"])

	_, msg4, err := conn.ReadMessage()
	assert.NoError(t, err)
	var resp4 map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg4, &resp4))
	assert.Equal(t, "completion", resp4["type"])
	assert.Equal(t, "sess_1", resp4["session_id"])
}

func TestStreamHandler_StatusWhenNotRunning_NoCommand(t *testing.T) {
	server := NewServer(nil, &config.DaemonConfig{
		Port:       0,
		Host:       "127.0.0.1",
		EnableCORS: true,
		APIKey:     "",
	})

	task := &ActiveTask{
		ID:          "run_2",
		SessionID:   "sess_2",
		Command:     "original",
		Title:       "Title",
		Status:      "completed",
		Broadcaster: NewEventBroadcaster(),
	}

	server.tasksMutex.Lock()
	server.activeTasks["sess_2"] = task
	server.tasksMutex.Unlock()

	r := mux.NewRouter()
	r.HandleFunc("/api/v1/stream", server.streamHandler).Methods("GET")
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if !assert.NoError(t, err) {
		return
	}
	t.Cleanup(func() { _ = conn.Close() })

	err = conn.WriteJSON(map[string]interface{}{
		"session_id": "sess_2",
		"command":    "",
	})
	assert.NoError(t, err)

	_, msg, err := conn.ReadMessage()
	assert.NoError(t, err)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg, &resp))
	assert.Equal(t, "status", resp["type"])
	assert.Equal(t, "sess_2", resp["session_id"])
	assert.Equal(t, "completed", resp["status"])
	assert.Equal(t, "Title", resp["title"])
}

func TestStreamHandler_SubscribeCompletedSession_WithNilStore(t *testing.T) {
	server := NewServer(nil, &config.DaemonConfig{
		Port:       0,
		Host:       "127.0.0.1",
		EnableCORS: true,
		APIKey:     "",
	})

	task := &ActiveTask{
		ID:         "run_3",
		SessionID:  "sess_3",
		Command:    "original",
		Title:      "Title",
		Status:     "completed",
		TokenUsage: &event.TokenStats{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}

	server.tasksMutex.Lock()
	server.activeTasks["sess_3"] = task
	server.tasksMutex.Unlock()

	r := mux.NewRouter()
	r.HandleFunc("/api/v1/stream", server.streamHandler).Methods("GET")
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if !assert.NoError(t, err) {
		return
	}
	t.Cleanup(func() { _ = conn.Close() })

	err = conn.WriteJSON(map[string]interface{}{
		"type":       "subscribe",
		"session_id": "sess_3",
	})
	assert.NoError(t, err)

	_, msg1, err := conn.ReadMessage()
	assert.NoError(t, err)
	var resp1 map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg1, &resp1))
	assert.Equal(t, "session_start", resp1["type"])
	assert.Equal(t, "sess_3", resp1["session_id"])
	assert.Equal(t, "run_3", resp1["run_id"])

	_, msg2, err := conn.ReadMessage()
	assert.NoError(t, err)
	var resp2 map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg2, &resp2))
	assert.Equal(t, "completion", resp2["type"])
	assert.Equal(t, "sess_3", resp2["session_id"])
	assert.Equal(t, "completed", resp2["status"])
	assert.Equal(t, float64(0), resp2["last_seq"])
}
