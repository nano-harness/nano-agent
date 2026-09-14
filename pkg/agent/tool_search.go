package agent

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// Defaults for MCP tool lazy loading (tool search mode). The threshold ratio
// mirrors Claude Code's behavior of deferring tool definitions once they
// exceed roughly 10% of the context window; Anthropic's tool search report
// measured ~85% token reduction for large tool libraries under this pattern.
const (
	defaultToolSearchThresholdRatio = 0.10
	defaultToolSearchContextWindow  = 200000
	// charsPerToken approximates the token density of tool JSON definitions.
	charsPerToken = 4
	// toolDefinitionOverhead covers the JSON wrapper around each definition.
	toolDefinitionOverhead = 8
)

// ToolSearchDecision captures the outcome of a threshold evaluation.
type ToolSearchDecision struct {
	// Lazy is true when MCP tool definitions exceed the context threshold and
	// must be loaded on demand via the discover_tools meta-tool.
	Lazy            bool
	MCPToolCount    int
	MCPToolTokens   int
	ThresholdTokens int
	ContextWindow   int
}

// EstimateToolDefinitionTokens estimates how many tokens a tool's full
// definition (name + description + JSON schema) costs in the prompt.
func EstimateToolDefinitionTokens(t interfaces.Tool) int {
	if t == nil {
		return 0
	}
	chars := len(t.Name()) + len(t.Description())
	if schema := t.Schema(); schema != nil {
		if data, err := json.Marshal(schema); err == nil {
			chars += len(data)
		}
	}
	return chars/charsPerToken + toolDefinitionOverhead
}

// isMCPTool reports whether a tool originates from an MCP server.
func isMCPTool(t interfaces.Tool) bool {
	if t == nil {
		return false
	}
	return t.Category() == interfaces.CategoryMCP || strings.HasPrefix(t.Name(), "mcp_")
}

// EvaluateToolSearch decides whether MCP tools should be lazily loaded by
// comparing the aggregate token estimate of all MCP tool definitions against
// ratio * contextWindow.
func EvaluateToolSearch(tools []interfaces.Tool, contextWindow int, ratio float64) ToolSearchDecision {
	if contextWindow <= 0 {
		contextWindow = defaultToolSearchContextWindow
	}
	if ratio <= 0 || ratio >= 1 {
		ratio = defaultToolSearchThresholdRatio
	}
	total := 0
	count := 0
	for _, tool := range tools {
		if !isMCPTool(tool) {
			continue
		}
		count++
		total += EstimateToolDefinitionTokens(tool)
	}
	threshold := int(float64(contextWindow) * ratio)
	return ToolSearchDecision{
		Lazy:            count > 0 && total > threshold,
		MCPToolCount:    count,
		MCPToolTokens:   total,
		ThresholdTokens: threshold,
		ContextWindow:   contextWindow,
	}
}

// toolSearchConfigResolved flattens the user configuration into concrete
// parameters, applying defaults for unset values.
func toolSearchConfigResolved(cfg *config.Config) (enabled bool, contextWindow int, ratio float64) {
	enabled = true
	ratio = defaultToolSearchThresholdRatio
	if cfg != nil {
		contextWindow = cfg.ContextConfig.ModelContextWindow
		if ts := cfg.ToolSearch; ts != nil {
			if ts.Enabled != nil {
				enabled = *ts.Enabled
			}
			if ts.ThresholdRatio > 0 && ts.ThresholdRatio < 1 {
				ratio = ts.ThresholdRatio
			}
			if ts.ContextWindow > 0 {
				contextWindow = ts.ContextWindow
			}
		}
	}
	if contextWindow <= 0 {
		contextWindow = defaultToolSearchContextWindow
	}
	return enabled, contextWindow, ratio
}

// ToolSearchGate implements interfaces.ToolGate for MCP tools. When lazy
// loading is active, MCP tool schemas are withheld until the agent expands
// them via discover_tools (delegating to ProgressiveDisclosure); when
// inactive, MCP tools are exposed with full schemas like core tools.
type ToolSearchGate struct {
	pd   *ProgressiveDisclosure
	lazy atomic.Bool

	mu       sync.RWMutex
	mcpTools map[string]struct{}
}

// NewToolSearchGate creates a gate wrapping the given ProgressiveDisclosure.
func NewToolSearchGate(pd *ProgressiveDisclosure) *ToolSearchGate {
	g := &ToolSearchGate{
		pd:       pd,
		mcpTools: make(map[string]struct{}),
	}
	// Preserve the legacy lazy behavior until the first evaluation runs.
	g.lazy.Store(true)
	return g
}

// Update records the current tool inventory and the latest decision.
func (g *ToolSearchGate) Update(tools []interfaces.Tool, decision ToolSearchDecision) {
	g.mu.Lock()
	for name := range g.mcpTools {
		delete(g.mcpTools, name)
	}
	for _, tool := range tools {
		if isMCPTool(tool) {
			g.mcpTools[tool.Name()] = struct{}{}
		}
	}
	g.mu.Unlock()
	g.lazy.Store(decision.Lazy)
}

// Lazy reports whether lazy loading (tool search mode) is active.
func (g *ToolSearchGate) Lazy() bool {
	if g == nil {
		return false
	}
	return g.lazy.Load()
}

// ShouldExpose implements interfaces.ToolGate.
func (g *ToolSearchGate) ShouldExpose(toolName string) bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	_, tracked := g.mcpTools[toolName]
	g.mu.RUnlock()
	isMCP := tracked || strings.HasPrefix(toolName, "mcp_")
	if isMCP && !g.lazy.Load() {
		return true
	}
	if g.pd == nil {
		return false
	}
	return g.pd.ShouldExpose(toolName)
}

var _ interfaces.ToolGate = (*ToolSearchGate)(nil)
