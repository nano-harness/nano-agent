package llm

import "strings"

// ModelProfile holds context window characteristics for a model.
type ModelProfile struct {
	ContextWindow   int     // Model context window size (tokens)
	MaxOutputTokens int     // Maximum output tokens
	ThresholdRatio  float64 // Compression trigger threshold (fraction of ContextWindow)
	PreserveRatio   float64 // Fraction of recent context to preserve during compression
}

// modelRegistry maps model IDs (without vendor prefix) to their profiles.
// Data sourced from OpenRouter API 2026-04-12; only representative entries
// per vendor are listed – same-series variants are covered by prefix matching.
//
//nolint:gochecknoglobals
var modelRegistry = map[string]ModelProfile{
	// ── OpenAI ──────────────────────────────────────────────────────────
	"gpt-5.4":                {1_050_000, 128_000, 0.92, 0.08},
	"gpt-5.4-pro":            {1_050_000, 128_000, 0.92, 0.08},
	"gpt-5.4-mini":           {400_000, 128_000, 0.88, 0.12},
	"gpt-5.4-nano":           {400_000, 128_000, 0.88, 0.12},
	"gpt-4.1":                {1_047_576, 0, 0.92, 0.08},
	"gpt-4.1-mini":           {1_047_576, 32_768, 0.92, 0.08},
	"gpt-4.1-nano":           {1_047_576, 32_768, 0.92, 0.08},
	"gpt-5":                  {400_000, 128_000, 0.88, 0.12},
	"gpt-5-mini":             {400_000, 128_000, 0.88, 0.12},
	"gpt-5-codex":            {400_000, 128_000, 0.88, 0.12},
	"gpt-5.1":                {400_000, 128_000, 0.88, 0.12},
	"gpt-5.2":                {400_000, 128_000, 0.88, 0.12},
	"gpt-5.3-codex":          {400_000, 128_000, 0.88, 0.12},
	"o3":                     {200_000, 100_000, 0.85, 0.15},
	"o4-mini":                {200_000, 100_000, 0.85, 0.15},
	"o1":                     {200_000, 100_000, 0.85, 0.15},
	"gpt-4o":                 {128_000, 16_384, 0.80, 0.20},
	"gpt-4-turbo":            {128_000, 4_096, 0.80, 0.20},
	"gpt-3.5-turbo":          {16_385, 4_096, 0.60, 0.35},
	"gpt-4":                  {8_191, 4_096, 0.55, 0.40},
	"gpt-3.5-turbo-instruct": {4_095, 4_096, 0.50, 0.45},

	// ── Anthropic ───────────────────────────────────────────────────────
	"claude-opus-4.6":      {1_000_000, 128_000, 0.92, 0.08},
	"claude-opus-4.6-fast": {1_000_000, 128_000, 0.92, 0.08},
	"claude-sonnet-4.6":    {1_000_000, 128_000, 0.92, 0.08},
	"claude-sonnet-4.5":    {1_000_000, 64_000, 0.92, 0.08},
	"claude-sonnet-4":      {1_000_000, 64_000, 0.92, 0.08},
	"claude-opus-4.5":      {200_000, 64_000, 0.85, 0.15},
	"claude-haiku-4.5":     {200_000, 64_000, 0.85, 0.15},
	"claude-opus-4.1":      {200_000, 32_000, 0.85, 0.15},
	"claude-opus-4":        {200_000, 32_000, 0.85, 0.15},
	"claude-3.7-sonnet":    {200_000, 128_000, 0.85, 0.15},
	"claude-3.5-haiku":     {200_000, 8_192, 0.85, 0.15},
	"claude-3-haiku":       {200_000, 4_096, 0.85, 0.15},

	// ── Google ──────────────────────────────────────────────────────────
	"gemini-3.1-pro-preview":        {1_048_576, 65_536, 0.92, 0.08},
	"gemini-3.1-flash-lite-preview": {1_048_576, 65_536, 0.92, 0.08},
	"gemini-3-flash-preview":        {1_048_576, 65_536, 0.92, 0.08},
	"gemini-2.5-pro":                {1_048_576, 65_536, 0.92, 0.08},
	"gemini-2.5-flash":              {1_048_576, 65_535, 0.92, 0.08},
	"gemini-2.5-flash-lite":         {1_048_576, 65_535, 0.92, 0.08},
	"gemini-2.0-flash":              {1_048_576, 8_192, 0.92, 0.08},
	"gemma-4-31b-it":                {262_144, 32_768, 0.85, 0.15},
	"gemma-3-27b-it":                {131_072, 8_192, 0.80, 0.20},
	"gemma-3-12b-it":                {32_768, 8_192, 0.70, 0.30},

	// ── DeepSeek ────────────────────────────────────────────────────────
	"deepseek-chat":                {163_840, 163_840, 0.82, 0.18},
	"deepseek-v3.2":                {163_840, 0, 0.82, 0.18},
	"deepseek-r1-0528":             {163_840, 65_536, 0.82, 0.18},
	"deepseek-r1":                  {64_000, 16_000, 0.75, 0.25},
	"deepseek-r1-distill-qwen-32b": {32_768, 32_768, 0.70, 0.30},

	// ── Qwen ────────────────────────────────────────────────────────────
	"qwen3.6-plus":          {1_000_000, 65_536, 0.92, 0.08},
	"qwen3-coder-plus":      {1_000_000, 65_536, 0.92, 0.08},
	"qwen-plus":             {1_000_000, 32_768, 0.92, 0.08},
	"qwen3.5-397b-a17b":     {262_144, 65_536, 0.85, 0.15},
	"qwen3-max":             {262_144, 32_768, 0.85, 0.15},
	"qwen3-coder":           {262_000, 262_000, 0.85, 0.15},
	"qwen3.5-9b":            {256_000, 32_768, 0.85, 0.15},
	"qwen3-235b-a22b":       {131_072, 8_192, 0.80, 0.20},
	"qwq-32b":               {131_072, 131_072, 0.80, 0.20},
	"qwen-turbo":            {131_072, 8_192, 0.80, 0.20},
	"qwen3-32b":             {40_960, 40_960, 0.72, 0.28},
	"qwen-max":              {32_768, 8_192, 0.70, 0.30},
	"qwen-2.5-72b-instruct": {32_768, 16_384, 0.70, 0.30},

	// ── Meta Llama ──────────────────────────────────────────────────────
	"llama-4-maverick":              {1_048_576, 16_384, 0.92, 0.08},
	"llama-4-scout":                 {327_680, 16_384, 0.88, 0.12},
	"llama-3.3-70b-instruct":        {65_536, 0, 0.75, 0.25},
	"llama-3.1-70b-instruct":        {131_072, 0, 0.80, 0.20},
	"llama-3.2-11b-vision-instruct": {131_072, 16_384, 0.80, 0.20},
	"llama-3.1-8b-instruct":         {16_384, 16_384, 0.65, 0.30},
	"llama-3-8b-instruct":           {8_192, 16_384, 0.55, 0.40},

	// ── Mistral ─────────────────────────────────────────────────────────
	"mistral-large-2512":     {262_144, 0, 0.85, 0.15},
	"devstral-2512":          {262_144, 0, 0.85, 0.15},
	"mistral-small-2603":     {262_144, 0, 0.85, 0.15},
	"codestral-2508":         {256_000, 0, 0.85, 0.15},
	"mistral-large-2411":     {131_072, 0, 0.80, 0.20},
	"mistral-large":          {128_000, 0, 0.80, 0.20},
	"mixtral-8x22b-instruct": {65_536, 0, 0.75, 0.25},
	"mixtral-8x7b-instruct":  {32_768, 16_384, 0.70, 0.30},

	// ── Moonshot / Kimi ─────────────────────────────────────────────────
	"kimi-k2.5":    {262_144, 65_535, 0.85, 0.15},
	"kimi-k2-0905": {262_144, 262_144, 0.85, 0.15},
	"kimi-k2":      {131_072, 131_072, 0.80, 0.20},

	// ── xAI Grok ────────────────────────────────────────────────────────
	"grok-4.20":     {2_000_000, 0, 0.93, 0.07},
	"grok-4.1-fast": {2_000_000, 30_000, 0.93, 0.07},
	"grok-4-fast":   {2_000_000, 30_000, 0.93, 0.07},
	"grok-4":        {256_000, 0, 0.85, 0.15},
	"grok-3":        {131_072, 0, 0.80, 0.20},

	// ── Z.AI / Zhipu GLM ────────────────────────────────────────────────
	"glm-4.6":     {204_800, 204_800, 0.85, 0.15},
	"glm-5.1":     {202_752, 65_535, 0.85, 0.15},
	"glm-5-turbo": {202_752, 131_072, 0.85, 0.15},
	"glm-4.7":     {202_752, 65_535, 0.85, 0.15},
	"glm-4.5":     {131_072, 98_304, 0.80, 0.20},
	"glm-5":       {80_000, 131_072, 0.75, 0.25},
	"glm-4.5v":    {65_536, 16_384, 0.75, 0.25},

	// ── MiniMax ─────────────────────────────────────────────────────────
	"minimax-01":     {1_000_192, 1_000_192, 0.92, 0.08},
	"minimax-m1":     {1_000_000, 40_000, 0.92, 0.08},
	"minimax-m2.7":   {204_800, 131_072, 0.85, 0.15},
	"minimax-m2.5":   {196_608, 8_192, 0.85, 0.15},
	"minimax-m2":     {196_608, 196_608, 0.85, 0.15},
	"minimax-m2-her": {65_536, 2_048, 0.75, 0.25},

	// ── NVIDIA ──────────────────────────────────────────────────────────
	"nemotron-3-super-120b-a12b":       {262_144, 262_144, 0.85, 0.15},
	"nemotron-3-nano-30b-a3b":          {256_000, 0, 0.85, 0.15},
	"llama-3.1-nemotron-ultra-253b-v1": {131_072, 0, 0.80, 0.20},
	"llama-3.1-nemotron-70b-instruct":  {131_072, 16_384, 0.80, 0.20},

	// ── Xiaomi ──────────────────────────────────────────────────────────
	"mimo-v2-pro":   {1_048_576, 131_072, 0.92, 0.08},
	"mimo-v2-omni":  {262_144, 65_536, 0.85, 0.15},
	"mimo-v2-flash": {262_144, 65_536, 0.85, 0.15},

	// ── StepFun ─────────────────────────────────────────────────────────
	"step-3.5-flash": {262_144, 65_536, 0.85, 0.15},

	// ── Cohere ──────────────────────────────────────────────────────────
	"command-a":              {256_000, 8_192, 0.85, 0.15},
	"command-r-plus-08-2024": {128_000, 4_000, 0.80, 0.20},

	// ── Microsoft ───────────────────────────────────────────────────────
	"wizardlm-2-8x22b": {65_535, 8_000, 0.75, 0.25},
	"phi-4":            {16_384, 16_384, 0.65, 0.30},
}

