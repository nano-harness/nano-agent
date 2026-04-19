package workspace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGitConfigManager_DefaultConfig(t *testing.T) {
	manager := NewGitConfigManager(nil)
	config := manager.GetConfig()

	assert.Equal(t, "origin", config.DefaultRemote)
	assert.Equal(t, 30*time.Second, config.CommandTimeout)
	assert.Equal(t, 1024*1024, config.MaxOutputSize)
	assert.True(t, config.EnableCache)
	assert.Equal(t, 5*time.Second, config.CacheExpiration)
	assert.Contains(t, config.AllowedCommands, "status")
	assert.Contains(t, config.AllowedRemoteURLs, "https://github.com/*")
}

func TestGitConfigManager_CustomConfig(t *testing.T) {
	userConfig := map[string]interface{}{
		"git": map[string]interface{}{
			"command_timeout":     "15s",
			"max_output_size":     512,
			"enable_cache":        false,
			"cache_expiration":    "10s",
			"default_remote":      "upstream",
			"allowed_commands":    []interface{}{"status", "log"},
			"allowed_remote_urls": []interface{}{"https://custom.com/*"},
		},
	}

	manager := NewGitConfigManager(userConfig)
	config := manager.GetConfig()

	assert.Equal(t, "upstream", config.DefaultRemote)
	assert.Equal(t, 15*time.Second, config.CommandTimeout)
	assert.Equal(t, 512, config.MaxOutputSize)
	assert.False(t, config.EnableCache)
	assert.Equal(t, 10*time.Second, config.CacheExpiration)
	assert.Equal(t, []string{"status", "log"}, config.AllowedCommands)
	assert.Equal(t, []string{"https://custom.com/*"}, config.AllowedRemoteURLs)
}

func TestGitConfigManager_ValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      map[string]interface{}
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "valid config",
			config:      nil,
			shouldError: false,
		},
		{
			name: "invalid timeout",
			config: map[string]interface{}{
				"git": map[string]interface{}{
					"command_timeout": "-1s",
				},
			},
			shouldError: true,
			errorMsg:    "command timeout must be positive",
		},
		{
			name: "invalid max output size",
			config: map[string]interface{}{
				"git": map[string]interface{}{
					"max_output_size": -1,
				},
			},
			shouldError: true,
			errorMsg:    "max output size must be positive",
		},
		{
			name: "invalid cache expiration",
			config: map[string]interface{}{
				"git": map[string]interface{}{
					"cache_expiration": "-1s",
				},
			},
			shouldError: true,
			errorMsg:    "cache expiration must be positive",
		},
		{
			name: "empty allowed commands",
			config: map[string]interface{}{
				"git": map[string]interface{}{
					"allowed_commands": []interface{}{},
				},
			},
			shouldError: true,
			errorMsg:    "allowed commands cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewGitConfigManager(tt.config)
			err := manager.ValidateConfig()

			if tt.shouldError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGitConfigManager_UpdateConfig(t *testing.T) {
	manager := NewGitConfigManager(nil)

	// Test valid update
	updates := map[string]interface{}{
		"command_timeout": "20s",
		"enable_cache":    false,
	}

	err := manager.UpdateConfig(updates)
	assert.NoError(t, err)

	config := manager.GetConfig()
	assert.Equal(t, 20*time.Second, config.CommandTimeout)
	assert.False(t, config.EnableCache)

	// Test invalid update
	invalidUpdates := map[string]interface{}{
		"command_timeout": "-5s",
	}

	err = manager.UpdateConfig(invalidUpdates)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid configuration update")

	// Config should remain unchanged after invalid update
	config = manager.GetConfig()
	assert.Equal(t, 20*time.Second, config.CommandTimeout)
}

func TestGitConfigManager_PartialConfig(t *testing.T) {
	// Test with partial configuration
	userConfig := map[string]interface{}{
		"git": map[string]interface{}{
			"command_timeout": "25s",
			// Other fields should use defaults
		},
	}

	manager := NewGitConfigManager(userConfig)
	config := manager.GetConfig()

	// Updated field
	assert.Equal(t, 25*time.Second, config.CommandTimeout)

	// Default fields
	assert.Equal(t, "origin", config.DefaultRemote)
	assert.Equal(t, 1024*1024, config.MaxOutputSize)
	assert.True(t, config.EnableCache)
}

func TestGitConfigManager_InvalidDurations(t *testing.T) {
	// Test with invalid duration strings
	userConfig := map[string]interface{}{
		"git": map[string]interface{}{
			"command_timeout":  "invalid",
			"cache_expiration": "also_invalid",
		},
	}

	manager := NewGitConfigManager(userConfig)
	config := manager.GetConfig()

	// Should use defaults when parsing fails
	assert.Equal(t, 30*time.Second, config.CommandTimeout)
	assert.Equal(t, 5*time.Second, config.CacheExpiration)
}

func TestGitConfigManager_TypeConversions(t *testing.T) {
	// Test with wrong types that should be ignored
	userConfig := map[string]interface{}{
		"git": map[string]interface{}{
			"command_timeout":     123,            // Should be string
			"max_output_size":     "not_a_number", // Should be int
			"enable_cache":        "not_a_bool",   // Should be bool
			"allowed_commands":    "not_an_array", // Should be array
			"allowed_remote_urls": 123,            // Should be array
		},
	}

	manager := NewGitConfigManager(userConfig)
	config := manager.GetConfig()

	// Should use defaults when type conversion fails
	assert.Equal(t, 30*time.Second, config.CommandTimeout)
	assert.Equal(t, 1024*1024, config.MaxOutputSize)
	assert.True(t, config.EnableCache)
	assert.Contains(t, config.AllowedCommands, "status")
	assert.Contains(t, config.AllowedRemoteURLs, "https://github.com/*")
}
