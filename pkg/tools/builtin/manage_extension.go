package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/agentprofile"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/extension"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/skill"
)

// ManageExtensionTool provides a unified management surface for skills, MCP
// servers, built-in tools, agent extensions, and slash command extensions.
type ManageExtensionTool struct {
	skillManager *skill.Manager
	cfg          *config.Config
	configPath   string
	registry     interfaces.ToolRegistry
	confirmFn    func(summary string) bool
}

func NewManageExtensionTool(sm *skill.Manager, cfg *config.Config, configPath string, registry interfaces.ToolRegistry, confirmFn func(string) bool) *ManageExtensionTool {
	return &ManageExtensionTool{
		skillManager: sm,
		cfg:          cfg,
		configPath:   configPath,
		registry:     registry,
		confirmFn:    confirmFn,
	}
}

func (t *ManageExtensionTool) Name() string { return "manage_extension" }

func (t *ManageExtensionTool) Description() string {
	return "Unified extension ecosystem manager for skills, MCP servers, tools, agent extensions, and command extensions: list, status, manifest, install, update, enable, disable, remove, doctor, trust, or audit."
}

func (t *ManageExtensionTool) Category() interfaces.ToolCategory { return interfaces.CategoryAgent }
func (t *ManageExtensionTool) RequiresConfirmation() bool        { return false }
func (t *ManageExtensionTool) ConcurrencySafe() bool             { return false }

func (t *ManageExtensionTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"Manage extensions across skills, MCP servers, tools, and agent extensions",
		map[string]*interfaces.PropertySchema{
			"action": {
				Type:        "string",
				Description: "Action to perform",
				Enum:        []string{"list", "status", "manifest", "install", "enable", "disable", "update", "remove", "doctor", "trust", "audit"},
			},
			"kind": {
				Type:        "string",
				Description: "Extension kind: skill, mcp, tool, agent, or command",
				Enum:        []string{"skill", "mcp", "tool", "agent", "command"},
			},
			"name": {
				Type:        "string",
				Description: "Extension name",
			},
			"source": {
				Type:        "string",
				Description: "Install/update source for skills, or URL for MCP streamable servers",
			},
			"description": {
				Type:        "string",
				Description: "Description for MCP install/update",
			},
			"transport": {
				Type:        "string",
				Description: "MCP transport: stdio or streamable",
				Enum:        []string{"stdio", "streamable"},
			},
			"command": {
				Type:        "array",
				Description: "MCP stdio command",
				Items:       &interfaces.PropertySchema{Type: "string"},
			},
		},
		[]string{"action"},
	)
}

func (t *ManageExtensionTool) Execute(ctx context.Context, args map[string]interface{}) (*interfaces.ToolResult, error) {
	action, _ := args["action"].(string)
	if action == "" {
		return nil, fmt.Errorf("action is required")
	}
	switch action {
	case "list", "status":
		return t.list(action)
	case "manifest":
		return t.manifest(args)
	case "install":
		return t.install(ctx, args)
	case "enable", "disable":
		return t.toggle(args, action == "enable")
	case "update":
		return t.update(ctx, args)
	case "remove":
		return t.remove(args)
	case "doctor":
		return t.doctor(args)
	case "trust":
		return t.trust(args)
	case "audit":
		return t.audit(args)
	default:
		return nil, fmt.Errorf("unknown action %q; valid actions: list, status, manifest, install, enable, disable, update, remove, doctor, trust, audit", action)
	}
}

func (t *ManageExtensionTool) list(action string) (*interfaces.ToolResult, error) {
	manifests := t.extensionRegistry().List()
	if action == "status" {
		return t.jsonResult(manifests)
	}
	if len(manifests) == 0 {
		return &interfaces.ToolResult{Success: true, LLMContent: "No extensions registered.", UserContent: "No extensions registered."}, nil
	}
	var sb strings.Builder
	for _, manifest := range manifests {
		enabled := "disabled"
		if manifest.Enabled {
			enabled = "enabled"
		}
		fmt.Fprintf(&sb, "- %s/%s [%s, health=%s]: %s\n", manifest.Kind, manifest.Name, enabled, manifest.Health.Status, manifest.Description)
	}
	listing := sb.String()
	return &interfaces.ToolResult{Success: true, LLMContent: listing, UserContent: listing, Data: manifests}, nil
}

func (t *ManageExtensionTool) manifest(args map[string]interface{}) (*interfaces.ToolResult, error) {
	kind, name, err := parseKindName(args)
	if err != nil {
		return nil, err
	}
	manifest, ok := t.extensionRegistry().Get(kind, name)
	if !ok {
		msg := fmt.Sprintf("Extension %s/%s not found", kind, name)
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}
	return t.jsonResult(manifest)
}

