package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

type anthropicRequestCapture struct {
	method string
	path   string
	header http.Header
}

func newAnthropicCaptureServer(t *testing.T, capture *anthropicRequestCapture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.method = r.Method
		capture.path = r.URL.Path
		capture.header = r.Header.Clone()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_test",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4.6",
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn",
			"stop_sequence":null,
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
}

func TestIsAnthropicRoute(t *testing.T) {
	tests := []struct {
		name     string
		route    ResolvedRoute
		expected bool
	}{
		{
			name:     "explicit anthropic providerID",
			route:    ResolvedRoute{ProviderID: "anthropic", Model: "claude-sonnet-4.6"},
			expected: true,
		},
		{
			name:     "anthropic providerID case insensitive",
			route:    ResolvedRoute{ProviderID: "ANTHROPIC", Model: "claude-sonnet-4.6"},
			expected: true,
		},
		{
			name:     "anthropic base URL",
			route:    ResolvedRoute{BaseURL: "https://api.anthropic.com", Model: "claude-3-opus"},
			expected: true,
		},
		{
			name:     "claude model prefix fallback",
			route:    ResolvedRoute{Model: "claude-sonnet-4.6"},
			expected: true,
		},
		{
			name:     "claude model with provider prefix",
			route:    ResolvedRoute{Model: "anthropic/claude-haiku-4.5"},
			expected: true,
		},
		{
			name:     "openai model not anthropic",
			route:    ResolvedRoute{ProviderID: "openai", Model: "gpt-4.1"},
			expected: false,
		},
		{
			name:     "deepseek model not anthropic",
			route:    ResolvedRoute{Model: "deepseek-chat"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAnthropicRoute(tt.route)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestNewAnthropicClientWithCustomBaseURL(t *testing.T) {
	resetCircuitBreakerRegistryForTest()
	t.Cleanup(resetCircuitBreakerRegistryForTest)

	capture := &anthropicRequestCapture{}
	server := newAnthropicCaptureServer(t, capture)
	t.Cleanup(server.Close)

	client := NewAnthropicClient("test-key", server.URL, "claude-sonnet-4.6", nil, nil, &config.Config{
		HTTPTimeout: time.Second,
	})

	content, err := client.GenerateContent(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "ok", content)
	assert.Equal(t, http.MethodPost, capture.method)
	assert.Equal(t, "/v1/messages", capture.path)

	expectedCB := getOrCreateCircuitBreaker("anthropic", server.URL, DefaultCircuitBreakerConfig())
	assert.Same(t, expectedCB, client.cb)
}

func TestNewAnthropicClientWithHeaders(t *testing.T) {
	capture := &anthropicRequestCapture{}
	server := newAnthropicCaptureServer(t, capture)
	t.Cleanup(server.Close)

	client := NewAnthropicClient(
		"test-key",
		server.URL,
		"claude-sonnet-4.6",
		map[string]string{"X-Route-Header": "route-value"},
		nil,
		&config.Config{HTTPTimeout: time.Second},
	)

	_, err := client.GenerateContent(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "route-value", capture.header.Get("X-Route-Header"))
}

func TestNewAnthropicClientDefaultBaseURL(t *testing.T) {
	resetCircuitBreakerRegistryForTest()
	t.Cleanup(resetCircuitBreakerRegistryForTest)

	client := NewAnthropicClient("test-key", "", "claude-sonnet-4.6", nil, nil, nil)

	expectedCB := getOrCreateCircuitBreaker("anthropic", "https://api.anthropic.com", DefaultCircuitBreakerConfig())
	assert.Same(t, expectedCB, client.cb)
}

func TestNewAnthropicClientVerboseLogging(t *testing.T) {
	client := NewAnthropicClient("test-key", "", "claude-sonnet-4.6", nil, nil, &config.Config{Verbose: true})

	require.IsType(t, &loggingRoundTripper{}, client.transport)
}

func TestNewClientForRoutePassesAnthropicRouteMetadata(t *testing.T) {
	capture := &anthropicRequestCapture{}
	server := newAnthropicCaptureServer(t, capture)
	t.Cleanup(server.Close)

	client := NewClientForRoute(ResolvedRoute{
		ProviderID: "anthropic",
		APIKey:     "test-key",
		BaseURL:    server.URL,
		Model:      "claude-sonnet-4.6",
		Headers:    map[string]string{"X-Factory-Header": "factory-value"},
	}, nil, &config.Config{HTTPTimeout: time.Second})

	content, err := client.GenerateContent(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "ok", content)
	assert.Equal(t, "/v1/messages", capture.path)
	assert.Equal(t, "factory-value", capture.header.Get("X-Factory-Header"))
}

func TestSplitSystemByCacheBoundary(t *testing.T) {
	t.Run("no boundary marker", func(t *testing.T) {
		text := "system prompt without boundary"
		blocks := splitSystemByCacheBoundary(text)
		require.Len(t, blocks, 1)
		assert.Equal(t, text, blocks[0].Text)
		// No boundary → no cache_control needed; the single block may or may not have it
	})

	t.Run("with boundary marker", func(t *testing.T) {
		cacheable := "cacheable part"
		dynamic := "dynamic part"
		text := cacheable + cacheBoundaryMarker + dynamic
		blocks := splitSystemByCacheBoundary(text)
		require.Len(t, blocks, 2)
		assert.Equal(t, cacheable, blocks[0].Text)
		// Cacheable prefix should have cache_control set (non-zero value)
		assert.NotEqual(t, anthropic.CacheControlEphemeralParam{}, blocks[0].CacheControl)
		assert.Equal(t, dynamic, blocks[1].Text)
	})

	t.Run("empty system prompt", func(t *testing.T) {
		blocks := splitSystemByCacheBoundary("")
		assert.Empty(t, blocks)
	})

	t.Run("only cacheable part (no dynamic)", func(t *testing.T) {
		cacheable := "only cacheable"
		text := cacheable + cacheBoundaryMarker
		blocks := splitSystemByCacheBoundary(text)
		require.Len(t, blocks, 1)
		assert.Equal(t, cacheable, blocks[0].Text)
		assert.NotEqual(t, anthropic.CacheControlEphemeralParam{}, blocks[0].CacheControl)
	})
}

func TestMergeConsecutiveRoles(t *testing.T) {
	merged := mergeConsecutiveRoles([]anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		anthropic.NewUserMessage(anthropic.NewTextBlock("world")),
		anthropic.NewAssistantMessage(anthropic.NewTextBlock("reply")),
	})
	require.Len(t, merged, 2)
	assert.Equal(t, anthropic.MessageParamRoleUser, merged[0].Role)
	assert.Len(t, merged[0].Content, 2)
	assert.Equal(t, anthropic.MessageParamRoleAssistant, merged[1].Role)
}

func TestInferProviderIDAnthropicURL(t *testing.T) {
	got := InferProviderID("https://api.anthropic.com", "claude-sonnet-4.6")
	assert.Equal(t, "anthropic", got)
}

func TestModelCapabilitiesRegistryAnthropicModels(t *testing.T) {
	claudeModels := []string{
		"claude-opus-4.6",
		"claude-sonnet-4.6",
		"claude-sonnet-4.5",
		"claude-haiku-4.5",
		"claude-3.7-sonnet",
	}
	for _, m := range claudeModels {
		t.Run(m, func(t *testing.T) {
			desc := DescribeModel("anthropic", m)
			assert.True(t, desc.Known, "model should be known")
			assert.Equal(t, "anthropic", desc.ProviderID)
			assert.True(t, desc.Capabilities.Tools)
		})
	}
}

func TestConvertImageURL(t *testing.T) {
	t.Run("data URI base64 JPEG", func(t *testing.T) {
		url := "data:image/jpeg;base64,/9j/4AAQ=="
		block, ok := convertImageURL(url)
		assert.True(t, ok)
		require.NotNil(t, block.OfImage)
	})

	t.Run("https URL", func(t *testing.T) {
		url := "https://example.com/image.png"
		block, ok := convertImageURL(url)
		assert.True(t, ok)
		require.NotNil(t, block.OfImage)
	})

	t.Run("unsupported scheme", func(t *testing.T) {
		_, ok := convertImageURL("ftp://example.com/image.png")
		assert.False(t, ok)
	})
}
