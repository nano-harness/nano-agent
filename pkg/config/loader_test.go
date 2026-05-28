package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_DeepMerge_UserAndProject(t *testing.T) {
	// Setup temp directories
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	configDir := filepath.Join(homeDir, ".config", "nano")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	projectDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	require.NoError(t, os.Chdir(projectDir))

	// Create user config with security allow/deny rules
	userConfig := `
api_key: "user-api-key"
security:
  allow_rules:
    - "Bash(git *)"
  deny_rules:
    - "Bash(rm -rf /)"
`
	userConfigPath := filepath.Join(configDir, "config.yaml")
	require.NoError(t, os.WriteFile(userConfigPath, []byte(userConfig), 0644))

	// Point NANO_USER_CONFIG to our temp user config
	t.Setenv("NANO_USER_CONFIG", userConfigPath)

	// Create project config with MCP
	projectConfig := `
mcp:
  enable_client: true
  servers:
    - name: "project-server"
      command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
`
	require.NoError(t, os.MkdirAll(".nano", 0755))
	require.NoError(t, os.WriteFile(filepath.Join(".nano", "nano.yaml"), []byte(projectConfig), 0644))

	// Load config
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify API key from user config
	assert.Equal(t, "user-api-key", cfg.APIKey)

	// Verify MCP from project config
	assert.NotNil(t, cfg.MCP)
	assert.True(t, cfg.MCP.EnableClient)
	assert.Len(t, cfg.MCP.Servers, 1)
	assert.Equal(t, "project-server", cfg.MCP.Servers[0].Name)

	// Verify security rules were appended
	assert.NotNil(t, cfg.Security)
	assert.Contains(t, cfg.Security.AllowRules, "Bash(git *)")
	assert.Contains(t, cfg.Security.DenyRules, "Bash(rm -rf /)")
}

func TestLoadConfig_DeepMerge_OnlyUserConfig(t *testing.T) {
	// Setup temp directories
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	configDir := filepath.Join(homeDir, ".config", "nano")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	projectDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	require.NoError(t, os.Chdir(projectDir))

	// Create user config only
	userConfig := `
api_key: "user-only-key"
`
	userConfigPath := filepath.Join(configDir, "config.yaml")
	require.NoError(t, os.WriteFile(userConfigPath, []byte(userConfig), 0644))

	// Point NANO_USER_CONFIG to our temp user config
	t.Setenv("NANO_USER_CONFIG", userConfigPath)

	// No project config
	// Load config
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify user config was loaded
	assert.Equal(t, "user-only-key", cfg.APIKey)
}

func TestLoadConfig_DeepMerge_OnlyProjectConfig(t *testing.T) {
	// Setup temp directory for project
	projectDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	require.NoError(t, os.Chdir(projectDir))

	// Create project config only
	projectConfig := `
api_key: "project-only-key"
mcp:
  enable_client: true
`
	require.NoError(t, os.MkdirAll(".nano", 0755))
	require.NoError(t, os.WriteFile(filepath.Join(".nano", "nano.yaml"), []byte(projectConfig), 0644))

	// Load config
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify project config was loaded
	assert.Equal(t, "project-only-key", cfg.APIKey)
	assert.NotNil(t, cfg.MCP)
	assert.True(t, cfg.MCP.EnableClient)
}

func TestLoadConfig_DeepMerge_ExplicitEmptyArrayClearsField(t *testing.T) {
	// Setup temp directories
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	configDir := filepath.Join(homeDir, ".config", "nano")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	projectDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	require.NoError(t, os.Chdir(projectDir))

	// Create user config with MCP servers
	userConfig := `
mcp:
  servers:
    - name: "server1"
      command: ["npx"]
`
	userConfigPath := filepath.Join(configDir, "config.yaml")
	require.NoError(t, os.WriteFile(userConfigPath, []byte(userConfig), 0644))

	// Point NANO_USER_CONFIG to our temp user config
	t.Setenv("NANO_USER_CONFIG", userConfigPath)

	// Create project config that explicitly clears servers
	projectConfig := `
mcp:
  servers: []
  enable_client: true
`
	require.NoError(t, os.MkdirAll(".nano", 0755))
	require.NoError(t, os.WriteFile(filepath.Join(".nano", "nano.yaml"), []byte(projectConfig), 0644))

	// Load config
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify MCP servers were explicitly cleared
	assert.NotNil(t, cfg.MCP)
	assert.True(t, cfg.MCP.EnableClient)
	assert.Len(t, cfg.MCP.Servers, 0) // Explicitly cleared
}

