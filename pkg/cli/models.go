package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// NewModelsCommand creates the legacy plural model/provider command.
func NewModelsCommand() *cobra.Command {
	return newModelCommand("models", "List provider/model presets and inspect model capabilities")
}

// NewModelCommand creates the singular command group described by the model
// routing refactor plan while keeping behavior shared with `models`.
func NewModelCommand() *cobra.Command {
	return newModelCommand("model", "Inspect and update model routing configuration")
}

func newModelCommand(use, short string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
	}

	var jsonOutput bool
	cmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output JSON")

	cmd.AddCommand(newModelListCommand(&jsonOutput))
	cmd.AddCommand(newModelDoctorCommand(&jsonOutput))
	cmd.AddCommand(newModelStatusCommand(&jsonOutput))
	cmd.AddCommand(newModelUseCommand())
	cmd.AddCommand(newModelFallbackCommand(&jsonOutput))

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	}
	return cmd
}

func newModelListCommand(jsonOutput *bool) *cobra.Command {
	var providersOnly bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List known provider/model presets",
		RunE: func(_ *cobra.Command, _ []string) error {
			presets := llm.KnownProviderPresets()
			if *jsonOutput {
				return writeJSON(presets)
			}
			for _, preset := range presets {
				fmt.Printf("%s (%s)\n", preset.DisplayName, preset.ID)
				if preset.BaseURL != "" {
					fmt.Printf("  base_url: %s\n", preset.BaseURL)
				}
				if providersOnly {
					continue
				}
				for _, model := range preset.Models {
					fmt.Printf("  - %s/%s", preset.ID, model.ID)
					if model.Capabilities.Reasoning {
						fmt.Print(" reasoning")
					}
					if model.Capabilities.Vision {
						fmt.Print(" vision")
					}
					if model.Capabilities.Embedding {
						fmt.Print(" embedding")
					}
					if model.Capabilities.LongContext {
						fmt.Print(" long-context")
					}
					fmt.Println()
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&providersOnly, "providers", false, "list providers without model entries")
	return cmd
}

func newModelDoctorCommand(jsonOutput *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor [model]",
		Short: "Inspect configured or specified model capabilities",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := loadModelConfig()
			if err != nil {
				return err
			}
			model := cfg.Model
			if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
				model = args[0]
			}
			desc := llm.DescribeConfiguredModel(cfg.BaseURL, model)
			if *jsonOutput {
				return writeJSON(desc)
			}
			printModelDescriptor(desc)
			return nil
		},
	}
}

func newModelStatusCommand(jsonOutput *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show active model, provider, reasoning, and fallback routes",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadModelConfig()
			if err != nil {
				return err
			}
			routes, routeErr := llm.ResolveRouteList(cfg)
			desc := llm.DescribeConfiguredModel(cfg.BaseURL, cfg.Model)
			fallbacks := configuredModelFallbacks(cfg)
			status := map[string]any{
				"model":          cfg.Model,
				"base_url":       cfg.BaseURL,
				"provider":       desc.ProviderID,
				"known":          desc.Known,
				"profile":        desc.Profile,
				"capabilities":   desc.Capabilities,
				"fallbacks":      fallbacks,
				"fallback_count": len(fallbacks),
			}
			if routeErr == nil {
				status["routes"] = sanitizedRoutes(routes)
				if len(routes) > 0 {
					status["model"] = routes[0].ProviderID + "/" + routes[0].Model
					status["base_url"] = routes[0].BaseURL
					status["provider"] = routes[0].ProviderID
					status["fallback_count"] = len(routes) - 1
				}
			} else {
				status["route_error"] = routeErr.Error()
			}
			if cfg.Reasoning != nil {
				status["reasoning"] = map[string]any{
					"enabled":    cfg.Reasoning.IsEffectivelyEnabled(),
					"source":     cfg.Reasoning.GetRuntimeSource(),
					"effort":     cfg.Reasoning.Effort,
					"max_tokens": cfg.Reasoning.MaxTokens,
					"exclude":    cfg.Reasoning.Exclude,
				}
			}
			if *jsonOutput {
				return writeJSON(status)
			}
			fmt.Printf("model: %s\n", cfg.Model)
			fmt.Printf("provider: %s\n", desc.ProviderID)
			fmt.Printf("base_url: %s\n", cfg.BaseURL)
			fmt.Printf("known: %t\n", desc.Known)
			if cfg.Reasoning != nil {
				fmt.Printf("thinking: enabled=%t source=%s effort=%s max_tokens=%d exclude=%t\n",
					cfg.Reasoning.IsEffectivelyEnabled(),
					cfg.Reasoning.GetRuntimeSource(),
					cfg.Reasoning.Effort,
					cfg.Reasoning.MaxTokens,
					cfg.Reasoning.Exclude,
				)
			}
			if routeErr != nil {
				fmt.Printf("route_error: %v\n", routeErr)
			} else {
				fmt.Printf("routes: %d\n", len(routes))
				for i, route := range routes {
					role := "fallback"
					if i == 0 {
						role = "primary"
					}
					fmt.Printf("  - %s %s provider=%s model=%s base_url=%s api_key=%s breaker_state=unavailable\n",
						role, route.Name, route.ProviderID, route.Model, route.BaseURL, routeAPIKeyStatus(route))
				}
			}
			return nil
		},
	}
}

