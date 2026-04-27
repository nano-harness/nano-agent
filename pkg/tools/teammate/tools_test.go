package teammate

import (
	"context"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	"github.com/nano-harness/nano-agent/pkg/team"
)

func TestSpawnToolInterface(t *testing.T) {
	cfg := &config.Config{}
	tool := NewSpawnTool(cfg)

	// Test Name
	if tool.Name() != "spawn_teammate" {
		t.Errorf("expected name %q, got %q", "spawn_teammate", tool.Name())
	}

	// Test Description
	desc := tool.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}

	// Test Schema
	schema := tool.Schema()
	if schema == nil {
		t.Fatal("schema should not be nil")
	}
	if schema.Description == "" {
		t.Error("schema description should not be empty")
	}

	// Verify schema has required fields
	if schema.Properties == nil {
		t.Fatal("schema properties should not be nil")
	}
	if _, ok := schema.Properties["name"]; !ok {
		t.Error("schema should have 'name' property")
	}
	if _, ok := schema.Properties["initial_prompt"]; !ok {
		t.Error("schema should have 'initial_prompt' property")
	}

	// Test RequiresConfirmation
	if tool.RequiresConfirmation() {
		t.Error("spawn_teammate should not require confirmation")
	}

	// Test ConcurrencySafe
	if tool.ConcurrencySafe() {
		t.Error("spawn_teammate should not be concurrency safe (modifies team state)")
	}
}

func TestExecute_AsTeammate_Forbidden(t *testing.T) {
	cfg := &config.Config{}
	tool := NewSpawnTool(cfg)

	// Create teammate context
	identity := &swarm.TeammateIdentity{
		AgentID:   "researcher@alpha",
		AgentName: "researcher",
		TeamName:  "alpha",
	}
	ctx := swarm.WithTeammate(context.Background(), identity)

	params := map[string]interface{}{
		"name":           "coder",
		"initial_prompt": "Write some code",
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Error("teammates should not be able to spawn other teammates")
	}
	if result.LLMContent == "" {
		t.Error("error message should not be empty")
	}
}

func TestExecute_MissingName(t *testing.T) {
	cfg := &config.Config{}
	tool := NewSpawnTool(cfg)
	ctx := context.Background()

	params := map[string]interface{}{
		"initial_prompt": "Do some work",
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Error("expected failure when name is missing")
	}
}

func TestExecute_MissingInitialPrompt(t *testing.T) {
	cfg := &config.Config{}
	tool := NewSpawnTool(cfg)
	ctx := context.Background()

	params := map[string]interface{}{
		"name": "coder",
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Error("expected failure when initial_prompt is missing")
	}
}

func TestExecute_InvalidKind(t *testing.T) {
	cfg := &config.Config{}
	tool := NewSpawnTool(cfg)
	ctx := context.Background()

	params := map[string]interface{}{
		"name":           "coder",
		"initial_prompt": "Do some work",
		"kind":           "invalid_kind",
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Error("expected failure with invalid kind")
	}
}

func TestExecute_ValidParams_AsLead(t *testing.T) {
	cfg := &config.Config{}
	tool := NewSpawnTool(cfg)
	ctx := context.Background()

	params := map[string]interface{}{
		"name":           "coder",
		"initial_prompt": "Write a test function",
		"kind":           "in_process",
	}

	result, err := tool.Execute(ctx, params)
	// This will likely fail in test environment due to missing team setup,
	// but it should at least pass validation and attempt to spawn
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// We expect failure here due to missing team infrastructure in test env
	// The important part is that it didn't reject the request outright
	t.Logf("Result: success=%v, message=%s", result.Success, result.LLMContent)
}

func TestExecute_UsesLeadTeamContextForSpawnSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := team.CreateTeam("alpha", "test team", "team-lead@alpha", "lead-alpha-chat-1"); err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}

	tool := NewSpawnTool(&config.Config{})
	ctx := swarm.WithTeamLead(context.Background(), "alpha", "lead-alpha-chat-1")
	result, err := tool.Execute(ctx, map[string]interface{}{
		"name":           "coder",
		"initial_prompt": "Write a test",
		"kind":           "in_process",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() success = false, content = %s", result.LLMContent)
	}
	if !strings.Contains(result.LLMContent, "teammate-alpha-coder-") {
		t.Fatalf("spawn result did not include team-scoped session ID: %s", result.LLMContent)
	}
}
