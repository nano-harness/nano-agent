package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agentprofile"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Initialize global configuration to prevent panic in tools that call config.Get()
	_, _ = config.LoadConfig("")
	os.Exit(m.Run())
}

// newTestConfig returns a minimal config suitable for unit tests.
func newTestConfig() *config.Config {
	return &config.Config{
		Model:              "gpt-4",
		APIKey:             "test-key",
		BaseURL:            "https://api.openai.com/v1",
		ResponseTimeout:    30 * time.Second,
		MaxFileSize:        1024 * 1024,
		CustomSystemPrompt: "You are a test agent.",
		Memory: &config.MemoryConfig{
			APIKey:    "",
			OrgID:     "",
			ProjectID: "",
			AgentID:   "",
			UserID:    "",
		},
		ImageGenerator: &config.ImageGeneratorConfig{
			Providers: []config.ImageGeneratorProviderConfig{},
		},
		OSS: &config.OSSConfig{
			Enabled: false,
		},
		Sandbox: &config.SandboxConfig{
			Enabled: false,
		},
		AllowedCommands: []string{},
		BlockedCommands: []string{},
		AllowedEnvVars:  []string{},
		BlockedEnvVars:  []string{},
		WebSearchAPIKeys: config.WebSearchAPIKeys{
			Serper: "",
			Tavily: "",
		},
		EnableMCP: false,
		MCP:       &config.MCPConfig{},
	}
}

func TestAgentToolSchema(t *testing.T) {
	cfg := newTestConfig()
	resolver := agentprofile.NewResolver("")
	tool := NewAgentTool(cfg, resolver)

	// Test tool properties
	assert.Equal(t, "Agent", tool.Name())
	assert.Equal(t, interfaces.CategoryAgent, tool.Category())
	assert.False(t, tool.RequiresConfirmation())
	assert.False(t, tool.ConcurrencySafe())

	// Test schema
	schema := tool.Schema()
	assert.NotNil(t, schema)
	assert.Contains(t, schema.Properties, "description")
	assert.Contains(t, schema.Properties, "prompt")
	assert.Contains(t, schema.Properties, "subagent_type")
	assert.Contains(t, schema.Properties, "model")
	assert.Contains(t, schema.Properties, "isolation")
	assert.Contains(t, schema.Properties, "run_in_background")
	assert.Contains(t, schema.Properties, "fork_from")
	assert.Contains(t, schema.Properties, "resume_from")
	assert.Contains(t, schema.Required, "description")
	assert.Contains(t, schema.Required, "prompt")
	assert.Contains(t, schema.Required, "subagent_type")
}

func TestAgentToolExecute_MissingPrompt(t *testing.T) {
	cfg := newTestConfig()
	resolver := agentprofile.NewResolver("")
	tool := NewAgentTool(cfg, resolver)

	ctx := context.Background()
	result, err := tool.Execute(ctx, map[string]interface{}{
		"description":   "test",
		"subagent_type": "explore",
	})
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "prompt")
}

func TestAgentToolExecute_UnknownType(t *testing.T) {
	cfg := newTestConfig()
	resolver := agentprofile.NewResolver("")
	tool := NewAgentTool(cfg, resolver)

	ctx := context.Background()
	result, err := tool.Execute(ctx, map[string]interface{}{
		"description":   "test",
		"prompt":        "do something",
		"subagent_type": "nonexistent-type",
	})
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "unknown subagent_type")
}

func TestAgentToolsRegistration(t *testing.T) {
	cfg := newTestConfig()

	// Create a mock registry
	registry := &MockToolRegistry{
		tools: make(map[string]interfaces.Tool),
	}

	// Test RegisterAgentTools
	RegisterAgentTools(registry, cfg)

	// Verify Agent tool is registered
	agentTool, exists := registry.Get("Agent")
	assert.True(t, exists)
	assert.NotNil(t, agentTool)

	// Test GetAgentToolNames
	toolNames := GetAgentToolNames()
	assert.Contains(t, toolNames, "Agent")
	assert.Contains(t, toolNames, "TaskOutput")
	assert.Contains(t, toolNames, "TaskStop")
	assert.Contains(t, toolNames, "send_message")
	assert.Len(t, toolNames, 4)
}

