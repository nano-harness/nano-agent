package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/gorilla/mux"
)

func TestTeamLeadSessionRecordEventStoresAndBroadcasts(t *testing.T) {
	session := &TeamLeadSession{
		ID:          "lead-alpha-chat-1",
		TeamName:    "alpha",
		Store:       NewTaskEventStore(100),
		Broadcaster: NewEventBroadcaster(),
	}
	sub := session.Broadcaster.Subscribe()
	defer session.Broadcaster.Unsubscribe(sub)

	session.enrichAndRecordEvent(event.StreamEvent{Type: event.EventTypeContent, Content: "hello"})

	stored := session.Store.Since(0, func(event.StreamEvent) bool { return true })
	if len(stored) != 1 {
		t.Fatalf("stored events = %d, want 1", len(stored))
	}
	if stored[0].Seq != 1 || stored[0].SessionID != session.ID || stored[0].RunID == "" {
		t.Fatalf("stored event missing session/run sequencing: %+v", stored[0])
	}

	select {
	case ev := <-sub:
		if ev.Seq != 1 || ev.Content != "hello" {
			t.Fatalf("broadcast event = %+v", ev)
		}
	default:
		t.Fatal("expected broadcast event")
	}
}

func TestTeamLeadSessionCancelActiveTasksCancelsRunningTasks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := &TeamLeadSession{
		ID:          "lead-alpha-chat-1",
		activeTasks: make(map[string]*ActiveTask),
		Store:       NewTaskEventStore(100),
		Broadcaster: NewEventBroadcaster(),
	}
	taskCtx, taskCancel := context.WithCancel(ctx)
	session.activeTasks["task-1"] = &ActiveTask{
		ID:     "task-1",
		Status: "running",
		Cancel: taskCancel,
	}

	if cancelled := session.CancelActiveTasks(); cancelled != 1 {
		t.Fatalf("CancelActiveTasks() = %d, want 1", cancelled)
	}
	if session.activeTasks["task-1"].Status != "cancelled" {
		t.Fatalf("task status = %q, want cancelled", session.activeTasks["task-1"].Status)
	}
	select {
	case <-taskCtx.Done():
	default:
		t.Fatal("task context was not cancelled")
	}
}

func TestTeamLeadSessionEventsHandlerReplaysSinceSeq(t *testing.T) {
	session := &TeamLeadSession{
		ID:          "lead-alpha-chat-1",
		TeamName:    "alpha",
		Store:       NewTaskEventStore(100),
		Broadcaster: NewEventBroadcaster(),
	}
	session.enrichAndRecordEvent(event.StreamEvent{Type: event.EventTypeContent, Content: "one"})
	session.enrichAndRecordEvent(event.StreamEvent{Type: event.EventTypeContent, Content: "two"})

	server := &Server{teamLeadRegistry: &TeamLeadRegistry{
		sessions: map[string]*TeamLeadSession{session.ID: session},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/sessions/"+session.ID+"/events?since_seq=1", nil)
	req = mux.SetURLVars(req, map[string]string{"id": session.ID})
	rec := httptest.NewRecorder()

	server.teamLeadSessionEventsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		SessionID string              `json:"session_id"`
		SinceSeq  int64               `json:"since_seq"`
		LastSeq   int64               `json:"last_seq"`
		Events    []event.StreamEvent `json:"events"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.LastSeq != 2 || len(body.Events) != 1 || body.Events[0].Content != "two" {
		t.Fatalf("unexpected replay response: %+v", body)
	}
}

func TestCancelTeamLeadSessionHandlerCancelsRunningTasks(t *testing.T) {
	_, taskCancel := context.WithCancel(context.Background())
	session := &TeamLeadSession{
		ID:          "lead-alpha-chat-1",
		activeTasks: map[string]*ActiveTask{"task-1": {ID: "task-1", Status: "running", Cancel: taskCancel}},
	}
	server := &Server{teamLeadRegistry: &TeamLeadRegistry{
		sessions: map[string]*TeamLeadSession{session.ID: session},
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/sessions/"+session.ID+"/cancel", nil)
	req = mux.SetURLVars(req, map[string]string{"id": session.ID})
	rec := httptest.NewRecorder()

	server.cancelTeamLeadSessionHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Success        bool `json:"success"`
		CancelledTasks int  `json:"cancelled_tasks"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success || body.CancelledTasks != 1 {
		t.Fatalf("unexpected cancel response: %+v", body)
	}
}
