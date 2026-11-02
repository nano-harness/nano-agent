package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestTeamLeadSessionStreamHandler_LeadInputExecutesAndStreams(t *testing.T) {
	session := &TeamLeadSession{
		ID:          "lead-alpha-chat",
		TeamName:    "alpha",
		Store:       NewTaskEventStore(100),
		Broadcaster: NewEventBroadcaster(),
		activeTasks: make(map[string]*ActiveTask),
		executeFunc: func(_ context.Context, taskID, command string, callback func(event.StreamEvent)) error {
			callback(event.StreamEvent{
				Type:    event.EventTypeContent,
				Content: "echo: " + command,
				TaskID:  taskID,
			})
			callback(event.StreamEvent{
				Type:   event.EventTypeTaskCompletion,
				TaskID: taskID,
				Done:   true,
			})
			return nil
		},
	}
	server := NewServer(nil, &config.DaemonConfig{
		Port:       0,
		Host:       "127.0.0.1",
		EnableCORS: true,
		APIKey:     "",
	})
	server.teamLeadRegistry = &TeamLeadRegistry{
		sessions: map[string]*TeamLeadSession{session.ID: session},
	}

	r := mux.NewRouter()
	r.HandleFunc("/api/v1/teams/sessions/{id}/stream", server.teamLeadSessionStreamHandler).Methods("GET")
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/teams/sessions/lead-alpha-chat/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if !assert.NoError(t, err) {
		return
	}
	t.Cleanup(func() { _ = conn.Close() })

	err = conn.WriteJSON(map[string]interface{}{
		"type":    "lead_input",
		"command": "hello",
		"task_id": "task-fixed",
	})
	assert.NoError(t, err)

	_, msg1, err := conn.ReadMessage()
	assert.NoError(t, err)
	var ack map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg1, &ack))
	assert.Equal(t, "lead_input_ack", ack["type"])
	assert.Equal(t, "lead-alpha-chat", ack["session_id"])
	assert.Equal(t, "task-fixed", ack["task_id"])

	_, msg2, err := conn.ReadMessage()
	assert.NoError(t, err)
	var content map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg2, &content))
	assert.Equal(t, "content", content["type"])
	assert.Equal(t, "echo: hello", content["content"])
	assert.Equal(t, float64(1), content["seq"])

	_, msg3, err := conn.ReadMessage()
	assert.NoError(t, err)
	var done map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg3, &done))
	assert.Equal(t, "task_completion", done["type"])
	assert.Equal(t, float64(2), done["seq"])

	_, msg4, err := conn.ReadMessage()
	assert.NoError(t, err)
	var completion map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg4, &completion))
	assert.Equal(t, "completion", completion["type"])
	assert.Equal(t, "completed", completion["status"])
	assert.Equal(t, float64(2), completion["last_seq"])
}

func TestTeamLeadSessionInteractiveApprovalPublishesWaitingEvent(t *testing.T) {
	session := &TeamLeadSession{
		ID:               "lead-alpha-chat",
		TeamName:         "alpha",
		Store:            NewTaskEventStore(100),
		Broadcaster:      NewEventBroadcaster(),
		activeTasks:      make(map[string]*ActiveTask),
		pendingApprovals: make(map[string]chan agent.ApprovalDecision),
	}

	// requestToolApprovalV2 blocks until SubmitToolApproval sends a decision,
	// so run it in a goroutine and verify the waiting event was published.
	done := make(chan agent.ApprovalDecision, 1)
	go func() {
		decision := session.requestToolApprovalV2(&agent.ToolCallInfo{
			ID:         "call-1",
			Name:       "bash",
			Parameters: map[string]interface{}{"command": "rm -rf tmp"},
		})
		done <- decision
	}()

	// Give the goroutine time to register the pending approval and publish the event.
	time.Sleep(50 * time.Millisecond)

	events := session.Store.Since(0, func(event.StreamEvent) bool { return true })
	if assert.Len(t, events, 1) {
		assert.Equal(t, event.EventTypeWaitingForUser, events[0].Type)
		assert.Equal(t, "tool_approval_request", events[0].Metadata["kind"])
		assert.Equal(t, "call-1", events[0].Metadata["call_id"])
		assert.Equal(t, "bash", events[0].Metadata["tool_name"])
	}

	// Unblock the handler
	require.NoError(t, session.SubmitToolApproval("call-1", false))
	decision := <-done
	assert.Equal(t, agent.ApprovalReject, decision)
}

