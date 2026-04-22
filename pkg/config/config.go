package config //nolint:revive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/joho/godotenv"
	"gopkg.in/yaml.v2"
)

// LoopDetectionConfig defines loop detection behavior
type LoopDetectionConfig struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
}

type ToolRetryPolicy struct { //nolint:revive
	MaxRetries        int           `mapstructure:"max_retries" yaml:"max_retries"`
	RetryDelay        time.Duration `mapstructure:"retry_delay" yaml:"retry_delay"`
	BackoffMultiplier float64       `mapstructure:"backoff_multiplier" yaml:"backoff_multiplier"`
	MaxDelay          time.Duration `mapstructure:"max_delay" yaml:"max_delay"`
	JitterRatio       float64       `mapstructure:"jitter_ratio" yaml:"jitter_ratio"`
}

type ToolRecoveryConfig struct { //nolint:revive
	Default ToolRetryPolicy            `mapstructure:"default" yaml:"default"`
	PerTool map[string]ToolRetryPolicy `mapstructure:"per_tool" yaml:"per_tool"`
}

// WebSearchAPIKeys holds API keys for web search engines
type WebSearchAPIKeys struct {
	Serper     string `mapstructure:"serper" yaml:"serper"`
	Tavily     string `mapstructure:"tavily" yaml:"tavily"`
	DuckDuckGo string `mapstructure:"duckduckgo" yaml:"duckduckgo"` // DuckDuckGo doesn't require API key but kept for consistency
}

// ImageGeneratorProviderConfig holds configuration for a single image generation provider
type ImageGeneratorProviderConfig struct {
	Provider   string `mapstructure:"provider" yaml:"provider"` // Provider name: "openrouter", "seedream", etc.
	Model      string `mapstructure:"model" yaml:"model"`
	EndpointID string `mapstructure:"endpoint_id" yaml:"endpoint_id"` // VolcEngine ARK endpoint ID for Seedream
	APIKey     string `mapstructure:"api_key" yaml:"api_key"`
	BaseURL    string `mapstructure:"base_url" yaml:"base_url"`
	Enabled    bool   `mapstructure:"enabled" yaml:"enabled"`
	// Provider-specific configuration
	Config map[string]interface{} `mapstructure:"config" yaml:"config"`
}

// ImageGeneratorConfig holds simplified image generation configuration
type ImageGeneratorConfig struct {
	// List of configured providers (required)
	Providers []ImageGeneratorProviderConfig `mapstructure:"providers" yaml:"providers"`
}

// GetProvider returns the configuration for a specific provider
func (c *ImageGeneratorConfig) GetProvider(providerName string) (*ImageGeneratorProviderConfig, bool) {
	if c == nil {
		return nil, false
	}

	// Check if provider has valid configuration (API key required and provider enabled)
	for _, provider := range c.Providers {
		if provider.Provider == providerName && provider.APIKey != "" && provider.Enabled {
			return &provider, true
		}
	}

	return nil, false
}

// GetDefaultProvider returns the default provider configuration
func (c *ImageGeneratorConfig) GetDefaultProvider() (*ImageGeneratorProviderConfig, bool) {
	if c == nil {
		return nil, false
	}

	// Return first provider with valid configuration (API key required and provider enabled)
	for _, provider := range c.Providers {
		if provider.APIKey != "" && provider.Enabled {
			return &provider, true
		}
	}

	return nil, false
}

// GetEnabledProviders returns all enabled providers
func (c *ImageGeneratorConfig) GetEnabledProviders() []ImageGeneratorProviderConfig {
	if c == nil {
		return nil
	}

	var enabled []ImageGeneratorProviderConfig

	// Add all providers with valid configuration (API key required and provider enabled)
	for _, provider := range c.Providers {
		if provider.APIKey != "" && provider.Enabled {
			enabled = append(enabled, provider)
		}
	}

	return enabled
}

// SandboxConfig controls process-level and path-level sandboxing.
//
// Shell commands (run_shell_command) are wrapped using OS-native tools:
//   - Linux:  Bubblewrap (bwrap)
//   - macOS:  sandbox-exec
//
// Filesystem tools (read_file, write_file, …) use AllowedPaths / BlockedPaths /
// ReadOnlyPaths for Go-level path access control.
type SandboxConfig struct {
	// Enabled activates process-level sandboxing for shell commands.
	// Default: true (secure-by-default).
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// NetworkAccess controls whether sandboxed shell commands can use the network.
	// Default: true (network allowed). Set to false to isolate network.
	NetworkAccess bool `mapstructure:"network_access" yaml:"network_access"`

	// AllowedPaths is an optional whitelist for filesystem tools.
	// When non-empty, read_file / write_file / etc. may only access paths
	// that match at least one entry.  Shell commands are not affected (use
	// ExtraReadOnlyPaths / ExtraWritablePaths for bwrap/sandbox-exec visibility).
	AllowedPaths []string `mapstructure:"allowed_paths" yaml:"allowed_paths"`

	// BlockedPaths lists paths that filesystem tools are never allowed to access,
	// regardless of AllowedPaths.  Takes priority over AllowedPaths.
	BlockedPaths []string `mapstructure:"blocked_paths" yaml:"blocked_paths"`

	// ReadOnlyPaths lists paths that filesystem tools may read but not write/delete.
	ReadOnlyPaths []string `mapstructure:"read_only_paths" yaml:"read_only_paths"`

	// ExtraReadOnlyPaths are additional host paths mounted read-only inside
	// the bwrap/sandbox-exec container in addition to the default system paths.
	ExtraReadOnlyPaths []string `mapstructure:"extra_read_only_paths" yaml:"extra_read_only_paths"`

	// ExtraWritablePaths are additional host paths mounted read-write inside
	// the bwrap/sandbox-exec container (beyond the working directory and /tmp).
	ExtraWritablePaths []string `mapstructure:"extra_writable_paths" yaml:"extra_writable_paths"`

	// BwrapPath is the absolute path to the bwrap binary on Linux.
	// When empty, bwrap is located via $PATH.
	BwrapPath string `mapstructure:"bwrap_path" yaml:"bwrap_path"`
}

// HookConfig is a user-defined shell hook fired before/after tool execution.
type HookConfig struct {
	Name    string `mapstructure:"name" yaml:"name"`
	Event   string `mapstructure:"event" yaml:"event"`     // "pre_tool_use" | "post_tool_use"
	Pattern string `mapstructure:"pattern" yaml:"pattern"` // e.g. "bash:*"
	Command string `mapstructure:"command" yaml:"command"` // Shell script body
	Enabled bool   `mapstructure:"enabled" yaml:"enabled"`
}

// SecurityConfig configures the CommandGuard four-layer security system.
type SecurityConfig struct {
	// AllowRules are Layer 1 config rules that auto-approve matching commands.
	// Format: "Bash(git status:*)" or plain glob pattern.
	AllowRules []string `mapstructure:"allow_rules" yaml:"allow_rules"`
	// DenyRules are Layer 1 config rules that hard-block matching commands.
	DenyRules []string `mapstructure:"deny_rules" yaml:"deny_rules"`
	// Hooks are Layer 2 user-defined shell scripts.
	Hooks []HookConfig `mapstructure:"hooks" yaml:"hooks"`
	// MaxFileSizeBytes is the maximum allowed file write size (default 100MB).
	MaxFileSizeBytes int64 `mapstructure:"max_file_size_bytes" yaml:"max_file_size_bytes"`
}

// MiddlewareConfig configures the middleware chain applied to all tool executions.
type MiddlewareConfig struct {
	// EnableAudit enables the audit logging middleware (writes to ~/.nano/audit.jsonl).
	EnableAudit bool `mapstructure:"enable_audit" yaml:"enable_audit"`
	// AuditLogPath overrides the default audit log path.
	AuditLogPath string `mapstructure:"audit_log_path" yaml:"audit_log_path"`
	// EnableMetrics enables the metrics middleware.
	EnableMetrics bool `mapstructure:"enable_metrics" yaml:"enable_metrics"`
	// EnableResilience enables automatic retry with backoff.
	EnableResilience bool `mapstructure:"enable_resilience" yaml:"enable_resilience"`
	// MaxRetries is the maximum number of retry attempts (default 3).
	MaxRetries int `mapstructure:"max_retries" yaml:"max_retries"`
}

// CriticConfig configures the Critic security evaluator (Prompt Injection Protection Layer 3.5)
type CriticConfig struct {
	Enabled       bool     `mapstructure:"enabled" yaml:"enabled"`
	Model         string   `mapstructure:"model" yaml:"model"`                     // Model for evaluation (can use cheaper model)
	HighRiskTools []string `mapstructure:"high_risk_tools" yaml:"high_risk_tools"` // Tools requiring Critic evaluation
	MaxLatencyMs  int      `mapstructure:"max_latency_ms" yaml:"max_latency_ms"`   // Timeout for evaluation
	CacheEnabled  bool     `mapstructure:"cache_enabled" yaml:"cache_enabled"`     // Enable caching of evaluation results
}

// AdvancedConfig holds advanced configuration options.
type AdvancedConfig struct {
	Fork           *ForkAdvConfig           `yaml:"fork,omitempty"`
	CircuitBreaker *CircuitBreakerAdvConfig `yaml:"circuit_breaker,omitempty"`
}

// ForkAdvConfig holds fork-specific advanced configuration.
type ForkAdvConfig struct {
	MaxDepth      int `yaml:"max_depth"`
	MaxConcurrent int `yaml:"max_concurrent"` // Maximum number of concurrent sub-agents (default: 5)
}

// CircuitBreakerAdvConfig holds circuit breaker configuration for LLM client retries.
type CircuitBreakerAdvConfig struct {
	MaxRetries    int   `yaml:"max_retries,omitempty"`
	BaseDelayMs   int64 `yaml:"base_delay_ms,omitempty"`
	MaxDelayMs    int64 `yaml:"max_delay_ms,omitempty"`
	OpenTimeoutMs int64 `yaml:"open_timeout_ms,omitempty"`
}

// OSSConfig holds Alibaba Cloud OSS configuration
type OSSConfig struct {
	AccessKeyID     string `mapstructure:"access_key_id" yaml:"access_key_id"`
	AccessKeySecret string `mapstructure:"access_key_secret" yaml:"access_key_secret"`
	Endpoint        string `mapstructure:"endpoint" yaml:"endpoint"`
	DefaultBucket   string `mapstructure:"default_bucket" yaml:"default_bucket"`
	Region          string `mapstructure:"region" yaml:"region"`
	Timeout         int    `mapstructure:"timeout" yaml:"timeout"` // Timeout in seconds
	Enabled         bool   `mapstructure:"enabled" yaml:"enabled"`
	CallbackURL     string `mapstructure:"callback_url" yaml:"callback_url"`
	CallbackToken   string `mapstructure:"callback_token" yaml:"callback_token"`
}