func newModelUseCommand() *cobra.Command {
	var providerID, baseURL string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "use <model>",
		Short: "Persist the primary model in .nano.yaml",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			model := strings.TrimSpace(args[0])
			if model == "" {
				return fmt.Errorf("model is required")
			}
			resolvedBaseURL := strings.TrimSpace(baseURL)
			var preset llm.ProviderPreset
			if providerID != "" {
				var ok bool
				preset, ok = llm.GetProviderPreset(providerID)
				if !ok {
					return fmt.Errorf("unknown provider %q", providerID)
				}
				if resolvedBaseURL == "" {
					resolvedBaseURL = preset.BaseURL
				}
			}
			if dryRun {
				fmt.Printf("model: %s\n", model)
				if resolvedBaseURL != "" {
					fmt.Printf("base_url: %s\n", resolvedBaseURL)
				}
				return nil
			}
			cfgMap, err := loadProjectConfigMap()
			if err != nil {
				return err
			}
			setNestedRawKey(cfgMap, "model", model)
			if resolvedBaseURL != "" {
				setNestedRawKey(cfgMap, "base_url", resolvedBaseURL)
			}
			if err := writeProjectConfigMap(cfgMap); err != nil {
				return err
			}
			fmt.Printf("Set model = %s\n", model)
			if resolvedBaseURL != "" {
				fmt.Printf("Set base_url = %s\n", resolvedBaseURL)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&providerID, "provider", "", "provider preset id")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "OpenAI-compatible base URL")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print changes without writing .nano.yaml")
	return cmd
}

func newModelFallbackCommand(jsonOutput *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fallback",
		Short: "Manage model fallback routes",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printModelFallbacks(*jsonOutput)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List configured fallback routes",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printModelFallbacks(*jsonOutput)
		},
	})
	cmd.AddCommand(newModelFallbackAddCommand())
	cmd.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Remove all configured fallback routes",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfgMap, err := loadProjectConfigMap()
			if err != nil {
				return err
			}
			setNestedRawKey(cfgMap, "fallbacks", []string{})
			setNestedRawKey(cfgMap, "model_routing.fallbacks", []map[string]interface{}{})
			if err := writeProjectConfigMap(cfgMap); err != nil {
				return err
			}
			fmt.Println("Cleared model fallback routes")
			return nil
		},
	})
	return cmd
}

