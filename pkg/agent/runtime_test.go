package agent

import (
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
)

func TestAgentRuntimeUsesConfiguredDependencies(t *testing.T) {
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "You are a test agent.",
	}
	mockClient := llm.NewMockClient()

	ag, err := New(cfg, nil, WithLLMClient(mockClient))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	defer func() { _ = ag.Shutdown() }()

	rt := ag.Runtime()
	if rt == nil {
		t.Fatal("expected runtime")
	}
	if rt.LLM != mockClient {
		t.Fatal("expected runtime to use configured LLM client")
	}
	if rt.Toolbox != ag.GetToolbox() {
		t.Fatal("expected runtime to expose agent toolbox")
	}
	if rt.Sessions != ag.GetSessionManager() {
		t.Fatal("expected runtime to expose agent session manager")
	}
}

func TestAgentNewRejectsNilConfig(t *testing.T) {
	_, err := New(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	if !strings.Contains(err.Error(), "config cannot be nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAgentNewInitializesBootstrapDependencies(t *testing.T) {
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		WorkingDir:         t.TempDir(),
		CustomSystemPrompt: "You are a test agent.",
		PermissionMode:     string(permission.ModeYOLO),
	}
	mockClient := llm.NewMockClient()

	ag, err := New(cfg, nil, WithLLMClient(mockClient))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	defer func() { _ = ag.Shutdown() }()

	if ag.GetToolbox() == nil {
		t.Fatal("expected toolbox to be initialized")
	}
	if ag.GetToolScheduler() == nil {
		t.Fatal("expected tool scheduler to be initialized")
	}
	if ag.GetMemoryManager() == nil {
		t.Fatal("expected memory manager to be initialized")
	}
	if ag.GetSessionManager() == nil {
		t.Fatal("expected session manager to be initialized")
	}
	if ag.GetPermissionManager() == nil {
		t.Fatal("expected permission manager to be initialized")
	}
	if got := ag.GetPermissionManager().GetMode(); got != permission.ModeYOLO {
		t.Fatalf("permission mode = %q, want %q", got, permission.ModeYOLO)
	}
	if ag.GetLLMClient() != mockClient {
		t.Fatal("expected WithLLMClient to override bootstrapped client")
	}
	if ag.Runtime() == nil || ag.Runtime().LLM != mockClient {
		t.Fatal("expected runtime to be assembled after options are applied")
	}
}
