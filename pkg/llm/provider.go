package llm

import "strings"

const defaultOpenAIBaseURL = "https://api.openai.com/v1"

// ProviderInfo captures provider-specific behavior that is inferred from the
// configured base URL. This keeps provider quirks out of request construction.
type ProviderInfo struct {
	BaseURL string
}

func NewProviderInfo(baseURL string) ProviderInfo {
	effectiveBaseURL := strings.TrimSpace(baseURL)
	if effectiveBaseURL == "" {
		effectiveBaseURL = defaultOpenAIBaseURL
	}
	return ProviderInfo{BaseURL: effectiveBaseURL}
}

func newProviderInfo(baseURL string) ProviderInfo {
	return NewProviderInfo(baseURL)
}

func (p ProviderInfo) UsesCustomBaseURL() bool {
	return p.BaseURL != "" && p.BaseURL != defaultOpenAIBaseURL
}

func (p ProviderInfo) RequiresReasoningContentInMessages() bool {
	lowerURL := strings.ToLower(p.BaseURL)
	knownProviders := []string{"deepseek", "moonshot"}
	for _, provider := range knownProviders {
		if strings.Contains(lowerURL, provider) {
			return true
		}
	}
	return false
}
