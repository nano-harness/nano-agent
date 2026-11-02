package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadMCPConfigFile(t *testing.T) {
	t.Run("StreamableExplicit", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mcp.json")
		require.NoError(t, os.WriteFile(path, []byte(`{
			"mcpServers": {
				"web": { "type": "http", "url": "http://localhost:8080/sse" }
			}
		}`), 0644))
		servers, err := LoadMCPConfigFile(path)
		require.NoError(t, err)
		require.Len(t, servers, 1)
		require.Equal(t, "web", servers[0].Name)
		require.Equal(t, "streamable", servers[0].Transport)
		require.Equal(t, "http://localhost:8080/sse", servers[0].URL)
	})

	t.Run("StdioAutoDetect", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mcp.json")
		require.NoError(t, os.WriteFile(path, []byte(`{
			"mcpServers": {
				"local": { "command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem"] }
			}
		}`), 0644))
		servers, err := LoadMCPConfigFile(path)
		require.NoError(t, err)
		require.Len(t, servers, 1)
		require.Equal(t, "local", servers[0].Name)
		require.Equal(t, "stdio", servers[0].Transport)
		require.Equal(t, []string{"npx", "-y", "@modelcontextprotocol/server-filesystem"}, servers[0].Command)
	})

	t.Run("StreamableAutoDetect", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mcp.json")
		require.NoError(t, os.WriteFile(path, []byte(`{
			"mcpServers": {
				"remote": { "url": "https://example.com/mcp" }
			}
		}`), 0644))
		servers, err := LoadMCPConfigFile(path)
		require.NoError(t, err)
		require.Len(t, servers, 1)
		require.Equal(t, "remote", servers[0].Name)
		require.Equal(t, "streamable", servers[0].Transport)
		require.Equal(t, "https://example.com/mcp", servers[0].URL)
	})

	t.Run("MultipleServers", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mcp.json")
		require.NoError(t, os.WriteFile(path, []byte(`{
			"mcpServers": {
				"a": { "type": "stdio", "command": "cmd-a" },
				"b": { "type": "sse", "url": "http://b" }
			}
		}`), 0644))
		servers, err := LoadMCPConfigFile(path)
		require.NoError(t, err)
		require.Len(t, servers, 2)
	})

	t.Run("EmptyOK", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mcp.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"mcpServers": {}}`), 0644))
		servers, err := LoadMCPConfigFile(path)
		require.NoError(t, err)
		require.Len(t, servers, 0)
	})

	t.Run("StdioMissingCommand", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mcp.json")
		require.NoError(t, os.WriteFile(path, []byte(`{
			"mcpServers": {
				"bad": { "type": "stdio" }
			}
		}`), 0644))
		_, err := LoadMCPConfigFile(path)
		require.Error(t, err)
		require.Contains(t, err.Error(), "requires `command`")
	})

	t.Run("StreamableMissingURL", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mcp.json")
		require.NoError(t, os.WriteFile(path, []byte(`{
			"mcpServers": {
				"bad": { "type": "sse" }
			}
		}`), 0644))
		_, err := LoadMCPConfigFile(path)
		require.Error(t, err)
		require.Contains(t, err.Error(), "requires `url`")
	})

	t.Run("UnknownType", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mcp.json")
		require.NoError(t, os.WriteFile(path, []byte(`{
			"mcpServers": {
				"bad": { "type": "grpc" }
			}
		}`), 0644))
		_, err := LoadMCPConfigFile(path)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown transport")
	})

	t.Run("AmbiguousAutoDetect", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mcp.json")
		require.NoError(t, os.WriteFile(path, []byte(`{
			"mcpServers": {
				"bad": { "headers": {"X":"1"} }
			}
		}`), 0644))
		_, err := LoadMCPConfigFile(path)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot detect transport")
	})

	t.Run("FileMissing", func(t *testing.T) {
		_, err := LoadMCPConfigFile("/nonexistent/mcp.json")
		require.Error(t, err)
		require.Contains(t, err.Error(), "read")
	})
}
