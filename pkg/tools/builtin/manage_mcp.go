package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"gopkg.in/yaml.v2"
)

// MCPServerEntry mirrors config.MCPServerConfig for writing back to YAML.
type MCPServerEntry struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Command     []string          `yaml:"command,omitempty"`
	URL         string            `yaml:"url,omitempty"`
	Transport   string            `yaml:"transport,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	Enabled     bool              `yaml:"enabled"`
}

// ManageMCPTool lets the agent add, remove, enable, disable, list, or check
// status of MCP servers through conversation.  Mutating actions (add/remove)
// require user confirmation before writing to config.
type ManageMCPTool struct {
	configPath string // path to .nano/nano.yaml being managed
	cfg        *config.Config
	confirmFn  func(summary string) bool
}

// NewManageMCPTool creates a ManageMCPTool.
// configPath is the YAML config file to update.  If empty, the tool returns
// an error for mutating actions.
func NewManageMCPTool(cfg *config.Config, configPath string, confirmFn func(string) bool) *ManageMCPTool {
	return &ManageMCPTool{
		configPath: configPath,
		cfg:        cfg,
		confirmFn:  confirmFn,
	}
}

// Name returns the tool name.
func (t *ManageMCPTool) Name() string { return "manage_mcp_server" }

// Description returns the tool description.
func (t *ManageMCPTool) Description() string {
	return "Manage MCP servers: add, remove, enable, disable, list, or check status. Add/remove require user confirmation and write to .nano/nano.yaml."
}

// Category returns the tool category.
func (t *ManageMCPTool) Category() interfaces.ToolCategory { return interfaces.CategoryAgent }

// RequiresConfirmation returns false — handled internally.
func (t *ManageMCPTool) RequiresConfirmation() bool { return false }

// ConcurrencySafe returns false.
func (t *ManageMCPTool) ConcurrencySafe() bool { return false }

// Schema returns the JSON schema.
func (t *ManageMCPTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"Manage MCP servers: add, remove, enable, disable, list, or status",
		map[string]*interfaces.PropertySchema{
			"action": {
				Type:        "string",
				Description: "Action to perform",
				Enum:        []string{"add", "remove", "enable", "disable", "list", "status"},
			},
			"name": {
				Type:        "string",
				Description: "MCP server name",
			},
			"command": {
				Type:        "array",
				Description: "Command to start the server, e.g. [\"npx\",\"-y\",\"@modelcontextprotocol/server-filesystem\",\"/tmp\"]",
				Items:       &interfaces.PropertySchema{Type: "string"},
			},
			"url": {
				Type:        "string",
				Description: "HTTP URL for streamable transport",
			},
			"transport": {
				Type:        "string",
				Description: "Transport type: stdio (default), streamable (HTTP)",
				Enum:        []string{"stdio", "streamable"},
			},
			"description": {
				Type:        "string",
				Description: "Human-readable description of the server",
			},
		},
		[]string{"action"},
	)
}

// Execute runs the MCP management action.
func (t *ManageMCPTool) Execute(_ context.Context, args map[string]interface{}) (*interfaces.ToolResult, error) {
	action, ok := args["action"].(string)
	if !ok || action == "" {
		return nil, fmt.Errorf("action is required")
	}

	switch action {
	case "list":
		return t.listServers()
	case "status":
		return t.statusServers()
	case "add":
		return t.addServer(args)
	case "remove":
		return t.removeServer(args)
	case "enable", "disable":
		return t.toggleServer(args, action == "enable")
	default:
		return nil, fmt.Errorf("unknown action %q; valid: add, remove, enable, disable, list, status", action)
	}
}

func (t *ManageMCPTool) listServers() (*interfaces.ToolResult, error) {
	if t.cfg == nil || t.cfg.MCP == nil || len(t.cfg.MCP.Servers) == 0 {
		msg := "No MCP servers configured"
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	}

	var sb strings.Builder
	for _, s := range t.cfg.MCP.Servers {
		status := "disabled"
		if s.Enabled {
			status = "enabled"
		}
		fmt.Fprintf(&sb, "- %s [%s]: %s (transport: %s)\n", s.Name, status, s.Description, s.Transport)
	}
	listing := sb.String()
	return &interfaces.ToolResult{Success: true, LLMContent: listing, UserContent: listing}, nil
}

func (t *ManageMCPTool) statusServers() (*interfaces.ToolResult, error) {
	// Return config-level information since we don't have runtime health checks
	return t.listServers()
}

func (t *ManageMCPTool) addServer(args map[string]interface{}) (*interfaces.ToolResult, error) {
	if t.configPath == "" {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "Config path not set; cannot write MCP server configuration",
			UserContent: "Config path not set; cannot write MCP server configuration",
		}, nil
	}

	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name is required for add action")
	}

	// Build the new server entry
	entry := MCPServerEntry{
		Name:    name,
		Enabled: true,
	}
	if desc, ok := args["description"].(string); ok {
		entry.Description = desc
	}
	if transport, ok := args["transport"].(string); ok && transport != "" {
		entry.Transport = transport
	} else {
		entry.Transport = "stdio"
	}
	if url, ok := args["url"].(string); ok {
		entry.URL = url
	}
	if cmds, ok := args["command"].([]interface{}); ok {
		for _, c := range cmds {
			if s, ok := c.(string); ok {
				entry.Command = append(entry.Command, s)
			}
		}
	}

	// Validate streamable transport requires URL
	if entry.Transport == "streamable" && entry.URL == "" {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "URL is required for streamable transport",
			UserContent: "URL is required for streamable transport",
		}, nil
	}

	// Build confirmation summary
	preview := jsonPretty(entry)
	summary := fmt.Sprintf("Add MCP server %q to %s:\n%s", name, t.configPath, preview)
	if t.confirmFn != nil && !t.confirmFn(summary) {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "MCP server addition cancelled by user",
			UserContent: "MCP server addition cancelled by user",
		}, nil
	}

	if err := t.appendServerToConfig(entry); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Failed to write config: %v", err),
			UserContent: fmt.Sprintf("Failed to write config: %v", err),
		}, nil
	}

	// Update in-memory config so subsequent list/status calls reflect the change.
	if t.cfg != nil {
		if t.cfg.MCP == nil {
			t.cfg.MCP = &config.MCPConfig{}
		}
		t.cfg.MCP.EnableClient = true
		t.cfg.MCP.Servers = append(t.cfg.MCP.Servers, config.MCPServerConfig{
			Name:        entry.Name,
			Description: entry.Description,
			Command:     entry.Command,
			URL:         entry.URL,
			Transport:   entry.Transport,
			Enabled:     entry.Enabled,
		})
	}

	msg := fmt.Sprintf("MCP server %q added to %s. Restart to apply.", name, t.configPath)
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  msg,
		UserContent: msg,
	}, nil
}

func (t *ManageMCPTool) removeServer(args map[string]interface{}) (*interfaces.ToolResult, error) {
	if t.configPath == "" {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "Config path not set; cannot modify MCP server configuration",
			UserContent: "Config path not set; cannot modify MCP server configuration",
		}, nil
	}

	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name is required for remove action")
	}

	summary := fmt.Sprintf("Remove MCP server %q from %s", name, t.configPath)
	if t.confirmFn != nil && !t.confirmFn(summary) {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "MCP server removal cancelled by user",
			UserContent: "MCP server removal cancelled by user",
		}, nil
	}

	if err := t.removeServerFromConfig(name); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Failed to update config: %v", err),
			UserContent: fmt.Sprintf("Failed to update config: %v", err),
		}, nil
	}

	// Update in-memory config so subsequent list/status calls reflect the change.
	if t.cfg != nil && t.cfg.MCP != nil {
		filtered := t.cfg.MCP.Servers[:0]
		for _, s := range t.cfg.MCP.Servers {
			if s.Name != name {
				filtered = append(filtered, s)
			}
		}
		t.cfg.MCP.Servers = filtered
	}

	msg := fmt.Sprintf("MCP server %q removed from %s. Restart to apply.", name, t.configPath)
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  msg,
		UserContent: msg,
	}, nil
}

func (t *ManageMCPTool) toggleServer(args map[string]interface{}, enable bool) (*interfaces.ToolResult, error) {
	if t.configPath == "" {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "Config path not set",
			UserContent: "Config path not set",
		}, nil
	}

	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name is required for enable/disable action")
	}

	if err := t.setServerEnabled(name, enable); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Failed to update config: %v", err),
			UserContent: fmt.Sprintf("Failed to update config: %v", err),
		}, nil
	}

	action := "disabled"
	if enable {
		action = "enabled"
	}
	msg := fmt.Sprintf("MCP server %q %s. Restart to apply.", name, action)
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  msg,
		UserContent: msg,
	}, nil
}

// appendServerToConfig reads the YAML file, appends the server, and writes it back.
func (t *ManageMCPTool) appendServerToConfig(entry MCPServerEntry) error {
	raw, err := t.readOrInitConfig()
	if err != nil {
		return err
	}

	// Backup
	if err := backupFile(t.configPath); err != nil {
		return fmt.Errorf("backup config: %w", err)
	}

	// Navigate/create mcp.servers
	mcpMap := ensureMap(raw, "mcp")
	mcpMap["enable_client"] = true

	var servers []interface{}
	if existing, ok := mcpMap["servers"]; ok {
		if s, ok := existing.([]interface{}); ok {
			servers = s
		}
	}

	// Convert entry to map for generic YAML
	entryData := map[interface{}]interface{}{
		"name":      entry.Name,
		"enabled":   entry.Enabled,
		"transport": entry.Transport,
	}
	if entry.Description != "" {
		entryData["description"] = entry.Description
	}
	if entry.URL != "" {
		entryData["url"] = entry.URL
	}
	if len(entry.Command) > 0 {
		cmds := make([]interface{}, len(entry.Command))
		for i, c := range entry.Command {
			cmds[i] = c
		}
		entryData["command"] = cmds
	}

	servers = append(servers, entryData)
	mcpMap["servers"] = servers

	return t.writeConfig(raw)
}

// removeServerFromConfig removes the named server from the YAML config.
func (t *ManageMCPTool) removeServerFromConfig(name string) error {
	raw, err := t.readOrInitConfig()
	if err != nil {
		return err
	}

	if err := backupFile(t.configPath); err != nil {
		return fmt.Errorf("backup config: %w", err)
	}

	mcpMap, ok := raw["mcp"]
	if !ok {
		return nil // nothing to remove
	}
	mcpMapTyped, ok := mcpMap.(map[interface{}]interface{})
	if !ok {
		return nil
	}

	servers, ok := mcpMapTyped["servers"].([]interface{})
	if !ok {
		return nil
	}

	filtered := servers[:0]
	for _, s := range servers {
		if sm, ok := s.(map[interface{}]interface{}); ok {
			if sm["name"] != name {
				filtered = append(filtered, s)
			}
		} else {
			// Preserve unknown / non-map YAML entries unchanged.
			filtered = append(filtered, s)
		}
	}
	mcpMapTyped["servers"] = filtered

	return t.writeConfig(raw)
}

// setServerEnabled toggles the enabled flag for a server.
func (t *ManageMCPTool) setServerEnabled(name string, enabled bool) error {
	raw, err := t.readOrInitConfig()
	if err != nil {
		return err
	}

	if err := backupFile(t.configPath); err != nil {
		return fmt.Errorf("backup config: %w", err)
	}

	mcpMap, ok := raw["mcp"]
	if !ok {
		return fmt.Errorf("no MCP configuration found in %s", t.configPath)
	}
	mcpMapTyped, ok := mcpMap.(map[interface{}]interface{})
	if !ok {
		return fmt.Errorf("invalid MCP configuration format")
	}

	servers, ok := mcpMapTyped["servers"].([]interface{})
	if !ok {
		return fmt.Errorf("server %q not found", name)
	}

	found := false
	for _, s := range servers {
		if sm, ok := s.(map[interface{}]interface{}); ok {
			if sm["name"] == name {
				sm["enabled"] = enabled
				found = true
			}
		}
	}
	if !found {
		return fmt.Errorf("server %q not found in config", name)
	}

	return t.writeConfig(raw)
}

// readOrInitConfig reads the config file as a generic YAML map, or returns empty map.
func (t *ManageMCPTool) readOrInitConfig() (map[interface{}]interface{}, error) {
	data, err := os.ReadFile(t.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[interface{}]interface{}), nil
		}
		return nil, fmt.Errorf("read config %q: %w", t.configPath, err)
	}

	var raw map[interface{}]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", t.configPath, err)
	}
	if raw == nil {
		raw = make(map[interface{}]interface{})
	}
	return raw, nil
}

func (t *ManageMCPTool) writeConfig(raw map[interface{}]interface{}) error {
	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(t.configPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(t.configPath, out, 0o600)
}

// ensureMap creates or returns the sub-map under key k.
func ensureMap(m map[interface{}]interface{}, k string) map[interface{}]interface{} {
	if v, ok := m[k]; ok {
		if mm, ok := v.(map[interface{}]interface{}); ok {
			return mm
		}
	}
	mm := make(map[interface{}]interface{})
	m[k] = mm
	return mm
}

// backupFile creates a .bak copy of the file (best-effort).
func backupFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.WriteFile(path+".bak", data, 0o600)
}

// jsonPretty returns a pretty-printed JSON string or a plain string fallback.
func jsonPretty(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