// defaultProfile is the conservative fallback used when no match is found.
var defaultProfile = ModelProfile{ //nolint:gochecknoglobals
	ContextWindow:  128_000,
	ThresholdRatio: 0.80,
	PreserveRatio:  0.20,
}

// InferModelProfile returns a ModelProfile for the given model name using a
// four-level matching strategy:
//  1. Exact match (with or without vendor prefix, e.g. "openai/gpt-4o")
//  2. Prefix match for versioned variants (e.g. "gpt-4o-2024-08-06" → "gpt-4o")
//  3. Keyword inference from context-window hints embedded in the name
//     (e.g. "my-custom-128k-model")
//  4. Conservative default (128K window, 0.80 threshold)
func InferModelProfile(modelName string) ModelProfile {
	if modelName == "" {
		return defaultProfile
	}

	// Strip vendor prefix (e.g. "openai/gpt-4o" → "gpt-4o")
	bare := modelName
	if idx := strings.Index(modelName, "/"); idx >= 0 {
		bare = modelName[idx+1:]
	}
	lower := strings.ToLower(bare)

	// 1. Exact match
	if p, ok := modelRegistry[lower]; ok {
		return p
	}

	// 2. Prefix match: find the longest registry key that is a prefix of the model name
	best := ""
	for key := range modelRegistry {
		if strings.HasPrefix(lower, key) && len(key) > len(best) {
			// Ensure the prefix is followed by a non-alphanumeric character or end-of-string
			// to avoid "gpt-4" matching "gpt-4o"
			rest := lower[len(key):]
			if rest == "" || !isAlphaNum(rune(rest[0])) {
				best = key
			}
		}
	}
	if best != "" {
		return modelRegistry[best]
	}

	// 3. Keyword inference from context-window size hints in the name
	if p, ok := inferFromKeywords(lower); ok {
		return p
	}

	// 4. Conservative default
	return defaultProfile
}

