package agentprofile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v2"
)

const (
	frontmatterStart = "---\n"
	frontmatterEnd   = "\n---"
	frontmatterFence = "---"
)

// AgentProfile describes a reusable sub-agent declared under .claude/agents or .nano/agents.
type AgentProfile struct {
	Name           string   `json:"name" yaml:"name"`
	Description    string   `json:"description,omitempty" yaml:"description,omitempty"`
	InitialPrompt  string   `json:"initial_prompt,omitempty" yaml:"initial_prompt,omitempty"`
	PermissionMode string   `json:"permission_mode,omitempty" yaml:"permission_mode,omitempty"`
	Model          string   `json:"model,omitempty" yaml:"model,omitempty"`
	Fallbacks      []string `json:"fallbacks,omitempty" yaml:"fallbacks,omitempty"`
	Kind           string   `json:"kind,omitempty" yaml:"kind,omitempty"`
	Color          string   `json:"color,omitempty" yaml:"color,omitempty"`
	Source         string   `json:"source,omitempty" yaml:"source,omitempty"`

	// New Claude-Code-aligned fields
	Tools            []string `json:"tools,omitempty" yaml:"tools,omitempty"`                         // Tool whitelist (replaces AllowedTools); ['*'] means all
	DisallowedTools  []string `json:"disallowed_tools,omitempty" yaml:"disallowed_tools,omitempty"`   // Explicit tool denylist
	McpServers       []string `json:"mcp_servers,omitempty" yaml:"mcp_servers,omitempty"`             // MCP server references
	Background       bool     `json:"background,omitempty" yaml:"background,omitempty"`               // If true, always runs async
	Isolation        string   `json:"isolation,omitempty" yaml:"isolation,omitempty"`                 // "worktree" for git worktree isolation
	MaxTurns         int      `json:"max_turns,omitempty" yaml:"max_turns,omitempty"`                 // Max LLM turns before forced stop
	OmitClaudeMd     bool     `json:"omit_claude_md,omitempty" yaml:"omit_claude_md,omitempty"`       // Skip .claude.md injection
	Plugin           string   `json:"plugin,omitempty" yaml:"plugin,omitempty"`                       // Plugin namespace ("plugin:name")
	ContextProviders []string `json:"context_providers,omitempty" yaml:"context_providers,omitempty"` // Constrained context sources

	// Deprecated: use Tools instead. Kept for backward compat during load only.
	AllowedTools []string `json:"allowed_tools,omitempty" yaml:"allowed_tools,omitempty"`
}

// Manager discovers AgentProfile files from .nano/agents.
type Manager struct {
	cwd      string
	profiles map[string]AgentProfile
}

// NewManager creates a profile manager rooted at cwd.
func NewManager(cwd string) *Manager {
	m := &Manager{cwd: cwd, profiles: map[string]AgentProfile{}}
	m.loadAll()
	return m
}

// List returns all discovered profiles sorted by name.
func (m *Manager) List() []AgentProfile {
	out := make([]AgentProfile, 0, len(m.profiles))
	for _, profile := range m.profiles {
		out = append(out, profile)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Find returns a profile by name.
func (m *Manager) Find(name string) (AgentProfile, bool) {
	profile, ok := m.profiles[strings.TrimSpace(name)]
	return profile, ok
}

func (m *Manager) loadAll() {
	if strings.TrimSpace(m.cwd) == "" {
		return
	}
	// Search .claude/agents first, then .nano/agents (project-local overrides all)
	roots := []string{
		filepath.Join(m.cwd, ".claude", "agents"),
		filepath.Join(m.cwd, ".nano", "agents"),
	}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			profile, err := readProfile(path)
			if err != nil || profile.Name == "" {
				return nil
			}
			// Later roots override earlier ones (project-local wins)
			m.profiles[profile.Name] = profile
			return nil
		})
	}
}

func readProfile(path string) (AgentProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AgentProfile{}, err
	}
	profile := AgentProfile{Source: path}
	body := string(data)
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &profile); err != nil {
			return AgentProfile{}, err
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &profile); err != nil {
			return AgentProfile{}, err
		}
	case ".md":
		frontmatter, markdownBody, ok := parseFrontmatter(body)
		if ok {
			if err := yaml.Unmarshal([]byte(frontmatter), &profile); err != nil {
				return AgentProfile{}, err
			}
			body = markdownBody
		}
		if profile.InitialPrompt == "" {
			profile.InitialPrompt = body
		}
	default:
		return AgentProfile{}, fmt.Errorf("unsupported agent profile extension %q", filepath.Ext(path))
	}
	if profile.Source == "" {
		profile.Source = path
	}
	if strings.TrimSpace(profile.Name) == "" {
		profile.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	profile.Name = strings.TrimSpace(profile.Name)
	profile.PermissionMode = strings.TrimSpace(profile.PermissionMode)
	if profile.Kind == "" {
		profile.Kind = "in_process"
	}
	// Migrate AllowedTools → Tools (backward compat)
	if len(profile.AllowedTools) > 0 && len(profile.Tools) == 0 {
		profile.Tools = append([]string(nil), profile.AllowedTools...)
	}
	profile.AllowedTools = nil
	return profile, nil
}

func parseFrontmatter(body string) (string, string, bool) {
	if !strings.HasPrefix(body, frontmatterStart) {
		return "", body, false
	}
	frontmatterBody := body[len(frontmatterStart):]
	idx := findFrontmatterEnd(frontmatterBody)
	if idx < 0 {
		return "", body, false
	}
	frontmatter := frontmatterBody[:idx]
	markdownBody := strings.TrimSpace(frontmatterBody[frontmatterBodyStart(idx):])
	return frontmatter, markdownBody, true
}

func findFrontmatterEnd(body string) int {
	if strings.HasPrefix(body, frontmatterFence) {
		if len(body) == len(frontmatterFence) || body[len(frontmatterFence)] == '\n' || body[len(frontmatterFence)] == '\r' {
			return 0
		}
	}
	return strings.Index(body, frontmatterEnd)
}

func frontmatterBodyStart(endIdx int) int {
	if endIdx == 0 {
		return len(frontmatterFence)
	}
	return endIdx + len(frontmatterEnd)
}