func (t *ManageExtensionTool) install(ctx context.Context, args map[string]interface{}) (*interfaces.ToolResult, error) {
	kind, err := parseKindArg(args)
	if err != nil {
		return nil, err
	}
	switch kind {
	case extension.KindSkill:
		return t.installSkill(ctx, args, "Install skill extension")
	case extension.KindMCP:
		if result, ok := t.requireRemoteConfirmation(args, "Install MCP extension"); ok {
			return result, nil
		}
		return t.mcpTool().addServer(extensionArgsToMCPArgs(args, "add"))
	default:
		msg := fmt.Sprintf("Install is not supported for %s extensions; they are discovered from runtime registration.", kind)
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}
}

func (t *ManageExtensionTool) toggle(args map[string]interface{}, enable bool) (*interfaces.ToolResult, error) {
	kind, name, err := parseKindName(args)
	if err != nil {
		return nil, err
	}
	switch kind {
	case extension.KindSkill:
		if t.skillManager == nil {
			return unavailable("Skill manager is not available"), nil
		}
		var opErr error
		if enable {
			opErr = t.skillManager.ActivateSkill(name)
		} else {
			opErr = t.skillManager.DeactivateSkill(name)
		}
		if opErr != nil {
			msg := fmt.Sprintf("Failed to update skill %q: %v", name, opErr)
			return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
		}
		state := "disabled"
		if enable {
			state = "enabled"
		}
		msg := fmt.Sprintf("Skill extension %q %s", name, state)
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	case extension.KindMCP:
		return t.mcpTool().toggleServer(map[string]interface{}{"name": name}, enable)
	default:
		msg := fmt.Sprintf("Enable/disable is not supported for %s extensions; use permissions or config for access control.", kind)
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}
}

func (t *ManageExtensionTool) update(ctx context.Context, args map[string]interface{}) (*interfaces.ToolResult, error) {
	kind, err := parseKindArg(args)
	if err != nil {
		return nil, err
	}
	switch kind {
	case extension.KindSkill:
		return t.installSkill(ctx, args, "Update skill extension")
	case extension.KindMCP:
		name, _ := args["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name is required for MCP update")
		}
		if result, ok := t.requireRemoteConfirmation(args, fmt.Sprintf("Update MCP extension %q", name)); ok {
			return result, nil
		}
		summary := fmt.Sprintf("Update MCP extension %q in %s", name, t.configPath)
		if t.confirmFn != nil && !t.confirmFn(summary) {
			return &interfaces.ToolResult{Success: false, LLMContent: "MCP extension update cancelled by user", UserContent: "MCP extension update cancelled by user"}, nil
		}
		mt := t.mcpTool()
		if err := mt.removeServerFromConfig(name); err != nil {
			msg := fmt.Sprintf("Failed to remove old MCP config: %v", err)
			return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
		}
		entry := mcpEntryFromExtensionArgs(args)
		if err := mt.appendServerToConfig(entry); err != nil {
			msg := fmt.Sprintf("Failed to write MCP config: %v", err)
			return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
		}
		t.replaceMCPConfig(entry)
		msg := fmt.Sprintf("MCP extension %q updated. Restart to apply.", name)
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	default:
		msg := fmt.Sprintf("Update is not supported for %s extensions", kind)
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}
}

func (t *ManageExtensionTool) remove(args map[string]interface{}) (*interfaces.ToolResult, error) {
	kind, name, err := parseKindName(args)
	if err != nil {
		return nil, err
	}
	switch kind {
	case extension.KindSkill:
		if t.skillManager == nil {
			return unavailable("Skill manager is not available"), nil
		}
		summary := fmt.Sprintf("Remove skill extension %q", name)
		if t.confirmFn != nil && !t.confirmFn(summary) {
			return &interfaces.ToolResult{Success: false, LLMContent: "Skill extension removal cancelled by user", UserContent: "Skill extension removal cancelled by user"}, nil
		}
		if err := t.skillManager.RemoveSkill(name); err != nil {
			msg := fmt.Sprintf("Failed to remove skill extension %q: %v", name, err)
			return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
		}
		msg := fmt.Sprintf("Skill extension %q removed", name)
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	case extension.KindMCP:
		return t.mcpTool().removeServer(map[string]interface{}{"name": name})
	default:
		msg := fmt.Sprintf("Remove is not supported for %s extensions; remove the runtime registration or project declaration instead.", kind)
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}
}

