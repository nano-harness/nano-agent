package config

// ToolCfg exposes tool-related configuration as a read-only interface.
type ToolCfg interface {
	GetPermissionMode() string
}

// SandboxCfg exposes sandbox-related configuration as a read-only interface.
type SandboxCfg interface {
	GetSandboxEnabled() bool
	GetBlockedPaths() []string
	GetAllowedPaths() []string
}

// LLMCfg exposes LLM/model-related configuration as a read-only interface.
type LLMCfg interface {
	GetModel() string
	GetAPIKey() string
	GetBaseURL() string
}

// configToolAdapter adapts *Config to ToolCfg.
type configToolAdapter struct{ c *Config }

func (a *configToolAdapter) GetPermissionMode() string {
	if a.c == nil {
		return ""
	}
	return a.c.PermissionMode
}

// configSandboxAdapter adapts *Config to SandboxCfg.
type configSandboxAdapter struct{ c *Config }

func (a *configSandboxAdapter) GetSandboxEnabled() bool {
	if a.c == nil || a.c.Sandbox == nil {
		return true
	}
	return a.c.Sandbox.Enabled
}
func (a *configSandboxAdapter) GetBlockedPaths() []string {
	if a.c == nil || a.c.Sandbox == nil {
		return nil
	}
	return a.c.Sandbox.BlockedPaths
}
func (a *configSandboxAdapter) GetAllowedPaths() []string {
	if a.c == nil || a.c.Sandbox == nil {
		return nil
	}
	return a.c.Sandbox.AllowedPaths
}

// configLLMAdapter adapts *Config to LLMCfg.
type configLLMAdapter struct{ c *Config }

func (a *configLLMAdapter) GetModel() string {
	if a.c == nil {
		return ""
	}
	return a.c.Model
}
func (a *configLLMAdapter) GetAPIKey() string {
	if a.c == nil {
		return ""
	}
	return a.c.APIKey
}
func (a *configLLMAdapter) GetBaseURL() string {
	if a.c == nil {
		return ""
	}
	return a.c.BaseURL
}

// AsToolCfg returns the Config as a ToolCfg interface.
func (c *Config) AsToolCfg() ToolCfg {
	return &configToolAdapter{c: c}
}

// AsSandboxCfg returns the Config as a SandboxCfg interface.
func (c *Config) AsSandboxCfg() SandboxCfg {
	return &configSandboxAdapter{c: c}
}

// AsLLMCfg returns the Config as a LLMCfg interface.
func (c *Config) AsLLMCfg() LLMCfg {
	return &configLLMAdapter{c: c}
}
