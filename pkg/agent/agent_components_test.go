package agent

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
)

// TestAgentComponentInterfaces verifies that Agent satisfies the decomposed
// component interfaces (ToolRunner, HookRunner, MemoryStore), allowing tests
// to construct minimal mocks targeting individual subsystems.
func TestAgentComponentInterfaces(t *testing.T) {
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "You are a test agent.",
	}

	mockClient := llm.NewMockClient()
	agent, err := New(cfg, nil, WithLLMClient(mockClient))
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// Test ToolRunner interface
	var tr ToolRunner = agent
	if tr.GetToolbox() == nil {
		t.Error("ToolRunner.GetToolbox() returned nil")
	}
	if tr.GetToolScheduler() == nil {
		t.Error("ToolRunner.GetToolScheduler() returned nil")
	}

	// Test HookRunner interface
	var hr HookRunner = agent
	// hookEngine may be nil if hooks not configured; just verify the interface compiles
	_ = hr.GetHookEngine()

	// Test MemoryStore interface
	var ms MemoryStore = agent
	// memoryManager is set during init
	_ = ms.GetMemoryManager()
}

// TestAgentWithNilMemoryManager verifies that an Agent can be constructed and
// used when the MemoryStore returns nil (empty memory). This satisfies the P2-2
// acceptance criterion: "单测可用空 Memory 构造 Agent".
func TestAgentWithNilMemoryManager(t *testing.T) {
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "You are a test agent.",
	}

	mockClient := llm.NewMockClient()
	agent, err := New(cfg, nil, WithLLMClient(mockClient))
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// Explicitly set memory manager to nil to simulate "empty memory"
	agent.memoryManager = nil

	var ms MemoryStore = agent
	if ms.GetMemoryManager() != nil {
		t.Error("Expected nil MemoryManager for empty-memory agent")
	}

	// Agent should still function without memory manager
	if agent.GetToolbox() == nil {
		t.Error("Agent should have toolbox even without memory")
	}
	if agent.GetLLMClient() == nil {
		t.Error("Agent should have LLM client even without memory")
	}
}

// TestTurnSubStructs verifies that the Turn sub-struct types can be
// independently constructed and their fields are accessible.
func TestTurnSubStructs(t *testing.T) {
	// TurnRequest
	req := TurnRequest{
		ID:        "turn-1",
		SessionID: "sess-1",
		UserInput: "hello",
	}
	if req.ID != "turn-1" {
		t.Error("TurnRequest.ID mismatch")
	}

	// TurnDecisions
	dec := TurnDecisions{
		TerminationCause: "task_done",
	}
	if dec.TerminationCause != "task_done" {
		t.Error("TurnDecisions.TerminationCause mismatch")
	}

	// TurnTelemetry
	tel := TurnTelemetry{
		HasError: true,
		ErrorMsg: "test error",
	}
	if !tel.HasError {
		t.Error("TurnTelemetry.HasError should be true")
	}

	// TurnToolBatch
	batch := TurnToolBatch{
		Actions: []string{"read_file", "write_file"},
	}
	if len(batch.Actions) != 2 {
		t.Error("TurnToolBatch.Actions length mismatch")
	}
}