func TestTeamLeadSessionStreamHandler_ToolApprovalFrame(t *testing.T) {
	var gotCallID string
	var gotApproved bool
	session := &TeamLeadSession{
		ID:          "lead-alpha-chat",
		TeamName:    "alpha",
		Store:       NewTaskEventStore(100),
		Broadcaster: NewEventBroadcaster(),
		activeTasks: make(map[string]*ActiveTask),
		approveFunc: func(callID string, approved bool) error {
			gotCallID = callID
			gotApproved = approved
			return nil
		},
	}
	server := NewServer(nil, &config.DaemonConfig{
		Port:       0,
		Host:       "127.0.0.1",
		EnableCORS: true,
		APIKey:     "",
	})
	server.teamLeadRegistry = &TeamLeadRegistry{
		sessions: map[string]*TeamLeadSession{session.ID: session},
	}

	r := mux.NewRouter()
	r.HandleFunc("/api/v1/teams/sessions/{id}/stream", server.teamLeadSessionStreamHandler).Methods("GET")
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/teams/sessions/lead-alpha-chat/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if !assert.NoError(t, err) {
		return
	}
	t.Cleanup(func() { _ = conn.Close() })

	err = conn.WriteJSON(map[string]interface{}{
		"type":     "tool_approval",
		"call_id":  "call-1",
		"approved": true,
	})
	assert.NoError(t, err)

	_, msg, err := conn.ReadMessage()
	assert.NoError(t, err)
	var ack map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg, &ack))
	assert.Equal(t, "tool_approval_ack", ack["type"])
	assert.Equal(t, "call-1", ack["call_id"])
	assert.Equal(t, true, ack["approved"])
	assert.Equal(t, "call-1", gotCallID)
	assert.True(t, gotApproved)
}

func TestTeamLeadSessionStreamHandler_ApproveRejectAliases(t *testing.T) {
	var approvals []bool
	session := &TeamLeadSession{
		ID:          "lead-alpha-chat",
		TeamName:    "alpha",
		Store:       NewTaskEventStore(100),
		Broadcaster: NewEventBroadcaster(),
		activeTasks: make(map[string]*ActiveTask),
		approveFunc: func(callID string, approved bool) error {
			assert.Equal(t, "call-1", callID)
			approvals = append(approvals, approved)
			return nil
		},
	}
	server := NewServer(nil, &config.DaemonConfig{
		Port:       0,
		Host:       "127.0.0.1",
		EnableCORS: true,
		APIKey:     "",
	})
	server.teamLeadRegistry = &TeamLeadRegistry{
		sessions: map[string]*TeamLeadSession{session.ID: session},
	}

	r := mux.NewRouter()
	r.HandleFunc("/api/v1/teams/sessions/{id}/stream", server.teamLeadSessionStreamHandler).Methods("GET")
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/teams/sessions/lead-alpha-chat/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if !assert.NoError(t, err) {
		return
	}
	t.Cleanup(func() { _ = conn.Close() })

	for _, frameType := range []string{"approve", "reject"} {
		assert.NoError(t, conn.WriteJSON(map[string]interface{}{
			"type":    frameType,
			"call_id": "call-1",
		}))
		_, msg, err := conn.ReadMessage()
		assert.NoError(t, err)
		var ack map[string]interface{}
		assert.NoError(t, json.Unmarshal(msg, &ack))
		assert.Equal(t, "tool_approval_ack", ack["type"])
	}
	assert.Equal(t, []bool{true, false}, approvals)
}