func newModelFallbackAddCommand() *cobra.Command {
	var name, providerID, baseURL, apiKey, apiKeyEnv string
	cmd := &cobra.Command{
		Use:   "add <model>",
		Short: "Add or replace a fallback route in .nano.yaml",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			model := strings.TrimSpace(args[0])
			if model == "" {
				return fmt.Errorf("model is required")
			}
			if !strings.Contains(model, "/") && providerID == "" {
				return fmt.Errorf("fallback model must be provider/model or use --provider")
			}
			if providerID != "" && !strings.Contains(model, "/") {
				model = strings.TrimSpace(providerID) + "/" + model
			}
			routeName := strings.TrimSpace(name)
			if routeName == "" {
				routeName = model
			}
			cfgMap, err := loadProjectConfigMap()
			if err != nil {
				return err
			}
			refs := fallbackRefs(cfgMap)
			replaced := false
			for i, existing := range refs {
				if existing == routeName || existing == model {
					refs[i] = model
					replaced = true
					break
				}
			}
			if !replaced {
				refs = append(refs, model)
			}
			setNestedRawKey(cfgMap, "fallbacks", refs)
			routes := fallbackRouteMaps(cfgMap)
			legacyModel := model
			if providerID != "" {
				legacyModel = strings.TrimPrefix(model, strings.TrimSpace(providerID)+"/")
			}
			route := map[string]interface{}{"name": routeName, "model": legacyModel}
			if strings.TrimSpace(baseURL) != "" {
				route["base_url"] = strings.TrimSpace(baseURL)
			}
			if strings.TrimSpace(apiKey) != "" {
				route["api_key"] = strings.TrimSpace(apiKey)
			}
			if strings.TrimSpace(apiKeyEnv) != "" {
				route["api_key_env"] = strings.TrimSpace(apiKeyEnv)
			}
			replaced = false
			for i, existing := range routes {
				if existingName, _ := existing["name"].(string); existingName == routeName {
					routes[i] = route
					replaced = true
					break
				}
			}
			if !replaced {
				routes = append(routes, route)
			}
			setNestedRawKey(cfgMap, "model_routing.fallbacks", routes)
			provider, _ := llm.ParseModelRef(model, "")
			if provider != "" {
				if strings.TrimSpace(baseURL) != "" {
					setNestedRawKey(cfgMap, "providers."+provider+".base_url", strings.TrimSpace(baseURL))
				}
				if strings.TrimSpace(apiKey) != "" {
					setNestedRawKey(cfgMap, "providers."+provider+".api_key", strings.TrimSpace(apiKey))
				}
				if strings.TrimSpace(apiKeyEnv) != "" {
					setNestedRawKey(cfgMap, "providers."+provider+".api_key_env", strings.TrimSpace(apiKeyEnv))
				}
			}
			if err := writeProjectConfigMap(cfgMap); err != nil {
				return err
			}
			fmt.Printf("Added fallback %s\n", model)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "fallback route name")
	cmd.Flags().StringVar(&providerID, "provider", "", "provider preset id")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "OpenAI-compatible base URL")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key to store in .nano.yaml (prefer --api-key-env)")
	cmd.Flags().StringVar(&apiKeyEnv, "api-key-env", "", "environment variable that contains the provider API key")
	return cmd
}

// NewThinkCommand creates `nano think on|off|status`, a CLI companion to the
// interactive /think command and reasoning configuration.
func NewThinkCommand() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "think",
		Short: "Manage reasoning/thinking mode",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output JSON")
	cmd.AddCommand(newThinkStatusCommand(&jsonOutput))
	cmd.AddCommand(newThinkSetCommand("on", true))
	cmd.AddCommand(newThinkSetCommand("off", false))
	return cmd
}

