package agent

import (
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

func mcpTestTool(name, description string) interfaces.Tool {
	return promptTestTool{name: name, description: description, category: interfaces.CategoryMCP}
}

func TestEstimateToolDefinitionTokens(t *testing.T) {
	tests := []struct {
		name     string
		tool     interfaces.Tool
		wantZero bool
	}{
		{name: "nil tool", tool: nil, wantZero: true},
		{name: "realistic tool", tool: mcpTestTool("mcp_fs_read", "read a file from the MCP filesystem server")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateToolDefinitionTokens(tt.tool)
			if tt.wantZero && got != 0 {
				t.Errorf("tokens = %d, want 0", got)
			}
			if !tt.wantZero && got <= toolDefinitionOverhead {
				t.Errorf("tokens = %d, want > overhead %d", got, toolDefinitionOverhead)
			}
		})
	}
}

func TestEvaluateToolSearch(t *testing.T) {
	smallInventory := []interfaces.Tool{
		mcpTestTool("mcp_fs_read", "read a file"),
		mcpTestTool("mcp_fs_write", "write a file"),
		promptTestTool{name: "read_file", description: "built-in file reader", category: interfaces.CategoryFileSystem},
	}

	// Build an inventory whose aggregate definition size clearly exceeds any
	// small threshold.
	largeInventory := make([]interfaces.Tool, 0, 150)
	for i := 0; i < 150; i++ {
		largeInventory = append(largeInventory, mcpTestTool(
			"mcp_bigserver_tool_with_a_rather_long_name_"+strings.Repeat("x", 10),
			"A very verbose tool description that explains in great detail what this tool does, "+
				"including usage notes, caveats, and examples "+strings.Repeat("y", 200)))
	}

	tests := []struct {
		name          string
		tools         []interfaces.Tool
		contextWindow int
		ratio         float64
		wantLazy      bool
		wantCount     int
	}{
		{
			name:          "small inventory under threshold stays eager",
			tools:         smallInventory,
			contextWindow: 200000,
			ratio:         0.10,
			wantLazy:      false,
			wantCount:     2,
		},
		{
			name:          "large inventory over threshold goes lazy",
			tools:         largeInventory,
			contextWindow: 200000,
			ratio:         0.10,
			wantLazy:      true,
			wantCount:     150,
		},
		{
			name:          "no MCP tools never lazy",
			tools:         []interfaces.Tool{smallInventory[2]},
			contextWindow: 200000,
			ratio:         0.10,
			wantLazy:      false,
			wantCount:     0,
		},
		{
			name:          "tiny context window forces lazy",
			tools:         smallInventory,
			contextWindow: 1000,
			ratio:         0.10,
			wantLazy:      true,
			wantCount:     2,
		},
		{
			name:          "zero context window uses default",
			tools:         smallInventory,
			contextWindow: 0,
			ratio:         0.10,
			wantLazy:      false,
			wantCount:     2,
		},
		{
			name:          "invalid ratio uses default",
			tools:         smallInventory,
			contextWindow: 200000,
			ratio:         5.0,
			wantLazy:      false,
			wantCount:     2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := EvaluateToolSearch(tt.tools, tt.contextWindow, tt.ratio)
			if decision.Lazy != tt.wantLazy {
				t.Errorf("Lazy = %t, want %t (tokens=%d threshold=%d)",
					decision.Lazy, tt.wantLazy, decision.MCPToolTokens, decision.ThresholdTokens)
			}
			if decision.MCPToolCount != tt.wantCount {
				t.Errorf("MCPToolCount = %d, want %d", decision.MCPToolCount, tt.wantCount)
			}
			if tt.contextWindow > 0 && decision.ContextWindow != tt.contextWindow {
				t.Errorf("ContextWindow = %d, want %d", decision.ContextWindow, tt.contextWindow)
			}
		})
	}
}