func (o *OSSConfig) NormalizedEndpoint() string { //nolint:revive
	if o == nil {
		return ""
	}
	endpoint := strings.TrimSpace(o.Endpoint)
	scheme := ""
	if strings.HasPrefix(endpoint, "https://") {
		scheme = "https://"
		endpoint = strings.TrimPrefix(endpoint, "https://")
	} else if strings.HasPrefix(endpoint, "http://") {
		scheme = "http://"
		endpoint = strings.TrimPrefix(endpoint, "http://")
	}

	endpoint = strings.TrimSuffix(endpoint, "/")
	if bucket := strings.TrimSpace(o.DefaultBucket); bucket != "" {
		prefix := bucket + "."
		if strings.HasPrefix(endpoint, prefix) { //nolint:staticcheck
			endpoint = strings.TrimPrefix(endpoint, prefix)
		}
	}
	return scheme + endpoint
}

func (o *OSSConfig) ValidateIfEnabled() error { //nolint:revive
	if o == nil || !o.Enabled {
		return nil
	}
	if strings.TrimSpace(o.AccessKeyID) == "" {
		return fmt.Errorf("missing oss.access_key_id")
	}
	if strings.TrimSpace(o.AccessKeySecret) == "" {
		return fmt.Errorf("missing oss.access_key_secret")
	}
	if strings.TrimSpace(o.Endpoint) == "" {
		return fmt.Errorf("missing oss.endpoint")
	}
	if strings.TrimSpace(o.DefaultBucket) == "" {
		return fmt.Errorf("missing oss.default_bucket")
	}
	return nil
}

// ReasoningConfig holds reasoning tokens configuration
type ReasoningConfig struct {
	Enabled   bool   `mapstructure:"enabled" yaml:"enabled"`       // Enable reasoning tokens
	Effort    string `mapstructure:"effort" yaml:"effort"`         // "high", "medium", "low" (OpenAI-style)
	MaxTokens int    `mapstructure:"max_tokens" yaml:"max_tokens"` // Specific token limit (Anthropic-style)
	Exclude   bool   `mapstructure:"exclude" yaml:"exclude"`       // Exclude reasoning tokens from response

	// Runtime override: -1 = unset, 0 = disabled, 1 = enabled
	runtimeOverride    int
	runtimeOverrideSet bool
	mu                 sync.RWMutex
}

// Validate validates the reasoning configuration and returns an error if invalid
func (r *ReasoningConfig) Validate() error {
	if r == nil {
		return nil // nil config is valid (will use defaults)
	}

	// Validate effort level
	if r.Effort != "" {
		effort := strings.ToLower(r.Effort)
		if effort != "low" && effort != "medium" && effort != "high" {
			return fmt.Errorf("invalid reasoning effort level '%s': must be 'low', 'medium', or 'high'", r.Effort)
		}
	}

	// Validate max tokens
	if r.MaxTokens < 0 {
		return fmt.Errorf("invalid reasoning max_tokens %d: must be non-negative", r.MaxTokens)
	}

	// Warn about conflicting settings
	if r.MaxTokens > 0 && r.Effort != "" {
		logger.Warnf("Both reasoning max_tokens (%d) and effort (%s) are set. max_tokens will take precedence.", r.MaxTokens, r.Effort)
	}

	return nil
}

// Normalize normalizes the reasoning configuration values
func (r *ReasoningConfig) Normalize() {
	if r == nil {
		return
	}

	// Initialize runtime override sentinel so config values are not shadowed
	// by the zero value (disabled) before any runtime toggle occurs.
	if !r.runtimeOverrideSet {
		r.runtimeOverride = -1
	}

	// Normalize effort level to lowercase
	if r.Effort != "" {
		r.Effort = strings.ToLower(r.Effort)
	}
}

// IsEffortBased returns true if the configuration uses effort-based reasoning
func (r *ReasoningConfig) IsEffortBased() bool {
	return r != nil && r.MaxTokens == 0 && r.Effort != ""
}

// IsTokenBased returns true if the configuration uses token-based reasoning
func (r *ReasoningConfig) IsTokenBased() bool {
	return r != nil && r.MaxTokens > 0
}

// GetEffectiveSettings returns the effective reasoning settings with fallbacks
func (r *ReasoningConfig) GetEffectiveSettings() (enabled bool, effort string, maxTokens int, exclude bool) {
	if r == nil {
		return false, "medium", 0, false
	}

	enabled = r.Enabled
	effort = r.Effort
	if effort == "" {
		effort = "medium"
	}
	maxTokens = r.MaxTokens
	exclude = r.Exclude

	return
}

// SetRuntimeEnabled sets the runtime override for reasoning enabled state
func (r *ReasoningConfig) SetRuntimeEnabled(enabled bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if enabled {
		r.runtimeOverride = 1
	} else {
		r.runtimeOverride = 0
	}
	r.runtimeOverrideSet = true
}

// ClearRuntimeOverride clears the runtime override, restoring config file value
func (r *ReasoningConfig) ClearRuntimeOverride() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runtimeOverride = -1
	r.runtimeOverrideSet = false
}

// IsEffectivelyEnabled returns the effective enabled state, considering runtime override
func (r *ReasoningConfig) IsEffectivelyEnabled() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Runtime override takes precedence
	if r.runtimeOverrideSet {
		return r.runtimeOverride == 1
	}

	// Fall back to config value
	return r.Enabled
}

