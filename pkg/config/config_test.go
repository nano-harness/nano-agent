package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_DefaultBehavior(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	_ = os.Chdir(tempDir)

	originalVerbose := os.Getenv("NANO_VERBOSE")
	defer func() {
		if originalVerbose != "" {
			_ = os.Setenv("NANO_VERBOSE", originalVerbose)
		} else {
			_ = os.Unsetenv("NANO_VERBOSE")
		}
	}()
	_ = os.Unsetenv("NANO_VERBOSE")

	// Test loading config without any config file
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("Expected config to be non-nil")
	}

	// Check that model is set (don't check specific value as it may come from global config)
	if cfg.Model == "" {
		t.Error("Expected model to be set, got empty string")
	}

	if cfg.ReadFileMaxLines != 200 {
		t.Errorf("Expected default ReadFileMaxLines 200, got %d", cfg.ReadFileMaxLines)
	}
}

func TestLoadConfig_WithNanoYaml(t *testing.T) {
	// Save and clear environment variables that might override config
	originalAPIKey := os.Getenv("NANO_API_KEY")
	originalModel := os.Getenv("NANO_MODEL")
	originalVerbose := os.Getenv("NANO_VERBOSE")
	defer func() {
		if originalAPIKey != "" {
			_ = os.Setenv("NANO_API_KEY", originalAPIKey)
		} else {
			_ = os.Unsetenv("NANO_API_KEY")
		}
		if originalModel != "" {
			_ = os.Setenv("NANO_MODEL", originalModel)
		} else {
			_ = os.Unsetenv("NANO_MODEL")
		}
		if originalVerbose != "" {
			_ = os.Setenv("NANO_VERBOSE", originalVerbose)
		} else {
			_ = os.Unsetenv("NANO_VERBOSE")
		}
	}()
	_ = os.Unsetenv("NANO_API_KEY")
	_ = os.Unsetenv("NANO_MODEL")
	_ = os.Unsetenv("NANO_VERBOSE")

	// Create a temporary directory
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	// Change to temp directory
	_ = os.Chdir(tempDir)

	// Create a .nano.yaml file
	nanoYamlContent := `api_key: test-key
model: test-model
verbose: false
read_file_max_lines: 500
`
	err := os.WriteFile(".nano.yaml", []byte(nanoYamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create .nano.yaml: %v", err)
	}

	// Load config
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Check that values from .nano.yaml are loaded
	if cfg.APIKey != "test-key" {
		t.Errorf("Expected APIKey 'test-key', got '%s'", cfg.APIKey)
	}

	if cfg.Model != "test-model" {
		t.Errorf("Expected Model 'test-model', got '%s'", cfg.Model)
	}

	if cfg.Verbose != false {
		t.Errorf("Expected Verbose false, got %v", cfg.Verbose)
	}

	if cfg.ReadFileMaxLines != 500 {
		t.Errorf("Expected ReadFileMaxLines 500, got %d", cfg.ReadFileMaxLines)
	}
}

func TestLoadConfig_EnvInterpolation(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	_ = os.Chdir(tempDir)
	t.Setenv("SYMPHONY_MCP_URL", "http://127.0.0.1:3456/mcp")
	t.Setenv("SYMPHONY_TOKEN", "secret-token")

	content := `mcp:
  servers:
    - name: symphony
      transport: streamable
      url: "${env:SYMPHONY_MCP_URL}"
      headers:
        X-Symphony-Token: "${env:SYMPHONY_TOKEN}"
`
	if err := os.WriteFile(".nano.yaml", []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if got := cfg.MCP.Servers[0].URL; got != "http://127.0.0.1:3456/mcp" {
		t.Fatalf("URL = %q", got)
	}
	if got := cfg.MCP.Servers[0].Headers["X-Symphony-Token"]; got != "secret-token" {
		t.Fatalf("header token = %q", got)
	}
}

func TestLoadConfig_EnvInterpolationMissingVar(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	_ = os.Chdir(tempDir)

	if err := os.WriteFile(".nano.yaml", []byte(`api_key: "${env:NANO_MISSING_TEST_SECRET}"`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig("")
	if err == nil || !strings.Contains(err.Error(), "NANO_MISSING_TEST_SECRET") {
		t.Fatalf("expected missing env error, got %v", err)
	}
}

func TestLoadConfig_SymphonyProfileInjection(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	_ = os.Chdir(tempDir)
	t.Setenv("SYMPHONY_MCP_URL", "http://127.0.0.1:3456/mcp")
	t.Setenv("SYMPHONY_TOKEN", "secret-token")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if !cfg.EnableMCP || len(cfg.MCP.Servers) == 0 {
		t.Fatalf("expected MCP server injection: %#v", cfg.MCP)
	}
	if cfg.MCP.Servers[0].Name != "symphony" || cfg.MCP.Servers[0].Transport != "streamable" {
		t.Fatalf("unexpected server: %#v", cfg.MCP.Servers[0])
	}
	found := false
	for _, name := range cfg.Skills.AutoActivate {
		if name == "nano-symphony" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected nano-symphony auto activation: %#v", cfg.Skills.AutoActivate)
	}
}

func TestLoadConfig_SpecificFile(t *testing.T) {
	// Save and clear environment variables that might override config
	originalAPIKey := os.Getenv("NANO_API_KEY")
	originalModel := os.Getenv("NANO_MODEL")
	defer func() {
		if originalAPIKey != "" {
			_ = os.Setenv("NANO_API_KEY", originalAPIKey)
		} else {
			_ = os.Unsetenv("NANO_API_KEY")
		}
		if originalModel != "" {
			_ = os.Setenv("NANO_MODEL", originalModel)
		} else {
			_ = os.Unsetenv("NANO_MODEL")
		}
	}()
	_ = os.Unsetenv("NANO_API_KEY")
	_ = os.Unsetenv("NANO_MODEL")

	// Create a temporary directory
	tempDir := t.TempDir()

	// Create a custom config file
	configPath := filepath.Join(tempDir, "custom.yaml")
	configContent := `api_key: custom-key
model: custom-model
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create custom config: %v", err)
	}

	// Load config with specific file
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Check that values from custom config are loaded
	if cfg.APIKey != "custom-key" {
		t.Errorf("Expected APIKey 'custom-key', got '%s'", cfg.APIKey)
	}

	if cfg.Model != "custom-model" {
		t.Errorf("Expected Model 'custom-model', got '%s'", cfg.Model)
	}
}

func TestLoadConfig_DisablesOSSWhenEnabledButIncomplete(t *testing.T) {
	originalOSSEnabled := os.Getenv("OSS_ENABLED")
	defer func() {
		if originalOSSEnabled != "" {
			_ = os.Setenv("OSS_ENABLED", originalOSSEnabled)
		} else {
			_ = os.Unsetenv("OSS_ENABLED")
		}
	}()

	_ = os.Setenv("OSS_ENABLED", "true")
	_ = os.Unsetenv("OSS_ACCESS_KEY_ID")
	_ = os.Unsetenv("OSS_ACCESS_KEY_SECRET")
	_ = os.Unsetenv("OSS_ENDPOINT")
	_ = os.Unsetenv("OSS_DEFAULT_BUCKET")
	_ = os.Unsetenv("OSS_REGION")
	_ = os.Unsetenv("ALIYUN_OSS_ACCESS_KEY_ID")
	_ = os.Unsetenv("ALIYUN_OSS_ACCESS_KEY_SECRET")
	_ = os.Unsetenv("ALIYUN_OSS_ENDPOINT")
	_ = os.Unsetenv("ALIYUN_OSS_BUCKET_NAME")
	_ = os.Unsetenv("ALIYUN_OSS_REGION")

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "custom.yaml")
	configContent := `model: test-model
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create custom config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.OSS == nil {
		t.Fatal("Expected OSS config to be non-nil")
	}
	if cfg.OSS.Enabled {
		t.Error("Expected OSS to be disabled when enabled but incomplete")
	}
}

func TestGetConfigLocations(t *testing.T) {
	// Test with empty config file
	locations := GetConfigLocations("")

	// Should have at least the project location (.nano.yaml)
	found := false
	for _, loc := range locations {
		if loc.Type == "Project" && filepath.Base(loc.Path) == ".nano.yaml" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected to find Project location for .nano.yaml")
	}

	// Test with specific config file
	locations = GetConfigLocations("/path/to/custom.yaml")

	// Should have user-specified location
	found = false
	for _, loc := range locations {
		if loc.Type == "User-specified" && loc.Path == "/path/to/custom.yaml" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected to find User-specified location")
	}
}

func TestGet(t *testing.T) {
	// First load a config
	_, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Test Get function
	cfg := Get()
	if cfg == nil {
		t.Fatal("Expected Get() to return non-nil config")
	}

	// Check that model is set (don't check specific value as it may come from global config)
	if cfg.Model == "" {
		t.Error("Expected model to be set, got empty string")
	}
}

func TestLoadConfig_ImageProviderEnvOverrides(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	_ = os.Chdir(tempDir)

	// Create a minimal .nano.yaml to prevent falling back to the user's global config
	minimalYAML := "api_key: \"test-key\"\nmodel: \"test-model\"\n"
	if err := os.WriteFile(filepath.Join(tempDir, ".nano.yaml"), []byte(minimalYAML), 0600); err != nil {
		t.Fatalf("failed to create .nano.yaml: %v", err)
	}

	// Set provider-specific env vars
	envVars := map[string]string{
		"SEEDREAM_API_KEY":       "seedream-test-key",
		"SEEDREAM_IMAGE_MODEL":   "doubao-seedream-4-0-test",
		"SEEDREAM_BASE_URL":      "https://ark.test.volces.com/api/v3",
		"OPENROUTER_IMAGE_MODEL": "google/gemini-2.0-flash-exp",
		"IMAGE_API_KEY":          "openrouter-test-key",
	}
	for k, v := range envVars {
		k, v := k, v // capture per-iteration
		old := os.Getenv(k)
		_ = os.Setenv(k, v)
		defer func() {
			if old != "" {
				_ = os.Setenv(k, old)
			} else {
				_ = os.Unsetenv(k)
			}
		}()
	}

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.ImageGenerator == nil {
		t.Fatal("Expected ImageGenerator config to be non-nil")
	}

	// Look up seedream provider
	seedreamCfg, found := cfg.ImageGenerator.GetProvider("seedream")
	if !found {
		t.Fatal("Expected seedream provider to be configured")
	}
	if seedreamCfg.APIKey != "seedream-test-key" {
		t.Errorf("Expected seedream APIKey 'seedream-test-key', got %q", seedreamCfg.APIKey)
	}
	if seedreamCfg.Model != "doubao-seedream-4-0-test" {
		t.Errorf("Expected seedream Model 'doubao-seedream-4-0-test', got %q", seedreamCfg.Model)
	}
	if seedreamCfg.BaseURL != "https://ark.test.volces.com/api/v3" {
		t.Errorf("Expected seedream BaseURL 'https://ark.test.volces.com/api/v3', got %q", seedreamCfg.BaseURL)
	}

	// Look up openrouter provider
	openrouterCfg, found := cfg.ImageGenerator.GetProvider("openrouter")
	if !found {
		t.Fatal("Expected openrouter provider to be configured")
	}
	if openrouterCfg.Model != "google/gemini-2.0-flash-exp" {
		t.Errorf("Expected openrouter Model 'google/gemini-2.0-flash-exp', got %q", openrouterCfg.Model)
	}
}

func TestOverrideImageProviderFromEnv_UpdateExisting(t *testing.T) {
	cfg := &ImageGeneratorConfig{
		Providers: []ImageGeneratorProviderConfig{
			{Provider: "openrouter", APIKey: "old-key", Model: "old-model"},
		},
	}

	overrideImageProviderFromEnv(cfg, "openrouter", "", "new-model", "")

	if cfg.Providers[0].Model != "new-model" {
		t.Errorf("Expected model 'new-model', got %q", cfg.Providers[0].Model)
	}
	// APIKey should not change when empty apiKey is passed
	if cfg.Providers[0].APIKey != "old-key" {
		t.Errorf("Expected APIKey to remain 'old-key', got %q", cfg.Providers[0].APIKey)
	}
}

func TestOverrideImageProviderFromEnv_CreateNew(t *testing.T) {
	cfg := &ImageGeneratorConfig{}

	overrideImageProviderFromEnv(cfg, "seedream", "my-key", "my-model", "https://example.com")

	if len(cfg.Providers) != 1 {
		t.Fatalf("Expected 1 provider, got %d", len(cfg.Providers))
	}
	p := cfg.Providers[0]
	if p.Provider != "seedream" {
		t.Errorf("Expected provider 'seedream', got %q", p.Provider)
	}
	if p.APIKey != "my-key" {
		t.Errorf("Expected APIKey 'my-key', got %q", p.APIKey)
	}
	if p.Model != "my-model" {
		t.Errorf("Expected Model 'my-model', got %q", p.Model)
	}
	if p.BaseURL != "https://example.com" {
		t.Errorf("Expected BaseURL 'https://example.com', got %q", p.BaseURL)
	}
	if !p.Enabled {
		t.Error("Expected Enabled to be true when APIKey is set")
	}
}

func TestOverrideImageProviderFromEnv_NoOp(t *testing.T) {
	cfg := &ImageGeneratorConfig{
		Providers: []ImageGeneratorProviderConfig{
			{Provider: "openrouter", APIKey: "key", Model: "model"},
		},
	}

	// All empty – should be a no-op
	overrideImageProviderFromEnv(cfg, "openrouter", "", "", "")

	if cfg.Providers[0].APIKey != "key" {
		t.Errorf("Expected APIKey to remain 'key', got %q", cfg.Providers[0].APIKey)
	}
	if cfg.Providers[0].Model != "model" {
		t.Errorf("Expected Model to remain 'model', got %q", cfg.Providers[0].Model)
	}
}
