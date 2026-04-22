package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
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
// CustomSystemPrompt is set to avoid subprocess-spawning tool detection.
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

func TestMainAgentTool(t *testing.T) {
	cfg := newTestConfig()

	// Create a mock approval handler
	approvalHandler := func(toolCall *agent.ToolCallInfo) bool {
		return true // Auto-approve all tool calls in tests
	}

	// Create a mock agent (in real scenario, this would be a proper agent)
	mockAgent, err := agent.New(cfg, approvalHandler)
	require.NoError(t, err)

	// Create MainAgentTool
	tool := NewMainAgentTool(cfg, mockAgent)

	// Test tool properties
	assert.Equal(t, "main_agent", tool.Name())
	assert.Equal(t, "Execute tasks using the main agent with full capabilities", tool.Description())
	assert.Equal(t, interfaces.CategoryBuild, tool.Category())
	assert.False(t, tool.RequiresConfirmation())

	// Test schema
	schema := tool.Schema()
	assert.NotNil(t, schema)
	assert.Contains(t, schema.Properties, "task")
	assert.Contains(t, schema.Properties, "context")
	assert.Contains(t, schema.Properties, "stream")
	assert.Contains(t, schema.Required, "task")

	// Test execution with missing task parameter
	ctx := context.Background()
	result, err := tool.Execute(ctx, map[string]interface{}{})
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "task parameter is required")

	// Test execution with nil agent returns an error result (no real LLM call)
	nilTool := NewMainAgentTool(cfg, nil)
	params := map[string]interface{}{
		"task": "Test task execution",
	}
	result, err = nilTool.Execute(ctx, params)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "main agent not initialized")
}

func TestAgentToolsRegistration(t *testing.T) {
	cfg := newTestConfig()

	// Create a mock registry
	registry := &MockToolRegistry{
		tools: make(map[string]interfaces.Tool),
	}

	// Create a mock approval handler
	approvalHandler := func(toolCall *agent.ToolCallInfo) bool {
		return true // Auto-approve all tool calls in tests
	}

	// Create a mock agent
	mockAgent, err := agent.New(cfg, approvalHandler)
	require.NoError(t, err)

	// Test RegisterAgentTools
	RegisterAgentTools(registry, cfg, mockAgent)

	// Verify main_agent tool is registered by RegisterAgentTools
	mainTool, exists := registry.Get("main_agent")
	assert.True(t, exists)
	assert.NotNil(t, mainTool)

	// Test GetAgentToolNames
	toolNames := GetAgentToolNames()
	assert.Contains(t, toolNames, "main_agent")
	assert.Contains(t, toolNames, "task")
	assert.Len(t, toolNames, 2)
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
