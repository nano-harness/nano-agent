package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/skill"
)

// ToolSummary holds the abbreviated information about an MCP/built-in tool for
// the directory layer of Progressive Disclosure.
type ToolSummary struct {
	Name        string
	Description string // single sentence
	Server      string // MCP server name or "built-in"
	Category    string
}

// SkillSummary holds the abbreviated information about a skill.
type SkillSummary struct {
	Name        string
	Description string // single sentence
	Scope       string // "personal" or "project"
	Active      bool
}

// ProgressiveDisclosure manages three-layer context injection for tools and skills:
//
//	Layer 1 (directory): name + one-line description only (~15 tokens/item)
//	Layer 2 (detail):    full schema / full instructions (on-demand via Discover* tools)
//	Layer 3 (cleanup):   automatic eviction of old tool results
type ProgressiveDisclosure struct {
	mu               sync.RWMutex
	toolSummaries    map[string]*ToolSummary
	skillSummaries   map[string]*SkillSummary
	expandedTools    map[string]bool // tools whose full schema has been requested
	maxExpandedTools int             // evict oldest when exceeded
	expandedOrder    []string        // insertion-order queue for eviction
	resultRetention  int             // keep last N tool results in conversation
}

// NewProgressiveDisclosure creates a ProgressiveDisclosure manager.
//
//	maxExpandedTools: maximum number of tools whose full schema stays in context.
//	resultRetention:  how many recent tool results to keep per cleanup pass.
func NewProgressiveDisclosure(maxExpandedTools, resultRetention int) *ProgressiveDisclosure {
	if maxExpandedTools <= 0 {
		maxExpandedTools = 5
	}
	if resultRetention <= 0 {
		resultRetention = 5
	}
	return &ProgressiveDisclosure{
		toolSummaries:    make(map[string]*ToolSummary),
		skillSummaries:   make(map[string]*SkillSummary),
		expandedTools:    make(map[string]bool),
		maxExpandedTools: maxExpandedTools,
		expandedOrder:    make([]string, 0, maxExpandedTools+1),
		resultRetention:  resultRetention,
	}
}

// IndexTools indexes tools for the directory layer.
func (pd *ProgressiveDisclosure) IndexTools(tools []interfaces.Tool) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	for _, t := range tools {
		pd.toolSummaries[t.Name()] = &ToolSummary{
			Name:        t.Name(),
			Description: firstSentence(t.Description()),
			Category:    string(t.Category()),
		}
	}
}

// IndexSkills indexes skills for the directory layer.
func (pd *ProgressiveDisclosure) IndexSkills(skills []skill.Skill, activeFn func(string) bool) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	for _, s := range skills {
		active := false
		if activeFn != nil {
			active = activeFn(s.Name)
		}
		pd.skillSummaries[s.Name] = &SkillSummary{
			Name:        s.Name,
			Description: firstSentence(s.Description),
			Scope:       string(s.Scope),
			Active:      active,
		}
	}
}

// SetToolServer associates a tool with an MCP server name.
func (pd *ProgressiveDisclosure) SetToolServer(toolName, serverName string) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	if ts, ok := pd.toolSummaries[toolName]; ok {
		ts.Server = serverName
	}
}

// MarkExpanded records that the agent has requested the full schema for a tool.
// If more than maxExpandedTools are recorded, the oldest is evicted.
func (pd *ProgressiveDisclosure) MarkExpanded(toolName string) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if pd.expandedTools[toolName] {
		return // already marked
	}

	pd.expandedTools[toolName] = true
	pd.expandedOrder = append(pd.expandedOrder, toolName)

	// Evict oldest if over limit
	for len(pd.expandedOrder) > pd.maxExpandedTools {
		oldest := pd.expandedOrder[0]
		pd.expandedOrder = pd.expandedOrder[1:]
		delete(pd.expandedTools, oldest)
	}
}

// BuildToolDirectory returns the Layer-1 directory string for all indexed tools.
func (pd *ProgressiveDisclosure) BuildToolDirectory() string {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	if len(pd.toolSummaries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Available Tools (Directory)\n\n")
	sb.WriteString("Use `discover_tools` to get full details about any tool.\n\n")
	sb.WriteString("| Tool | Description | Category |\n")
	sb.WriteString("|------|-------------|----------|\n")

	for _, ts := range pd.toolSummaries {
		server := ts.Server
		if server == "" {
			server = "built-in"
		}
		fmt.Fprintf(&sb, "| %s | %s | %s/%s |\n", ts.Name, ts.Description, ts.Category, server)
	}
	return sb.String()
}

// BuildSkillDirectory returns the Layer-1 directory string for all indexed skills.
func (pd *ProgressiveDisclosure) BuildSkillDirectory() string {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	if len(pd.skillSummaries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Available Skills (Directory)\n\n")
	sb.WriteString("Use `discover_skills` to get full instructions for any skill.\n\n")
	sb.WriteString("| Skill | Description | Scope | Active |\n")
	sb.WriteString("|-------|-------------|-------|--------|\n")

	for _, ss := range pd.skillSummaries {
		active := "no"
		if ss.Active {
			active = "yes"
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", ss.Name, ss.Description, ss.Scope, active)
	}
	return sb.String()
}

// SearchTools returns tools whose name or description contains the query.
func (pd *ProgressiveDisclosure) SearchTools(query string) []*ToolSummary {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	q := strings.ToLower(query)
	var results []*ToolSummary
	for _, ts := range pd.toolSummaries {
		if strings.Contains(strings.ToLower(ts.Name), q) ||
			strings.Contains(strings.ToLower(ts.Description), q) ||
			strings.Contains(strings.ToLower(ts.Category), q) {
			results = append(results, ts)
		}
	}
	return results
}

// SearchSkills returns skills whose name or description contains the query.
func (pd *ProgressiveDisclosure) SearchSkills(query string) []*SkillSummary {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	q := strings.ToLower(query)
	var results []*SkillSummary
	for _, ss := range pd.skillSummaries {
		if strings.Contains(strings.ToLower(ss.Name), q) ||
			strings.Contains(strings.ToLower(ss.Description), q) {
			results = append(results, ss)
		}
	}
	return results
}

// GetTool returns the ToolSummary for a named tool.
func (pd *ProgressiveDisclosure) GetTool(name string) (*ToolSummary, bool) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	ts, ok := pd.toolSummaries[name]
	return ts, ok
}

// GetSkill returns the SkillSummary for a named skill.
func (pd *ProgressiveDisclosure) GetSkill(name string) (*SkillSummary, bool) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	ss, ok := pd.skillSummaries[name]
	return ss, ok
}

// firstSentence extracts the first sentence from a description string.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{". ", ".\n", "! ", "!\n", "? ", "?\n"} {
		if idx := strings.Index(s, sep); idx >= 0 {
			return strings.TrimSpace(s[:idx+1])
		}
	}
	// No sentence boundary found — truncate at 120 chars
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
