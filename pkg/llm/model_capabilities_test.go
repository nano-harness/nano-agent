package llm

import "testing"

func TestDescribeConfiguredModelInfersProviderFromBaseURL(t *testing.T) {
	cases := []struct {
		name         string
		baseURL      string
		model        string
		wantProvider string
	}{
		{"openai default", "", "gpt-4o", "openai"},
		{"moonshot", "https://api.moonshot.cn/v1", "kimi-k2", "moonshot"},
		{"deepseek", "https://api.deepseek.com/v1", "deepseek-r1", "deepseek"},
		{"gemini", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.5-pro", "gemini"},
		{"ollama", "http://localhost:11434/v1", "llama-3.1-8b-instruct", "ollama"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			desc := DescribeConfiguredModel(tc.baseURL, tc.model)
			if desc.ProviderID != tc.wantProvider {
				t.Fatalf("ProviderID = %q, want %q", desc.ProviderID, tc.wantProvider)
			}
			if desc.ID != NormalizeModelID(tc.model) {
				t.Fatalf("ID = %q, want normalized %q", desc.ID, NormalizeModelID(tc.model))
			}
		})
	}
}

func TestDescribeModelKnownCapabilities(t *testing.T) {
	desc := DescribeModel("moonshot", "moonshot/kimi-k2.5")
	if !desc.Known {
		t.Fatal("expected kimi-k2.5 to be known")
	}
	if !desc.Capabilities.Tools || !desc.Capabilities.Streaming || !desc.Capabilities.LongContext {
		t.Fatalf("unexpected capabilities: %+v", desc.Capabilities)
	}
	if desc.Profile.ContextWindow != 262_144 {
		t.Fatalf("ContextWindow = %d, want 262144", desc.Profile.ContextWindow)
	}
}

func TestDescribeModelCanDisableDefaultCapability(t *testing.T) {
	desc := DescribeModel("ollama", "llama-3.1-8b-instruct")
	if !desc.Known {
		t.Fatal("expected llama-3.1-8b-instruct to be known")
	}
	if desc.Capabilities.JSONSchema {
		t.Fatalf("JSONSchema = true, want false for conservative Ollama preset")
	}
	if !desc.Capabilities.Tools || !desc.Capabilities.Streaming {
		t.Fatalf("unexpected chat capabilities: %+v", desc.Capabilities)
	}
}

func TestDescribeModelUnknownUsesConservativeChatDefaults(t *testing.T) {
	desc := DescribeModel("", "custom-32k-model")
	if desc.Known {
		t.Fatal("custom model should not be known")
	}
	if desc.ProviderID != "openai-compatible" {
		t.Fatalf("ProviderID = %q, want openai-compatible", desc.ProviderID)
	}
	if !desc.Capabilities.Tools || !desc.Capabilities.Streaming || !desc.Capabilities.JSONSchema {
		t.Fatalf("unexpected default capabilities: %+v", desc.Capabilities)
	}
	if desc.Capabilities.Reasoning || desc.Capabilities.Vision || desc.Capabilities.Embedding {
		t.Fatalf("unexpected opt-in capabilities: %+v", desc.Capabilities)
	}
}

func TestKnownProviderPresetsIncludesCoreProviders(t *testing.T) {
	presets := KnownProviderPresets()
	want := map[string]bool{
		"openai":            false,
		"moonshot":          false,
		"deepseek":          false,
		"gemini":            false,
		"ollama":            false,
		"openai-compatible": false,
	}
	for _, preset := range presets {
		if _, ok := want[preset.ID]; ok {
			want[preset.ID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Fatalf("missing provider preset %q", id)
		}
	}
}
