package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nano-harness/nano-agent/pkg/config/merger"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"gopkg.in/yaml.v2"
)

// configLayer represents a single configuration layer with its source
type configLayer struct {
	Name   string
	Path   string
	Data   map[string]interface{}
	Exists bool
}

// loadConfigLayers loads all configuration layers in priority order
func loadConfigLayers(explicitPath string) ([]configLayer, error) {
	layers := []configLayer{}

	// Layer 1: Defaults (embedded in DefaultConfig)
	// We'll handle this by loading DefaultConfig first and converting it to map

	// Layer 2: User config (~/.config/nano/config.yaml)
	homeDir, err := os.UserHomeDir()
	if err == nil {
		userPath := filepath.Join(homeDir, ".config", "nano", "config.yaml")
		data, err := loadConfigFile(userPath)
		if err != nil {
			// Only ignore if file doesn't exist
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to load user config %q: %w", userPath, err)
			}
		} else {
			layers = append(layers, configLayer{
				Name:   "user",
				Path:   userPath,
				Data:   data,
				Exists: true,
			})
		}
	}

	// Layer 3: Project config (.nano.yaml)
	projectPath := ".nano.yaml"
	data, err := loadConfigFile(projectPath)
	if err != nil {
		// Only ignore if file doesn't exist
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load project config %q: %w", projectPath, err)
		}
	} else {
		layers = append(layers, configLayer{
			Name:   "project",
			Path:   projectPath,
			Data:   data,
			Exists: true,
		})
	}

	// Layer 4: Explicit path (if provided via --config)
	if explicitPath != "" {
		data, err := loadConfigFile(explicitPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load explicit config %q: %w", explicitPath, err)
		}
		layers = append(layers, configLayer{
			Name:   "explicit",
			Path:   explicitPath,
			Data:   data,
			Exists: true,
		})
	}

	return layers, nil
}

// loadConfigFile reads and parses a YAML config file
func loadConfigFile(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Expand ${env:VAR} references
	expanded, err := expandConfigEnvRefs(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to expand env references: %w", err)
	}

	// Expand $VAR references
	expanded = os.ExpandEnv(expanded)

	// Unmarshal to map - yaml.v2 produces map[interface{}]interface{}
	var rawResult interface{}
	if err := yaml.Unmarshal([]byte(expanded), &rawResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML: %w", err)
	}

	// Convert to map[string]interface{} recursively
	result, err := convertToStringMap(rawResult)
	if err != nil {
		return nil, fmt.Errorf("failed to convert YAML structure: %w", err)
	}

	return result, nil
}

// convertToStringMap recursively converts map[interface{}]interface{} to map[string]interface{}
// This is necessary because yaml.v2 unmarshals to map[interface{}]interface{}
func convertToStringMap(v interface{}) (map[string]interface{}, error) {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		result := make(map[string]interface{})
		for k, v := range val {
			strKey, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("non-string key in map: %v (%T)", k, k)
			}
			converted, err := convertValue(v)
			if err != nil {
				return nil, err
			}
			result[strKey] = converted
		}
		return result, nil
	case map[string]interface{}:
		// Already correct type, but recurse to convert nested structures
		result := make(map[string]interface{})
		for k, v := range val {
			converted, err := convertValue(v)
			if err != nil {
				return nil, err
			}
			result[k] = converted
		}
		return result, nil
	default:
		return nil, fmt.Errorf("expected map, got %T", v)
	}
}

// convertValue recursively converts any value from YAML types to standard types
func convertValue(v interface{}) (interface{}, error) {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		return convertToStringMap(val)
	case map[string]interface{}:
		return convertToStringMap(val)
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			converted, err := convertValue(item)
			if err != nil {
				return nil, err
			}
			result[i] = converted
		}
		return result, nil
	default:
		// Primitives (string, int, bool, etc.) are already fine
		return v, nil
	}
}

