package agent

import (
	"context"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/llm"
)

// AgentRouter automatically routes tasks to the appropriate agent type.
type AgentRouter struct {
	llmClient llm.LLMClient
}

// NewAgentRouter creates a new AgentRouter.
func NewAgentRouter(client llm.LLMClient) *AgentRouter {
	return &AgentRouter{llmClient: client}
}

// Route determines the appropriate agent type for the given user input
// using fast keyword-based rules without an LLM call.
func (r *AgentRouter) Route(_ context.Context, userInput string, _ []llm.Message) AgentType {
	lower := strings.ToLower(userInput)

	exploreKeywords := []string{
		"explore", "search", "find", "look", "read", "list",
		"show me", "what is", "where is", "understand", "explain",
	}
	for _, kw := range exploreKeywords {
		if strings.Contains(lower, kw) {
			return AgentTypeExplore
		}
	}

	planKeywords := []string{
		"plan", "outline", "design", "architecture", "propose",
		"strategy", "roadmap", "steps to",
	}
	for _, kw := range planKeywords {
		if strings.Contains(lower, kw) {
			return AgentTypePlan
		}
	}

	verifyKeywords := []string{
		"verify", "validate", "check", "test", "confirm",
		"ensure", "assert", "review",
	}
	for _, kw := range verifyKeywords {
		if strings.Contains(lower, kw) {
			return AgentTypeVerify
		}
	}

	return AgentTypeExecute
}
