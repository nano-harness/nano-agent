package system

import (
	"context"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

func TestShellToolExecuteCommandIncludesSandboxMetadata(t *testing.T) {
	tempDir := t.TempDir()
	tool := NewShellTool(tempDir, nil, &config.SandboxConfig{Enabled: false})

	result, err := tool.executeCommand(context.Background(), "echo hello", tempDir, 5*time.Second, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("command failed: %#v", result)
	}

	sandboxMeta, ok := result.Metadata["sandbox"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing sandbox metadata: %#v", result.Metadata)
	}
	if sandboxMeta["backend"] != "none" {
		t.Fatalf("sandbox backend = %#v, want none", sandboxMeta["backend"])
	}
	if sandboxMeta["enabled"] != false {
		t.Fatalf("sandbox enabled = %#v, want false", sandboxMeta["enabled"])
	}
	if sandboxMeta["network"] != "inherited" {
		t.Fatalf("sandbox network = %#v, want inherited", sandboxMeta["network"])
	}
}

func TestShellToolPublishesSandboxCommandEvents(t *testing.T) {
	tempDir := t.TempDir()
	tool := NewShellTool(tempDir, nil, &config.SandboxConfig{Enabled: false})
	pub := &recordingSandboxPublisher{}
	ctx := sandbox.WithEventPublisher(context.Background(), pub)

	result, err := tool.executeCommand(ctx, "echo hello", tempDir, 5*time.Second, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("command failed: %#v", result)
	}

	seen := map[string]sandbox.Event{}
	for _, ev := range pub.events {
		seen[ev.Type] = ev
	}
	for _, want := range []string{
		sandbox.EventTypeSandboxDecisionCreated,
		sandbox.EventTypeSandboxEnvironmentCreated,
		sandbox.EventTypeSandboxCommandStarted,
		sandbox.EventTypeSandboxCommandFinished,
		sandbox.EventTypeSandboxEnvironmentCleaned,
	} {
		if _, ok := seen[want]; !ok {
			t.Fatalf("missing sandbox event %s from %#v", want, pub.events)
		}
	}
	finished := seen[sandbox.EventTypeSandboxCommandFinished]
	if finished.Metadata["exit_code"] != 0 {
		t.Fatalf("finished exit_code = %#v, want 0", finished.Metadata["exit_code"])
	}
	if finished.Metadata["sandbox"] == nil {
		t.Fatalf("finished event missing sandbox metadata: %#v", finished)
	}
}

type recordingSandboxPublisher struct {
	events []sandbox.Event
}

func (p *recordingSandboxPublisher) PublishSandboxEvent(ev sandbox.Event) {
	p.events = append(p.events, ev)
}
