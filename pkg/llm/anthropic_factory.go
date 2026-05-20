package llm

import (
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// IsAnthropicRoute determines if a route should use the Anthropic native client.
// It applies a layered strategy:
//  1. Explicit ProviderID match ("anthropic")
//  2. BaseURL inference (contains "anthropic")
//  3. Model name prefix fallback (starts with "claude-")
func IsAnthropicRoute(route ResolvedRoute) bool {
	if strings.EqualFold(route.ProviderID, "anthropic") {
		return true
	}
	if strings.Contains(strings.ToLower(route.BaseURL), "anthropic") {
		return true
	}
	return strings.HasPrefix(NormalizeModelID(route.Model), "claude-")
}

// NewClientForRoute creates the appropriate LLMClient based on the route's provider.
// Anthropic routes get an AnthropicClient; all other routes use the OpenAI-compatible Client.
func NewClientForRoute(route ResolvedRoute, tools []interfaces.Tool, cfg *config.Config) LLMClient {
	if IsAnthropicRoute(route) {
		return NewAnthropicClient(route.APIKey, route.BaseURL, route.Model, route.Headers, tools, cfg)
	}
	return NewClient(route.APIKey, route.BaseURL, route.Model, tools)
}