// ComputeProfileFromContextWindow returns a ModelProfile computed solely from
// the context window size, using tiered thresholds. Useful when the model is
// not in the registry but the context window is known.
func ComputeProfileFromContextWindow(contextWindow int) ModelProfile {
	switch {
	case contextWindow <= 8_192:
		return ModelProfile{ContextWindow: contextWindow, ThresholdRatio: 0.55, PreserveRatio: 0.40}
	case contextWindow <= 16_384:
		return ModelProfile{ContextWindow: contextWindow, ThresholdRatio: 0.60, PreserveRatio: 0.35}
	case contextWindow <= 65_536:
		return ModelProfile{ContextWindow: contextWindow, ThresholdRatio: 0.72, PreserveRatio: 0.28}
	case contextWindow <= 200_000:
		return ModelProfile{ContextWindow: contextWindow, ThresholdRatio: 0.82, PreserveRatio: 0.18}
	case contextWindow <= 400_000:
		return ModelProfile{ContextWindow: contextWindow, ThresholdRatio: 0.88, PreserveRatio: 0.12}
	case contextWindow <= 1_100_000:
		return ModelProfile{ContextWindow: contextWindow, ThresholdRatio: 0.92, PreserveRatio: 0.08}
	default: // > 1.1M
		return ModelProfile{ContextWindow: contextWindow, ThresholdRatio: 0.93, PreserveRatio: 0.07}
	}
}

// inferFromKeywords checks for context-window size keywords embedded in the
// model name (e.g. "128k", "32k", "1m") and returns an appropriate profile.
// All "k" suffixes use binary kibibytes (1k = 1024), consistent with how AI
// providers name their models (e.g. "128k" = 128×1024 = 131,072 tokens).
func inferFromKeywords(lower string) (ModelProfile, bool) {
	hints := []struct {
		keyword string
		window  int
	}{
		{"2m", 2_000_000},
		{"1m", 1_000_000},
		{"1048k", 1_048_576},
		{"256k", 262_144},
		{"200k", 200_000},
		{"128k", 131_072},
		{"64k", 65_536},
		{"32k", 32_768},
		{"16k", 16_384},
		{"8k", 8_192},
	}
	for _, h := range hints {
		if strings.Contains(lower, h.keyword) {
			return ComputeProfileFromContextWindow(h.window), true
		}
	}
	return ModelProfile{}, false
}

// isAlphaNum reports whether r is an ASCII letter or digit.
func isAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