// GetRuntimeSource returns the source of the current enabled state: "runtime", "config", or "default"
func (r *ReasoningConfig) GetRuntimeSource() string {
	if r == nil {
		return "default"
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.runtimeOverrideSet {
		return "runtime"
	}
	return "config"
}

// Config represents the configuration structure
type Config struct {
	// Basic LLM configuration
	APIKey  string `mapstructure:"api_key" yaml:"api_key"`
	BaseURL string `mapstructure:"base_url" yaml:"base_url"`
	Model   string `mapstructure:"model" yaml:"model"`
	Verbose bool   `mapstructure:"verbose" yaml:"verbose"`

	// WorkingDir overrides the working directory for the agent.
	// When non-empty, this takes precedence over os.Getwd() at startup.
	// Useful in daemon mode to pin the agent to a specific project directory.
	WorkingDir string `mapstructure:"working_dir" yaml:"working_dir"`

	// IsSubAgent marks this config as belonging to a dynamically-created sub-agent.
	// When true, the agent will not register tools that are only meaningful for the
	// top-level main agent (e.g. spawn_sub_agents) to prevent recursive spawning.
	IsSubAgent bool `mapstructure:"-" yaml:"-"`

	// IsDaemon marks whether this config is for daemon mode.
	// Used to determine which session storage backend to use.
	IsDaemon bool `mapstructure:"-" yaml:"-"`

	// Reasoning configuration
	Reasoning *ReasoningConfig `mapstructure:"reasoning" yaml:"reasoning"`

	// System limits and timeouts
	MaxFileSize     int64         `mapstructure:"max_file_size" yaml:"max_file_size"`       // Max file size for processing
	ResponseTimeout time.Duration `mapstructure:"response_timeout" yaml:"response_timeout"` // LLM response timeout
	HTTPTimeout     time.Duration `mapstructure:"http_timeout" yaml:"http_timeout"`         // HTTP client timeout

	// Context management
	ContextConfig ContextConfig `mapstructure:"context" yaml:"context"`

	// Memory management
	Memory *MemoryConfig `mapstructure:"memory" yaml:"memory"`

	// OSS configuration
	OSS *OSSConfig `mapstructure:"oss" yaml:"oss"`

	// Image generator configuration
	ImageGenerator *ImageGeneratorConfig `mapstructure:"image_generator" yaml:"image_generator"`

	// Tool configuration - moved from CustomConfig
	ReadFileMaxLines      int `mapstructure:"read_file_max_lines" yaml:"read_file_max_lines"`
	SearchMaxResults      int `mapstructure:"search_max_results" yaml:"search_max_results"`
	WebRequestTimeout     int `mapstructure:"web_request_timeout" yaml:"web_request_timeout"`       // Web fetch timeout in seconds
	WebSearchTimeout      int `mapstructure:"web_search_timeout" yaml:"web_search_timeout"`         // Web search timeout in seconds
	WebMaxContentSize     int `mapstructure:"web_max_content_size" yaml:"web_max_content_size"`     // Max web content size in bytes
	WebSearchMaxResults   int `mapstructure:"web_search_max_results" yaml:"web_search_max_results"` // Max search results
	FileDiffMaxLines      int `mapstructure:"file_diff_max_lines" yaml:"file_diff_max_lines"`       // Max lines in file diff display
	GitMaxLogEntries      int `mapstructure:"git_max_log_entries" yaml:"git_max_log_entries"`
	MemoryMaxEntries      int `mapstructure:"memory_max_entries" yaml:"memory_max_entries"`
	ListDirectoryMaxDepth int `mapstructure:"list_directory_max_depth" yaml:"list_directory_max_depth"`

	// Tool management
	EnabledTools  []string `mapstructure:"enabled_tools" yaml:"enabled_tools"`
	DisabledTools []string `mapstructure:"disabled_tools" yaml:"disabled_tools"`

	// MCP integration
	EnableMCP bool       `mapstructure:"enable_mcp" yaml:"enable_mcp"`
	MCP       *MCPConfig `mapstructure:"mcp" yaml:"mcp"`

	// Safety settings
	ConfirmDestructive bool `mapstructure:"confirm_destructive" yaml:"confirm_destructive"`

	// Loop detection
	LoopDetection *LoopDetectionConfig `mapstructure:"loop_detection" yaml:"loop_detection"`

	// Turn execution controls
	Turn *TurnExecutionConfig `mapstructure:"turn" yaml:"turn"`

	// Tool execution recovery (retries/backoff)
	ToolRecovery *ToolRecoveryConfig `mapstructure:"tool_recovery" yaml:"tool_recovery"`

	// Web search API keys
	WebSearchAPIKeys WebSearchAPIKeys `mapstructure:"web_search_api_keys" yaml:"web_search_api_keys"`

	// User information for context
	UserInfo *UserInfoConfig `mapstructure:"user_info" yaml:"user_info"`

	// Built-in pprof configuration (local-only listener)
	// Elevate to top-level so it applies to daemon, binary, and TUI modes
	EnablePprof bool `mapstructure:"enable_pprof" yaml:"enable_pprof"`
	PprofPort   int  `mapstructure:"pprof_port" yaml:"pprof_port"`

	// Daemon configuration
	Daemon *DaemonConfig `mapstructure:"daemon" yaml:"daemon"`

	// Secret redaction configuration (global)
	SecretRedaction *SecretRedactionConfig `mapstructure:"secret_redaction" yaml:"secret_redaction"`

	// Tool security (global defaults; daemon-level may override later)
	AllowedCommands []string `mapstructure:"allowed_commands" yaml:"allowed_commands"`
	BlockedCommands []string `mapstructure:"blocked_commands" yaml:"blocked_commands"`
	AllowedEnvVars  []string `mapstructure:"allowed_env_vars" yaml:"allowed_env_vars"`
	BlockedEnvVars  []string `mapstructure:"blocked_env_vars" yaml:"blocked_env_vars"`
	Strict          bool     `mapstructure:"strict" yaml:"strict"`

	CustomSystemPrompt string `mapstructure:"custom_system_prompt" yaml:"custom_system_prompt"`

	// OpenSpec integration configuration
	OpenSpec *OpenSpecConfig `mapstructure:"openspec" yaml:"openspec"`

	// Skills configuration (Claude Code compatible)
	Skills *SkillsConfig `mapstructure:"skills" yaml:"skills"`

	// Scheduler configuration for TUI-mode recurring tasks.
	Scheduler *SchedulerConfig `mapstructure:"scheduler" yaml:"scheduler"`

	// Cron configuration for cron-specific behavior.
	Cron *CronConfig `mapstructure:"cron" yaml:"cron"`

	// PermissionMode controls how tool-execution confirmations are handled.
	// Valid values: "default" (confirm all), "acceptEdits" (auto-approve file
	// edits), "yolo" (skip all confirmations).
	PermissionMode string `mapstructure:"permission_mode" yaml:"permission_mode"`

	// AllowedRules is a list of pre-defined session allowlist rules applied at
	// startup.  Each entry follows the "ToolName" or "ToolName(specifier)"
	// format (e.g. "Bash(git *)", "write_file(*.go)").
	AllowedRules []string `mapstructure:"allowed_rules" yaml:"allowed_rules"`

	// Advanced configuration options
	Advanced *AdvancedConfig `yaml:"advanced,omitempty"`

	// Sandbox controls process-level and path-level sandboxing for tool execution.
	Sandbox *SandboxConfig `mapstructure:"sandbox" yaml:"sandbox"`

	// Security configures the CommandGuard four-layer security system.
	Security *SecurityConfig `mapstructure:"security" yaml:"security"`

	// Critic configures the Critic security evaluator (Prompt Injection Protection Layer 3.5)
	Critic *CriticConfig `mapstructure:"critic" yaml:"critic"`

	// Middleware configures the tool execution middleware chain.
	Middleware *MiddlewareConfig `mapstructure:"middleware" yaml:"middleware"`
}

// SchedulerConfig configures the TUI-mode recurring task scheduler.
type SchedulerConfig struct {
	// Enabled controls whether the TUI scheduler is active (default: true).
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
	// StateFile is the path to the state file.  Defaults to ~/.nano/state.json.
	StateFile string `mapstructure:"state_file" yaml:"state_file"`
}

// CronConfig configures cron-specific behavior for scheduled tasks.
type CronConfig struct {
	// PermissionPolicy controls how tool confirmation is handled in cron tasks.
	// Valid values: "auto_approve" (default), "auto_reject", "inherit"
	PermissionPolicy string `mapstructure:"permission_policy" yaml:"permission_policy"`
	// TurnTimeout is the maximum duration for a single turn in cron task execution.
	// Default: 10 minutes
	TurnTimeout time.Duration `mapstructure:"turn_timeout" yaml:"turn_timeout"`
	// LogPath is the path to the task execution log file.
	// Default: ~/.nano/task_log.jsonl
	LogPath string `mapstructure:"log_path" yaml:"log_path"`
	// LogRetentionDays is how many days of task logs to retain.
	// Default: 30
	LogRetentionDays int `mapstructure:"log_retention_days" yaml:"log_retention_days"`
	// LogCleanupInterval is how often to run log cleanup.
	// Default: 24 hours
	LogCleanupInterval time.Duration `mapstructure:"log_cleanup_interval" yaml:"log_cleanup_interval"`
	// EventsDir is the directory for storing detailed event audit files.
	// Default: ~/.nano/cron-events
	EventsDir string `mapstructure:"events_dir" yaml:"events_dir"`
}

type TurnExecutionConfig struct { //nolint:revive
	// Removed: StrictTaskDone and AutoCompleteOnNoTaskDone
	// Task completion is now implicit based on OpenAI SDK finish_reason
	MaxDuration time.Duration `mapstructure:"max_duration" yaml:"max_duration"` // Maximum duration for a single turn (default: 30m)
}

// ContextConfig configures conversation context management
type ContextConfig struct {
	MaxTokens           int     `mapstructure:"max_tokens" yaml:"max_tokens"`                       // Default: 0 (auto-detect from model registry)
	CompressionRatio    float64 `mapstructure:"compression_ratio" yaml:"compression_ratio"`         // Default: 0.3
	PreserveRecentTurns int     `mapstructure:"preserve_recent_turns" yaml:"preserve_recent_turns"` // Default: 4
	EnableCompression   bool    `mapstructure:"enable_compression" yaml:"enable_compression"`       // Default: true
	ModelContextWindow  int     `mapstructure:"model_context_window" yaml:"model_context_window"`   // Override inferred context window size
}

// OpenSpecConfig configures OpenSpec (spec-driven development) integration.
// When enabled, the agent supports /opsx: slash commands for structured
// proposal → specs → design → tasks → implementation workflows.
type OpenSpecConfig struct {
	Enabled             bool   `mapstructure:"enabled" yaml:"enabled"`                             // Enable OpenSpec integration
	RootDir             string `mapstructure:"root_dir" yaml:"root_dir"`                           // OpenSpec root directory (default: "openspec")
	DefaultSchema       string `mapstructure:"default_schema" yaml:"default_schema"`               // Default schema name (default: "spec-driven")
	AutoDetect          bool   `mapstructure:"auto_detect" yaml:"auto_detect"`                     // Auto-detect openspec/ directory in project
	ApplyMode           string `mapstructure:"apply_mode" yaml:"apply_mode"`                       // Task execution mode: "sequential" or "interactive"
	VerifyBeforeArchive bool   `mapstructure:"verify_before_archive" yaml:"verify_before_archive"` // Run verify before archive
	InjectContext       bool   `mapstructure:"inject_context" yaml:"inject_context"`               // Inject spec context into system prompt
	MaxArtifactSize     int64  `mapstructure:"-" yaml:"-"`                                         // Max artifact file size in bytes (internal, default: 512KB)
}

// SkillsConfig configures Claude Code compatible skills support.
// Skills are loaded from SKILL.md files in personal (~/.nano/skills/) and
// project (.nano/skills/) directories.
type SkillsConfig struct {
	Enabled         bool   `mapstructure:"enabled" yaml:"enabled"`                     // Enable skills support
	PersonalDir     string `mapstructure:"personal_dir" yaml:"personal_dir"`           // Personal skills directory (default: ~/.nano/skills)
	ProjectDir      string `mapstructure:"project_dir" yaml:"project_dir"`             // Project skills directory (default: .nano/skills)
	MaxSkillSize    int64  `mapstructure:"max_skill_size" yaml:"max_skill_size"`       // Max SKILL.md file size (default: 65536)
	MaxSkills       int    `mapstructure:"max_skills" yaml:"max_skills"`               // Max total number of skills (default: 50)
	AutoInvoke      bool   `mapstructure:"auto_invoke" yaml:"auto_invoke"`             // Global auto-invoke switch (default: true)
	MaxActiveSkills int    `mapstructure:"max_active_skills" yaml:"max_active_skills"` // Max simultaneously active skills (default: 5)
}

// MemoryConfig defines memory management settings for Mem0 integration
type MemoryConfig struct {
	APIKey    string `mapstructure:"api_key" yaml:"api_key"`       // Mem0 API key
	BaseURL   string `mapstructure:"base_url" yaml:"base_url"`     // Mem0 base URL, default: "https://api.mem0.ai"
	OrgID     string `mapstructure:"org_id" yaml:"org_id"`         // Organization ID for Mem0
	ProjectID string `mapstructure:"project_id" yaml:"project_id"` // Project ID for Mem0
	UserID    string `mapstructure:"user_id" yaml:"user_id"`       // User ID for Mem0 memory isolation
	AgentID   string `mapstructure:"agent_id" yaml:"agent_id"`     // Agent ID for Mem0 memory isolation (optional, auto-generated from subagent selection if not set)
}

// MCPConfig holds consolidated MCP configuration
type MCPConfig struct {
	EnableClient        bool              `mapstructure:"enable_client" yaml:"enable_client"`
	Servers             []MCPServerConfig `mapstructure:"servers" yaml:"servers"`
	DefaultTransport    string            `mapstructure:"default_transport" yaml:"default_transport"`
	Timeout             time.Duration     `mapstructure:"timeout" yaml:"timeout"`
	MaxRetries          int               `mapstructure:"max_retries" yaml:"max_retries"`
	EnableAuth          bool              `mapstructure:"enable_auth" yaml:"enable_auth"`
	AuthTokens          map[string]string `mapstructure:"auth_tokens" yaml:"auth_tokens"`
	TLS                 *MCPTLSConfig     `mapstructure:"tls" yaml:"tls"`
	EnableHealthCheck   bool              `mapstructure:"enable_health_check" yaml:"enable_health_check"`
	HealthCheckInterval time.Duration     `mapstructure:"health_check_interval" yaml:"health_check_interval"`
	HealthCheckTimeout  time.Duration     `mapstructure:"health_check_timeout" yaml:"health_check_timeout"`
}

// MCPServerConfig defines configuration for connecting to an MCP server
type MCPServerConfig struct {
	Name        string            `mapstructure:"name" yaml:"name"`
	Description string            `mapstructure:"description" yaml:"description"`
	Command     []string          `mapstructure:"command" yaml:"command"`
	URL         string            `mapstructure:"url" yaml:"url"`
	Transport   string            `mapstructure:"transport" yaml:"transport"`
	Headers     map[string]string `mapstructure:"headers" yaml:"headers"`
	Enabled     bool              `mapstructure:"enabled" yaml:"enabled"`
	Timeout     time.Duration     `mapstructure:"timeout" yaml:"timeout"`
	OAuth       *MCPOAuthConfig   `mapstructure:"oauth" yaml:"oauth,omitempty"`
}

// MCPOAuthConfig holds OAuth 2.0 configuration for an MCP server
type MCPOAuthConfig struct {
	AuthorizationURL string `mapstructure:"authorization_url" yaml:"authorization_url"`
	TokenURL         string `mapstructure:"token_url" yaml:"token_url"`
	ClientID         string `mapstructure:"client_id" yaml:"client_id"`
	ClientSecret     string `mapstructure:"client_secret" yaml:"client_secret,omitempty"`
	Scopes           string `mapstructure:"scopes" yaml:"scopes,omitempty"`
	RedirectPort     int    `mapstructure:"redirect_port" yaml:"redirect_port,omitempty"`
}

// MCPTLSConfig holds MCP TLS configuration
type MCPTLSConfig struct {
	Enabled    bool   `mapstructure:"enabled" yaml:"enabled"`
	CertFile   string `mapstructure:"cert_file" yaml:"cert_file"`
	KeyFile    string `mapstructure:"key_file" yaml:"key_file"`
	CAFile     string `mapstructure:"ca_file" yaml:"ca_file"`
	SkipVerify bool   `mapstructure:"skip_verify" yaml:"skip_verify"`
}

// UserInfoConfig holds user-specific information for context
type UserInfoConfig struct {
	Timezone           string            `mapstructure:"timezone" yaml:"timezone"`                           // User's timezone (e.g. "Asia/Shanghai")
	OperatingSystem    string            `mapstructure:"operating_system" yaml:"operating_system"`           // OS info (e.g. "macOS 13.0", "Ubuntu 22.04")
	Shell              string            `mapstructure:"shell" yaml:"shell"`                                 // Shell type (e.g. "/bin/zsh", "/bin/bash")
	Editor             string            `mapstructure:"editor" yaml:"editor"`                               // Preferred editor (e.g. "vim", "vscode")
	Language           string            `mapstructure:"language" yaml:"language"`                           // Preferred language (e.g. "en", "zh-CN")
	ProgrammingTools   map[string]string `mapstructure:"programming_tools" yaml:"programming_tools"`         // Programming tools and versions
	WorkingDirectory   string            `mapstructure:"working_directory" yaml:"working_directory"`         // Current working directory
	AutoDetectUserInfo bool              `mapstructure:"auto_detect_user_info" yaml:"auto_detect_user_info"` // Whether to auto-detect user info
}

// DaemonConfig holds consolidated daemon configuration
// ConfirmPolicy defines how ActionConfirm decisions are handled in daemon mode
type ConfirmPolicy string

const (
	// ConfirmPolicyAllow treats ActionConfirm as ActionAllow (auto-approve)
	ConfirmPolicyAllow ConfirmPolicy = "allow"
	// ConfirmPolicyBlock treats ActionConfirm as ActionBlock (reject)
	ConfirmPolicyBlock ConfirmPolicy = "block"
	// ConfirmPolicyAllowlist auto-approves only tools in the allow list, rejects others
	ConfirmPolicyAllowlist ConfirmPolicy = "allowlist"
)

type DaemonConfig struct {
	Port        int    `mapstructure:"port" yaml:"port"`
	Host        string `mapstructure:"host" yaml:"host"`
	PidFile     string `mapstructure:"pid_file" yaml:"pid_file"`
	LogFile     string `mapstructure:"log_file" yaml:"log_file"`
	EnableCORS  bool   `mapstructure:"enable_cors" yaml:"enable_cors"`
	APIKey      string `mapstructure:"api_key" yaml:"api_key"`
	TLSCertFile string `mapstructure:"tls_cert_file" yaml:"tls_cert_file"`
	TLSKeyFile  string `mapstructure:"tls_key_file" yaml:"tls_key_file"`

	// Optional daemon-level override for secret redaction
	SecretRedaction *SecretRedactionConfig `mapstructure:"secret_redaction" yaml:"secret_redaction"`

	// Policy for handling ActionConfirm decisions when no approval handler is present
	// Default is "allow" for backward compatibility
	ConfirmPolicy ConfirmPolicy `mapstructure:"confirm_policy" yaml:"confirm_policy"`

	// List of tool names to auto-approve when ConfirmPolicy is "allowlist"
	AllowlistedTools []string `mapstructure:"allowlisted_tools" yaml:"allowlisted_tools"`
}

// SecretRedactionConfig controls sensitive data masking
type SecretRedactionConfig struct {
	Enabled         bool               `mapstructure:"enabled" yaml:"enabled"`
	IncludeDefaults bool               `mapstructure:"include_defaults" yaml:"include_defaults"`
	SensitiveKeys   []string           `mapstructure:"sensitive_keys" yaml:"sensitive_keys"`
	Additional      []RedactionPattern `mapstructure:"additional_patterns" yaml:"additional_patterns"`
}

// RedactionPattern allows user-defined regex-based redaction
type RedactionPattern struct {
	Name        string `mapstructure:"name" yaml:"name"`
	Regex       string `mapstructure:"regex" yaml:"regex"`
	Replacement string `mapstructure:"replacement" yaml:"replacement"`
}

var cfg *Config

// DefaultConfig returns a new Config with default values
func DefaultConfig() *Config {
	return &Config{
		// Basic LLM settings
		Model:   "deepseek-chat",
		Verbose: true,

		// System limits
		MaxFileSize:     10 * 1024 * 1024, // 10MB
		ResponseTimeout: 15 * time.Minute,
		HTTPTimeout:     180 * time.Second,

		// Tool-specific configurations
		ReadFileMaxLines:      200,
		SearchMaxResults:      20,
		WebRequestTimeout:     30,              // 30 seconds
		WebSearchTimeout:      10,              // 10 seconds
		WebMaxContentSize:     2 * 1024 * 1024, // 2MB
		WebSearchMaxResults:   10,
		FileDiffMaxLines:      20,
		GitMaxLogEntries:      100,
		MemoryMaxEntries:      100,
		ListDirectoryMaxDepth: 3,

		// Default context management settings (auto-tuned via model registry)
		ContextConfig: ContextConfig{
			MaxTokens:           0, // 0 = auto-detect from model registry
			CompressionRatio:    0, // 0 = use registry-derived threshold; non-zero = explicit user override
			PreserveRecentTurns: 6,
			EnableCompression:   true,
		},

		// Default reasoning configuration
		Reasoning: &ReasoningConfig{
			Enabled:   false,    // Disabled by default
			Effort:    "medium", // Default effort level
			MaxTokens: 0,        // Use effort-based allocation by default
			Exclude:   false,    // Include reasoning tokens in response by default
			// Use sentinel so config value isn't overridden by zero-value runtime flag
			runtimeOverride: -1,
		},

		// Default memory settings for Mem0 integration
		Memory: &MemoryConfig{
			BaseURL: "https://api.mem0.ai", // Default Mem0 API base URL
			UserID:  "default-user",        // Default user ID
			AgentID: "nano-agent",          // Default agent ID
		},

		// Default enabled tools
		EnabledTools: []string{
			// Core filesystem tools
			"read_file", "write_file", "edit_file", "list_directory",
			// System tools
			"run_shell_command", "task_done",
			// Search tools
			"search_file_content", "glob",
			// Memory tools
			"save_memory", "search_memory",
			// Web tools
			"web_search", "web_fetch", "image_generate",
			// Workspace management tools
			"oss_manager",
			// OpenSpec tools
			"opsx_status", "opsx_read_artifact", "opsx_write_artifact", "opsx_update_task", "opsx_list_changes",
		},

		// MCP default settings
		EnableMCP: false,
		MCP: &MCPConfig{
			EnableClient:     true,
			DefaultTransport: "stdio",
			Timeout:          30 * time.Second,
			MaxRetries:       3,
			EnableAuth:       false,
			TLS: &MCPTLSConfig{
				Enabled:    false,
				SkipVerify: true,
			},
			EnableHealthCheck:   true,
			HealthCheckInterval: 60 * time.Second,
			HealthCheckTimeout:  10 * time.Second,
		},

		// Safety defaults
		ConfirmDestructive: true,

		// Loop detection defaults
		LoopDetection: &LoopDetectionConfig{
			Enabled: true,
		},

		// Turn execution defaults (empty - using implicit completion)
		Turn: &TurnExecutionConfig{
			MaxDuration: 30 * time.Minute, // Default 30 minutes
		},

		ToolRecovery: &ToolRecoveryConfig{
			Default: ToolRetryPolicy{
				MaxRetries:        3,
				RetryDelay:        time.Second,
				BackoffMultiplier: 2.0,
				MaxDelay:          30 * time.Second,
				JitterRatio:       0.2,
			},
			PerTool: nil,
		},

		// Web search API keys (loaded from .env)
		WebSearchAPIKeys: WebSearchAPIKeys{},

		// Default user info settings
		UserInfo: &UserInfoConfig{
			Timezone:           "UTC",
			OperatingSystem:    "Unknown",
			Shell:              "/bin/sh",
			Editor:             "nano",
			Language:           "en",
			ProgrammingTools:   make(map[string]string),
			WorkingDirectory:   ".",
			AutoDetectUserInfo: true,
		},

		// Default OSS settings
		OSS: &OSSConfig{
			Timeout: 30, // 30 seconds
			Enabled: false,
		},

		// Default image generator settings (provider-agnostic; loaded from env)
		ImageGenerator: &ImageGeneratorConfig{},

		// Default secret redaction settings
		SecretRedaction: &SecretRedactionConfig{
			Enabled:         true,
			IncludeDefaults: true,
			SensitiveKeys: []string{
				"password", "passwd", "secret", "api_key", "apikey", "token", "access_token",
				"authorization", "auth", "cookie", "set-cookie", "session", "private_key", "client_secret",
			},
			Additional: nil,
		},

		// Default tool security (no restrictions unless configured)
		AllowedCommands: []string{},
		BlockedCommands: []string{},
		AllowedEnvVars:  []string{},
		BlockedEnvVars:  []string{},
		Strict:          false,

		// OpenSpec integration (enabled by default)
		OpenSpec: &OpenSpecConfig{
			Enabled:             true,
			RootDir:             "openspec",
			DefaultSchema:       "spec-driven",
			AutoDetect:          true,
			ApplyMode:           "sequential",
			VerifyBeforeArchive: true,
			InjectContext:       true,
			MaxArtifactSize:     512 * 1024, // 512KB
		},

		// Skills support (enabled by default)
		Skills: &SkillsConfig{
			Enabled:         true,
			PersonalDir:     "",             // Default: ~/.nano/skills
			ProjectDir:      ".nano/skills", // Default: .nano/skills
			MaxSkillSize:    64 * 1024,      // 64KB
			MaxSkills:       50,
			AutoInvoke:      true,
			MaxActiveSkills: 5,
		},

		// Cron defaults
		Cron: &CronConfig{
			PermissionPolicy:   "auto_approve",
			TurnTimeout:        10 * time.Minute,
			LogPath:            "", // Will be set to ~/.nano/task_log.jsonl if empty
			LogRetentionDays:   30,
			LogCleanupInterval: 24 * time.Hour,
			EventsDir:          "", // Will be set to ~/.nano/cron-events if empty
		},

		// Top-level pprof defaults: disabled unless configured
		// Use port 0 as sentinel to indicate "unspecified"
		EnablePprof: false,
		PprofPort:   0,

		// Sandbox defaults: disabled; network allowed by default when sandbox is on.
		Sandbox: &SandboxConfig{
			Enabled:            false,
			NetworkAccess:      true,
			AllowedPaths:       []string{},
			BlockedPaths:       []string{"/etc", "/sys", "/proc", "/dev"},
			ReadOnlyPaths:      []string{},
			ExtraReadOnlyPaths: []string{},
			ExtraWritablePaths: []string{},
		},

		// Default Critic configuration (prompt injection protection)
		Critic: &CriticConfig{
			Enabled:       false, // Disabled by default, opt-in
			Model:         "",    // Empty means use main model
			HighRiskTools: []string{"run_shell_command", "write_file", "edit_file", "delete_file"},
			MaxLatencyMs:  5000, // 5 second timeout
			CacheEnabled:  true,
		},
	}
}

// LoadConfig loads configuration from file and environment
func LoadConfig(configPath string) (*Config, error) {
	// Load .env file from the project root
	if err := godotenv.Load(); err != nil {
		// Ignore if .env file doesn't exist
		if !os.IsNotExist(err) {
			// Use logger instead of fmt.Printf to prevent TUI interference
			logger.Warnf("Failed to load .env file: %v", err)
		}
	}

	cfg = DefaultConfig()

	// Override with API keys from environment
	if apiKey := os.Getenv("API_KEY"); apiKey != "" {
		cfg.APIKey = apiKey
	}
	if baseURL := os.Getenv("BASE_URL"); baseURL != "" {
		cfg.BaseURL = baseURL
	}
	if model := os.Getenv("MODEL"); model != "" {
		cfg.Model = model
	}
	if serperAPIKey := os.Getenv("SERPER_API_KEY"); serperAPIKey != "" {
		cfg.WebSearchAPIKeys.Serper = serperAPIKey
	}
	if tavilyAPIKey := os.Getenv("TAVILY_API_KEY"); tavilyAPIKey != "" {
		cfg.WebSearchAPIKeys.Tavily = tavilyAPIKey
	}
	// Image generator env overrides (provider-agnostic)
	if imageAPIKey := os.Getenv("IMAGE_API_KEY"); imageAPIKey != "" {
		if cfg.ImageGenerator == nil {
			cfg.ImageGenerator = &ImageGeneratorConfig{}
		}
		// Add default provider if not exists
		if len(cfg.ImageGenerator.Providers) == 0 {
			cfg.ImageGenerator.Providers = []ImageGeneratorProviderConfig{{
				Provider: "openrouter",
				Model:    "black-forest-labs/flux-schnell",
			}}
		}
		// Update first provider with API key and mark it enabled
		if len(cfg.ImageGenerator.Providers) > 0 {
			cfg.ImageGenerator.Providers[0].APIKey = imageAPIKey
			cfg.ImageGenerator.Providers[0].Enabled = true
		}
	}
	if imageBaseURL := os.Getenv("IMAGE_BASE_URL"); imageBaseURL != "" {
		if cfg.ImageGenerator == nil {
			cfg.ImageGenerator = &ImageGeneratorConfig{}
		}
		// Add default provider if not exists
		if len(cfg.ImageGenerator.Providers) == 0 {
			cfg.ImageGenerator.Providers = []ImageGeneratorProviderConfig{{
				Provider: "openrouter",
				Model:    "black-forest-labs/flux-schnell",
			}}
		}
		// Update first provider with base URL
		if len(cfg.ImageGenerator.Providers) > 0 {
			cfg.ImageGenerator.Providers[0].BaseURL = imageBaseURL
		}
	}

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

		expanded := os.ExpandEnv(string(data))
		err = yaml.Unmarshal([]byte(expanded), cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	// Override with environment variables (string values)
	overrideStringFromEnv(&cfg.APIKey, "NANO_API_KEY")
	overrideStringFromEnv(&cfg.BaseURL, "NANO_BASE_URL")
	overrideStringFromEnv(&cfg.Model, "NANO_MODEL")
	overrideStringFromEnv(&cfg.WorkingDir, "NANO_WORKING_DIR")
	overrideStringFromEnv(&cfg.WebSearchAPIKeys.Serper, "SERPER_API_KEY")
	overrideStringFromEnv(&cfg.WebSearchAPIKeys.Tavily, "TAVILY_API_KEY")
	// Image generator overrides - update first provider if exists
	if cfg.ImageGenerator != nil && len(cfg.ImageGenerator.Providers) > 0 {
		overrideStringFromEnv(&cfg.ImageGenerator.Providers[0].APIKey, "IMAGE_API_KEY")
		overrideStringFromEnv(&cfg.ImageGenerator.Providers[0].BaseURL, "IMAGE_BASE_URL")
		if cfg.ImageGenerator.Providers[0].APIKey != "" {
			cfg.ImageGenerator.Providers[0].Enabled = true
		}
	}
	// Provider-specific image generator env overrides
	if cfg.ImageGenerator != nil {
		overrideImageProviderFromEnv(cfg.ImageGenerator, "seedream",
			os.Getenv("SEEDREAM_API_KEY"),
			os.Getenv("SEEDREAM_IMAGE_MODEL"),
			os.Getenv("SEEDREAM_BASE_URL"),
		)
		overrideImageProviderFromEnv(cfg.ImageGenerator, "openrouter",
			"",
			os.Getenv("OPENROUTER_IMAGE_MODEL"),
			"",
		)
	}

	// Override OSS configuration from environment
	overrideStringFromEnv(&cfg.OSS.AccessKeyID, "OSS_ACCESS_KEY_ID")
	overrideStringFromEnv(&cfg.OSS.AccessKeySecret, "OSS_ACCESS_KEY_SECRET")
	overrideStringFromEnv(&cfg.OSS.Endpoint, "OSS_ENDPOINT")
	overrideStringFromEnv(&cfg.OSS.DefaultBucket, "OSS_DEFAULT_BUCKET")
	overrideStringFromEnv(&cfg.OSS.Region, "OSS_REGION")
	overrideStringFromEnv(&cfg.OSS.CallbackURL, "OSS_CALLBACK_URL")
	overrideStringFromEnv(&cfg.OSS.CallbackToken, "OSS_CALLBACK_TOKEN")
	setStringIfEmptyFromEnv(&cfg.OSS.AccessKeyID, "ALIYUN_OSS_ACCESS_KEY_ID")
	setStringIfEmptyFromEnv(&cfg.OSS.AccessKeySecret, "ALIYUN_OSS_ACCESS_KEY_SECRET")
	setStringIfEmptyFromEnv(&cfg.OSS.Endpoint, "ALIYUN_OSS_ENDPOINT")
	setStringIfEmptyFromEnv(&cfg.OSS.DefaultBucket, "ALIYUN_OSS_BUCKET_NAME")
	setStringIfEmptyFromEnv(&cfg.OSS.Region, "ALIYUN_OSS_REGION")

	// Override with environment variables (integer values)
	overrideIntFromEnv(&cfg.ReadFileMaxLines, "NANO_READ_FILE_MAX_LINES")
	overrideIntFromEnv(&cfg.SearchMaxResults, "NANO_SEARCH_MAX_RESULTS")
	overrideIntFromEnv(&cfg.WebRequestTimeout, "NANO_WEB_REQUEST_TIMEOUT")
	overrideIntFromEnv(&cfg.WebSearchTimeout, "NANO_WEB_SEARCH_TIMEOUT")
	overrideIntFromEnv(&cfg.WebMaxContentSize, "NANO_WEB_MAX_CONTENT_SIZE")
	overrideIntFromEnv(&cfg.WebSearchMaxResults, "NANO_WEB_SEARCH_MAX_RESULTS")
	overrideIntFromEnv(&cfg.FileDiffMaxLines, "NANO_FILE_DIFF_MAX_LINES")
	overrideIntFromEnv(&cfg.GitMaxLogEntries, "NANO_GIT_MAX_LOG_ENTRIES")
	overrideIntFromEnv(&cfg.MemoryMaxEntries, "NANO_MEMORY_MAX_ENTRIES")
	overrideIntFromEnv(&cfg.ListDirectoryMaxDepth, "NANO_LIST_DIRECTORY_MAX_DEPTH")

	// Override with environment variables (boolean values)
	overrideBoolFromEnv(&cfg.Verbose, "NANO_VERBOSE")
	overrideBoolFromEnv(&cfg.OSS.Enabled, "OSS_ENABLED")
	// Top-level pprof enable
	overrideBoolFromEnv(&cfg.EnablePprof, "NANO_ENABLE_PPROF")

	// Override OSS timeout from environment
	overrideIntFromEnv(&cfg.OSS.Timeout, "OSS_TIMEOUT")
	// Top-level pprof port
	overrideIntFromEnv(&cfg.PprofPort, "NANO_PPROF_PORT")

	// Override file size limit
	overrideInt64FromEnv(&cfg.MaxFileSize, "NANO_MAX_FILE_SIZE")

	// Override timeouts from environment
	overrideDurationFromEnv(&cfg.ResponseTimeout, "NANO_RESPONSE_TIMEOUT")
	overrideDurationFromEnv(&cfg.HTTPTimeout, "NANO_HTTP_TIMEOUT")

	// Override tool recovery defaults from environment
	if cfg.ToolRecovery == nil {
		cfg.ToolRecovery = &ToolRecoveryConfig{}
	}
	overrideIntFromEnv(&cfg.ToolRecovery.Default.MaxRetries, "NANO_TOOL_RECOVERY_MAX_RETRIES")
	overrideDurationFromEnv(&cfg.ToolRecovery.Default.RetryDelay, "NANO_TOOL_RECOVERY_RETRY_DELAY")
	overrideFloatFromEnv(&cfg.ToolRecovery.Default.BackoffMultiplier, "NANO_TOOL_RECOVERY_BACKOFF_MULTIPLIER")
	overrideDurationFromEnv(&cfg.ToolRecovery.Default.MaxDelay, "NANO_TOOL_RECOVERY_MAX_DELAY")
	overrideFloatFromEnv(&cfg.ToolRecovery.Default.JitterRatio, "NANO_TOOL_RECOVERY_JITTER_RATIO")

	// Override context configuration from environment
	overrideIntFromEnv(&cfg.ContextConfig.MaxTokens, "NANO_CONTEXT_MAX_TOKENS")
	overrideFloatFromEnv(&cfg.ContextConfig.CompressionRatio, "NANO_CONTEXT_COMPRESSION_RATIO")
	overrideIntFromEnv(&cfg.ContextConfig.PreserveRecentTurns, "NANO_CONTEXT_PRESERVE_RECENT_TURNS")
	overrideBoolFromEnv(&cfg.ContextConfig.EnableCompression, "NANO_CONTEXT_ENABLE_COMPRESSION")
	overrideIntFromEnv(&cfg.ContextConfig.ModelContextWindow, "NANO_CONTEXT_MODEL_WINDOW")

	// Override Daemon configuration from environment
	if cfg.Daemon == nil {
		cfg.Daemon = &DaemonConfig{
			ConfirmPolicy: ConfirmPolicyAllow, // Default to allow for backward compatibility
		}
	}
	// Set default confirm policy if not specified
	if cfg.Daemon.ConfirmPolicy == "" {
		cfg.Daemon.ConfirmPolicy = ConfirmPolicyAllow
	}
	// Numeric and string overrides
	overrideIntFromEnv(&cfg.Daemon.Port, "NANO_DAEMON_PORT")
	overrideStringFromEnv(&cfg.Daemon.Host, "NANO_DAEMON_HOST")
	overrideDaemonFilePathFromEnvIfWritable(&cfg.Daemon.PidFile, "NANO_DAEMON_PID_FILE")
	overrideDaemonFilePathFromEnvIfWritable(&cfg.Daemon.LogFile, "NANO_DAEMON_LOG_FILE")
	// Booleans
	overrideBoolFromEnv(&cfg.Daemon.EnableCORS, "NANO_DAEMON_ENABLE_CORS")
	// API key (support both preferred and backward-compatible env names)
	overrideStringFromEnv(&cfg.Daemon.APIKey, "NANO_DAEMON_API_KEY")
	overrideStringFromEnv(&cfg.Daemon.APIKey, "DAEMON_API_KEY")
	// TLS paths
	overrideStringFromEnv(&cfg.Daemon.TLSCertFile, "NANO_DAEMON_TLS_CERT_FILE")
	overrideStringFromEnv(&cfg.Daemon.TLSKeyFile, "NANO_DAEMON_TLS_KEY_FILE")

	// Override reasoning configuration from environment
	if cfg.Reasoning != nil {
		overrideBoolFromEnv(&cfg.Reasoning.Enabled, "NANO_REASONING_ENABLED")
		overrideStringFromEnv(&cfg.Reasoning.Effort, "NANO_REASONING_EFFORT")
		overrideIntFromEnv(&cfg.Reasoning.MaxTokens, "NANO_REASONING_MAX_TOKENS")
		overrideBoolFromEnv(&cfg.Reasoning.Exclude, "NANO_REASONING_EXCLUDE")
	}

	// Override memory configuration from environment
	if cfg.Memory != nil {
		overrideStringFromEnv(&cfg.Memory.APIKey, "NANO_MEMORY_API_KEY")
		overrideStringFromEnv(&cfg.Memory.BaseURL, "NANO_MEMORY_BASE_URL")
		overrideStringFromEnv(&cfg.Memory.OrgID, "NANO_MEMORY_ORG_ID")
		overrideStringFromEnv(&cfg.Memory.ProjectID, "NANO_MEMORY_PROJECT_ID")
		overrideStringFromEnv(&cfg.Memory.UserID, "NANO_MEMORY_USER_ID")
		overrideStringFromEnv(&cfg.Memory.AgentID, "NANO_MEMORY_AGENT_ID")
	}

	// Override MCP configuration from environment
	overrideBoolFromEnv(&cfg.EnableMCP, "NANO_ENABLE_MCP")
	if cfg.MCP != nil {
		overrideStringFromEnv(&cfg.MCP.DefaultTransport, "NANO_MCP_DEFAULT_TRANSPORT")
		overrideDurationFromEnv(&cfg.MCP.Timeout, "NANO_MCP_TIMEOUT")
		overrideIntFromEnv(&cfg.MCP.MaxRetries, "NANO_MCP_MAX_RETRIES")
		overrideBoolFromEnv(&cfg.MCP.EnableHealthCheck, "NANO_MCP_ENABLE_HEALTH_CHECK")
		overrideDurationFromEnv(&cfg.MCP.HealthCheckInterval, "NANO_MCP_HEALTH_CHECK_INTERVAL")
		overrideDurationFromEnv(&cfg.MCP.HealthCheckTimeout, "NANO_MCP_HEALTH_CHECK_TIMEOUT")
	}

	// Override OpenSpec configuration from environment
	if cfg.OpenSpec != nil {
		overrideBoolFromEnv(&cfg.OpenSpec.Enabled, "NANO_OPENSPEC_ENABLED")
		overrideStringFromEnv(&cfg.OpenSpec.RootDir, "NANO_OPENSPEC_ROOT_DIR")
		overrideStringFromEnv(&cfg.OpenSpec.DefaultSchema, "NANO_OPENSPEC_DEFAULT_SCHEMA")
		overrideBoolFromEnv(&cfg.OpenSpec.AutoDetect, "NANO_OPENSPEC_AUTO_DETECT")
		overrideStringFromEnv(&cfg.OpenSpec.ApplyMode, "NANO_OPENSPEC_APPLY_MODE")
		overrideBoolFromEnv(&cfg.OpenSpec.VerifyBeforeArchive, "NANO_OPENSPEC_VERIFY_BEFORE_ARCHIVE")
		overrideBoolFromEnv(&cfg.OpenSpec.InjectContext, "NANO_OPENSPEC_INJECT_CONTEXT")
	}

	// Override tools configuration from environment
	overrideStringSliceFromEnv(&cfg.EnabledTools, "NANO_ENABLED_TOOLS")
	overrideStringSliceFromEnv(&cfg.DisabledTools, "NANO_DISABLED_TOOLS")
	overrideStringSliceFromEnv(&cfg.AllowedCommands, "NANO_ALLOWED_COMMANDS")
	overrideStringSliceFromEnv(&cfg.BlockedCommands, "NANO_BLOCKED_COMMANDS")
	overrideStringSliceFromEnv(&cfg.AllowedEnvVars, "NANO_ALLOWED_ENV_VARS")
	overrideStringSliceFromEnv(&cfg.BlockedEnvVars, "NANO_BLOCKED_ENV_VARS")
	overrideBoolFromEnv(&cfg.Strict, "NANO_STRICT")

	// Override secret redaction configuration from environment
	if cfg.SecretRedaction != nil {
		overrideBoolFromEnv(&cfg.SecretRedaction.Enabled, "NANO_REDACTION_ENABLED")
		overrideBoolFromEnv(&cfg.SecretRedaction.IncludeDefaults, "NANO_REDACTION_INCLUDE_DEFAULTS")
		overrideStringSliceFromEnv(&cfg.SecretRedaction.SensitiveKeys, "NANO_REDACTION_SENSITIVE_KEYS")
		if v := os.Getenv("NANO_REDACTION_ADDITIONAL"); v != "" {
			var patterns []RedactionPattern
			if err := json.Unmarshal([]byte(v), &patterns); err == nil {
				cfg.SecretRedaction.Additional = patterns
			}
		}
	}

	// Override destructive confirmation setting
	overrideBoolFromEnv(&cfg.ConfirmDestructive, "NANO_CONFIRM_DESTRUCTIVE")

	if cfg.OSS != nil {
		if err := cfg.OSS.ValidateIfEnabled(); err != nil {
			logger.Warnf("OSS is enabled but configuration is invalid (%v); disabling OSS", err)
			cfg.OSS.Enabled = false
		}
	}

	// Override sandbox configuration from environment
	if cfg.Sandbox == nil {
		cfg.Sandbox = &SandboxConfig{}
	}
	overrideBoolFromEnv(&cfg.Sandbox.Enabled, "NANO_SANDBOX_ENABLED")
	overrideBoolFromEnv(&cfg.Sandbox.NetworkAccess, "NANO_SANDBOX_NETWORK_ACCESS")
	overrideStringFromEnv(&cfg.Sandbox.BwrapPath, "NANO_SANDBOX_BWRAP_PATH")
	overrideStringSliceFromEnv(&cfg.Sandbox.AllowedPaths, "NANO_SANDBOX_ALLOWED_PATHS")
	overrideStringSliceFromEnv(&cfg.Sandbox.BlockedPaths, "NANO_SANDBOX_BLOCKED_PATHS")
	overrideStringSliceFromEnv(&cfg.Sandbox.ReadOnlyPaths, "NANO_SANDBOX_READ_ONLY_PATHS")
	overrideStringSliceFromEnv(&cfg.Sandbox.ExtraReadOnlyPaths, "NANO_SANDBOX_EXTRA_READ_ONLY_PATHS")
	overrideStringSliceFromEnv(&cfg.Sandbox.ExtraWritablePaths, "NANO_SANDBOX_EXTRA_WRITABLE_PATHS")

	// Validate and normalize reasoning configuration
	if cfg.Reasoning != nil {
		if err := cfg.Reasoning.Validate(); err != nil {
			return nil, fmt.Errorf("invalid reasoning configuration: %w", err)
		}
		cfg.Reasoning.Normalize()
		logger.Debug("Reasoning configuration validated and normalized: enabled=%v, effort=%s, max_tokens=%d, exclude=%v",
			cfg.Reasoning.Enabled, cfg.Reasoning.Effort, cfg.Reasoning.MaxTokens, cfg.Reasoning.Exclude)
	}

	return cfg, nil
}

// Get returns the loaded configuration
func Get() *Config {
	return cfg
}

// SetGlobalConfig manually sets the global configuration for testing purposes
func SetGlobalConfig(c *Config) {
	cfg = c
}

// overrideStringFromEnv overrides a string value from an environment variable
func overrideStringFromEnv(value *string, envVar string) {
	if v := os.Getenv(envVar); v != "" {
		*value = v
	}
}

// overrideImageProviderFromEnv finds or creates an image provider config entry by name
// and applies non-empty env values to it. When an API key is set, Enabled is also set to true.
func overrideImageProviderFromEnv(cfg *ImageGeneratorConfig, providerName, apiKey, model, baseURL string) {
	if apiKey == "" && model == "" && baseURL == "" {
		return
	}
	// Look for an existing entry
	for i := range cfg.Providers {
		if cfg.Providers[i].Provider == providerName {
			if apiKey != "" {
				cfg.Providers[i].APIKey = apiKey
				cfg.Providers[i].Enabled = true
			}
			if model != "" {
				cfg.Providers[i].Model = model
			}
			if baseURL != "" {
				cfg.Providers[i].BaseURL = baseURL
			}
			return
		}
	}
	// Provider not found – create a new entry
	newProvider := ImageGeneratorProviderConfig{
		Provider: providerName,
		APIKey:   apiKey,
		Model:    model,
		BaseURL:  baseURL,
		Enabled:  apiKey != "",
	}
	cfg.Providers = append(cfg.Providers, newProvider)
}

func setStringIfEmptyFromEnv(value *string, envVar string) {
	if strings.TrimSpace(*value) != "" {
		return
	}
	if v := os.Getenv(envVar); v != "" {
		*value = v
	}
}

func overrideDaemonFilePathFromEnvIfWritable(value *string, envVar string) {
	v := os.Getenv(envVar)
	if v == "" {
		return
	}
	if !isWritableFilePath(v) {
		logger.Warnf("Ignoring %s=%q: path is not writable", envVar, v)
		return
	}

	current := strings.TrimSpace(*value)
	if current == "" {
		*value = v
		return
	}

	sep := string(os.PathSeparator)
	if strings.Contains(filepath.Clean(current), sep+".nano"+sep) {
		*value = v
		return
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	defaultDir := filepath.Clean(filepath.Join(homeDir, ".nano"))
	cleanCurrent := filepath.Clean(current)
	if strings.HasPrefix(cleanCurrent, defaultDir+sep) || cleanCurrent == defaultDir {
		*value = v
	}
}

func isWritableFilePath(path string) bool {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func overrideInt64FromEnv(value *int64, envVar string) {
	if v := os.Getenv(envVar); v != "" {
		if intVal, err := strconv.ParseInt(v, 10, 64); err == nil {
			*value = intVal
		}
	}
}

// overrideIntFromEnv overrides an integer value from an environment variable
func overrideIntFromEnv(value *int, envVar string) {
	if v := os.Getenv(envVar); v != "" {
		if intVal, err := strconv.Atoi(v); err == nil {
			*value = intVal
		}
	}
}

// overrideBoolFromEnv overrides a boolean value from an environment variable
func overrideBoolFromEnv(value *bool, envVar string) {
	if v := os.Getenv(envVar); v != "" {
		if boolVal, err := strconv.ParseBool(v); err == nil {
			*value = boolVal
		}
	}
}

func overrideFloatFromEnv(value *float64, envVar string) {
	if v := os.Getenv(envVar); v != "" {
		if floatVal, err := strconv.ParseFloat(v, 64); err == nil {
			*value = floatVal
		}
	}
}

func overrideDurationFromEnv(value *time.Duration, envVar string) {
	if v := os.Getenv(envVar); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			*value = d
			return
		}
		if intVal, err := strconv.Atoi(v); err == nil {
			*value = time.Duration(intVal) * time.Second
		}
	}
}

func overrideStringSliceFromEnv(value *[]string, envVar string) {
	if v := os.Getenv(envVar); v != "" {
		parts := strings.Split(v, ",")
		var out []string
		for _, p := range parts {
			s := strings.TrimSpace(p)
			if s != "" {
				out = append(out, s)
			}
		}
		*value = out
	}
}

// ConfigLocation represents a potential location for a configuration file.
type ConfigLocation struct { //nolint:revive
	Type   string
	Path   string
	Exists bool
}

// DeepCopy creates a deep copy of the Config struct.
// This is necessary to prevent child agents from modifying parent config during fork operations.
func (c *Config) DeepCopy() *Config {
	if c == nil {
		return nil
	}

	// Create a new config with all scalar fields copied
	copied := &Config{
		APIKey:                c.APIKey,
		BaseURL:               c.BaseURL,
		Model:                 c.Model,
		Verbose:               c.Verbose,
		WorkingDir:            c.WorkingDir,
		IsSubAgent:            c.IsSubAgent,
		IsDaemon:              c.IsDaemon,
		MaxFileSize:           c.MaxFileSize,
		ResponseTimeout:       c.ResponseTimeout,
		HTTPTimeout:           c.HTTPTimeout,
		ReadFileMaxLines:      c.ReadFileMaxLines,
		SearchMaxResults:      c.SearchMaxResults,
		WebRequestTimeout:     c.WebRequestTimeout,
		WebSearchTimeout:      c.WebSearchTimeout,
		WebMaxContentSize:     c.WebMaxContentSize,
		WebSearchMaxResults:   c.WebSearchMaxResults,
		FileDiffMaxLines:      c.FileDiffMaxLines,
		GitMaxLogEntries:      c.GitMaxLogEntries,
		MemoryMaxEntries:      c.MemoryMaxEntries,
		ListDirectoryMaxDepth: c.ListDirectoryMaxDepth,
		EnableMCP:             c.EnableMCP,
		ConfirmDestructive:    c.ConfirmDestructive,
		Strict:                c.Strict,
		CustomSystemPrompt:    c.CustomSystemPrompt,
		PermissionMode:        c.PermissionMode,
		EnablePprof:           c.EnablePprof,
		PprofPort:             c.PprofPort,
	}

	// Deep copy slices
	copied.EnabledTools = append([]string(nil), c.EnabledTools...)
	copied.DisabledTools = append([]string(nil), c.DisabledTools...)
	copied.AllowedCommands = append([]string(nil), c.AllowedCommands...)
	copied.BlockedCommands = append([]string(nil), c.BlockedCommands...)
	copied.AllowedEnvVars = append([]string(nil), c.AllowedEnvVars...)
	copied.BlockedEnvVars = append([]string(nil), c.BlockedEnvVars...)
	copied.AllowedRules = append([]string(nil), c.AllowedRules...)

	// Deep copy Reasoning
	if c.Reasoning != nil {
		copied.Reasoning = &ReasoningConfig{
			Enabled:            c.Reasoning.Enabled,
			Effort:             c.Reasoning.Effort,
			MaxTokens:          c.Reasoning.MaxTokens,
			Exclude:            c.Reasoning.Exclude,
			runtimeOverride:    c.Reasoning.runtimeOverride,
			runtimeOverrideSet: c.Reasoning.runtimeOverrideSet,
		}
	}

	// Deep copy ContextConfig
	copied.ContextConfig = ContextConfig{
		MaxTokens:           c.ContextConfig.MaxTokens,
		CompressionRatio:    c.ContextConfig.CompressionRatio,
		PreserveRecentTurns: c.ContextConfig.PreserveRecentTurns,
		EnableCompression:   c.ContextConfig.EnableCompression,
		ModelContextWindow:  c.ContextConfig.ModelContextWindow,
	}

	// Deep copy Memory
	if c.Memory != nil {
		copied.Memory = &MemoryConfig{
			APIKey:    c.Memory.APIKey,
			BaseURL:   c.Memory.BaseURL,
			OrgID:     c.Memory.OrgID,
			ProjectID: c.Memory.ProjectID,
			UserID:    c.Memory.UserID,
			AgentID:   c.Memory.AgentID,
		}
	}

	// Deep copy OSS
	if c.OSS != nil {
		copied.OSS = &OSSConfig{
			AccessKeyID:     c.OSS.AccessKeyID,
			AccessKeySecret: c.OSS.AccessKeySecret,
			Endpoint:        c.OSS.Endpoint,
			DefaultBucket:   c.OSS.DefaultBucket,
			Region:          c.OSS.Region,
			Timeout:         c.OSS.Timeout,
			Enabled:         c.OSS.Enabled,
			CallbackURL:     c.OSS.CallbackURL,
			CallbackToken:   c.OSS.CallbackToken,
		}
	}

	// Deep copy ImageGenerator
	if c.ImageGenerator != nil {
		copied.ImageGenerator = &ImageGeneratorConfig{}
		for _, p := range c.ImageGenerator.Providers {
			provider := ImageGeneratorProviderConfig{
				Provider:   p.Provider,
				Model:      p.Model,
				EndpointID: p.EndpointID,
				APIKey:     p.APIKey,
				BaseURL:    p.BaseURL,
				Enabled:    p.Enabled,
			}
			if p.Config != nil {
				provider.Config = make(map[string]interface{}, len(p.Config))
				for k, v := range p.Config {
					provider.Config[k] = v
				}
			}
			copied.ImageGenerator.Providers = append(copied.ImageGenerator.Providers, provider)
		}
	}

	// Deep copy LoopDetection
	if c.LoopDetection != nil {
		copied.LoopDetection = &LoopDetectionConfig{
			Enabled: c.LoopDetection.Enabled,
		}
	}

	// Deep copy Turn
	if c.Turn != nil {
		copied.Turn = &TurnExecutionConfig{
			MaxDuration: c.Turn.MaxDuration,
		}
	}

	// Deep copy ToolRecovery
	if c.ToolRecovery != nil {
		copied.ToolRecovery = &ToolRecoveryConfig{
			Default: c.ToolRecovery.Default,
		}
		if c.ToolRecovery.PerTool != nil {
			copied.ToolRecovery.PerTool = make(map[string]ToolRetryPolicy, len(c.ToolRecovery.PerTool))
			for k, v := range c.ToolRecovery.PerTool {
				copied.ToolRecovery.PerTool[k] = v
			}
		}
	}

	// Deep copy WebSearchAPIKeys
	copied.WebSearchAPIKeys = WebSearchAPIKeys{
		Serper:     c.WebSearchAPIKeys.Serper,
		Tavily:     c.WebSearchAPIKeys.Tavily,
		DuckDuckGo: c.WebSearchAPIKeys.DuckDuckGo,
	}

	// Deep copy UserInfo
	if c.UserInfo != nil {
		copied.UserInfo = &UserInfoConfig{
			Timezone:           c.UserInfo.Timezone,
			OperatingSystem:    c.UserInfo.OperatingSystem,
			Shell:              c.UserInfo.Shell,
			Editor:             c.UserInfo.Editor,
			Language:           c.UserInfo.Language,
			WorkingDirectory:   c.UserInfo.WorkingDirectory,
			AutoDetectUserInfo: c.UserInfo.AutoDetectUserInfo,
		}
		if c.UserInfo.ProgrammingTools != nil {
			copied.UserInfo.ProgrammingTools = make(map[string]string, len(c.UserInfo.ProgrammingTools))
			for k, v := range c.UserInfo.ProgrammingTools {
				copied.UserInfo.ProgrammingTools[k] = v
			}
		}
	}

	// Deep copy Daemon
	if c.Daemon != nil {
		copied.Daemon = &DaemonConfig{
			Port:          c.Daemon.Port,
			Host:          c.Daemon.Host,
			PidFile:       c.Daemon.PidFile,
			LogFile:       c.Daemon.LogFile,
			EnableCORS:    c.Daemon.EnableCORS,
			APIKey:        c.Daemon.APIKey,
			TLSCertFile:   c.Daemon.TLSCertFile,
			TLSKeyFile:    c.Daemon.TLSKeyFile,
			ConfirmPolicy: c.Daemon.ConfirmPolicy,
		}
		copied.Daemon.AllowlistedTools = append([]string(nil), c.Daemon.AllowlistedTools...)
		if c.Daemon.SecretRedaction != nil {
			copied.Daemon.SecretRedaction = deepCopySecretRedaction(c.Daemon.SecretRedaction)
		}
	}

	// Deep copy SecretRedaction
	if c.SecretRedaction != nil {
		copied.SecretRedaction = deepCopySecretRedaction(c.SecretRedaction)
	}

	// Deep copy OpenSpec
	if c.OpenSpec != nil {
		copied.OpenSpec = &OpenSpecConfig{
			Enabled:             c.OpenSpec.Enabled,
			RootDir:             c.OpenSpec.RootDir,
			DefaultSchema:       c.OpenSpec.DefaultSchema,
			AutoDetect:          c.OpenSpec.AutoDetect,
			ApplyMode:           c.OpenSpec.ApplyMode,
			VerifyBeforeArchive: c.OpenSpec.VerifyBeforeArchive,
			InjectContext:       c.OpenSpec.InjectContext,
			MaxArtifactSize:     c.OpenSpec.MaxArtifactSize,
		}
	}

	// Deep copy Skills
	if c.Skills != nil {
		copied.Skills = &SkillsConfig{
			Enabled:         c.Skills.Enabled,
			PersonalDir:     c.Skills.PersonalDir,
			ProjectDir:      c.Skills.ProjectDir,
			MaxSkillSize:    c.Skills.MaxSkillSize,
			MaxSkills:       c.Skills.MaxSkills,
			AutoInvoke:      c.Skills.AutoInvoke,
			MaxActiveSkills: c.Skills.MaxActiveSkills,
		}
	}

	// Deep copy Advanced
	if c.Advanced != nil {
		copied.Advanced = &AdvancedConfig{}
		if c.Advanced.Fork != nil {
			copied.Advanced.Fork = &ForkAdvConfig{
				MaxDepth: c.Advanced.Fork.MaxDepth,
			}
		}
	}

	// Deep copy Sandbox
	if c.Sandbox != nil {
		copied.Sandbox = &SandboxConfig{
			Enabled:       c.Sandbox.Enabled,
			NetworkAccess: c.Sandbox.NetworkAccess,
			BwrapPath:     c.Sandbox.BwrapPath,
		}
		copied.Sandbox.AllowedPaths = append([]string(nil), c.Sandbox.AllowedPaths...)
		copied.Sandbox.BlockedPaths = append([]string(nil), c.Sandbox.BlockedPaths...)
		copied.Sandbox.ReadOnlyPaths = append([]string(nil), c.Sandbox.ReadOnlyPaths...)
		copied.Sandbox.ExtraReadOnlyPaths = append([]string(nil), c.Sandbox.ExtraReadOnlyPaths...)
		copied.Sandbox.ExtraWritablePaths = append([]string(nil), c.Sandbox.ExtraWritablePaths...)
	}

	// Deep copy Security
	if c.Security != nil {
		copied.Security = &SecurityConfig{
			MaxFileSizeBytes: c.Security.MaxFileSizeBytes,
		}
		copied.Security.AllowRules = append([]string(nil), c.Security.AllowRules...)
		copied.Security.DenyRules = append([]string(nil), c.Security.DenyRules...)
		if c.Security.Hooks != nil {
			copied.Security.Hooks = make([]HookConfig, len(c.Security.Hooks))
			copy(copied.Security.Hooks, c.Security.Hooks)
		}
	}

	// Deep copy Critic
	if c.Critic != nil {
		copied.Critic = &CriticConfig{
			Enabled:      c.Critic.Enabled,
			Model:        c.Critic.Model,
			MaxLatencyMs: c.Critic.MaxLatencyMs,
			CacheEnabled: c.Critic.CacheEnabled,
		}
		copied.Critic.HighRiskTools = append([]string(nil), c.Critic.HighRiskTools...)
	}

	// Deep copy Middleware
	if c.Middleware != nil {
		copied.Middleware = &MiddlewareConfig{
			EnableAudit:      c.Middleware.EnableAudit,
			AuditLogPath:     c.Middleware.AuditLogPath,
			EnableMetrics:    c.Middleware.EnableMetrics,
			EnableResilience: c.Middleware.EnableResilience,
			MaxRetries:       c.Middleware.MaxRetries,
		}
	}

	// Deep copy MCP
	if c.MCP != nil {
		copied.MCP = &MCPConfig{
			EnableClient:        c.MCP.EnableClient,
			DefaultTransport:    c.MCP.DefaultTransport,
			Timeout:             c.MCP.Timeout,
			MaxRetries:          c.MCP.MaxRetries,
			EnableAuth:          c.MCP.EnableAuth,
			EnableHealthCheck:   c.MCP.EnableHealthCheck,
			HealthCheckInterval: c.MCP.HealthCheckInterval,
			HealthCheckTimeout:  c.MCP.HealthCheckTimeout,
		}
		if c.MCP.Servers != nil {
			copied.MCP.Servers = make([]MCPServerConfig, len(c.MCP.Servers))
			for i, s := range c.MCP.Servers {
				copied.MCP.Servers[i] = MCPServerConfig{
					Name:        s.Name,
					Description: s.Description,
					URL:         s.URL,
					Transport:   s.Transport,
					Enabled:     s.Enabled,
					Timeout:     s.Timeout,
				}
				copied.MCP.Servers[i].Command = append([]string(nil), s.Command...)
				if s.Headers != nil {
					copied.MCP.Servers[i].Headers = make(map[string]string, len(s.Headers))
					for k, v := range s.Headers {
						copied.MCP.Servers[i].Headers[k] = v
					}
				}
			}
		}
		if c.MCP.AuthTokens != nil {
			copied.MCP.AuthTokens = make(map[string]string, len(c.MCP.AuthTokens))
			for k, v := range c.MCP.AuthTokens {
				copied.MCP.AuthTokens[k] = v
			}
		}
		if c.MCP.TLS != nil {
			copied.MCP.TLS = &MCPTLSConfig{
				Enabled:    c.MCP.TLS.Enabled,
				CertFile:   c.MCP.TLS.CertFile,
				KeyFile:    c.MCP.TLS.KeyFile,
				CAFile:     c.MCP.TLS.CAFile,
				SkipVerify: c.MCP.TLS.SkipVerify,
			}
		}
	}

	// Deep copy Scheduler
	if c.Scheduler != nil {
		copied.Scheduler = &SchedulerConfig{
			Enabled:   c.Scheduler.Enabled,
			StateFile: c.Scheduler.StateFile,
		}
	}

	// Deep copy Cron
	if c.Cron != nil {
		copied.Cron = &CronConfig{
			PermissionPolicy:   c.Cron.PermissionPolicy,
			TurnTimeout:        c.Cron.TurnTimeout,
			LogPath:            c.Cron.LogPath,
			LogRetentionDays:   c.Cron.LogRetentionDays,
			LogCleanupInterval: c.Cron.LogCleanupInterval,
			EventsDir:          c.Cron.EventsDir,
		}
	}

	return copied
}

// deepCopySecretRedaction is a helper to deep copy SecretRedactionConfig
func deepCopySecretRedaction(src *SecretRedactionConfig) *SecretRedactionConfig {
	if src == nil {
		return nil
	}
	copied := &SecretRedactionConfig{
		Enabled:         src.Enabled,
		IncludeDefaults: src.IncludeDefaults,
	}
	copied.SensitiveKeys = append([]string(nil), src.SensitiveKeys...)
	if src.Additional != nil {
		copied.Additional = make([]RedactionPattern, len(src.Additional))
		copy(copied.Additional, src.Additional)
	}
	return copied
}

// GetConfigLocations returns the potential configuration file locations in priority order,
// each with its type, path, and whether the file currently exists.
func GetConfigLocations(configFile string) []ConfigLocation {
	var locations []ConfigLocation

	// 1. User-specified config file
	if configFile != "" {
		_, err := os.Stat(configFile)
		locations = append(locations, ConfigLocation{
			Type:   "User-specified",
			Path:   configFile,
			Exists: err == nil,
		})
	}

	// 2. Current directory
	currentDir, err := os.Getwd()
	if err == nil {
		p := filepath.Join(currentDir, ".nano.yaml")
		_, err := os.Stat(p)
		locations = append(locations, ConfigLocation{
			Type:   "Project",
			Path:   p,
			Exists: err == nil,
		})
	}

	// 3. Global config
	homeDir, err := os.UserHomeDir()
	if err == nil {
		p := filepath.Join(homeDir, ".config", "nano", "config.yaml")
		_, err := os.Stat(p)
		locations = append(locations, ConfigLocation{
			Type:   "Global",
			Path:   p,
			Exists: err == nil,
		})
	}

	return locations
}