type extensionDoctorReport struct {
	Kind        extension.Kind `json:"kind"`
	Name        string         `json:"name"`
	Enabled     bool           `json:"enabled"`
	Installed   bool           `json:"installed"`
	Health      string         `json:"health"`
	HealthMsg   string         `json:"health_message,omitempty"`
	TrustLevel  string         `json:"trust_level"`
	Trusted     bool           `json:"trusted"`
	Permissions int            `json:"permissions"`
	Issues      []string       `json:"issues,omitempty"`
}

func (t *ManageExtensionTool) doctor(args map[string]interface{}) (*interfaces.ToolResult, error) {
	manifests, err := t.filteredManifests(args)
	if err != nil {
		return nil, err
	}
	reports := make([]extensionDoctorReport, 0, len(manifests))
	for _, manifest := range manifests {
		report := extensionDoctorReport{
			Kind:        manifest.Kind,
			Name:        manifest.Name,
			Enabled:     manifest.Enabled,
			Installed:   manifest.Installed,
			Health:      string(manifest.Health.Status),
			HealthMsg:   manifest.Health.Message,
			TrustLevel:  manifest.Trust.Level,
			Trusted:     manifest.Trust.Trusted,
			Permissions: len(manifest.Permissions),
		}
		if !manifest.Installed {
			report.Issues = append(report.Issues, "extension is not installed")
		}
		if !manifest.Enabled {
			report.Issues = append(report.Issues, "extension is disabled")
		}
		if manifest.Health.Status == extension.HealthDegraded || manifest.Health.Status == extension.HealthUnknown {
			report.Issues = append(report.Issues, manifest.Health.Message)
		}
		if !manifest.Trust.Trusted {
			report.Issues = append(report.Issues, manifest.Trust.Reason)
		}
		reports = append(reports, report)
	}
	return t.jsonResult(reports)
}

func (t *ManageExtensionTool) trust(args map[string]interface{}) (*interfaces.ToolResult, error) {
	kind, name, err := parseKindName(args)
	if err != nil {
		return nil, err
	}
	manifest, ok := t.extensionRegistry().Get(kind, name)
	if !ok {
		msg := fmt.Sprintf("Extension %s/%s not found", kind, name)
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}
	return t.jsonResult(map[string]interface{}{
		"kind":   manifest.Kind,
		"name":   manifest.Name,
		"source": manifest.Source,
		"trust":  manifest.Trust,
	})
}

func (t *ManageExtensionTool) audit(args map[string]interface{}) (*interfaces.ToolResult, error) {
	manifests, err := t.filteredManifests(args)
	if err != nil {
		return nil, err
	}
	entries := make([]map[string]interface{}, 0, len(manifests))
	for _, manifest := range manifests {
		entries = append(entries, map[string]interface{}{
			"kind":        manifest.Kind,
			"name":        manifest.Name,
			"source":      manifest.Source,
			"enabled":     manifest.Enabled,
			"installed":   manifest.Installed,
			"health":      manifest.Health,
			"trust":       manifest.Trust,
			"permissions": manifest.Permissions,
			"metadata":    manifest.Metadata,
		})
	}
	return t.jsonResult(entries)
}

func (t *ManageExtensionTool) installSkill(ctx context.Context, args map[string]interface{}, verb string) (*interfaces.ToolResult, error) {
	if t.skillManager == nil {
		return unavailable("Skill manager is not available"), nil
	}
	source, _ := args["source"].(string)
	if source == "" {
		return nil, fmt.Errorf("source is required for skill install/update")
	}
	if result, ok := t.requireRemoteConfirmation(args, verb); ok {
		return result, nil
	}
	if t.confirmFn != nil && !t.confirmFn(fmt.Sprintf("%s from: %s", verb, source)) {
		return &interfaces.ToolResult{Success: false, LLMContent: "Skill extension change cancelled by user", UserContent: "Skill extension change cancelled by user"}, nil
	}
	installed, err := t.skillManager.InstallSkill(ctx, source)
	if err != nil {
		msg := fmt.Sprintf("Failed to install skill extension from %q: %v", source, err)
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}
	msg := fmt.Sprintf("Skill extension %q installed/updated successfully", installed.Name)
	return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg, Data: map[string]interface{}{"name": installed.Name, "kind": "skill"}}, nil
}

func (t *ManageExtensionTool) requireRemoteConfirmation(args map[string]interface{}, verb string) (*interfaces.ToolResult, bool) {
	source, _ := args["source"].(string)
	if source == "" {
		// manage_extension accepts "source" while ManageMCPTool's native
		// schema uses "url"; check both before forwarding translated args.
		source, _ = args["url"].(string)
	}
	if !isRemoteSource(source) || t.confirmFn != nil {
		return nil, false
	}
	msg := fmt.Sprintf("%s from remote source %q requires explicit user confirmation", verb, source)
	return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, true
}

