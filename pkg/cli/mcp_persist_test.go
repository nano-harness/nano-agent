package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPersistMCPOnly(t *testing.T) {
	t.Run("SurgicalAdd", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "config.yaml")
		require.NoError(t, os.WriteFile(file, []byte(`
foo: 1
bar:
  baz: 2
`), 0644))

		mcpCfg := &config.MCPConfig{Servers: []config.MCPServerConfig{
			{Name: "symphony", Transport: "streamable", URL: "http://localhost:8080/sse"},
		}}
		require.NoError(t, persistMCPOnly(file, mcpCfg))

		data, err := os.ReadFile(file)
		require.NoError(t, err)
		var doc map[string]interface{}
		require.NoError(t, yaml.Unmarshal(data, &doc))

		require.Equal(t, 1, doc["foo"])
		require.NotNil(t, doc["bar"])
		require.NotNil(t, doc["mcp"])
	})

	t.Run("SurgicalUpdate", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "config.yaml")
		require.NoError(t, os.WriteFile(file, []byte(`
foo: 1
mcp:
  servers:
    - name: old
      transport: stdio
      command: ["cmd"]
`), 0644))

		mcpCfg := &config.MCPConfig{Servers: []config.MCPServerConfig{
			{Name: "new", Transport: "streamable", URL: "http://new"},
		}}
		require.NoError(t, persistMCPOnly(file, mcpCfg))

		data, err := os.ReadFile(file)
		require.NoError(t, err)
		var doc map[string]interface{}
		require.NoError(t, yaml.Unmarshal(data, &doc))

		require.Equal(t, 1, doc["foo"])
		mcp := doc["mcp"].(map[string]interface{})
		servers := mcp["servers"].([]interface{})
		require.Len(t, servers, 1)
		require.Equal(t, "new", servers[0].(map[string]interface{})["name"])
	})

	t.Run("MissingKey", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "config.yaml")
		require.NoError(t, os.WriteFile(file, []byte(`
foo: 1
`), 0644))

		mcpCfg := &config.MCPConfig{Servers: []config.MCPServerConfig{
			{Name: "x", Transport: "stdio", Command: []string{"cmd"}},
		}}
		require.NoError(t, persistMCPOnly(file, mcpCfg))

		data, err := os.ReadFile(file)
		require.NoError(t, err)
		var doc map[string]interface{}
		require.NoError(t, yaml.Unmarshal(data, &doc))
		require.Equal(t, 1, doc["foo"])
		require.NotNil(t, doc["mcp"])
	})

	t.Run("EmptyFile", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "config.yaml")
		require.NoError(t, os.WriteFile(file, []byte{}, 0644))

		mcpCfg := &config.MCPConfig{Servers: []config.MCPServerConfig{}}
		require.NoError(t, persistMCPOnly(file, mcpCfg))

		data, err := os.ReadFile(file)
		require.NoError(t, err)
		var doc map[string]interface{}
		require.NoError(t, yaml.Unmarshal(data, &doc))
		require.NotNil(t, doc["mcp"])
	})

	t.Run("NoFileCreates", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "sub", "config.yaml")

		mcpCfg := &config.MCPConfig{Servers: []config.MCPServerConfig{
			{Name: "x", Transport: "stdio", Command: []string{"cmd"}},
		}}
		require.NoError(t, persistMCPOnly(file, mcpCfg))

		data, err := os.ReadFile(file)
		require.NoError(t, err)
		var doc map[string]interface{}
		require.NoError(t, yaml.Unmarshal(data, &doc))
		require.NotNil(t, doc["mcp"])
	})
}
