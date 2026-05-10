package slash

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	agentpkg "github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"gopkg.in/yaml.v2"
)

// BuildModelLister returns a TUI-friendly provider/model preset listing.
func BuildModelLister(cfg *config.Config) func() string {
	return func() string {
		var b strings.Builder
		for _, preset := range llm.KnownProviderPresets() {
			fmt.Fprintf(&b, "%s (%s)\n", preset.DisplayName, preset.ID)
			if preset.BaseURL != "" {
				fmt.Fprintf(&b, "  base_url: %s\n", preset.BaseURL)
			}
			for _, model := range preset.Models {
				fmt.Fprintf(&b, "  - %s/%s", preset.ID, model.ID)
				if cfg != nil && llm.NormalizeModelID(cfg.Model) == model.ID {
					b.WriteString(" current")
				}
				if model.Capabilities.Reasoning {
					b.WriteString(" reasoning")
				}
				if model.Capabilities.Vision {
					b.WriteString(" vision")
				}
				if model.Capabilities.Embedding {
					b.WriteString(" embedding")
				}
				if model.Capabilities.LongContext {
					b.WriteString(" long-context")
				}
				b.WriteString("\n")
			}
		}
		return strings.TrimRight(b.String(), "\n")
	}
}

func BuildModelStatusGetter(cfg *config.Config) func() string {
	return func() string {
		activeCfg := cfg
		if activeCfg == nil {
			activeCfg = config.Get()
		}
		if activeCfg == nil {
			return "⚠️  配置未加载。"
		}
		desc := llm.DescribeConfiguredModel(activeCfg.BaseURL, activeCfg.Model)
		var b strings.Builder
		fmt.Fprintf(&b, "model: %s\nprovider: %s\nbase_url: %s\nknown: %t",
			activeCfg.Model, desc.ProviderID, activeCfg.BaseURL, desc.Known)
		if activeCfg.Reasoning != nil {
			fmt.Fprintf(&b, "\nthinking: enabled=%t source=%s effort=%s max_tokens=%d exclude=%t",
				activeCfg.Reasoning.IsEffectivelyEnabled(),
				activeCfg.Reasoning.GetRuntimeSource(),
				activeCfg.Reasoning.Effort,
				activeCfg.Reasoning.MaxTokens,
				activeCfg.Reasoning.Exclude)
		}
		fallbacks := configuredFallbacks(activeCfg)
		fmt.Fprintf(&b, "\nfallbacks: %d", len(fallbacks))
		for _, route := range fallbacks {
			fmt.Fprintf(&b, "\n  - %s model=%s base_url=%s", route.Name, route.Model, route.BaseURL)
		}
		return b.String()
	}
}

func BuildModelSwitcher(cfgPath string) func(name string) string {
	return func(name string) string {
		name = strings.TrimSpace(name)
		if name == "" {
			return "用法：/model use <model>"
		}
		fields := strings.Fields(name)
		model := fields[0]
		if cfgPath == "" {
			cfgPath = ".nano.yaml"
		}
		cfgMap, err := loadConfigMapAt(cfgPath)
		if err != nil {
			return fmt.Sprintf("❌ 切换模型失败：%v", err)
		}
		setNestedRawKey(cfgMap, "model", model)
		provider, bareModel := llm.ParseModelRef(model, "")
		if provider != "" {
			if preset, ok := llm.GetProviderPreset(provider); ok && preset.BaseURL != "" {
				setNestedRawKey(cfgMap, "base_url", preset.BaseURL)
				setNestedRawKey(cfgMap, "model", bareModel)
			}
		}
		if err := writeConfigMapAt(cfgPath, cfgMap); err != nil {
			return fmt.Sprintf("❌ 切换模型失败：%v", err)
		}
		return fmt.Sprintf("✅ 已写入 %s。当前 TUI 引擎不会热加载配置，请重启 TUI 后生效。", cfgPath)
	}
}

