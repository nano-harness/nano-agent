package hookservice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPHookPostsAndAllows(t *testing.T) {
	var receivedBody []byte
	var receivedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"action":"allow","reason":"ok from server"}`))
	}))
	defer srv.Close()

	hook := Hook{
		Name:    "http-allow",
		Event:   EventPreToolUse,
		Pattern: "*",
		Type:    HookTypeHTTP,
		HTTPConfig: &HTTPHookConfig{
			URL:          srv.URL,
			URLAllowlist: []string{"127.0.0.1"},
		},
		Enabled: true,
	}
	svc := New([]Hook{hook})
	dec, err := svc.Execute(context.Background(), EventPreToolUse, "write_file", map[string]interface{}{"path": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != ActionAllow {
		t.Fatalf("expected allow, got %#v", dec)
	}
	if !strings.Contains(string(receivedBody), `"tool_name":"write_file"`) {
		t.Fatalf("server payload missing tool_name: %s", receivedBody)
	}
	if receivedContentType != "application/json" {
		t.Fatalf("expected content-type JSON, got %q", receivedContentType)
	}
}

func TestHTTPHookBlocksWithStructuredOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"action":"block","reason":"forbidden"}`))
	}))
	defer srv.Close()

	hook := Hook{
		Name: "http-block", Event: EventPreToolUse, Pattern: "*", Type: HookTypeHTTP,
		HTTPConfig: &HTTPHookConfig{URL: srv.URL}, Enabled: true,
	}
	dec, err := New([]Hook{hook}).Execute(context.Background(), EventPreToolUse, "rm", nil)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != ActionBlock || dec.Reason != "forbidden" {
		t.Fatalf("expected block with reason, got %#v", dec)
	}
}

func TestHTTPHookURLAllowlistRejectsUnknownHost(t *testing.T) {
	hook := Hook{
		Name: "http-deny", Event: EventPreToolUse, Pattern: "*", Type: HookTypeHTTP,
		HTTPConfig: &HTTPHookConfig{
			URL:          "http://evil.example.com/hook",
			URLAllowlist: []string{"trusted.example.com", "*.internal"},
		},
		FailurePolicy: FailurePolicyBlock,
		Enabled:       true,
	}
	dec, err := New([]Hook{hook}).Execute(context.Background(), EventPreToolUse, "write_file", nil)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != ActionBlock {
		t.Fatalf("expected block, got %#v", dec)
	}
	if !strings.Contains(dec.Reason, "allowlist") {
		t.Fatalf("expected reason to mention allowlist, got %q", dec.Reason)
	}
}

func TestSanitizeHeaderStripsCRLF(t *testing.T) {
	in := "value\r\nX-Inject: bad"
	if got := sanitizeHeader(in); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("sanitizeHeader did not strip CRLF: %q", got)
	}
}

func TestIsURLAllowedWildcard(t *testing.T) {
	if !isURLAllowed("https://api.foo.example.com/x", []string{"*.example.com"}) {
		t.Fatal("expected wildcard match")
	}
	if isURLAllowed("https://example.com/x", []string{"*.example.com"}) {
		t.Fatal("wildcard should require subdomain")
	}
	if !isURLAllowed("https://anything.example.com", nil) {
		t.Fatal("empty allowlist should allow all")
	}
}

type stubLLM struct {
	resp string
	err  error
}

func (s *stubLLM) Decide(_ context.Context, _ string, _ string, _ int) (string, error) {
	return s.resp, s.err
}

func TestPromptHookOK(t *testing.T) {
	hook := Hook{
		Name: "prompt-allow", Event: EventPreToolUse, Pattern: "*", Type: HookTypePrompt,
		PromptConfig: &PromptHookConfig{Prompt: "Tool={{.Tool}}", Model: "haiku", MaxTokens: 64},
		Enabled:      true,
	}
	svc := NewWithOptions([]Hook{hook}, Options{LLMDecider: &stubLLM{resp: `{"ok":true,"reason":"safe"}`}})
	dec, err := svc.Execute(context.Background(), EventPreToolUse, "read_file", nil)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != ActionAllow {
		t.Fatalf("expected allow, got %#v", dec)
	}
}

func TestPromptHookBlocks(t *testing.T) {
	hook := Hook{
		Name: "prompt-block", Event: EventPreToolUse, Pattern: "*", Type: HookTypePrompt,
		PromptConfig: &PromptHookConfig{Prompt: "x"},
		Enabled:      true,
	}
	svc := NewWithOptions([]Hook{hook}, Options{LLMDecider: &stubLLM{resp: `{"ok":false,"reason":"unsafe"}`}})
	dec, _ := svc.Execute(context.Background(), EventPreToolUse, "rm", nil)
	if dec.Action != ActionBlock || dec.Reason != "unsafe" {
		t.Fatalf("expected block(unsafe), got %#v", dec)
	}
}

type stubAgent struct{ resp string }

func (s *stubAgent) Run(_ context.Context, _ string, _ string) (string, error) {
	return s.resp, nil
}

func TestAgentHookDelegates(t *testing.T) {
	hook := Hook{
		Name: "agent", Event: EventPreToolUse, Pattern: "*", Type: HookTypeAgent,
		AgentConfig: &AgentHookConfig{Agent: "code-reviewer"},
		Enabled:     true,
	}
	svc := NewWithOptions([]Hook{hook}, Options{AgentDecider: &stubAgent{resp: `{"action":"confirm","reason":"ask user"}`}})
	dec, _ := svc.Execute(context.Background(), EventPreToolUse, "patch_file", nil)
	if dec.Action != ActionConfirm {
		t.Fatalf("expected confirm, got %#v", dec)
	}
}

func TestPromptHookFailsClosedWithoutDecider(t *testing.T) {
	hook := Hook{
		Name: "prompt", Event: EventPreToolUse, Pattern: "*", Type: HookTypePrompt,
		PromptConfig:  &PromptHookConfig{Prompt: "x"},
		FailurePolicy: FailurePolicyBlock,
		Enabled:       true,
	}
	svc := New([]Hook{hook})
	dec, _ := svc.Execute(context.Background(), EventPreToolUse, "rm", nil)
	if dec.Action != ActionBlock {
		t.Fatalf("expected block, got %#v", dec)
	}
}

// Make sure HTTPDoer is satisfied by *http.Client.
var _ HTTPDoer = (*http.Client)(nil)

// And by net/errors stub for completeness.
var _ HTTPDoer = stubHTTPClient{}

type stubHTTPClient struct{}

func (stubHTTPClient) Do(*http.Request) (*http.Response, error) {
	return nil, errors.New("not used")
}
