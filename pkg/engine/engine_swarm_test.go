package engine

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/swarm"
)

func testSwarmConfig() *config.Config {
	return &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "test prompt",
	}
}

func TestNewLeadEngineInjectsMailbox(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	eng, err := NewLeadEngine(testSwarmConfig(), "alpha")
	if err != nil {
		t.Fatalf("NewLeadEngine() error = %v", err)
	}
	defer eng.Shutdown()

	if eng.Agent.Mailbox() == nil {
		t.Fatal("lead mailbox was not injected")
	}
}

func TestNewTeammateEngineInjectsMailboxesAndIdleHook(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	identity := &swarm.TeammateIdentity{
		AgentID:         "coder@alpha",
		AgentName:       "coder",
		TeamName:        "alpha",
		ParentSessionID: "lead-alpha-chat-123",
	}
	eng, err := NewTeammateEngine(testSwarmConfig(), identity)
	if err != nil {
		t.Fatalf("NewTeammateEngine() error = %v", err)
	}
	defer eng.Shutdown()

	if eng.Agent.Mailbox() == nil {
		t.Fatal("teammate mailbox was not injected")
	}
	if eng.Agent.ParentMailbox() == nil {
		t.Fatal("parent mailbox was not injected")
	}
	if len(eng.Agent.StopHooks()) == 0 {
		t.Fatal("idle stop hook was not registered")
	}

	for _, hook := range eng.Agent.StopHooks() {
		hook(context.Background(), "test")
	}
	msgs, err := eng.Agent.ParentMailbox().Peek(context.Background(), 10)
	if err != nil {
		t.Fatalf("peek parent mailbox: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Topic != "idle_notification" {
		t.Fatalf("idle notification not delivered to lead mailbox: %#v", msgs)
	}
}
