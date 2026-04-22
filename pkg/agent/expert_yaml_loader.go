package agent

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// SubAgentConfig represents a sub-agent definition from .nano.yaml
// This mirrors the structure in .nano.yaml.example
type SubAgentConfig struct {
	AgentName      string   `yaml:"agent_name"`
	SystemPrompt   string   `yaml:"system_prompt"`
	WhenToUse      string   `yaml:"when_to_use"`
	Model          string   `yaml:"model"`
	ModelBaseURL   string   `yaml:"model_base_url"`
	ModelAPIKey    string   `yaml:"model_api_key"`
	AllowedTools   []string `yaml:"allowed_tools"`
	AutoSaveMemory bool     `yaml:"auto_save_memory"`
	Enabled        bool     `yaml:"enabled"`
}

// LoadYAMLSubAgentsAsExperts converts sub_agents from config into Expert definitions
// Names are automatically converted to kebab-case
func LoadYAMLSubAgentsAsExperts(registry *ExpertRegistry, subAgents []SubAgentConfig) error {
	for _, sa := range subAgents {
		if !sa.Enabled {
			logger.Infof("Skipping disabled YAML sub-agent: %s", sa.AgentName)
			continue
		}

		// Convert name to kebab-case
		expertName := toKebabCase(sa.AgentName)
		if expertName == "" || !isValidExpertName(expertName) {
			logger.Warnf("Skipping YAML sub-agent %q: cannot convert to valid kebab-case name", sa.AgentName)
			continue
		}

		// Create expert
		expert := &Expert{
			Name:           expertName,
			DisplayName:    sa.AgentName, // Keep original name as display name
			Description:    sa.WhenToUse,
			Source:         "yaml",
			SystemPrompt:   sa.SystemPrompt,
			QueryTemplate:  "${request}",
			Model:          sa.Model,
			Temperature:    0,  // Not specified in YAML, use default
			MaxTurns:       20, // Default
			MaxTimeMinutes: 10, // Default
			AllowedTools:   sa.AllowedTools,
			OutputName:     "result",
			InputSchema: &ExpertInputSchema{
				Type: "object",
				Properties: map[string]*ExpertPropertySchema{
					"request": {
						Type:        "string",
						Description: "The task request for this expert",
					},
				},
				Required: []string{"request"},
			},
		}

		// Set defaults
		if len(expert.AllowedTools) == 0 {
			expert.AllowedTools = []string{"*"}
		}

		if err := registry.Register(expert); err != nil {
			logger.Warnf("Failed to register YAML sub-agent %q as expert %q: %v", sa.AgentName, expertName, err)
			continue
		}

		logger.Infof("Loaded YAML sub-agent %q as expert %q", sa.AgentName, expertName)
	}

	return nil
}

// toKebabCase converts various naming conventions to kebab-case
// Examples:
//   - "coder" -> "coder"
//   - "myAgent" -> "my-agent" (camelCase)
//   - "my_agent" -> "my-agent" (snake_case)
//   - "MyAgent" -> "my-agent" (PascalCase)
//   - "穿越小说家" -> "" (non-ASCII, cannot convert)
func toKebabCase(s string) string {
	if s == "" {
		return ""
	}

	// Check if string contains only ASCII characters
	for _, r := range s {
		if r > unicode.MaxASCII {
			return "" // Non-ASCII, cannot convert
		}
	}

	var result strings.Builder
	var prevLower bool
	var prevUnderscore bool

	for i, r := range s {
		if r == '_' || r == '-' {
			// Convert underscores/hyphens to hyphen, avoid double hyphens
			if result.Len() > 0 && !prevUnderscore {
				result.WriteRune('-')
				prevUnderscore = true
			}
			prevLower = false
			continue
		}

		prevUnderscore = false

		if unicode.IsUpper(r) {
			// Add hyphen before uppercase if:
			// 1. Not at start
			// 2. Previous char was lowercase (camelCase boundary)
			if i > 0 && prevLower {
				result.WriteRune('-')
			}
			result.WriteRune(unicode.ToLower(r))
			prevLower = false
		} else if unicode.IsLower(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
			prevLower = unicode.IsLower(r)
		} else {
			// Other characters: skip
			prevLower = false
		}
	}

	// Clean up result
	kebab := result.String()
	kebab = strings.Trim(kebab, "-")
	kebab = regexp.MustCompile(`-+`).ReplaceAllString(kebab, "-") // Remove consecutive hyphens

	return kebab
}

// LoadConfigSubAgents is a helper that loads sub_agents from a Config object
// This function will be called from agent.go during initialization
func LoadConfigSubAgents(registry *ExpertRegistry, cfg *config.Config) error {
	// Note: SubAgents field may need to be added to config.Config
	// For now, this is a placeholder that will work once the field is added

	// TODO: Once config.Config has a SubAgents []SubAgentConfig field, use it here:
	// return LoadYAMLSubAgentsAsExperts(registry, cfg.SubAgents)

	// For now, return nil (no sub-agents to load from config)
	return nil
}