func TestLoadConfig_DeepMerge_AppendRules(t *testing.T) {
	// Setup temp directories
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	configDir := filepath.Join(homeDir, ".config", "nano")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	projectDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	require.NoError(t, os.Chdir(projectDir))

	// Create user config with some rules
	userConfig := `
security:
  allow_rules:
    - "Bash(git *)"
    - "Bash(ls)"
  deny_rules:
    - "Bash(rm -rf /)"
`
	userConfigPath := filepath.Join(configDir, "config.yaml")
	require.NoError(t, os.WriteFile(userConfigPath, []byte(userConfig), 0644))

	// Point NANO_USER_CONFIG to our temp user config
	t.Setenv("NANO_USER_CONFIG", userConfigPath)

	// Create project config with additional rules (including duplicates)
	projectConfig := `
security:
  allow_rules:
    - "Bash(git *)"  # Duplicate - should be deduped
    - "Bash(npm *)"  # New
  deny_rules:
    - "Bash(sudo *)"
`
	require.NoError(t, os.MkdirAll(".nano", 0755))
	require.NoError(t, os.WriteFile(filepath.Join(".nano", "nano.yaml"), []byte(projectConfig), 0644))

	// Load config
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify rules were appended and deduplicated
	assert.NotNil(t, cfg.Security)
	assert.Len(t, cfg.Security.AllowRules, 3) // git, ls, npm (deduplicated)
	assert.Contains(t, cfg.Security.AllowRules, "Bash(git *)")
	assert.Contains(t, cfg.Security.AllowRules, "Bash(ls)")
	assert.Contains(t, cfg.Security.AllowRules, "Bash(npm *)")

	assert.Len(t, cfg.Security.DenyRules, 2) // rm, sudo
	assert.Contains(t, cfg.Security.DenyRules, "Bash(rm -rf /)")
	assert.Contains(t, cfg.Security.DenyRules, "Bash(sudo *)")
}

func TestLoadConfig_LegacyMode(t *testing.T) {
	// Setup temp directories
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	configDir := filepath.Join(homeDir, ".config", "nano")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	projectDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	require.NoError(t, os.Chdir(projectDir))

	// Point NANO_USER_CONFIG to our temp user config
	userConfigPath := filepath.Join(configDir, "config.yaml")
	t.Setenv("NANO_USER_CONFIG", userConfigPath)

	// Enable legacy mode
	t.Setenv("NANO_CONFIG_LEGACY_SHADOW", "1")

	// Create user config with hooks
	userConfig := `
api_key: "user-key"
security:
  allow_rules:
    - "Bash(git *)"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(userConfig), 0644))

	// Create project config (in legacy mode, this should completely replace user config)
	projectConfig := `
api_key: "project-key"
mcp:
  enable_client: true
`
	require.NoError(t, os.MkdirAll(".nano", 0755))
	require.NoError(t, os.WriteFile(filepath.Join(".nano", "nano.yaml"), []byte(projectConfig), 0644))

	// Load config
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// In legacy mode, project config wins entirely
	assert.Equal(t, "project-key", cfg.APIKey)
	assert.NotNil(t, cfg.MCP)
	assert.True(t, cfg.MCP.EnableClient)

	// User security rules should NOT be present (legacy single-file behavior)
	if cfg.Security != nil {
		assert.NotContains(t, cfg.Security.AllowRules, "Bash(git *)")
	}
}

func TestLoadConfig_DeepMerge_MCPServers(t *testing.T) {
	// Setup temp directories
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	configDir := filepath.Join(homeDir, ".config", "nano")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	projectDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	require.NoError(t, os.Chdir(projectDir))

	// Create user config with MCP servers
	userConfig := `
mcp:
  enable_client: true
  servers:
    - name: "filesystem"
      command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/home"]
    - name: "github"
      command: ["npx", "-y", "@modelcontextprotocol/server-github"]
`
	userConfigPath := filepath.Join(configDir, "config.yaml")
	require.NoError(t, os.WriteFile(userConfigPath, []byte(userConfig), 0644))

	// Point NANO_USER_CONFIG to our temp user config
	t.Setenv("NANO_USER_CONFIG", userConfigPath)

	// Create project config that updates filesystem and adds another server
	projectConfig := `
mcp:
  servers:
    - name: "filesystem"
      command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]  # Updated command
    - name: "project-specific"
      command: ["node", "./mcp-server.js"]
`
	require.NoError(t, os.MkdirAll(".nano", 0755))
	require.NoError(t, os.WriteFile(filepath.Join(".nano", "nano.yaml"), []byte(projectConfig), 0644))

	// Load config
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify MCP servers were merged
	assert.NotNil(t, cfg.MCP)
	assert.True(t, cfg.MCP.EnableClient) // From user config
	assert.Len(t, cfg.MCP.Servers, 3)    // filesystem (updated), github, project-specific

	// Build map by name
	serversByName := make(map[string]MCPServerConfig)
	for _, server := range cfg.MCP.Servers {
		serversByName[server.Name] = server
	}

	// Verify filesystem was updated
	fs := serversByName["filesystem"]
	assert.Len(t, fs.Command, 4)           // npx, -y, @..., /tmp
	assert.Contains(t, fs.Command, "/tmp") // Updated from project config
	assert.Equal(t, []string{"npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"}, fs.Command)

	// Verify github still exists
	assert.Contains(t, serversByName, "github")

	// Verify project-specific was added
	ps := serversByName["project-specific"]
	assert.Equal(t, []string{"node", "./mcp-server.js"}, ps.Command)
}
