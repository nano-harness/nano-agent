package llm

import "strings"

// ModelCapabilities describes feature-level behavior for a model. The values
// are intentionally conservative: unknown models get broadly compatible
// OpenAI-compatible defaults, while known model families can opt in to more
// specific behavior.
type ModelCapabilities struct {
	Tools             bool `json:"tools"`
	Reasoning         bool `json:"reasoning"`
	Vision            bool `json:"vision"`
	Streaming         bool `json:"streaming"`
	ToolChoice        bool `json:"tool_choice"`
	ParallelToolCalls bool `json:"parallel_tool_calls"`
	JSONSchema        bool `json:"json_schema"`
	Embedding         bool `json:"embedding"`
	LongContext       bool `json:"long_context"`
}

// ModelDescriptor is the public model metadata exposed to CLI and daemon
// callers.
type ModelDescriptor struct {
	ID           string            `json:"id"`
	ProviderID   string            `json:"provider_id"`
	DisplayName  string            `json:"display_name,omitempty"`
	Capabilities ModelCapabilities `json:"capabilities"`
	Profile      ModelProfile      `json:"profile"`
	Known        bool              `json:"known"`
}

// ProviderPreset describes an OpenAI-compatible provider preset. It is a small
// manifest-like foundation that future extension/provider manifests can replace
// or augment.
type ProviderPreset struct {
	ID               string            `json:"id"`
	DisplayName      string            `json:"display_name"`
	BaseURL          string            `json:"base_url,omitempty"`
	APIKeyEnv        string            `json:"api_key_env,omitempty"`
	OpenAICompatible bool              `json:"openai_compatible"`
	Models           []ModelDescriptor `json:"models"`
}

type modelCapabilityEntry struct {
	providerID   string
	displayName  string
	capabilities ModelCapabilities
}

var defaultChatCapabilities = ModelCapabilities{ //nolint:gochecknoglobals
	Tools:             true,
	Streaming:         true,
	ToolChoice:        true,
	ParallelToolCalls: false,
	JSONSchema:        true,
}

var modelCapabilitiesRegistry = map[string]modelCapabilityEntry{ //nolint:gochecknoglobals
	"gpt-5.4":                {"openai", "GPT-5.4", chatCapabilities(capReasoning, capParallelToolCalls, capLongContext)},
	"gpt-5":                  {"openai", "GPT-5", chatCapabilities(capReasoning, capParallelToolCalls, capLongContext)},
	"gpt-4.1":                {"openai", "GPT-4.1", chatCapabilities(capParallelToolCalls, capLongContext)},
	"gpt-4o":                 {"openai", "GPT-4o", chatCapabilities(capVision, capParallelToolCalls, capLongContext)},
	"o3":                     {"openai", "o3", chatCapabilities(capReasoning, capLongContext)},
	"o4-mini":                {"openai", "o4-mini", chatCapabilities(capReasoning, capLongContext)},
	"deepseek-chat":          {"deepseek", "DeepSeek Chat", chatCapabilities(capLongContext)},
	"deepseek-r1":            {"deepseek", "DeepSeek R1", chatCapabilities(capReasoning)},
	"deepseek-v3.2":          {"deepseek", "DeepSeek V3.2", chatCapabilities(capLongContext)},
	"kimi-k2":                {"moonshot", "Kimi K2", chatCapabilities(capLongContext)},
	"kimi-k2-0905":           {"moonshot", "Kimi K2 0905", chatCapabilities(capLongContext)},
	"kimi-k2.5":              {"moonshot", "Kimi K2.5", chatCapabilities(capLongContext)},
	"gemini-2.5-pro":         {"gemini", "Gemini 2.5 Pro", chatCapabilities(capReasoning, capVision, capLongContext)},
	"gemini-2.5-flash":       {"gemini", "Gemini 2.5 Flash", chatCapabilities(capVision, capLongContext)},
	"llama-3.1-8b-instruct":  {"ollama", "Llama 3.1 8B Instruct", chatCapabilities(capNoJSONSchema)},
	"nomic-embed-text":       {"ollama", "Nomic Embed Text", ModelCapabilities{Embedding: true}},
	"qwen3-embedding":        {"ollama", "Qwen3 Embedding", ModelCapabilities{Embedding: true}},
	"mxbai-embed-large":      {"ollama", "mxbai Embed Large", ModelCapabilities{Embedding: true}},
	"text-embedding-3-small": {"openai", "Text Embedding 3 Small", ModelCapabilities{Embedding: true}},
	"text-embedding-3-large": {"openai", "Text Embedding 3 Large", ModelCapabilities{Embedding: true}},
	// Anthropic Claude models
	"claude-opus-4.6":   {"anthropic", "Claude Opus 4.6", chatCapabilities(capReasoning, capVision, capParallelToolCalls, capLongContext)},
	"claude-sonnet-4.6": {"anthropic", "Claude Sonnet 4.6", chatCapabilities(capReasoning, capVision, capParallelToolCalls, capLongContext)},
	"claude-sonnet-4.5": {"anthropic", "Claude Sonnet 4.5", chatCapabilities(capReasoning, capVision, capParallelToolCalls, capLongContext)},
	"claude-sonnet-4":   {"anthropic", "Claude Sonnet 4", chatCapabilities(capVision, capParallelToolCalls, capLongContext)},
	"claude-haiku-4.5":  {"anthropic", "Claude Haiku 4.5", chatCapabilities(capVision, capLongContext)},
	"claude-3.7-sonnet": {"anthropic", "Claude 3.7 Sonnet", chatCapabilities(capReasoning, capVision, capLongContext)},
	"claude-3-5-sonnet": {"anthropic", "Claude 3.5 Sonnet", chatCapabilities(capVision, capLongContext)},
	"claude-3-5-haiku":  {"anthropic", "Claude 3.5 Haiku", chatCapabilities(capVision, capLongContext)},
	"claude-3-opus":     {"anthropic", "Claude 3 Opus", chatCapabilities(capVision, capLongContext)},
}

