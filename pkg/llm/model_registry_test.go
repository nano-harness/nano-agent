package llm

import (
	"testing"
)

func TestInferModelProfile_ExactMatch(t *testing.T) {
	cases := []struct {
		model         string
		wantContext   int
		wantThreshold float64
	}{
		{"gpt-5.4", 1_050_000, 0.92},
		{"gpt-4o", 128_000, 0.80},
		{"gpt-3.5-turbo", 16_385, 0.60},
		{"gpt-4", 8_191, 0.55},
		{"deepseek-chat", 163_840, 0.82},
		{"deepseek-v3.2", 163_840, 0.82},
		{"claude-opus-4.6", 1_000_000, 0.92},
		{"claude-sonnet-4.6", 1_000_000, 0.92},
		{"grok-4.20", 2_000_000, 0.93},
		{"gpt-4.1", 1_047_576, 0.92},
		{"llama-4-maverick", 1_048_576, 0.92},
		{"qwen3.6-plus", 1_000_000, 0.92},
	}
	for _, tc := range cases {
		p := InferModelProfile(tc.model)
		if p.ContextWindow != tc.wantContext {
			t.Errorf("InferModelProfile(%q).ContextWindow = %d, want %d", tc.model, p.ContextWindow, tc.wantContext)
		}
		if p.ThresholdRatio != tc.wantThreshold {
			t.Errorf("InferModelProfile(%q).ThresholdRatio = %v, want %v", tc.model, p.ThresholdRatio, tc.wantThreshold)
		}
	}
}

func TestInferModelProfile_VendorPrefix(t *testing.T) {
	cases := []struct {
		model       string
		wantContext int
	}{
		{"openai/gpt-4o", 128_000},
		{"anthropic/claude-sonnet-4.6", 1_000_000},
		{"google/gemini-2.5-pro", 1_048_576},
		{"xai/grok-4.20", 2_000_000},
		{"meta-llama/llama-4-maverick", 1_048_576},
	}
	for _, tc := range cases {
		p := InferModelProfile(tc.model)
		if p.ContextWindow != tc.wantContext {
			t.Errorf("InferModelProfile(%q).ContextWindow = %d, want %d", tc.model, p.ContextWindow, tc.wantContext)
		}
	}
}

func TestInferModelProfile_AliyunDashVendorPrefix(t *testing.T) {
	p := InferModelProfile("aliyun-glm-5.1")
	if p.ContextWindow != modelRegistry["glm-5.1"].ContextWindow {
		t.Fatalf("ContextWindow = %d, want glm-5.1 profile", p.ContextWindow)
	}
}

func TestInferModelProfile_MultiLayerDashVendorPrefix(t *testing.T) {
	p := InferModelProfile("provider-org-glm-4.6")
	if p.ContextWindow != modelRegistry["glm-4.6"].ContextWindow {
		t.Fatalf("ContextWindow = %d, want glm-4.6 profile", p.ContextWindow)
	}
}

func TestInferModelProfile_DoesNotSplitLettersForVendorPrefix(t *testing.T) {
	p := InferModelProfile("xglm-5.1")
	if p.ContextWindow == modelRegistry["glm-5.1"].ContextWindow {
		t.Fatalf("xglm-5.1 should not match glm-5.1 profile: %+v", p)
	}
}

func TestInferModelProfile_PrefixMatch(t *testing.T) {
	cases := []struct {
		model       string
		wantContext int
	}{
		{"gpt-4o-2024-08-06", 128_000},
		{"gpt-5.1-codex-mini", 400_000},
		{"gpt-5.2-codex", 400_000},
		{"o3-mini", 200_000},
		{"grok-3-mini", 131_072},
		{"mistral-large-2411-instruct", 131_072},
	}
	for _, tc := range cases {
		p := InferModelProfile(tc.model)
		if p.ContextWindow != tc.wantContext {
			t.Errorf("InferModelProfile(%q).ContextWindow = %d, want %d", tc.model, p.ContextWindow, tc.wantContext)
		}
	}
}

func TestInferModelProfile_KeywordInference(t *testing.T) {
	cases := []struct {
		model         string
		wantThreshold float64
	}{
		{"my-custom-128k-model", 0.82},
		{"some-llm-32k", 0.72},
		{"unknown-8k-chat", 0.55},
		{"giant-1m-model", 0.92},
	}
	for _, tc := range cases {
		p := InferModelProfile(tc.model)
		if p.ThresholdRatio != tc.wantThreshold {
			t.Errorf("InferModelProfile(%q).ThresholdRatio = %v, want %v", tc.model, p.ThresholdRatio, tc.wantThreshold)
		}
	}
}

func TestInferModelProfile_UnknownModel(t *testing.T) {
	p := InferModelProfile("totally-unknown-model-xyz")
	if p.ContextWindow != 128_000 {
		t.Errorf("unknown model ContextWindow = %d, want 128000", p.ContextWindow)
	}
	if p.ThresholdRatio != 0.80 {
		t.Errorf("unknown model ThresholdRatio = %v, want 0.80", p.ThresholdRatio)
	}
	if p.PreserveRatio != 0.20 {
		t.Errorf("unknown model PreserveRatio = %v, want 0.20", p.PreserveRatio)
	}
}

func TestInferModelProfile_EmptyString(t *testing.T) {
	p := InferModelProfile("")
	if p.ContextWindow != 128_000 {
		t.Errorf("empty model ContextWindow = %d, want 128000", p.ContextWindow)
	}
}

func TestComputeProfileFromContextWindow(t *testing.T) {
	cases := []struct {
		window        int
		wantThreshold float64
		wantPreserve  float64
	}{
		{4_096, 0.55, 0.40},
		{8_192, 0.55, 0.40},
		{16_384, 0.60, 0.35},
		{32_768, 0.72, 0.28},
		{65_536, 0.72, 0.28},
		{128_000, 0.82, 0.18},
		{200_000, 0.82, 0.18},
		{400_000, 0.88, 0.12},
		{1_000_000, 0.92, 0.08},
		{1_100_000, 0.92, 0.08},
		{2_000_000, 0.93, 0.07},
	}
	for _, tc := range cases {
		p := ComputeProfileFromContextWindow(tc.window)
		if p.ContextWindow != tc.window {
			t.Errorf("ComputeProfileFromContextWindow(%d).ContextWindow = %d", tc.window, p.ContextWindow)
		}
		if p.ThresholdRatio != tc.wantThreshold {
			t.Errorf("ComputeProfileFromContextWindow(%d).ThresholdRatio = %v, want %v", tc.window, p.ThresholdRatio, tc.wantThreshold)
		}
		if p.PreserveRatio != tc.wantPreserve {
			t.Errorf("ComputeProfileFromContextWindow(%d).PreserveRatio = %v, want %v", tc.window, p.PreserveRatio, tc.wantPreserve)
		}
	}
}
