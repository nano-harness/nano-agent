// Package extension provides a unified view over nano-agent extension types.
package extension

import (
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agentprofile"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/skill"
)

// Kind identifies the extension backend.
type Kind string

const (
	KindSkill   Kind = "skill"
	KindMCP     Kind = "mcp"
	KindTool    Kind = "tool"
	KindAgent   Kind = "agent"
	KindCommand Kind = "command"
)

// HealthStatus is a coarse extension health state.
type HealthStatus string

const (
	HealthHealthy  HealthStatus = "healthy"
	HealthDisabled HealthStatus = "disabled"
	HealthDegraded HealthStatus = "degraded"
	HealthUnknown  HealthStatus = "unknown"
)

// Permission describes an extension capability or requested permission.
type Permission struct {
	Type        string `json:"type" yaml:"type"`
	Scope       string `json:"scope,omitempty" yaml:"scope,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Trust describes whether an extension source is trusted for runtime use.
type Trust struct {
	Trusted bool   `json:"trusted" yaml:"trusted"`
	Level   string `json:"level" yaml:"level"`
	Reason  string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// Health summarizes whether an extension is usable.
type Health struct {
	Status    HealthStatus `json:"status" yaml:"status"`
	Message   string       `json:"message,omitempty" yaml:"message,omitempty"`
	CheckedAt time.Time    `json:"checked_at,omitempty" yaml:"checked_at,omitempty"`
}

// Manifest is the normalized extension manifest used by registries and tools.
type Manifest struct {
	SchemaVersion string                 `json:"schema_version" yaml:"schema_version"`
	ID            string                 `json:"id" yaml:"id"`
	Name          string                 `json:"name" yaml:"name"`
	Kind          Kind                   `json:"kind" yaml:"kind"`
	Version       string                 `json:"version,omitempty" yaml:"version,omitempty"`
	Description   string                 `json:"description,omitempty" yaml:"description,omitempty"`
	Source        string                 `json:"source,omitempty" yaml:"source,omitempty"`
	Enabled       bool                   `json:"enabled" yaml:"enabled"`
	Installed     bool                   `json:"installed" yaml:"installed"`
	Permissions   []Permission           `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	Trust         Trust                  `json:"trust" yaml:"trust"`
	Health        Health                 `json:"health" yaml:"health"`
	Metadata      map[string]interface{} `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// Registry builds a unified extension registry from existing managers/config.
type Registry struct {
	skillManager  *skill.Manager
	mcpConfig     *config.MCPConfig
	tools         []interfaces.Tool
	commands      []*skill.CommandDef
	agentProfiles []agentprofile.AgentProfile
}

// NewRegistry creates a registry backed by current runtime/config state.
func NewRegistry(sm *skill.Manager, mcpCfg *config.MCPConfig, tools []interfaces.Tool) *Registry {
	return &Registry{skillManager: sm, mcpConfig: mcpCfg, tools: tools}
}

// NewRegistryWithCommands creates a registry that also exposes custom slash
// commands as command extensions.
func NewRegistryWithCommands(sm *skill.Manager, mcpCfg *config.MCPConfig, tools []interfaces.Tool, commands []*skill.CommandDef, agentProfiles ...agentprofile.AgentProfile) *Registry {
	return &Registry{skillManager: sm, mcpConfig: mcpCfg, tools: tools, commands: commands, agentProfiles: agentProfiles}
}

// List returns all known extensions sorted by kind/name.
func (r *Registry) List() []Manifest {
	var out []Manifest
	out = append(out, r.skillManifests()...)
	out = append(out, r.mcpManifests()...)
	out = append(out, r.toolManifests()...)
	out = append(out, r.agentProfileManifests()...)
	out = append(out, r.commandManifests()...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Get finds a manifest by kind and name.
func (r *Registry) Get(kind Kind, name string) (Manifest, bool) {
	for _, manifest := range r.List() {
		if manifest.Kind == kind && manifest.Name == name {
			return manifest, true
		}
	}
	return Manifest{}, false
}

func (r *Registry) skillManifests() []Manifest {
	if r.skillManager == nil {
		return nil
	}
	metas := r.skillManager.ListMetadata()
	out := make([]Manifest, 0, len(metas))
	for _, meta := range metas {
		enabled := r.skillManager.IsActive(meta.Name)
		source := meta.SourcePath
		health := Health{Status: HealthHealthy, Message: "skill discovered", CheckedAt: time.Now()}
		if source == "" {
			health = Health{Status: HealthDegraded, Message: "skill has no source path", CheckedAt: time.Now()}
		}
		out = append(out, Manifest{
			SchemaVersion: "1",
			ID:            string(KindSkill) + ":" + meta.Name,
			Name:          meta.Name,
			Kind:          KindSkill,
			Description:   meta.Description,
			Source:        source,
			Enabled:       enabled,
			Installed:     true,
			Permissions: []Permission{{
				Type:        "prompt_context",
				Scope:       string(meta.Scope),
				Description: "Injects skill instructions into agent context when active or matched",
			}},
			Trust:  trustForSource(source),
			Health: health,
			Metadata: map[string]interface{}{
				"scope":       string(meta.Scope),
				"triggers":    meta.Triggers,
				"globs":       meta.Globs,
				"auto_invoke": meta.IsAutoInvoke(),
				"priority":    meta.Priority,
			},
		})
	}
	return out
}

func (r *Registry) mcpManifests() []Manifest {
	if r.mcpConfig == nil {
		return nil
	}
	out := make([]Manifest, 0, len(r.mcpConfig.Servers))
	for _, server := range r.mcpConfig.Servers {
		health := Health{Status: HealthDisabled, Message: "server disabled", CheckedAt: time.Now()}
		if server.Enabled {
			health = Health{Status: HealthUnknown, Message: "configured; runtime connection not checked", CheckedAt: time.Now()}
		}
		source := server.URL
		if source == "" && len(server.Command) > 0 {
			source = strings.Join(server.Command, " ")
		}
		out = append(out, Manifest{
			SchemaVersion: "1",
			ID:            string(KindMCP) + ":" + server.Name,
			Name:          server.Name,
			Kind:          KindMCP,
			Description:   server.Description,
			Source:        source,
			Enabled:       server.Enabled,
			Installed:     true,
			Permissions:   mcpPermissions(server),
			Trust:         trustForSource(source),
			Health:        health,
			Metadata: map[string]interface{}{
				"transport": server.Transport,
				"timeout":   server.Timeout.String(),
			},
		})
	}
	return out
}

func (r *Registry) toolManifests() []Manifest {
	out := make([]Manifest, 0, len(r.tools))
	for _, tool := range r.tools {
		kind := KindTool
		if tool.Category() == interfaces.CategoryAgent {
			kind = KindAgent
		}
		name := tool.Name()
		health := Health{Status: HealthHealthy, Message: "tool registered", CheckedAt: time.Now()}
		if tool.Schema() == nil {
			health = Health{Status: HealthDegraded, Message: "tool registered without schema", CheckedAt: time.Now()}
		}
		permissions := []Permission{{
			Type:        "tool_execution",
			Scope:       string(tool.Category()),
			Description: "Can be invoked by the agent as a registered tool",
		}}
		if tool.RequiresConfirmation() {
			permissions = append(permissions, Permission{
				Type:        "user_approval",
				Description: "Tool declares that execution requires explicit approval",
			})
		}
		out = append(out, Manifest{
			SchemaVersion: "1",
			ID:            string(kind) + ":" + name,
			Name:          name,
			Kind:          kind,
			Description:   tool.Description(),
			Enabled:       true,
			Installed:     true,
			Permissions:   permissions,
			Trust:         Trust{Trusted: true, Level: "runtime", Reason: "registered in current process"},
			Health:        health,
			Metadata: map[string]interface{}{
				"category":         string(tool.Category()),
				"concurrency_safe": tool.ConcurrencySafe(),
			},
		})
	}
	return out
}

func (r *Registry) commandManifests() []Manifest {
	out := make([]Manifest, 0, len(r.commands))
	for _, cmd := range r.commands {
		if cmd == nil {
			continue
		}
		permissions := []Permission{{
			Type:        "prompt_context",
			Description: "Expands into agent instructions when invoked as a slash command",
		}}
		for _, tool := range cmd.AllowedTools {
			permissions = append(permissions, Permission{
				Type:        "tool_execution",
				Scope:       tool,
				Description: "Allowed by command frontmatter allowed-tools",
			})
		}
		if cmd.PermissionProfile != "" {
			permissions = append(permissions, Permission{
				Type:        "permission_profile",
				Scope:       cmd.PermissionProfile,
				Description: "Temporarily applies the command permission profile while executing",
			})
		}
		out = append(out, Manifest{
			SchemaVersion: "1",
			ID:            string(KindCommand) + ":" + cmd.Name,
			Name:          cmd.Name,
			Kind:          KindCommand,
			Description:   cmd.Description,
			Source:        cmd.Source,
			Enabled:       true,
			Installed:     true,
			Permissions:   permissions,
			Trust:         trustForSource(cmd.Source),
			Health:        Health{Status: HealthHealthy, Message: "command discovered", CheckedAt: time.Now()},
			Metadata: map[string]interface{}{
				"namespace":          cmd.Namespace,
				"allowed_tools":      cmd.AllowedTools,
				"permission_profile": cmd.PermissionProfile,
			},
		})
	}
	return out
}

func (r *Registry) agentProfileManifests() []Manifest {
	out := make([]Manifest, 0, len(r.agentProfiles))
	for _, profile := range r.agentProfiles {
		permissions := []Permission{{
			Type:        "agent_spawn",
			Scope:       profile.Kind,
			Description: "Can be invoked through /agent-name slash command or spawn_teammate",
		}}
		if profile.PermissionMode != "" {
			permissions = append(permissions, Permission{
				Type:        "permission_profile",
				Scope:       profile.PermissionMode,
				Description: "Applies an independent permission mode to the spawned teammate",
			})
		}
		for _, tool := range profile.AllowedTools {
			permissions = append(permissions, Permission{
				Type:        "tool_execution",
				Scope:       tool,
				Description: "Declared as an allowed tool for this agent profile",
			})
		}
		for _, provider := range profile.ContextProviders {
			permissions = append(permissions, Permission{
				Type:        "context_access",
				Scope:       provider,
				Description: "Declared as an allowed context provider for this agent profile",
			})
		}
		out = append(out, Manifest{
			SchemaVersion: "1",
			ID:            string(KindAgent) + ":" + profile.Name,
			Name:          profile.Name,
			Kind:          KindAgent,
			Description:   profile.Description,
			Source:        profile.Source,
			Enabled:       true,
			Installed:     true,
			Permissions:   permissions,
			Trust:         trustForSource(profile.Source),
			Health:        Health{Status: HealthHealthy, Message: "agent profile discovered", CheckedAt: time.Now()},
			Metadata: map[string]interface{}{
				"kind":              profile.Kind,
				"color":             profile.Color,
				"model":             profile.Model,
				"context_providers": profile.ContextProviders,
				"permission_mode":   profile.PermissionMode,
				"allowed_tools":     profile.AllowedTools,
			},
		})
	}
	return out
}

func mcpPermissions(server config.MCPServerConfig) []Permission {
	var permissions []Permission
	if len(server.Command) > 0 {
		permissions = append(permissions, Permission{
			Type:        "process_spawn",
			Scope:       server.Command[0],
			Description: "Starts an MCP server process",
		})
	}
	if server.URL != "" {
		scope := server.URL
		if u, err := url.Parse(server.URL); err == nil && u.Host != "" {
			scope = u.Host
		}
		permissions = append(permissions, Permission{
			Type:        "network",
			Scope:       scope,
			Description: "Connects to a remote MCP endpoint",
		})
	}
	if len(server.Headers) > 0 || server.OAuth != nil {
		permissions = append(permissions, Permission{
			Type:        "credential",
			Description: "Uses configured headers or OAuth credentials",
		})
	}
	if len(permissions) == 0 {
		permissions = append(permissions, Permission{
			Type:        "mcp",
			Scope:       filepath.Base(server.Name),
			Description: "MCP server capability",
		})
	}
	return permissions
}

// ParseKind validates a user-provided kind.
func ParseKind(raw string) (Kind, error) {
	switch Kind(strings.ToLower(strings.TrimSpace(raw))) {
	case KindSkill:
		return KindSkill, nil
	case KindMCP:
		return KindMCP, nil
	case KindTool:
		return KindTool, nil
	case KindAgent:
		return KindAgent, nil
	case KindCommand:
		return KindCommand, nil
	default:
		return "", fmt.Errorf("unknown extension kind %q", raw)
	}
}

func trustForSource(source string) Trust {
	source = strings.TrimSpace(source)
	switch {
	case source == "":
		return Trust{Trusted: true, Level: "runtime", Reason: "registered in current process"}
	case strings.HasPrefix(source, "project:"), strings.HasPrefix(source, "user:"):
		return Trust{Trusted: true, Level: "local", Reason: "loaded from local configuration or filesystem"}
	case strings.HasPrefix(source, "http://"):
		return Trust{Trusted: false, Level: "remote_insecure", Reason: "plain HTTP remote source requires explicit confirmation and transport upgrade"}
	case strings.HasPrefix(source, "https://"):
		return Trust{Trusted: false, Level: "remote", Reason: "remote source requires explicit install/update confirmation"}
	default:
		return Trust{Trusted: true, Level: "configured", Reason: "declared in local configuration"}
	}
}
