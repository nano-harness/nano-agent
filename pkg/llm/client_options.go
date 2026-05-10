package llm

import (
	"net/http"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/openai/openai-go/v3/option"
)

func newOpenAIRequestOptions(apiKey, baseURL string, cfg *config.Config) []option.RequestOption {
	httpTimeout := 60 * time.Second
	if cfg != nil {
		httpTimeout = cfg.HTTPTimeout
	}

	// Configure HTTP client for streaming-friendly behavior:
	// - Do NOT set http.Client.Timeout (set to 0) because it limits total body read time and breaks long-lived streams
	// - Use Transport.ResponseHeaderTimeout to bound the time to first byte/headers
	transport := &http.Transport{
		ResponseHeaderTimeout: httpTimeout,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	var httpTransport http.RoundTripper = transport
	if cfg != nil && cfg.Verbose {
		httpTransport = &loggingRoundTripper{wrapped: transport}
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(&http.Client{
			Timeout:   0,
			Transport: httpTransport,
		}),
	}

	provider := newProviderInfo(baseURL)
	if provider.UsesCustomBaseURL() {
		opts = append(opts, option.WithBaseURL(provider.BaseURL))
	}

	return opts
}

func newCircuitBreakerFromConfig(cfg *config.Config) *CircuitBreaker {
	return newCircuitBreakerForRoute("", "", cfg)
}

func newCircuitBreakerForRoute(providerID, baseURL string, cfg *config.Config) *CircuitBreaker {
	cbCfg := DefaultCircuitBreakerConfig()
	if cfg != nil && cfg.Advanced != nil && cfg.Advanced.CircuitBreaker != nil {
		cbAdv := cfg.Advanced.CircuitBreaker
		if cbAdv.MaxRetries > 0 {
			cbCfg.MaxRetries = cbAdv.MaxRetries
		}
		if cbAdv.BaseDelayMs > 0 {
			cbCfg.BaseDelay = time.Duration(cbAdv.BaseDelayMs) * time.Millisecond
		}
		if cbAdv.MaxDelayMs > 0 {
			cbCfg.MaxDelay = time.Duration(cbAdv.MaxDelayMs) * time.Millisecond
		}
		if cbAdv.OpenTimeoutMs > 0 {
			cbCfg.OpenTimeout = time.Duration(cbAdv.OpenTimeoutMs) * time.Millisecond
		}
		cbCfg.ExcludeNonFailback = cbAdv.ExcludeNonFailback
		cbCfg.excludeNonFailbackConfigured = true
	}
	return getOrCreateCircuitBreaker(providerID, baseURL, cbCfg)
}

func truncationDetectionEnabled(cfg *config.Config) bool {
	if cfg == nil || cfg.Advanced == nil || cfg.Advanced.CircuitBreaker == nil {
		return true
	}
	return cfg.Advanced.CircuitBreaker.TruncationDetection
}
