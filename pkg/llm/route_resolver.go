package llm

import (
	"fmt"
	"os"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

const legacyProviderID = "_legacy"

// ResolvedRoute is the normalized model endpoint consumed by runtime routing.
type ResolvedRoute struct {
	Name       string
	ProviderID string
	Model      string
	BaseURL    string
	APIKey     string
	Profile    ModelProfile
	Headers    map[string]string
}

// ResolveRoutes normalizes legacy and provider/model configuration into primary plus fallback routes.
func ResolveRoutes(cfg *config.Config) (ResolvedRoute, []ResolvedRoute, error) {
	if cfg == nil {
		return ResolvedRoute{}, nil, fmt.Errorf("config is required")
	}
	if len(cfg.ProvidersBlock) > 0 && hasLegacyLLMFields(cfg) {
		logger.Warn("providers block is configured; legacy api_key/base_url/model endpoint fields are ignored for provider routes")
	}

	primary, err := resolvePrimaryRoute(cfg)
	if err != nil {
		return ResolvedRoute{}, nil, err
	}

	fallbacks, err := resolveFallbackRoutes(cfg, primary)
	if err != nil {
		return ResolvedRoute{}, nil, err
	}
	return primary, fallbacks, nil
}

// ResolveRouteList returns all normalized routes in selection order.
func ResolveRouteList(cfg *config.Config) ([]ResolvedRoute, error) {
	primary, fallbacks, err := ResolveRoutes(cfg)
	if err != nil {
		return nil, err
	}
	routes := make([]ResolvedRoute, 0, 1+len(fallbacks))
	routes = append(routes, primary)
	routes = append(routes, fallbacks...)
	return routes, nil
}

// ParseModelRef parses "provider/model" or a bare model reference.
func ParseModelRef(ref string, defaultBaseURL string) (provider, model string) {
	ref = strings.TrimSpace(ref)
	if before, after, ok := strings.Cut(ref, "/"); ok {
		return strings.ToLower(strings.TrimSpace(before)), strings.TrimSpace(after)
	}
	if strings.TrimSpace(defaultBaseURL) == "" {
		return DescribeModel("", ref).ProviderID, ref
	}
	return InferProviderID(defaultBaseURL, ref), ref
}

func resolvePrimaryRoute(cfg *config.Config) (ResolvedRoute, error) {
	if len(cfg.ProvidersBlock) == 0 {
		return resolveLegacyPrimary(cfg)
	}
	return resolveProviderModelRoute(cfg, cfg.Model, "primary", nil)
}

func resolveLegacyPrimary(cfg *config.Config) (ResolvedRoute, error) {
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return ResolvedRoute{}, fmt.Errorf("model is required")
	}
	providerID, bareModel := ParseModelRef(model, cfg.BaseURL)
	if !strings.Contains(model, "/") {
		providerID = legacyProviderID
	}
	baseURL := NewProviderInfo(cfg.BaseURL).BaseURL
	return ResolvedRoute{
		Name:       routeName(providerID, bareModel, "primary"),
		ProviderID: providerID,
		Model:      bareModel,
		BaseURL:    baseURL,
		APIKey:     cfg.APIKey,
		Profile:    InferModelProfile(bareModel),
	}, nil
}

func resolveFallbackRoutes(cfg *config.Config, primary ResolvedRoute) ([]ResolvedRoute, error) {
	var routes []ResolvedRoute
	if len(cfg.Fallbacks) > 0 {
		for i, ref := range cfg.Fallbacks {
			route, err := resolveProviderModelRoute(cfg, ref, fmt.Sprintf("fallback-%d", i+1), nil)
			if err != nil {
				return nil, err
			}
			routes = append(routes, route)
		}
		return dedupeRouteNames(routes, primary.Name), nil
	}
	if cfg.ModelRouting == nil || len(cfg.ModelRouting.Fallbacks) == 0 {
		return nil, nil
	}
	for i, fallback := range cfg.ModelRouting.Fallbacks {
		if len(cfg.ProvidersBlock) > 0 {
			route, err := resolveProviderModelRoute(cfg, fallback.Model, fallbackName(fallback, i), &fallback)
			if err != nil {
				return nil, err
			}
			routes = append(routes, route)
			continue
		}
		route := resolveLegacyFallback(cfg, fallback, i, primary)
		routes = append(routes, route)
	}
	return dedupeRouteNames(routes, primary.Name), nil
}

func resolveLegacyFallback(cfg *config.Config, fallback config.ModelRouteConfig, idx int, primary ResolvedRoute) ResolvedRoute {
	model := strings.TrimSpace(fallback.Model)
	if model == "" {
		model = primary.Model
	}
	baseURL := strings.TrimSpace(fallback.BaseURL)
	if baseURL == "" {
		baseURL = cfg.BaseURL
	}
	providerID := legacyProviderID
	if strings.Contains(model, "/") {
		providerID, model = ParseModelRef(model, baseURL)
	}
	apiKey := cfg.APIKey
	if fallback.APIKey != "" {
		apiKey = fallback.APIKey
	} else if fallback.APIKeyEnv != "" {
		apiKey = os.Getenv(fallback.APIKeyEnv)
	}
	name := strings.TrimSpace(fallback.Name)
	if name == "" {
		name = fmt.Sprintf("fallback-%d", idx+1)
	}
	return ResolvedRoute{
		Name:       name,
		ProviderID: providerID,
		Model:      model,
		BaseURL:    NewProviderInfo(baseURL).BaseURL,
		APIKey:     apiKey,
		Profile:    InferModelProfile(model),
	}
}