var providerPresetDefinitions = []struct { //nolint:gochecknoglobals
	id          string
	displayName string
	baseURL     string
	apiKeyEnv   string
	models      []string
}{
	{"openai", "OpenAI", defaultOpenAIBaseURL, "OPENAI_API_KEY", []string{"gpt-5.4", "gpt-5", "gpt-4.1", "gpt-4o", "o3", "o4-mini", "text-embedding-3-small", "text-embedding-3-large"}},
	{"anthropic", "Anthropic", "https://api.anthropic.com", "ANTHROPIC_API_KEY", []string{"claude-opus-4.6", "claude-sonnet-4.6", "claude-sonnet-4.5", "claude-haiku-4.5"}},
	{"moonshot", "Moonshot / Kimi", "https://api.moonshot.cn/v1", "MOONSHOT_API_KEY", []string{"kimi-k2.5", "kimi-k2-0905", "kimi-k2"}},
	{"deepseek", "DeepSeek", "https://api.deepseek.com/v1", "DEEPSEEK_API_KEY", []string{"deepseek-chat", "deepseek-v3.2", "deepseek-r1"}},
	{"gemini", "Google Gemini", "https://generativelanguage.googleapis.com/v1beta/openai", "GEMINI_API_KEY", []string{"gemini-2.5-pro", "gemini-2.5-flash"}},
	{"ollama", "Ollama", "http://localhost:11434/v1", "", []string{"llama-3.1-8b-instruct", "nomic-embed-text", "qwen3-embedding", "mxbai-embed-large"}},
	{"openai-compatible", "OpenAI Compatible", "", "", nil},
}

func chatCapabilities(options ...func(*ModelCapabilities)) ModelCapabilities {
	caps := defaultChatCapabilities
	for _, option := range options {
		if option != nil {
			option(&caps)
		}
	}
	return caps
}

func capReasoning(caps *ModelCapabilities) {
	caps.Reasoning = true
}

func capVision(caps *ModelCapabilities) {
	caps.Vision = true
}

func capParallelToolCalls(caps *ModelCapabilities) {
	caps.ParallelToolCalls = true
}

func capLongContext(caps *ModelCapabilities) {
	caps.LongContext = true
}

func capNoJSONSchema(caps *ModelCapabilities) {
	caps.JSONSchema = false
}

