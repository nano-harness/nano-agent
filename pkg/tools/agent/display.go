package agent

import (
	"sync"
)

// agentColors provides deterministic color assignment for subagents.
var agentColors = []string{
	"#FF6B6B", // red
	"#4ECDC4", // teal
	"#45B7D1", // blue
	"#96CEB4", // green
	"#FFEAA7", // yellow
	"#DDA0DD", // plum
	"#98D8C8", // mint
	"#F7DC6F", // gold
	"#BB8FCE", // purple
	"#85C1E9", // sky
}

// AgentColorManager assigns colors to agents deterministically.
type AgentColorManager struct {
	mu       sync.Mutex
	assigned map[string]string
	nextIdx  int
}

// NewAgentColorManager creates a new color manager.
func NewAgentColorManager() *AgentColorManager {
	return &AgentColorManager{
		assigned: make(map[string]string),
	}
}

// ColorFor returns the color assigned to an agent, assigning one if needed.
func (m *AgentColorManager) ColorFor(agentID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if color, ok := m.assigned[agentID]; ok {
		return color
	}

	color := agentColors[m.nextIdx%len(agentColors)]
	m.assigned[agentID] = color
	m.nextIdx++
	return color
}

// DefaultColorManager is the global color manager instance.
var DefaultColorManager = NewAgentColorManager()
