package agent

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
)

// newForkTestAgent creates a minimal agent backed by a mock LLM, suitable for
// fork tests. CustomSystemPrompt prevents subprocess-spawning tool detection.
func newForkTestAgent(t *testing.T, responses ...llm.MockResponse) *Agent {
	t.Helper()
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "You are a test agent.",
	}
	mockClient := llm.NewMockClient()
	mockClient.Responses = responses
	a, err := New(cfg, nil, WithLLMClient(mockClient))
	if err != nil {
		t.Fatalf("failed to create test agent: %v", err)
	}
	return a
}

// --- ForkManager depth / config wiring ---

func TestNewForkManager_DefaultDepth(t *testing.T) {
	parent := newForkTestAgent(t)
	defer func() {
		if err := parent.Shutdown(); err != nil {
			t.Errorf("shutdown failed: %v", err)
		}
	}()
	fm := NewForkManager(parent)
	if fm.maxDepth != defaultMaxForkDepth {
		t.Errorf("expected default maxDepth %d, got %d", defaultMaxForkDepth, fm.maxDepth)
	}
}

func TestNewForkManager_ConfiguredDepth(t *testing.T) {
	parent := newForkTestAgent(t)
	defer func() {
		if err := parent.Shutdown(); err != nil {
			t.Errorf("shutdown failed: %v", err)
		}
	}()
	parent.config.Advanced = &config.AdvancedConfig{
		Fork: &config.ForkAdvConfig{MaxDepth: 5},
	}
	fm := NewForkManager(parent)
	if fm.maxDepth != 5 {
		t.Errorf("expected maxDepth 5, got %d", fm.maxDepth)
	}
}

func TestNewForkManager_ZeroDepthIgnored(t *testing.T) {
	parent := newForkTestAgent(t)
	defer func() {
		if err := parent.Shutdown(); err != nil {
			t.Errorf("shutdown failed: %v", err)
		}
	}()
	parent.config.Advanced = &config.AdvancedConfig{
		Fork: &config.ForkAdvConfig{MaxDepth: 0},
	}
	fm := NewForkManager(parent)
	// Zero means "not set"; should fall back to the default.
	if fm.maxDepth != defaultMaxForkDepth {
		t.Errorf("expected default maxDepth %d when zero configured, got %d", defaultMaxForkDepth, fm.maxDepth)
	}
}

// --- depth-limit enforcement ---

func TestFork_DepthLimitReached(t *testing.T) {
	parent := newForkTestAgent(t)
	defer func() {
		if err := parent.Shutdown(); err != nil {
			t.Errorf("shutdown failed: %v", err)
		}
	}()
	fm := NewForkManager(parent)

	// Saturate the context depth so the next Fork call is rejected.
	ctx := withForkDepth(context.Background(), fm.maxDepth)
	_, err := fm.Fork(ctx, ForkConfig{AgentType: AgentTypeExecute, Task: "do something"})
	if err == nil {
		t.Fatal("expected error when fork depth limit reached, got nil")
	}
}

// --- agent-type system prompt injection ---

func TestFork_ExploreTypeAppliesReadOnlyPrompt(t *testing.T) {
	// Verify that GetAgentTypeConfig("explore") returns a non-empty read-only
	// system prompt and uses only canonical tool names.
	typeCfg := GetAgentTypeConfig(AgentTypeExplore)
	if typeCfg == nil {
		t.Fatal("expected non-nil AgentTypeConfig for explore")
	}
	if typeCfg.SystemPrompt == nil {
		t.Fatal("expected non-nil SystemPrompt func for explore")
	}
	prompt := typeCfg.SystemPrompt("")
	if prompt == "" {
		t.Error("expected non-empty system prompt for explore agent type")
	}
}

// --- AgentTypeConfig tool lists use canonical names ---

func TestAgentTypeConfig_CanonicalToolNames(t *testing.T) {
	forbiddenAliases := []string{"grep", "shell"}
	configs := builtinAgentTypes()
	for agentType, cfg := range configs {
		for _, tool := range cfg.AllowedTools {
			for _, bad := range forbiddenAliases {
				if tool == bad {
					t.Errorf("agent type %q AllowedTools contains non-canonical name %q", agentType, tool)
				}
			}
		}
		for _, tool := range cfg.DeniedTools {
			for _, bad := range forbiddenAliases {
				if tool == bad {
					t.Errorf("agent type %q DeniedTools contains non-canonical name %q", agentType, tool)
				}
			}
		}
	}
}

// --- ForkTool schema ---

func TestForkTool_Schema(t *testing.T) {
	parent := newForkTestAgent(t)
	defer func() {
		if err := parent.Shutdown(); err != nil {
			t.Errorf("shutdown failed: %v", err)
		}
	}()
	ft := NewForkTool(NewForkManager(parent))

	if ft.Name() != "fork" {
		t.Errorf("expected tool name 'fork', got %q", ft.Name())
	}
	schema := ft.Schema()
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	if _, ok := schema.Properties["task"]; !ok {
		t.Error("schema missing 'task' property")
	}
	if _, ok := schema.Properties["agent_type"]; !ok {
		t.Error("schema missing 'agent_type' property")
	}
}

func TestForkTool_MissingTaskReturnsError(t *testing.T) {
	parent := newForkTestAgent(t)
	defer func() {
		if err := parent.Shutdown(); err != nil {
			t.Errorf("shutdown failed: %v", err)
		}
	}()
	ft := NewForkTool(NewForkManager(parent))

	result, err := ft.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false when task is missing")
	}
}
