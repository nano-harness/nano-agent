package agentprofile

import (
	"os"
	"path/filepath"
	"strings"
)

// LoadPluginAgents loads agent profiles from a plugin root directory.
// Plugin agents are stored under <pluginRoot>/agents/*.md and are namespaced
// as "plugin:<name>".
func LoadPluginAgents(pluginRoot string) (map[string]AgentProfile, error) {
	profiles := make(map[string]AgentProfile)
	agentsDir := filepath.Join(pluginRoot, "agents")

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return profiles, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".md" && ext != ".yaml" && ext != ".yml" && ext != ".json" {
			continue
		}

		path := filepath.Join(agentsDir, entry.Name())
		profile, err := readProfile(path)
		if err != nil || profile.Name == "" {
			continue
		}
		profile.Plugin = "plugin:" + profile.Name
		profiles[profile.Name] = profile
	}

	return profiles, nil
}

// RegisterPluginAgents loads plugin agents from a root and registers them with a resolver.
func RegisterPluginAgents(resolver *Resolver, pluginRoot string) error {
	profiles, err := LoadPluginAgents(pluginRoot)
	if err != nil {
		return err
	}
	for name, profile := range profiles {
		resolver.RegisterPlugin(name, profile)
	}
	return nil
}
