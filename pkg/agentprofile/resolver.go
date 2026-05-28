package agentprofile

import (
	"strings"
	"sync"
)

// Resolver implements four-level profile resolution:
// built-in < plugin < .claude/agents < .nano/agents
// Results are memoized per cwd.
type Resolver struct {
	mu       sync.RWMutex
	cwd      string
	manager  *Manager
	plugins  map[string]AgentProfile // plugin:name → profile
	resolved map[string]AgentProfile // cache of resolved profiles
}

// NewResolver creates a resolver for the given working directory.
func NewResolver(cwd string) *Resolver {
	return &Resolver{
		cwd:      cwd,
		manager:  NewManager(cwd),
		plugins:  make(map[string]AgentProfile),
		resolved: make(map[string]AgentProfile),
	}
}

// RegisterPlugin registers a plugin-provided agent profile.
// Plugin agents use "plugin:name" namespace.
func (r *Resolver) RegisterPlugin(name string, profile AgentProfile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[name] = profile
	// Invalidate cache
	delete(r.resolved, name)
	delete(r.resolved, "plugin:"+name)
}

// Resolve looks up a profile by name using four-level priority:
// 1. .nano/agents (project-local, highest priority)
// 2. .claude/agents
// 3. plugin namespace
// 4. built-in (lowest priority)
//
// Note: Manager.loadAll already handles .claude/agents < .nano/agents ordering.
func (r *Resolver) Resolve(name string) (AgentProfile, bool) {
	r.mu.RLock()
	if cached, ok := r.resolved[name]; ok {
		r.mu.RUnlock()
		return cached, true
	}
	r.mu.RUnlock()

	// Strip "plugin:" prefix for plugin lookup
	pluginName := name
	isPluginRef := strings.HasPrefix(name, "plugin:")
	if isPluginRef {
		pluginName = strings.TrimPrefix(name, "plugin:")
	}

	var profile AgentProfile
	var found bool

	// Level 1+2: project-local (.claude/agents + .nano/agents, handled by Manager)
	if !isPluginRef {
		profile, found = r.manager.Find(name)
	}

	// Level 3: plugin namespace
	if !found {
		r.mu.RLock()
		profile, found = r.plugins[pluginName]
		r.mu.RUnlock()
	}

	// Level 4: built-in
	if !found {
		profile, found = GetBuiltin(name)
	}

	if found {
		r.mu.Lock()
		r.resolved[name] = profile
		r.mu.Unlock()
	}

	return profile, found
}

// ListShort returns a short list of available agent types for prompt injection.
// Priority: built-in + plugin + user/project. Max 20 entries.
func (r *Resolver) ListShort() []AgentProfileSummary {
	seen := make(map[string]bool)
	var summaries []AgentProfileSummary

	// Built-ins first
	for _, p := range ListBuiltins() {
		if !seen[p.Name] {
			seen[p.Name] = true
			summaries = append(summaries, AgentProfileSummary{Name: p.Name, Description: p.Description})
		}
	}

	// Plugins
	r.mu.RLock()
	for name, p := range r.plugins {
		if !seen[name] {
			seen[name] = true
			summaries = append(summaries, AgentProfileSummary{Name: "plugin:" + name, Description: p.Description})
		}
	}
	r.mu.RUnlock()

	// Project-local
	for _, p := range r.manager.List() {
		if !seen[p.Name] {
			seen[p.Name] = true
			summaries = append(summaries, AgentProfileSummary{Name: p.Name, Description: p.Description})
		}
	}

	// Cap at 20
	if len(summaries) > 20 {
		summaries = summaries[:20]
	}
	return summaries
}

// AgentProfileSummary is a compact representation for prompt injection.
type AgentProfileSummary struct {
	Name        string
	Description string
}