func isRemoteSource(source string) bool {
	source = strings.TrimSpace(strings.ToLower(source))
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}

func (t *ManageExtensionTool) extensionRegistry() *extension.Registry {
	var tools []interfaces.Tool
	if t.registry != nil {
		tools = t.registry.List()
	}
	var mcpCfg *config.MCPConfig
	if t.cfg != nil {
		mcpCfg = t.cfg.MCP
	}
	var commands []*skill.CommandDef
	var agentProfiles []agentprofile.AgentProfile
	if t.configPath != "" {
		root := filepath.Dir(t.configPath)
		commands = skill.NewCommandManager(root).List()
		agentProfiles = agentprofile.NewManager(root).List()
	}
	return extension.NewRegistryWithCommands(t.skillManager, mcpCfg, tools, commands, agentProfiles...)
}

func (t *ManageExtensionTool) filteredManifests(args map[string]interface{}) ([]extension.Manifest, error) {
	kindRaw, _ := args["kind"].(string)
	name, _ := args["name"].(string)
	if kindRaw == "" && name == "" {
		return t.extensionRegistry().List(), nil
	}
	if kindRaw == "" || name == "" {
		return nil, fmt.Errorf("kind and name must be provided together")
	}
	kind, err := extension.ParseKind(kindRaw)
	if err != nil {
		return nil, err
	}
	manifest, ok := t.extensionRegistry().Get(kind, name)
	if !ok {
		return nil, fmt.Errorf("extension %s/%s not found", kind, name)
	}
	return []extension.Manifest{manifest}, nil
}

func (t *ManageExtensionTool) mcpTool() *ManageMCPTool {
	return NewManageMCPTool(t.cfg, t.configPath, t.confirmFn)
}

func (t *ManageExtensionTool) jsonResult(v interface{}) (*interfaces.ToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	text := string(data)
	return &interfaces.ToolResult{Success: true, LLMContent: text, UserContent: text, Data: v}, nil
}

func parseKindArg(args map[string]interface{}) (extension.Kind, error) {
	raw, _ := args["kind"].(string)
	if raw == "" {
		return "", fmt.Errorf("kind is required")
	}
	return extension.ParseKind(raw)
}

func parseKindName(args map[string]interface{}) (extension.Kind, string, error) {
	kind, err := parseKindArg(args)
	if err != nil {
		return "", "", err
	}
	name, _ := args["name"].(string)
	if name == "" {
		return "", "", fmt.Errorf("name is required")
	}
	return kind, name, nil
}

func extensionArgsToMCPArgs(args map[string]interface{}, action string) map[string]interface{} {
	out := map[string]interface{}{"action": action}
	for _, key := range []string{"name", "description", "transport", "command"} {
		if v, ok := args[key]; ok {
			out[key] = v
		}
	}
	if source, _ := args["source"].(string); source != "" {
		out["url"] = source
	}
	if url, _ := args["url"].(string); url != "" {
		out["url"] = url
	}
	return out
}

func mcpEntryFromExtensionArgs(args map[string]interface{}) MCPServerEntry {
	mcpArgs := extensionArgsToMCPArgs(args, "add")
	entry := MCPServerEntry{Enabled: true, Transport: "stdio"}
	entry.Name, _ = mcpArgs["name"].(string)
	entry.Description, _ = mcpArgs["description"].(string)
	if transport, _ := mcpArgs["transport"].(string); transport != "" {
		entry.Transport = transport
	}
	entry.URL, _ = mcpArgs["url"].(string)
	if cmds, ok := mcpArgs["command"].([]interface{}); ok {
		for _, c := range cmds {
			if s, ok := c.(string); ok {
				entry.Command = append(entry.Command, s)
			}
		}
	}
	return entry
}

func (t *ManageExtensionTool) replaceMCPConfig(entry MCPServerEntry) {
	if t.cfg == nil {
		return
	}
	if t.cfg.MCP == nil {
		t.cfg.MCP = &config.MCPConfig{}
	}
	filtered := t.cfg.MCP.Servers[:0]
	for _, s := range t.cfg.MCP.Servers {
		if s.Name != entry.Name {
			filtered = append(filtered, s)
		}
	}
	t.cfg.MCP.Servers = append(filtered, config.MCPServerConfig{
		Name:        entry.Name,
		Description: entry.Description,
		Command:     entry.Command,
		URL:         entry.URL,
		Transport:   entry.Transport,
		Enabled:     entry.Enabled,
	})
}

func unavailable(msg string) *interfaces.ToolResult {
	return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}
}
