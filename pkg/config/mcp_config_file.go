package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type claudeMCPServerEntry struct {
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
}

type claudeMCPConfigFile struct {
	MCPServers map[string]claudeMCPServerEntry `json:"mcpServers"`
}

// LoadMCPConfigFile reads a Claude Code-compatible .mcp.json and returns
// the equivalent nano MCPServerConfig slice.
func LoadMCPConfigFile(path string) ([]MCPServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc claudeMCPConfigFile
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make([]MCPServerConfig, 0, len(doc.MCPServers))
	for name, entry := range doc.MCPServers {
		transport, err := resolveTransport(entry)
		if err != nil {
			return nil, fmt.Errorf("server %q: %w", name, err)
		}
		srv := MCPServerConfig{Name: name, Transport: transport, Headers: entry.Headers, Enabled: true}
		if transport == "stdio" {
			if entry.Command == "" {
				return nil, fmt.Errorf("server %q: stdio requires `command`", name)
			}
			srv.Command = append([]string{entry.Command}, entry.Args...)
		} else {
			if entry.URL == "" {
				return nil, fmt.Errorf("server %q: %s transport requires `url`", name, transport)
			}
			srv.URL = entry.URL
		}
		out = append(out, srv)
	}
	return out, nil
}

func resolveTransport(e claudeMCPServerEntry) (string, error) {
	switch e.Type {
	case "http", "sse", "streamable":
		return "streamable", nil
	case "stdio":
		return "stdio", nil
	case "":
		if e.Command != "" {
			return "stdio", nil
		}
		if e.URL != "" {
			return "streamable", nil
		}
		return "", fmt.Errorf("cannot detect transport: missing `type`, `command`, and `url`")
	default:
		return "", fmt.Errorf("unknown transport %q (allowed: http, sse, streamable, stdio)", e.Type)
	}
}
