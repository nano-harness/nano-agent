package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
)

func TestClientQueryEventsBuildsFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/events" {
			t.Fatalf("path = %s, want /api/v1/events", r.URL.Path)
		}
		query := r.URL.Query()
		for key, want := range map[string]string{
			"session_id": "sess-1",
			"run_id":     "run-1",
			"type":       "sandbox.command.finished",
			"sandbox":    "true",
			"since_seq":  "7",
			"limit":      "9",
		} {
			if got := query.Get(key); got != want {
				t.Fatalf("%s = %q, want %q", key, got, want)
			}
		}
		_ = json.NewEncoder(w).Encode(EventQueryResponse{
			Events: []event.StreamEvent{{Type: event.EventTypeSandboxCommandFinished, Content: "done"}},
			Count:  1,
			Limit:  9,
		})
	}))
	defer server.Close()

	client := testClientForServer(server.URL)
	resp, err := client.QueryEvents(EventQuery{
		SessionID: "sess-1",
		RunID:     "run-1",
		Type:      "sandbox.command.finished",
		Sandbox:   true,
		SinceSeq:  7,
		Limit:     9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Count != 1 || len(resp.Events) != 1 {
		t.Fatalf("response = %#v", resp)
	}
}

func TestClientQueryAuditUsesAuditEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/audit" {
			t.Fatalf("path = %s, want /api/v1/audit", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(EventQueryResponse{AuditOnly: true})
	}))
	defer server.Close()

	resp, err := testClientForServer(server.URL).QueryAudit(EventQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.AuditOnly {
		t.Fatalf("AuditOnly = false, want true")
	}
}

func testClientForServer(rawURL string) *Client {
	return &Client{
		baseURL:   strings.TrimRight(rawURL, "/") + "/api/v1",
		client:    &http.Client{Timeout: 2 * time.Second},
		userAgent: "test",
	}
}
