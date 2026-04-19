package workspace

import (
	"fmt"
	"time"
)

// GitConfigManager handles Git configuration management
type GitConfigManager struct {
	config *GitConfig
}

// NewGitConfigManager creates a new configuration manager
func NewGitConfigManager(userConfig map[string]interface{}) *GitConfigManager {
	config := getDefaultGitConfig()

	if userConfig != nil {
		applyUserConfig(config, userConfig)
	}

	return &GitConfigManager{
		config: config,
	}
}

// GetConfig returns the current configuration
func (m *GitConfigManager) GetConfig() *GitConfig {
	return m.config
}

// ValidateConfig validates the configuration
func (m *GitConfigManager) ValidateConfig() error {
	if m.config == nil {
		return fmt.Errorf("configuration is nil")
	}

	if m.config.CommandTimeout <= 0 {
		return fmt.Errorf("command timeout must be positive")
	}

	if m.config.MaxOutputSize <= 0 {
		return fmt.Errorf("max output size must be positive")
	}

	if m.config.CacheExpiration <= 0 {
		return fmt.Errorf("cache expiration must be positive")
	}

	if len(m.config.AllowedCommands) == 0 {
		return fmt.Errorf("allowed commands cannot be empty")
	}

	return nil
}

// UpdateConfig updates specific configuration values
func (m *GitConfigManager) UpdateConfig(updates map[string]interface{}) error {
	if updates == nil {
		return nil
	}

	// Create a copy of current config
	newConfig := *m.config

	// Apply updates
	applyUserConfig(&newConfig, map[string]interface{}{"git": updates})

	// Validate new config
	tempManager := &GitConfigManager{config: &newConfig}
	if err := tempManager.ValidateConfig(); err != nil {
		return fmt.Errorf("invalid configuration update: %w", err)
	}

	// Apply if valid
	m.config = &newConfig
	return nil
}

// getDefaultGitConfig returns the default Git configuration
func getDefaultGitConfig() *GitConfig {
	return &GitConfig{
		DefaultRemote:   "origin",
		CommandTimeout:  30 * time.Second,
		MaxOutputSize:   1024 * 1024, // 1MB
		EnableCache:     true,
		CacheExpiration: 5 * time.Second,
		AllowedCommands: []string{
			"status", "add", "commit", "push", "pull",
			"branch", "checkout", "merge", "log", "init", "clone", "remote",
		},
		AllowedRemoteURLs: []string{
			"https://github.com/*",
			"https://gitlab.com/*",
			"git@github.com:*",
			"git@gitlab.com:*",
		},
	}
}

// applyUserConfig applies user configuration to the Git config
func applyUserConfig(config *GitConfig, userConfig map[string]interface{}) {
	// Handle direct configuration (for backward compatibility)
	if timeout, ok := userConfig["command_timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			config.CommandTimeout = d
		}
	}

	if maxSize, ok := userConfig["max_output_size"].(int); ok {
		config.MaxOutputSize = maxSize
	}

	if enableCache, ok := userConfig["enable_cache"].(bool); ok {
		config.EnableCache = enableCache
	}

	if cacheExpiration, ok := userConfig["cache_expiration"].(string); ok {
		if d, err := time.ParseDuration(cacheExpiration); err == nil {
			config.CacheExpiration = d
		}
	}

	if defaultRemote, ok := userConfig["default_remote"].(string); ok {
		config.DefaultRemote = defaultRemote
	}

	if allowedCommands, ok := userConfig["allowed_commands"].([]interface{}); ok {
		commands := make([]string, 0, len(allowedCommands))
		for _, cmd := range allowedCommands {
			if cmdStr, ok := cmd.(string); ok {
				commands = append(commands, cmdStr)
			}
		}
		config.AllowedCommands = commands
	}

	if allowedURLs, ok := userConfig["allowed_remote_urls"].([]interface{}); ok {
		urls := make([]string, 0, len(allowedURLs))
		for _, url := range allowedURLs {
			if urlStr, ok := url.(string); ok {
				urls = append(urls, urlStr)
			}
		}
		if len(urls) > 0 {
			config.AllowedRemoteURLs = urls
		}
	}

	// Handle nested configuration under "git" key
	if cfg, ok := userConfig["git"]; ok {
		if gitCfg, ok := cfg.(map[string]interface{}); ok {
			if timeout, ok := gitCfg["command_timeout"].(string); ok {
				if d, err := time.ParseDuration(timeout); err == nil {
					config.CommandTimeout = d
				}
			}

			if maxSize, ok := gitCfg["max_output_size"].(int); ok {
				config.MaxOutputSize = maxSize
			}

			if enableCache, ok := gitCfg["enable_cache"].(bool); ok {
				config.EnableCache = enableCache
			}

			if cacheExpiration, ok := gitCfg["cache_expiration"].(string); ok {
				if d, err := time.ParseDuration(cacheExpiration); err == nil {
					config.CacheExpiration = d
				}
			}

			if defaultRemote, ok := gitCfg["default_remote"].(string); ok {
				config.DefaultRemote = defaultRemote
			}

			if allowedCommands, ok := gitCfg["allowed_commands"].([]interface{}); ok {
				commands := make([]string, 0, len(allowedCommands))
				for _, cmd := range allowedCommands {
					if cmdStr, ok := cmd.(string); ok {
						commands = append(commands, cmdStr)
					}
				}
				config.AllowedCommands = commands
			}

			if allowedURLs, ok := gitCfg["allowed_remote_urls"].([]interface{}); ok {
				urls := make([]string, 0, len(allowedURLs))
				for _, url := range allowedURLs {
					if urlStr, ok := url.(string); ok {
						urls = append(urls, urlStr)
					}
				}
				if len(urls) > 0 {
					config.AllowedRemoteURLs = urls
				}
			}
		}
	}
}