func resolveProviderModelRoute(cfg *config.Config, ref, defaultName string, legacy *config.ModelRouteConfig) (ResolvedRoute, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ResolvedRoute{}, fmt.Errorf("model reference is required")
	}
	legacyBaseURL := ""
	if legacy != nil {
		legacyBaseURL = legacy.BaseURL
	}
	providerID, model := ParseModelRef(ref, legacyBaseURL)
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	if providerID == "" {
		providerID = "openai-compatible"
	}
	block := providerBlockFor(cfg, providerID)
	preset, hasPreset := GetProviderPreset(providerID)

	baseURL := strings.TrimSpace(block.BaseURL)
	if legacy != nil && strings.TrimSpace(legacy.BaseURL) != "" {
		baseURL = strings.TrimSpace(legacy.BaseURL)
	}
	if baseURL == "" && hasPreset {
		baseURL = preset.BaseURL
	}
	if baseURL == "" && providerID == "openai" {
		baseURL = defaultOpenAIBaseURL
	}
	apiKey := resolveProviderAPIKey(providerID, block, preset, hasPreset)
	if legacy != nil {
		if strings.TrimSpace(legacy.APIKey) != "" {
			apiKey = strings.TrimSpace(legacy.APIKey)
		} else if strings.TrimSpace(legacy.APIKeyEnv) != "" {
			apiKey = os.Getenv(strings.TrimSpace(legacy.APIKeyEnv))
		}
	}
	name := defaultName
	if legacy != nil && strings.TrimSpace(legacy.Name) != "" {
		name = strings.TrimSpace(legacy.Name)
	} else if defaultName == "primary" {
		name = routeName(providerID, model, defaultName)
	}
	return ResolvedRoute{
		Name:       name,
		ProviderID: providerID,
		Model:      model,
		BaseURL:    NewProviderInfo(baseURL).BaseURL,
		APIKey:     apiKey,
		Profile:    InferModelProfile(model),
		Headers:    copyHeaders(block.Headers),
	}, nil
}

func resolveProviderAPIKey(providerID string, block config.ProviderBlock, preset ProviderPreset, hasPreset bool) string {
	if strings.TrimSpace(block.APIKey) != "" {
		return strings.TrimSpace(block.APIKey)
	}
	if strings.TrimSpace(block.APIKeyEnv) != "" {
		return os.Getenv(strings.TrimSpace(block.APIKeyEnv))
	}
	if value := os.Getenv("NANO_" + providerEnvName(providerID) + "_API_KEY"); value != "" {
		return value
	}
	if hasPreset && strings.TrimSpace(preset.APIKeyEnv) != "" {
		return os.Getenv(strings.TrimSpace(preset.APIKeyEnv))
	}
	return ""
}

func providerBlockFor(cfg *config.Config, providerID string) config.ProviderBlock {
	if cfg == nil || len(cfg.ProvidersBlock) == 0 {
		return config.ProviderBlock{}
	}
	if block, ok := cfg.ProvidersBlock[providerID]; ok {
		return block
	}
	for key, block := range cfg.ProvidersBlock {
		if strings.EqualFold(strings.TrimSpace(key), providerID) {
			return block
		}
	}
	return config.ProviderBlock{}
}

func providerEnvName(providerID string) string {
	replacer := strings.NewReplacer("-", "_", ".", "_", "/", "_")
	return strings.ToUpper(replacer.Replace(providerID))
}

func hasLegacyLLMFields(cfg *config.Config) bool {
	return strings.TrimSpace(cfg.APIKey) != "" || strings.TrimSpace(cfg.BaseURL) != "" || (strings.TrimSpace(cfg.Model) != "" && !strings.Contains(cfg.Model, "/"))
}

func fallbackName(fallback config.ModelRouteConfig, idx int) string {
	if name := strings.TrimSpace(fallback.Name); name != "" {
		return name
	}
	return fmt.Sprintf("fallback-%d", idx+1)
}

func routeName(providerID, model, fallback string) string {
	if providerID == "" || model == "" {
		return fallback
	}
	return providerID + "/" + model
}

func dedupeRouteNames(routes []ResolvedRoute, used ...string) []ResolvedRoute {
	seen := make(map[string]int)
	for _, name := range used {
		if name != "" {
			seen[name]++
		}
	}
	out := make([]ResolvedRoute, len(routes))
	copy(out, routes)
	for i := range out {
		base := strings.TrimSpace(out[i].Name)
		if base == "" {
			base = fmt.Sprintf("route-%d", i+1)
		}
		seen[base]++
		if seen[base] > 1 {
			out[i].Name = fmt.Sprintf("%s-%d", base, seen[base])
		} else {
			out[i].Name = base
		}
	}
	return out
}

func copyHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		out[key] = value
	}
	return out
}