func BuildModelFallbackHandler(cfg *config.Config) func(args string) string {
	return func(args string) string {
		activeCfg := cfg
		if activeCfg == nil {
			activeCfg = config.Get()
		}
		if activeCfg == nil {
			return "⚠️  配置未加载。"
		}
		parts := strings.Fields(args)
		sub := "list"
		if len(parts) > 0 {
			sub = strings.ToLower(parts[0])
		}
		switch sub {
		case "", "list":
			fallbacks := configuredFallbacks(activeCfg)
			if len(fallbacks) == 0 {
				return "ℹ️  暂无模型 fallback 路由。"
			}
			var b strings.Builder
			for _, route := range fallbacks {
				fmt.Fprintf(&b, "%s model=%s base_url=%s\n", route.Name, route.Model, route.BaseURL)
			}
			return strings.TrimRight(b.String(), "\n")
		case "add":
			if len(parts) < 2 {
				return "用法：/model fallback add <provider/model>"
			}
			ref := parts[1]
			activeCfg.Fallbacks = append(activeCfg.Fallbacks, ref)
			return fmt.Sprintf("✅ 已添加 fallback %s（当前会话生效；如需持久化请更新 .nano.yaml）。", ref)
		case "clear":
			activeCfg.Fallbacks = nil
			if activeCfg.ModelRouting != nil {
				activeCfg.ModelRouting.Fallbacks = nil
			}
			return "✅ 已清空 fallback 路由（当前会话生效；如需持久化请更新 .nano.yaml）。"
		default:
			return fmt.Sprintf("❌ 未知 model fallback 子命令：%s", sub)
		}
	}
}

func BuildModelDoctor(cfg *config.Config) func(model string) string {
	return func(model string) string {
		activeCfg := cfg
		if activeCfg == nil {
			activeCfg = config.Get()
		}
		if activeCfg == nil {
			return "⚠️  配置未加载。"
		}
		model = strings.TrimSpace(model)
		if model == "" {
			model = activeCfg.Model
		}
		desc := llm.DescribeConfiguredModel(activeCfg.BaseURL, model)
		return formatModelDescriptor(desc)
	}
}

func BuildContextStatusGetter(agent interface{}) func() string {
	return func() string {
		type contextReporter interface {
			GetActiveSessionID() string
			GetContextStatus(string) (agentpkg.ContextStatus, bool)
		}
		reporter, ok := agent.(contextReporter)
		if !ok || reporter == nil {
			return "⚠️  当前 Agent 不支持上下文状态。"
		}
		sessionID := reporter.GetActiveSessionID()
		status, ok := reporter.GetContextStatus(sessionID)
		if !ok {
			return fmt.Sprintf("⚠️  未找到会话 %s 的上下文状态。", sessionID)
		}
		data, _ := json.MarshalIndent(status, "", "  ")
		return string(data)
	}
}

func formatModelDescriptor(desc llm.ModelDescriptor) string {
	return fmt.Sprintf("model: %s\nprovider: %s\nknown: %t\ncontext_window: %d\nmax_output_tokens: %d\ncapabilities: tools=%t reasoning=%t vision=%t streaming=%t tool_choice=%t parallel_tool_calls=%t json_schema=%t embedding=%t long_context=%t",
		desc.ID,
		desc.ProviderID,
		desc.Known,
		desc.Profile.ContextWindow,
		desc.Profile.MaxOutputTokens,
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

func configuredFallbacks(cfg *config.Config) []config.ModelRouteConfig {
	if cfg == nil {
		return nil
	}
	if len(cfg.Fallbacks) > 0 {
		out := make([]config.ModelRouteConfig, 0, len(cfg.Fallbacks))
		for _, ref := range cfg.Fallbacks {
			out = append(out, config.ModelRouteConfig{Name: ref, Model: ref})
		}
		return out
	}
	if cfg.ModelRouting == nil {
		return nil
	}
	return append([]config.ModelRouteConfig(nil), cfg.ModelRouting.Fallbacks...)
}

func loadConfigMapAt(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	cfgMap := make(map[string]interface{})
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &cfgMap); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
	}
	return cfgMap, nil
}

func writeConfigMapAt(path string, cfgMap map[string]interface{}) error {
	out, err := yaml.Marshal(cfgMap)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
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
		nextMap, ok := current[part].(map[string]interface{})
		if !ok {
			nextMap = make(map[string]interface{})
			current[part] = nextMap
		}
		current = nextMap
	}
}
