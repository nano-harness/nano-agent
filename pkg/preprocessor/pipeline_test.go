package preprocessor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/mailbox"
)

func TestPipelineRunsStepsInOrder(t *testing.T) {
	var trace []string
	p := NewPipeline(
		StepFunc{StepName: "a", Fn: func(_ context.Context, r *Request) error {
			trace = append(trace, "a")
			r.UserInput += "+a"
			return nil
		}},
		StepFunc{StepName: "b", Fn: func(_ context.Context, r *Request) error {
			trace = append(trace, "b")
			r.UserInput += "+b"
			return nil
		}},
	)

	req := &Request{UserInput: "hi"}
	if err := p.Run(context.Background(), req); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := req.UserInput, "hi+a+b"; got != want {
		t.Fatalf("UserInput = %q, want %q", got, want)
	}
	if len(trace) != 2 || trace[0] != "a" || trace[1] != "b" {
		t.Fatalf("trace = %v, want [a b]", trace)
	}
}

func TestPipelineShortCircuitsOnError(t *testing.T) {
	called := false
	boom := errors.New("boom")
	p := NewPipeline(
		StepFunc{StepName: "fail", Fn: func(_ context.Context, _ *Request) error {
			return boom
		}},
		StepFunc{StepName: "after", Fn: func(_ context.Context, _ *Request) error {
			called = true
			return nil
		}},
	)
	err := p.Run(context.Background(), &Request{})
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom error, got %v", err)
	}
	if called {
		t.Fatalf("subsequent step ran after error")
	}
}

func TestPipelineRejectsNilRequest(t *testing.T) {
	if err := NewPipeline().Run(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestRoutinesStepRewritesInput(t *testing.T) {
	req := &Request{UserInput: "/routines list"}
	if err := RoutinesStep().Apply(context.Background(), req); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if req.UserInput == "/routines list" {
		t.Fatalf("expected user input to be rewritten, got %q", req.UserInput)
	}
	if req.Metadata["routines.rewritten"] != "true" {
		t.Fatalf("expected routines.rewritten metadata to be set")
	}
}

func TestRoutinesStepLeavesUnrelatedInput(t *testing.T) {
	req := &Request{UserInput: "hello world"}
	if err := RoutinesStep().Apply(context.Background(), req); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if req.UserInput != "hello world" {
		t.Fatalf("user input mutated unexpectedly: %q", req.UserInput)
	}
	if _, ok := req.Metadata["routines.rewritten"]; ok {
		t.Fatalf("routines.rewritten metadata should not be set")
	}
}

func TestOpenSpecStepDisabled(t *testing.T) {
	called := false
	step := OpenSpecStep(func() OpenSpecOptions {
		called = true
		return OpenSpecOptions{Enabled: false}
	})
	req := &Request{UserInput: "/opsx:status"}
	if err := step.Apply(context.Background(), req); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if !called {
		t.Fatalf("opts function should be called even when disabled")
	}
	if req.UserInput != "/opsx:status" {
		t.Fatalf("disabled step should not mutate input, got %q", req.UserInput)
	}
}

func TestPipelineAppend(t *testing.T) {
	p := NewPipeline()
	p.Append(RoutinesStep()).Append(nil)
	if got := len(p.Steps()); got != 1 {
		t.Fatalf("Append(nil) should be ignored, got %d steps", got)
	}
}

func TestMailboxStepAppendsAndDrainsMessages(t *testing.T) {
	backend := mailbox.NewMemoryBackend(mailbox.DefaultOptions())
	mb, err := backend.Open("agent-1")
	if err != nil {
		t.Fatalf("Open mailbox: %v", err)
	}
	defer func() { _ = backend.Close() }()

	if err := mb.Send(context.Background(), mailbox.Message{
		ID:        "msg-1",
		From:      "teammate",
		To:        "agent-1",
		Topic:     mailbox.TopicProgress,
		Body:      map[string]interface{}{"content": "done"},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("Send mailbox message: %v", err)
	}

	req := &Request{UserInput: "original", Mailbox: mb}
	if err := MailboxStep().Apply(context.Background(), req); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if req.UserInput == "original" {
		t.Fatalf("expected mailbox attachment to be appended")
	}
	if req.Metadata["mailbox.drained"] != "true" {
		t.Fatalf("expected mailbox.drained metadata to be set")
	}
	count, err := mb.Count(context.Background())
	if err != nil {
		t.Fatalf("Count mailbox: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected mailbox to be drained, got %d messages", count)
	}
}

func TestMailboxStepNoopsWithoutMailbox(t *testing.T) {
	req := &Request{UserInput: "original"}
	if err := MailboxStep().Apply(context.Background(), req); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if req.UserInput != "original" {
		t.Fatalf("expected input to remain unchanged, got %q", req.UserInput)
	}
}