func TestToolFilter_DisallowedTools(t *testing.T) {
	profile := agentprofile.AgentProfile{
		Tools: []string{"*"},
	}
	parentTools := []string{"read_file", "write_file", "Agent", "ExitPlanMode", "run_shell_command"}
	result := ResolveAgentTools(profile, parentTools, false)

	// Agent and ExitPlanMode should be filtered
	assert.NotContains(t, result, "Agent")
	assert.NotContains(t, result, "ExitPlanMode")
	assert.Contains(t, result, "read_file")
	assert.Contains(t, result, "write_file")
	assert.Contains(t, result, "run_shell_command")
}

func TestToolFilter_AsyncTools(t *testing.T) {
	profile := agentprofile.AgentProfile{
		Tools: []string{"*"},
	}
	parentTools := []string{"read_file", "write_file"}

	// Sync agent should NOT get send_message
	syncResult := ResolveAgentTools(profile, parentTools, false)
	assert.NotContains(t, syncResult, "send_message")

	// Async agent SHOULD get send_message
	asyncResult := ResolveAgentTools(profile, parentTools, true)
	assert.Contains(t, asyncResult, "send_message")
}

func TestToolFilter_MCPPassthrough(t *testing.T) {
	profile := agentprofile.AgentProfile{
		Tools: []string{"read_file"},
	}
	parentTools := []string{"read_file", "mcp__server__tool1", "mcp__server__tool2"}
	result := ResolveAgentTools(profile, parentTools, false)

	// MCP tools should pass through
	assert.Contains(t, result, "mcp__server__tool1")
	assert.Contains(t, result, "mcp__server__tool2")
	assert.Contains(t, result, "read_file")
}

func TestToolFilter_ProfileWhitelist(t *testing.T) {
	profile := agentprofile.AgentProfile{
		Tools: []string{"read_file", "list_files"},
	}
	parentTools := []string{"read_file", "write_file", "list_files", "run_shell_command"}
	result := ResolveAgentTools(profile, parentTools, false)

	assert.Contains(t, result, "read_file")
	assert.Contains(t, result, "list_files")
	assert.NotContains(t, result, "write_file")
	assert.NotContains(t, result, "run_shell_command")
}

// MockToolRegistry is a simple mock implementation for testing
type MockToolRegistry struct {
	tools map[string]interfaces.Tool
}

func (r *MockToolRegistry) Register(tool interfaces.Tool) error {
	r.tools[tool.Name()] = tool
	return nil
}

func (r *MockToolRegistry) Unregister(name string) error {
	delete(r.tools, name)
	return nil
}

func (r *MockToolRegistry) Get(name string) (interfaces.Tool, bool) {
	tool, exists := r.tools[name]
	return tool, exists
}

func (r *MockToolRegistry) List() []interfaces.Tool {
	tools := make([]interfaces.Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

func (r *MockToolRegistry) ListByCategory(category interfaces.ToolCategory) []interfaces.Tool {
	var tools []interfaces.Tool
	for _, tool := range r.tools {
		if tool.Category() == category {
			tools = append(tools, tool)
		}
	}
	return tools
}

func (r *MockToolRegistry) Schemas() map[string]*interfaces.ToolSchema {
	schemas := make(map[string]*interfaces.ToolSchema)
	for name, tool := range r.tools {
		schemas[name] = tool.Schema()
	}
	return schemas
}

func (r *MockToolRegistry) Execute(ctx context.Context, name string, params map[string]interface{}) (*interfaces.ToolResult, error) {
	tool, exists := r.tools[name]
	if !exists {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "tool '" + name + "' not found",
		}, nil
	}
	return tool.Execute(ctx, params)
}
