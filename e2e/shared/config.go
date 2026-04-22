//go:build e2e

package shared

import (
	"github.com/nano-harness/nano-agent/pkg/config"
)

// NewTestConfig builds a hardened, isolated config for e2e tests.
// It disables external integrations (OSS, MCP, Skills) and autodetection
// to prevent tests from spawning subprocesses or accessing external resources.
func NewTestConfig(baseURL, workDir string, isDaemon bool) *config.Config {
	cfg := config.DefaultConfig()
	cfg.APIKey = "e2e-test-key"
	cfg.BaseURL = baseURL
	cfg.Model = "mock-gpt-4"

	// Turn config: using implicit completion based on finish_reason
	cfg.Turn = &config.TurnExecutionConfig{}

	// Loop detection enabled
	cfg.LoopDetection = &config.LoopDetectionConfig{Enabled: true}

	// UserInfo: static test configuration without autodetection
	cfg.UserInfo = &config.UserInfoConfig{
		Timezone:           "UTC",
		OperatingSystem:    "test",
		Shell:              "/bin/sh",
		Editor:             "nano",
		Language:           "en",
		ProgrammingTools:   map[string]string{},
		WorkingDirectory:   workDir,
		AutoDetectUserInfo: false, // Critical: prevents subprocess spawning
	}

	// Disable external integrations
	if cfg.OSS != nil {
		cfg.OSS.Enabled = false
	}
	cfg.EnableMCP = false
	if cfg.Skills != nil {
		cfg.Skills.Enabled = false
	}

	// 注入短退避 CircuitBreaker 配置，避免测试被长重试拖死
	if cfg.Advanced == nil {
		cfg.Advanced = &config.AdvancedConfig{}
	}
	cfg.Advanced.CircuitBreaker = &config.CircuitBreakerAdvConfig{
		MaxRetries:    2,
		BaseDelayMs:   50,
		MaxDelayMs:    200,
		OpenTimeoutMs: 500,
	}

	// Daemon-specific configuration
	// Note: Daemon is handled at startup, not via config.Daemon.Enabled
	// We just ensure the daemon config exists for tests if needed
	if isDaemon && cfg.Daemon != nil {
		cfg.Daemon.EnableCORS = true
		cfg.Daemon.APIKey = "" // No API key for tests
	}

	return cfg
}

// NewTestConfigWithFork builds a test config with custom fork concurrency settings.
// This is specifically for testing parallel sub-agent execution with controlled concurrency.
func NewTestConfigWithFork(baseURL, workDir string, isDaemon bool, maxConcurrent int) *config.Config {
	cfg := NewTestConfig(baseURL, workDir, isDaemon)

	// Configure fork settings
	if cfg.Advanced == nil {
		cfg.Advanced = &config.AdvancedConfig{}
	}
	if cfg.Advanced.Fork == nil {
		cfg.Advanced.Fork = &config.ForkAdvConfig{}
	}
	cfg.Advanced.Fork.MaxConcurrent = maxConcurrent
	// Keep default MaxDepth of 3 unless specifically testing depth limits

	return cfg
}