func newThinkStatusCommand(jsonOutput *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show effective reasoning/thinking configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadModelConfig()
			if err != nil {
				return err
			}
			reasoning := cfg.Reasoning
			status := map[string]any{
				"enabled": false,
				"source":  "default",
			}
			if reasoning != nil {
				status["enabled"] = reasoning.IsEffectivelyEnabled()
				status["source"] = reasoning.GetRuntimeSource()
				status["effort"] = reasoning.Effort
				status["max_tokens"] = reasoning.MaxTokens
				status["exclude"] = reasoning.Exclude
			}
			if *jsonOutput {
				return writeJSON(status)
			}
			fmt.Printf("thinking: enabled=%v source=%v", status["enabled"], status["source"])
			if reasoning != nil {
				fmt.Printf(" effort=%s max_tokens=%d exclude=%t", reasoning.Effort, reasoning.MaxTokens, reasoning.Exclude)
			}
			fmt.Println()
			return nil
		},
	}
}

func newThinkSetCommand(use string, enabled bool) *cobra.Command {
	var effort string
	var maxTokens int
	var exclude bool
	cmd := &cobra.Command{
		Use:   use,
		Short: fmt.Sprintf("Turn thinking mode %s", use),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgMap, err := loadProjectConfigMap()
			if err != nil {
				return err
			}
			setNestedRawKey(cfgMap, "reasoning.enabled", enabled)
			if strings.TrimSpace(effort) != "" {
				setNestedRawKey(cfgMap, "reasoning.effort", strings.ToLower(strings.TrimSpace(effort)))
			}
			if maxTokens >= 0 {
				setNestedRawKey(cfgMap, "reasoning.max_tokens", maxTokens)
			}
			if cmd.Flags().Changed("exclude") {
				setNestedRawKey(cfgMap, "reasoning.exclude", exclude)
			}
			if err := writeProjectConfigMap(cfgMap); err != nil {
				return err
			}
			fmt.Printf("thinking: enabled=%t\n", enabled)
			return nil
		},
	}
	cmd.Flags().StringVar(&effort, "effort", "", "reasoning effort: low, medium, or high")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", -1, "reasoning token budget")
	cmd.Flags().BoolVar(&exclude, "exclude", false, "exclude reasoning tokens from responses")
	return cmd
}

func loadModelConfig() (*config.Config, error) {
	cfg := config.Get()
	if cfg != nil {
		return cfg, nil
	}
	return config.LoadConfig("")
}

func printModelDescriptor(desc llm.ModelDescriptor) {
	fmt.Printf("model: %s\n", desc.ID)
	fmt.Printf("provider: %s\n", desc.ProviderID)
	fmt.Printf("known: %t\n", desc.Known)
	fmt.Printf("context_window: %d\n", desc.Profile.ContextWindow)
	fmt.Printf("max_output_tokens: %d\n", desc.Profile.MaxOutputTokens)
	fmt.Printf("capabilities: tools=%t reasoning=%t vision=%t streaming=%t tool_choice=%t parallel_tool_calls=%t json_schema=%t embedding=%t long_context=%t\n",
		desc.Capabilities.Tools,
		desc.Capabilities.Reasoning,
		desc.Capabilities.Vision,
		desc.Capabilities.Streaming,
		desc.Capabilities.ToolChoice,
		desc.Capabilities.ParallelToolCalls,
		desc.Capabilities.JSONSchema,
		desc.Capabilities.Embedding,
		desc.Capabilities.LongContext,
	)
}

func configuredModelFallbacks(cfg *config.Config) []config.ModelRouteConfig {
	if cfg == nil {
		return nil
	}
	if len(cfg.Fallbacks) > 0 {
		routes := make([]config.ModelRouteConfig, 0, len(cfg.Fallbacks))
		for _, ref := range cfg.Fallbacks {
			routes = append(routes, config.ModelRouteConfig{Name: ref, Model: ref})
		}
		return routes
	}
	if cfg.ModelRouting == nil {
		return nil
	}
	return append([]config.ModelRouteConfig(nil), cfg.ModelRouting.Fallbacks...)
}