func TestTeamLeadSessionStreamHandler_ReplayFrame(t *testing.T) {
	session := &TeamLeadSession{
		ID:          "lead-alpha-chat",
		TeamName:    "alpha",
		Store:       NewTaskEventStore(100),
		Broadcaster: NewEventBroadcaster(),
		activeTasks: make(map[string]*ActiveTask),
	}
	session.enrichAndRecordEvent(event.StreamEvent{Type: event.EventTypeContent, Content: "one"})
	session.enrichAndRecordEvent(event.StreamEvent{Type: event.EventTypeContent, Content: "two"})
	server := NewServer(nil, &config.DaemonConfig{
		Port:       0,
		Host:       "127.0.0.1",
		EnableCORS: true,
		APIKey:     "",
	})
	server.teamLeadRegistry = &TeamLeadRegistry{
		sessions: map[string]*TeamLeadSession{session.ID: session},
	}

	r := mux.NewRouter()
	r.HandleFunc("/api/v1/teams/sessions/{id}/stream", server.teamLeadSessionStreamHandler).Methods("GET")
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/teams/sessions/lead-alpha-chat/stream"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if !assert.NoError(t, err) {
		return
	}
	t.Cleanup(func() { _ = conn.Close() })

	assert.NoError(t, conn.WriteJSON(map[string]interface{}{
		"type":      "replay",
		"since_seq": 1,
	}))
	_, msg1, err := conn.ReadMessage()
	assert.NoError(t, err)
	var replayed map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg1, &replayed))
	assert.Equal(t, "content", replayed["type"])
	assert.Equal(t, "two", replayed["content"])

	_, msg2, err := conn.ReadMessage()
	assert.NoError(t, err)
	var done map[string]interface{}
	assert.NoError(t, json.Unmarshal(msg2, &done))
	assert.Equal(t, "replay_complete", done["type"])
	assert.Equal(t, float64(2), done["last_seq"])
	assert.Equal(t, float64(1), done["count"])
}

func TestClientSubscribeSessionWithResumeSendsToolApproval(t *testing.T) {
	var gotSubscribe map[string]interface{}
	var gotApproval map[string]interface{}

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if !assert.NoError(t, err) {
			return
		}
		defer func() { _ = conn.Close() }()

		assert.NoError(t, conn.ReadJSON(&gotSubscribe))
		assert.NoError(t, conn.WriteJSON(map[string]interface{}{
			"type": "waiting_for_user",
			"seq":  float64(1),
			"metadata": map[string]interface{}{
				"kind":       "tool_approval_request",
				"call_id":    "call-1",
				"tool_name":  "bash",
				"parameters": map[string]interface{}{"command": "echo ok"},
			},
		}))
		assert.NoError(t, conn.ReadJSON(&gotApproval))
		assert.NoError(t, conn.WriteJSON(map[string]interface{}{
			"type":     "completion",
			"last_seq": float64(1),
			"success":  true,
			"status":   "completed",
		}))
	}))
	t.Cleanup(ts.Close)

	client := &Client{
		baseURL: strings.TrimRight(ts.URL, "/") + "/api/v1",
		client:  ts.Client(),
	}
	_, lastSeq, err := client.SubscribeSessionWithResume(context.Background(), SubscribeOptions{
		SessionID: "sess-1",
		ApprovalHandler: func(req ToolApprovalRequest) bool {
			assert.Equal(t, "call-1", req.CallID)
			assert.Equal(t, "bash", req.ToolName)
			assert.Equal(t, "echo ok", req.Parameters["command"])
			return true
		},
	}, nil)

	assert.NoError(t, err)
	assert.Equal(t, int64(1), lastSeq)
	assert.Equal(t, "subscribe", gotSubscribe["type"])
	assert.Equal(t, "sess-1", gotSubscribe["session_id"])
	assert.Equal(t, "tool_approval", gotApproval["type"])
	assert.Equal(t, "call-1", gotApproval["call_id"])
	assert.Equal(t, true, gotApproval["approved"])
}