// KnownProviderPresets returns built-in provider presets and representative
// model manifests.
func KnownProviderPresets() []ProviderPreset {
	presets := make([]ProviderPreset, 0, len(providerPresetDefinitions))
	for _, preset := range providerPresetDefinitions {
		models := make([]ModelDescriptor, 0, len(preset.models))
		for _, model := range preset.models {
			models = append(models, DescribeModel(preset.id, model))
		}
		presets = append(presets, ProviderPreset{
			ID:               preset.id,
			DisplayName:      preset.displayName,
			BaseURL:          preset.baseURL,
			APIKeyEnv:        preset.apiKeyEnv,
			OpenAICompatible: true,
			Models:           models,
		})
	}
	return presets
}

// GetProviderPreset returns a provider preset by ID.
func GetProviderPreset(providerID string) (ProviderPreset, bool) {
	needle := strings.ToLower(strings.TrimSpace(providerID))
	for _, preset := range KnownProviderPresets() {
		if preset.ID == needle {
			return preset, true
		}
	}
	return ProviderPreset{}, false
}

// DescribeModel returns normalized model metadata for a provider/model pair.
func DescribeModel(providerID, modelName string) ModelDescriptor {
	modelID := NormalizeModelID(modelName)
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	entry, known := lookupModelCapabilityEntry(modelID)
	if providerID == "" {
		providerID = entry.providerID
	}
	if providerID == "" {
		providerID = "openai-compatible"
	}
	profile := InferModelProfile(modelID)
	caps := entry.capabilities
	if !known {
		caps = defaultChatCapabilities
	}
	caps.LongContext = caps.LongContext || profile.ContextWindow >= 128_000
	return ModelDescriptor{
		ID:           modelID,
		ProviderID:   providerID,
		DisplayName:  entry.displayName,
		Capabilities: caps,
		Profile:      profile,
		Known:        known,
	}
}

// DescribeConfiguredModel infers provider metadata from a configured base URL
// and model name.
func DescribeConfiguredModel(baseURL, modelName string) ModelDescriptor {
	return DescribeModel(InferProviderID(baseURL, modelName), modelName)
}

// InferProviderID infers the provider from base URL or model family.
func InferProviderID(baseURL, modelName string) string {
	lowerURL := strings.ToLower(strings.TrimSpace(baseURL))
	switch {
	case lowerURL == "" || lowerURL == defaultOpenAIBaseURL:
		return "openai"
	case strings.Contains(lowerURL, "anthropic"):
		return "anthropic"
	case strings.Contains(lowerURL, "moonshot"):
		return "moonshot"
	case strings.Contains(lowerURL, "deepseek"):
		return "deepseek"
	case strings.Contains(lowerURL, "generativelanguage") || strings.Contains(lowerURL, "googleapis"):
		return "gemini"
	case strings.Contains(lowerURL, "ollama") || strings.Contains(lowerURL, "localhost:11434") || strings.Contains(lowerURL, "127.0.0.1:11434"):
		return "ollama"
	}

	modelID := NormalizeModelID(modelName)
	if entry, ok := lookupModelCapabilityEntry(modelID); ok && entry.providerID != "" {
		return entry.providerID
	}
	return "openai-compatible"
}

// NormalizeModelID strips provider prefixes and lowercases model identifiers.
func NormalizeModelID(modelName string) string {
	modelID := strings.ToLower(strings.TrimSpace(modelName))
	if idx := strings.Index(modelID, "/"); idx >= 0 {
		modelID = modelID[idx+1:]
	}
	return modelID
}

func lookupModelCapabilityEntry(modelID string) (modelCapabilityEntry, bool) {
	modelID = NormalizeModelID(modelID)
	if entry, ok := modelCapabilitiesRegistry[modelID]; ok {
		return entry, true
	}
	best := ""
	for key := range modelCapabilitiesRegistry {
		if strings.HasPrefix(modelID, key) && len(key) > len(best) {
			rest := modelID[len(key):]
			if rest == "" || !isAlphaNum(rune(rest[0])) {
				best = key
			}
		}
	}
	if best != "" {
		return modelCapabilitiesRegistry[best], true
	}
	return modelCapabilityEntry{}, false
}