func printModelFallbacks(jsonOutput bool) error {
	cfg, err := loadModelConfig()
	if err != nil {
		return err
	}
	routes, routeErr := llm.ResolveRouteList(cfg)
	fallbacks := configuredModelFallbacks(cfg)
	if jsonOutput {
		if routeErr == nil && len(routes) > 1 {
			return writeJSON(sanitizedRoutes(routes[1:]))
		}
		return writeJSON(fallbacks)
	}
	if routeErr == nil && len(routes) > 1 {
		for _, route := range routes[1:] {
			fmt.Printf("%s provider=%s model=%s base_url=%s api_key=%s\n", route.Name, route.ProviderID, route.Model, route.BaseURL, routeAPIKeyStatus(route))
		}
		return nil
	}
	if len(fallbacks) == 0 {
		fmt.Println("No model fallback routes configured")
		return nil
	}
	for _, route := range fallbacks {
		fmt.Printf("%s model=%s base_url=%s\n", route.Name, route.Model, route.BaseURL)
	}
	return nil
}

func loadProjectConfigMap() (map[string]interface{}, error) {
	data, err := os.ReadFile(".nano.yaml")
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read .nano.yaml: %w", err)
	}
	cfgMap := make(map[string]interface{})
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &cfgMap); err != nil {
			return nil, fmt.Errorf("failed to parse .nano.yaml: %w", err)
		}
	}
	return cfgMap, nil
}

func writeProjectConfigMap(cfgMap map[string]interface{}) error {
	out, err := yaml.Marshal(cfgMap)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(".nano.yaml", out, 0o600); err != nil {
		return fmt.Errorf("failed to write .nano.yaml: %w", err)
	}
	return nil
}

func setNestedRawKey(m map[string]interface{}, key string, value interface{}) {
	parts := strings.Split(key, ".")
	current := m
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return
		}
		// Match `nano config set` behavior: if an intermediate key exists but is
		// not a map, replace it so the requested dot-path can be written.
		nextMap, ok := current[part].(map[string]interface{})
		if !ok {
			nextMap = make(map[string]interface{})
			current[part] = nextMap
		}
		current = nextMap
	}
}

func fallbackRouteMaps(cfgMap map[string]interface{}) []map[string]interface{} {
	modelRouting, ok := cfgMap["model_routing"].(map[string]interface{})
	if !ok {
		return nil
	}
	// yaml.v2 unmarshals file data as []interface{}, while tests and in-memory
	// updates can pass []map[string]interface{}; support both representations.
	rawRoutes, ok := modelRouting["fallbacks"].([]interface{})
	if !ok {
		if typed, ok := modelRouting["fallbacks"].([]map[string]interface{}); ok {
			return append([]map[string]interface{}(nil), typed...)
		}
		return nil
	}
	routes := make([]map[string]interface{}, 0, len(rawRoutes))
	for _, raw := range rawRoutes {
		if route, ok := raw.(map[string]interface{}); ok {
			routes = append(routes, route)
		}
	}
	return routes
}

func fallbackRefs(cfgMap map[string]interface{}) []string {
	raw, ok := cfgMap["fallbacks"].([]interface{})
	if !ok {
		if typed, ok := cfgMap["fallbacks"].([]string); ok {
			return append([]string(nil), typed...)
		}
		return nil
	}
	refs := make([]string, 0, len(raw))
	for _, item := range raw {
		if ref, ok := item.(string); ok && strings.TrimSpace(ref) != "" {
			refs = append(refs, strings.TrimSpace(ref))
		}
	}
	return refs
}

func sanitizedRoutes(routes []llm.ResolvedRoute) []map[string]any {
	out := make([]map[string]any, 0, len(routes))
	for _, route := range routes {
		out = append(out, map[string]any{
			"name":          route.Name,
			"provider":      route.ProviderID,
			"model":         route.Model,
			"base_url":      route.BaseURL,
			"profile":       route.Profile,
			"api_key":       routeAPIKeyStatus(route),
			"breaker_state": "unavailable",
		})
	}
	return out
}

func routeAPIKeyStatus(route llm.ResolvedRoute) string {
	if route.APIKey == "" {
		return "unset"
	}
	return "set"
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
