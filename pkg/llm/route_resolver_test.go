package llm

import (
	"os"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestResolveRoutesLegacyOnly(t *testing.T) {
	cfg := &config.Config{APIKey: "legacy-key", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat"}
	primary, fallbacks, err := ResolveRoutes(cfg)
	if err != nil {
		t.Fatalf("ResolveRoutes failed: %v", err)
	}
	if primary.ProviderID != legacyProviderID || primary.Model != "deepseek-chat" || primary.APIKey != "legacy-key" {
		t.Fatalf("unexpected primary: %+v", primary)
	}
	if len(fallbacks) != 0 {
		t.Fatalf("fallbacks = %d, want 0", len(fallbacks))
	}
}

func TestResolveRoutesLegacyWithModelRouting(t *testing.T) {
	cfg := &config.Config{
		APIKey:  "legacy-key",
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-chat",
		ModelRouting: &config.ModelRoutingConfig{Fallbacks: []config.ModelRouteConfig{{
			Name: "fast", Model: "gpt-4.1", BaseURL: "https://api.openai.com/v1", APIKey: "fallback-key",
		}}},
	}
	primary, fallbacks, err := ResolveRoutes(cfg)
	if err != nil {
		t.Fatalf("ResolveRoutes failed: %v", err)
	}
	if primary.ProviderID != legacyProviderID {
		t.Fatalf("primary provider = %q, want _legacy", primary.ProviderID)
	}
	if len(fallbacks) != 1 {
		t.Fatalf("fallbacks = %d, want 1", len(fallbacks))
	}
	if fallbacks[0].Name != "fast" || fallbacks[0].Model != "gpt-4.1" || fallbacks[0].APIKey != "fallback-key" {
		t.Fatalf("unexpected fallback: %+v", fallbacks[0])
	}
}

func TestResolveRoutesNewProvidersOnly(t *testing.T) {
	t.Setenv("TEST_DEEPSEEK_KEY", "deepseek-key")
	cfg := &config.Config{
		Model:     "deepseek/deepseek-chat",
		Fallbacks: []string{"openai/gpt-4.1"},
		ProvidersBlock: map[string]config.ProviderBlock{
			"deepseek": {APIKeyEnv: "TEST_DEEPSEEK_KEY"},
			"openai":   {APIKey: "openai-key"},
		},
	}
	primary, fallbacks, err := ResolveRoutes(cfg)
	if err != nil {
		t.Fatalf("ResolveRoutes failed: %v", err)
	}
	if primary.ProviderID != "deepseek" || primary.BaseURL != "https://api.deepseek.com/v1" || primary.APIKey != "deepseek-key" {
		t.Fatalf("unexpected primary: %+v", primary)
	}
	if len(fallbacks) != 1 || fallbacks[0].ProviderID != "openai" || fallbacks[0].APIKey != "openai-key" {
		t.Fatalf("unexpected fallbacks: %+v", fallbacks)
	}
}

func TestResolveRoutesProvidersPreferNewSchema(t *testing.T) {
	cfg := &config.Config{
		APIKey: "legacy-key", BaseURL: "https://api.deepseek.com/v1", Model: "openai/gpt-4.1",
		ProvidersBlock: map[string]config.ProviderBlock{"openai": {APIKey: "new-key"}},
	}
	primary, _, err := ResolveRoutes(cfg)
	if err != nil {
		t.Fatalf("ResolveRoutes failed: %v", err)
	}
	if primary.ProviderID != "openai" || primary.APIKey != "new-key" {
		t.Fatalf("providers did not win over legacy fields: %+v", primary)
	}
}

func TestResolveRoutesEnvAPIKeyPriority(t *testing.T) {
	t.Setenv("NANO_DEEPSEEK_API_KEY", "nano-provider-key")
	t.Setenv("DEEPSEEK_API_KEY", "preset-key")
	t.Setenv("NANO_API_KEY", "legacy-key")
	cfg := &config.Config{Model: "deepseek/deepseek-chat", ProvidersBlock: map[string]config.ProviderBlock{"deepseek": {}}}
	primary, _, err := ResolveRoutes(cfg)
	if err != nil {
		t.Fatalf("ResolveRoutes failed: %v", err)
	}
	if primary.APIKey != "nano-provider-key" {
		t.Fatalf("api key = %q, want provider-specific env", primary.APIKey)
	}
}

func TestResolveRoutesInferredProviderForBareNewModel(t *testing.T) {
	cfg := &config.Config{Model: "deepseek-chat", ProvidersBlock: map[string]config.ProviderBlock{"deepseek": {APIKey: "key"}}}
	primary, _, err := ResolveRoutes(cfg)
	if err != nil {
		t.Fatalf("ResolveRoutes failed: %v", err)
	}
	if primary.ProviderID != "deepseek" || primary.Model != "deepseek-chat" {
		t.Fatalf("unexpected inferred route: %+v", primary)
	}
}

func TestParseModelRefBareUsesDefaultBaseURL(t *testing.T) {
	provider, model := ParseModelRef("kimi-k2", "https://api.moonshot.cn/v1")
	if provider != "moonshot" || model != "kimi-k2" {
		t.Fatalf("ParseModelRef = %s/%s, want moonshot/kimi-k2", provider, model)
	}
}

func TestResolveRoutesProviderPresetEnvFallback(t *testing.T) {
	old := os.Getenv("OPENAI_API_KEY")
	t.Setenv("OPENAI_API_KEY", "preset-env-key")
	defer os.Setenv("OPENAI_API_KEY", old)
	cfg := &config.Config{Model: "openai/gpt-4.1", ProvidersBlock: map[string]config.ProviderBlock{"openai": {}}}
	primary, _, err := ResolveRoutes(cfg)
	if err != nil {
		t.Fatalf("ResolveRoutes failed: %v", err)
	}
	if primary.APIKey != "preset-env-key" {
		t.Fatalf("api key = %q, want preset env key", primary.APIKey)
	}
}

func TestResolveRoutesChildConfigWithCustomFallbacks(t *testing.T) {
	cfg := &config.Config{
		Model:     "openai/gpt-4o",
		Fallbacks: []string{"moonshot/kimi-k2"},
		ProvidersBlock: map[string]config.ProviderBlock{
			"deepseek": {APIKey: "deepseek-key"},
			"openai":   {APIKey: "openai-key"},
			"moonshot": {APIKey: "moonshot-key"},
		},
	}
	primary, fallbacks, err := ResolveRoutes(cfg)
	if err != nil {
		t.Fatalf("ResolveRoutes failed: %v", err)
	}
	if primary.ProviderID != "openai" || primary.APIKey != "openai-key" {
		t.Fatalf("unexpected primary: %+v", primary)
	}
	if len(fallbacks) != 1 || fallbacks[0].ProviderID != "moonshot" || fallbacks[0].APIKey != "moonshot-key" {
		t.Fatalf("unexpected fallbacks: %+v", fallbacks)
	}
}