// buildMergePolicies constructs merge policies for known config fields
func buildMergePolicies() map[string]merger.MergePolicy {
	policies := map[string]merger.MergePolicy{
		// SecurityConfig fields
		"security.allow_rules": {Strategy: merger.StrategyAppend},
		"security.deny_rules":  {Strategy: merger.StrategyAppend},
		"security.hooks":       {Strategy: merger.StrategyMergeByKey, KeyField: "name"},

		// MCP fields
		"mcp.servers": {Strategy: merger.StrategyMergeByKey, KeyField: "name"},

		// Additional append fields
		"sensitive_read_paths":    {Strategy: merger.StrategyAppend},
		"arbitrary_exec_commands": {Strategy: merger.StrategyAppend},
		"allowed_commands":        {Strategy: merger.StrategyAppend},
		"blocked_commands":        {Strategy: merger.StrategyAppend},
		"allowed_env_vars":        {Strategy: merger.StrategyAppend},
		"blocked_env_vars":        {Strategy: merger.StrategyAppend},
		"enabled_tools":           {Strategy: merger.StrategyAppend},
		"disabled_tools":          {Strategy: merger.StrategyAppend},
		"allowed_rules":           {Strategy: merger.StrategyAppend},

		// Fallbacks - using append for now, though the problem statement mentions merge_by_key
		// The current schema has fallbacks as []string, not []map
		"fallbacks": {Strategy: merger.StrategyAppend},

		// Model routing fallbacks
		"model_routing.fallbacks": {Strategy: merger.StrategyMergeByKey, KeyField: "name"},

		// Providers block - merge by provider name
		// Note: providers is a map[string]ProviderBlock, so it will merge naturally as nested maps

		// Image generator providers
		"image_generator.providers": {Strategy: merger.StrategyMergeByKey, KeyField: "provider"},
	}

	return policies
}

// loadConfigWithMerge loads configuration using deep merge strategy
func loadConfigWithMerge(configPath string) (*Config, error) {
	// Check for legacy mode
	if os.Getenv("NANO_CONFIG_LEGACY_SHADOW") == "1" {
		logger.Warnf("NANO_CONFIG_LEGACY_SHADOW=1: using legacy single-file config loading")
		return loadConfigLegacy(configPath)
	}

	// Start with defaults
	cfg := DefaultConfig()

	// Load all config layers
	layers, err := loadConfigLayers(configPath)
	if err != nil {
		return nil, err
	}

	if len(layers) > 0 {
		// Build merge policies
		policies := buildMergePolicies()

		// Prepare layer maps for merging
		layerMaps := make([]map[string]interface{}, len(layers))
		for i, layer := range layers {
			layerMaps[i] = layer.Data
			logger.Debugf("config layer %d: %s (%s)", i, layer.Name, layer.Path)
		}

		// Merge all layers
		merged, err := merger.Merge(layerMaps, policies)
		if err != nil {
			return nil, fmt.Errorf("failed to merge config layers: %w", err)
		}

		// Unmarshal merged result into cfg
		// We need to convert the merged map back to YAML and then unmarshal
		mergedYAML, err := yaml.Marshal(merged)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal merged config: %w", err)
		}

		if err := yaml.Unmarshal(mergedYAML, cfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal merged config: %w", err)
		}
	}

	return cfg, nil
}

// loadConfigLegacy implements the old single-file selection behavior
func loadConfigLegacy(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	// If configPath is empty, use default search paths with priority order
	if configPath == "" {
		// Priority order: project .nano.yaml > global ~/.config/nano/config.yaml
		if _, err := os.Stat(".nano.yaml"); err == nil {
			configPath = ".nano.yaml"
		} else {
			// Check global config
			homeDir, err := os.UserHomeDir()
			if err == nil {
				globalPath := filepath.Join(homeDir, ".config", "nano", "config.yaml")
				if _, err := os.Stat(globalPath); err == nil {
					configPath = globalPath
				}
			}
		}
	}

	// If a config file is found, read and unmarshal it
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		expanded, err := expandConfigEnvRefs(string(data))
		if err != nil {
			return nil, fmt.Errorf("failed to expand config environment variables: %w", err)
		}
		expanded = os.ExpandEnv(expanded)
		err = yaml.Unmarshal([]byte(expanded), cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	return cfg, nil
}