func TestToolSearchConfigResolved(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }
	tests := []struct {
		name        string
		cfg         *config.Config
		wantEnabled bool
		wantWindow  int
		wantRatio   float64
	}{
		{
			name:        "nil config uses defaults",
			cfg:         nil,
			wantEnabled: true,
			wantWindow:  defaultToolSearchContextWindow,
			wantRatio:   defaultToolSearchThresholdRatio,
		},
		{
			name:        "empty config uses defaults",
			cfg:         &config.Config{},
			wantEnabled: true,
			wantWindow:  defaultToolSearchContextWindow,
			wantRatio:   defaultToolSearchThresholdRatio,
		},
		{
			name: "explicit overrides win",
			cfg: &config.Config{
				ToolSearch: &config.ToolSearchConfig{
					Enabled:        boolPtr(false),
					ThresholdRatio: 0.25,
					ContextWindow:  128000,
				},
			},
			wantEnabled: false,
			wantWindow:  128000,
			wantRatio:   0.25,
		},
		{
			name: "model context window fallback",
			cfg: &config.Config{
				ContextConfig: config.ContextConfig{ModelContextWindow: 64000},
			},
			wantEnabled: true,
			wantWindow:  64000,
			wantRatio:   defaultToolSearchThresholdRatio,
		},
		{
			name: "tool_search window beats model context window",
			cfg: &config.Config{
				ContextConfig: config.ContextConfig{ModelContextWindow: 64000},
				ToolSearch:    &config.ToolSearchConfig{ContextWindow: 32000},
			},
			wantEnabled: true,
			wantWindow:  32000,
			wantRatio:   defaultToolSearchThresholdRatio,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled, window, ratio := toolSearchConfigResolved(tt.cfg)
			if enabled != tt.wantEnabled {
				t.Errorf("enabled = %t, want %t", enabled, tt.wantEnabled)
			}
			if window != tt.wantWindow {
				t.Errorf("window = %d, want %d", window, tt.wantWindow)
			}
			if ratio != tt.wantRatio {
				t.Errorf("ratio = %v, want %v", ratio, tt.wantRatio)
			}
		})
	}
}

func TestToolSearchGateShouldExpose(t *testing.T) {
	inventory := []interfaces.Tool{
		mcpTestTool("mcp_fs_read", "read a file"),
		promptTestTool{name: "custom_tool", description: "non-core built-in", category: interfaces.CategoryWeb},
	}

	tests := []struct {
		name     string
		lazy     bool
		expanded []string // tools marked expanded in ProgressiveDisclosure
		toolName string
		want     bool
	}{
		{name: "lazy mode hides MCP tool", lazy: true, toolName: "mcp_fs_read", want: false},
		{name: "eager mode exposes MCP tool", lazy: false, toolName: "mcp_fs_read", want: true},
		{name: "lazy mode exposes expanded MCP tool", lazy: true, expanded: []string{"mcp_fs_read"}, toolName: "mcp_fs_read", want: true},
		{name: "eager mode exposes expanded MCP tool", lazy: false, expanded: []string{"mcp_fs_read"}, toolName: "mcp_fs_read", want: true},
		{name: "non-MCP tool follows progressive disclosure", lazy: false, toolName: "custom_tool", want: false},
		{name: "non-MCP expanded tool exposed", lazy: false, expanded: []string{"custom_tool"}, toolName: "custom_tool", want: true},
		{name: "unknown mcp_ prefix treated as MCP", lazy: false, toolName: "mcp_unknown_thing", want: true},
		{name: "unknown mcp_ prefix hidden when lazy", lazy: true, toolName: "mcp_unknown_thing", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pd := NewProgressiveDisclosure(20, 5)
			pd.IndexTools(inventory)
			for _, name := range tt.expanded {
				pd.MarkExpanded(name)
			}
			gate := NewToolSearchGate(pd)
			gate.Update(inventory, ToolSearchDecision{Lazy: tt.lazy})
			if got := gate.ShouldExpose(tt.toolName); got != tt.want {
				t.Errorf("ShouldExpose(%q) = %t, want %t", tt.toolName, got, tt.want)
			}
			if gate.Lazy() != tt.lazy {
				t.Errorf("Lazy() = %t, want %t", gate.Lazy(), tt.lazy)
			}
		})
	}
}

func TestToolSearchGateNilSafety(t *testing.T) {
	var gate *ToolSearchGate
	if gate.ShouldExpose("mcp_fs_read") {
		t.Errorf("nil gate must not expose tools")
	}
	if gate.Lazy() {
		t.Errorf("nil gate must not report lazy")
	}
}

func TestSystemPromptMCPLazyLoadToggle(t *testing.T) {
	tools := []interfaces.Tool{
		mcpTestTool("mcp_demo_lookup", "lookup demo"),
	}
	spb := NewSystemPromptBuilder(t.TempDir(), tools, nil, &config.Config{IsSubAgent: true})

	// Default (lazy): full schema hidden.
	section := spb.buildToolsSection()
	if strings.Contains(section, "secret_param") {
		t.Fatalf("lazy mode must not render MCP full schema: %q", section)
	}

	// Eager: full schema rendered inline.
	spb.SetMCPLazyLoad(false)
	section = spb.buildToolsSection()
	if !strings.Contains(section, "secret_param") {
		t.Fatalf("eager mode must render MCP full schema: %q", section)
	}
}
