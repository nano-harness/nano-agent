package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_HooksStopCommand(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	require.NoError(t, os.Chdir(tempDir))

	configYAML := `
hooks:
  Stop:
    - matcher: "*"
      command: echo stop
      timeout: 30
`
	require.NoError(t, os.MkdirAll(".nano", 0755))
	require.NoError(t, os.WriteFile(filepath.Join(".nano", "nano.yaml"), []byte(configYAML), 0644))

	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.Hooks)
	require.NotNil(t, cfg.Hooks.Events)
	require.Len(t, cfg.Hooks.Events["Stop"], 1)
	assert.Equal(t, "*", cfg.Hooks.Events["Stop"][0].Matcher)
	assert.Equal(t, "echo stop", cfg.Hooks.Events["Stop"][0].Command)
	assert.Equal(t, 30, cfg.Hooks.Events["Stop"][0].Timeout)
}

func TestLoadConfig_StrictRejectsLegacySecurityHooks(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	require.NoError(t, os.Chdir(tempDir))

	legacyYAML := `
security:
  hooks:
    - name: legacy-hook
      event: pre_tool_use
      pattern: "*"
      type: http
      enabled: true
`
	require.NoError(t, os.MkdirAll(".nano", 0755))
	require.NoError(t, os.WriteFile(filepath.Join(".nano", "nano.yaml"), []byte(legacyYAML), 0644))

	_, err := LoadConfig("")
	require.Error(t, err)
}

func TestLoadConfig_StrictRejectsLegacyHookFields(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	require.NoError(t, os.Chdir(tempDir))

	legacyFields := `
hooks:
  Stop:
    - matcher: "*"
      command: echo stop
      type: http
`
	require.NoError(t, os.MkdirAll(".nano", 0755))
	require.NoError(t, os.WriteFile(filepath.Join(".nano", "nano.yaml"), []byte(legacyFields), 0644))

	_, err := LoadConfig("")
	require.Error(t, err)
}
