package config

import (
	"os"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// OrchestratorProfile detects an embedding orchestrator and injects runtime configuration.
// Profiles run during LoadConfig after file/env overrides and before managed settings,
// so enterprise managed settings can still override injected values. A profile should
// typically Detect on well-known environment variables and Inject MCP servers, skills,
// or prompt settings without requiring the orchestrator to write .nano/nano.yaml glue.
type OrchestratorProfile interface {
	Detect() bool
	Inject(*Config) error
}

func applyOrchestratorProfiles(cfg *Config) {
	for _, profile := range []OrchestratorProfile{
		symphonyProfile{},
	} {
		if !profile.Detect() {
			continue
		}
		if err := profile.Inject(cfg); err != nil {
			logger.Warnf("orchestrator profile injection failed: %v", err)
		}
	}
}

type symphonyProfile struct{}

func (symphonyProfile) Detect() bool {
	return os.Getenv("SYMPHONY_MCP_URL") != "" && os.Getenv("SYMPHONY_TOKEN") != ""
}

func (symphonyProfile) Inject(cfg *Config) error {
	if cfg.MCP == nil {
		cfg.MCP = &MCPConfig{}
	}
	cfg.EnableMCP = true
	cfg.MCP.EnableClient = true
	upsertMCPServer(&cfg.MCP.Servers, MCPServerConfig{
		Name:      "symphony",
		Transport: "streamable",
		URL:       os.Getenv("SYMPHONY_MCP_URL"),
		Headers: map[string]string{
			"X-Symphony-Token": os.Getenv("SYMPHONY_TOKEN"),
		},
		Enabled: true,
	})

	if cfg.Skills == nil {
		cfg.Skills = &SkillsConfig{}
	}
	cfg.Skills.Enabled = true
	if !containsString(cfg.Skills.AutoActivate, "nano-symphony") {
		cfg.Skills.AutoActivate = append(cfg.Skills.AutoActivate, "nano-symphony")
	}

	logger.Infof("Applied nano-symphony orchestrator profile")
	return nil
}

func upsertMCPServer(servers *[]MCPServerConfig, server MCPServerConfig) {
	for i := range *servers {
		if strings.EqualFold((*servers)[i].Name, server.Name) {
			(*servers)[i] = server
			return
		}
	}
	*servers = append(*servers, server)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
