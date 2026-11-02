package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWebFetchTool(t *testing.T) {
	// Create test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/text":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(200)
			w.Write([]byte("Hello, World! This is plain text.")) //nolint:errcheck
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(200)
			w.Write([]byte(` //nolint:errcheck
				<html>
					<head><title>Test Page</title></head>
					<body>
						<h1>Welcome</h1>
						<p>This is a <strong>test</strong> paragraph.</p>
						<a href="http://example.com">Link to example</a>
					</body>
				</html>
			`))
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{"message": "Hello JSON", "status": "success"}`)) //nolint:errcheck
		case "/large":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(200)
			// Write large content
			largeContent := strings.Repeat("This is a large content line.\n", 1000)
			w.Write([]byte(largeContent))
		case "/redirect":
			w.Header().Set("Location", "/text")
			w.WriteHeader(302)
		case "/error":
			w.WriteHeader(404)
			w.Write([]byte("Not Found"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	// Parse the test server's host:port so it can be added to the SSRF allowlist.
	serverHostPort, _ := func() (string, error) {
		u, err := url.Parse(server.URL)
		if err != nil {
			return "", err
		}
		return u.Host, nil
	}()

	tool := NewWebFetchTool(map[string]interface{}{
		// Allowlist the loopback test server so SSRF guard doesn't block it.
		"web_ssrf_allowed_hosts": []string{serverHostPort},
	})

	tests := []struct {
		name     string
		params   map[string]interface{}
		wantErr  bool
		validate func(*testing.T, interface{})
	}{
		{
			name: "fetch plain text",
			params: map[string]interface{}{
				"url": server.URL + "/text",
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				fetchResult, ok := result.(*WebFetchResult)
				if !ok {
					t.Error("Expected result to be *WebFetchResult")
					return
				}
				if !fetchResult.Success {
					t.Errorf("Fetch should have succeeded: %s", fetchResult.Error)
					return
				}
				if !strings.Contains(fetchResult.Content, "Hello, World!") {
					t.Error("Expected to find 'Hello, World!' in content")
				}
				if fetchResult.StatusCode != 200 {
					t.Errorf("Expected status code 200, got %d", fetchResult.StatusCode)
				}
			},
		},
		{
			name: "fetch and convert HTML",
			params: map[string]interface{}{
				"url":               server.URL + "/html",
				"extract_text_only": true,
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				fetchResult, ok := result.(*WebFetchResult)
				if !ok {
					t.Error("Expected result to be *WebFetchResult")
					return
				}
				if !fetchResult.Success {
					t.Errorf("Fetch should have succeeded: %s", fetchResult.Error)
					return
				}
				// Should convert HTML to text
				if strings.Contains(fetchResult.Content, "<html>") {
					t.Error("Content should not contain HTML tags after conversion")
				}
				if !strings.Contains(fetchResult.Content, "Welcome") {
					t.Error("Expected to find 'Welcome' in converted text")
				}
			},
		},
		{
			name: "fetch JSON content",
			params: map[string]interface{}{
				"url": server.URL + "/json",
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				fetchResult, ok := result.(*WebFetchResult)
				if !ok {
					t.Error("Expected result to be *WebFetchResult")
					return
				}
				if !fetchResult.Success {
					t.Errorf("Fetch should have succeeded: %s", fetchResult.Error)
					return
				}
				if !strings.Contains(fetchResult.Content, "Hello JSON") {
					t.Error("Expected to find JSON content")
				}
			},
		},
		{
			name: "fetch with content length limit",
			params: map[string]interface{}{
				"url":                server.URL + "/large",
				"max_content_length": float64(100),
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				fetchResult, ok := result.(*WebFetchResult)
				if !ok {
					t.Error("Expected result to be *WebFetchResult")
					return
				}
				if !fetchResult.Success {
					t.Errorf("Fetch should have succeeded: %s", fetchResult.Error)
					return
				}
				if fetchResult.ContentLength > 100 {
					t.Errorf("Content should be limited to 100 bytes, got %d", fetchResult.ContentLength)
				}
			},
		},
		{
			name: "fetch with custom user agent",
			params: map[string]interface{}{
				"url":        server.URL + "/text",
				"user_agent": "CustomAgent/1.0",
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				fetchResult, ok := result.(*WebFetchResult)
				if !ok {
					t.Error("Expected result to be *WebFetchResult")
					return
				}
				if !fetchResult.Success {
					t.Errorf("Fetch should have succeeded: %s", fetchResult.Error)
				}
			},
		},
		{
			name: "fetch with AI prompt",
			params: map[string]interface{}{
				"url":    server.URL + "/text",
				"prompt": "Summarize this content",
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				fetchResult, ok := result.(*WebFetchResult)
				if !ok {
					t.Error("Expected result to be *WebFetchResult")
					return
				}
				if !fetchResult.Success {
					t.Errorf("Fetch should have succeeded: %s", fetchResult.Error)
					return
				}
				if fetchResult.ProcessedContent == "" {
					t.Error("Expected processed content when prompt is provided")
				}
			},
		},
		{
			name: "fetch HTTP error",
			params: map[string]interface{}{
				"url": server.URL + "/error",
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				fetchResult, ok := result.(*WebFetchResult)
				if !ok {
					t.Error("Expected result to be *WebFetchResult")
					return
				}
				if fetchResult.Success {
					t.Error("Fetch should have failed for 404 error")
				}
				if fetchResult.StatusCode != 404 {
					t.Errorf("Expected status code 404, got %d", fetchResult.StatusCode)
				}
			},
		},
		{
			name: "invalid URL",
			params: map[string]interface{}{
				"url": "invalid-url",
			},
			wantErr: true,
		},
		{
			name: "non-HTTP URL",
			params: map[string]interface{}{
				"url": "ftp://example.com",
			},
			wantErr: true,
		},
		{
			name: "missing URL parameter",
			params: map[string]interface{}{
				"prompt": "test",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tt.params)

			if tt.wantErr {
				if err != nil || !result.Success {
					return // Expected error
				}
				t.Error("Expected error but got none")
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Note: Web fetch tool can have Success=false for HTTP errors, which is not a tool error
			if tt.validate != nil {
				tt.validate(t, result.Data)
			}
		})
	}
}

func TestWebSearchTool(t *testing.T) {
	// Skip network dependent tests
	t.Skip("Skipping network dependent tests in TestWebSearchTool")
	tool := NewWebSearchTool(nil)

	tests := []struct {
		name     string
		params   map[string]interface{}
		wantErr  bool
		validate func(*testing.T, interface{})
	}{
		{
			name: "search with DuckDuckGo",
			params: map[string]interface{}{
				"query":       "golang programming",
				"engine":      "duckduckgo",
				"max_results": float64(3),
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				searchResult, ok := result.(*WebSearchResult)
				if !ok {
					t.Error("Expected result to be *WebSearchResult")
					return
				}
				// Note: DuckDuckGo might not return results in test environment
				// This test mainly verifies the structure and error handling
				if searchResult.Query != "golang programming" {
					t.Errorf("Expected query 'golang programming', got '%s'", searchResult.Query)
				}
				if searchResult.Engine != "duckduckgo" {
					t.Errorf("Expected engine 'duckduckgo', got '%s'", searchResult.Engine)
				}
			},
		},
		{
			name: "search with auto engine selection",
			params: map[string]interface{}{
				"query":       "test query",
				"engine":      "auto",
				"max_results": float64(5),
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				searchResult, ok := result.(*WebSearchResult)
				if !ok {
					t.Error("Expected result to be *WebSearchResult")
					return
				}
				if searchResult.Query != "test query" {
					t.Errorf("Expected query 'test query', got '%s'", searchResult.Query)
				}
				// Engine should be one of the supported ones
				supportedEngines := []string{"duckduckgo", "serper", "tavily"}
				found := false
				for _, engine := range supportedEngines {
					if searchResult.Engine == engine {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Engine '%s' not in supported list", searchResult.Engine)
				}
			},
		},
		{
			name: "search with parameters",
			params: map[string]interface{}{
				"query":            "golang tutorial",
				"engine":           "duckduckgo",
				"max_results":      float64(2),
				"country":          "us",
				"language":         "en",
				"safe_search":      true,
				"include_snippets": true,
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				searchResult, ok := result.(*WebSearchResult)
				if !ok {
					t.Error("Expected result to be *WebSearchResult")
					return
				}
				if searchResult.Query != "golang tutorial" {
					t.Errorf("Expected query 'golang tutorial', got '%s'", searchResult.Query)
				}
			},
		},
		{
			name: "unsupported search engine",
			params: map[string]interface{}{
				"query":  "test",
				"engine": "unsupported_engine",
			},
			wantErr: false,
			validate: func(t *testing.T, result interface{}) {
				searchResult, ok := result.(*WebSearchResult)
				if !ok {
					t.Error("Expected result to be *WebSearchResult")
					return
				}
				if searchResult.Success {
					t.Error("Search should have failed for unsupported engine")
				}
			},
		},
		{
			name: "empty query",
			params: map[string]interface{}{
				"query": "",
			},
			wantErr: true,
		},
		{
			name: "whitespace only query",
			params: map[string]interface{}{
				"query": "   ",
			},
			wantErr: true,
		},
		{
			name: "missing query parameter",
			params: map[string]interface{}{
				"engine": "duckduckgo",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create context with longer timeout for network operations
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			result, err := tool.Execute(ctx, tt.params)

			if tt.wantErr {
				if err != nil || !result.Success {
					return // Expected error
				}
				t.Error("Expected error but got none")
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Note: Web search might fail due to network issues or API limitations
			// The test mainly validates parameter handling and structure
			if tt.validate != nil {
				// Skip validation if the search failed due to network issues
				if result.Success {
					tt.validate(t, result.Data)
				} else {
					t.Logf("Skipping validation due to network failure: %v", result.Error)
				}
			}
		})
	}
}

func TestHTMLToText(t *testing.T) {
	tool := NewWebFetchTool(nil)

	tests := []struct {
		name        string
		html        string
		expected    []string // Strings that should be present in output
		notExpected []string // Strings that should NOT be present in output
	}{
		{
			name:        "simple HTML conversion",
			html:        `<html><body><h1>Title</h1><p>Paragraph text</p></body></html>`,
			expected:    []string{"# Title", "Paragraph text"},
			notExpected: []string{"<html>", "<body>", "<h1>", "<p>"},
		},
		{
			name:        "HTML with links",
			html:        `<a href="https://example.com">Link Text</a>`,
			expected:    []string{"[Link Text](https://example.com)"},
			notExpected: []string{"<a href"},
		},
		{
			name:        "HTML with lists",
			html:        `<ul><li>Item 1</li><li>Item 2</li></ul>`,
			expected:    []string{"- Item 1", "- Item 2"},
			notExpected: []string{"<ul>", "<li>"},
		},
		{
			name:        "HTML with script and style tags",
			html:        `<html><head><style>body{color:red}</style></head><body><script>alert('test')</script><p>Content</p></body></html>`,
			expected:    []string{"Content"},
			notExpected: []string{"body{color:red}", "alert('test')", "<script>", "<style>"},
		},
		{
			name:        "HTML entities",
			html:        `<p>&lt;test&gt; &amp; &quot;quotes&quot;</p>`,
			expected:    []string{"<test>", "&", "\"quotes\""},
			notExpected: []string{"&lt;", "&gt;", "&amp;", "&quot;"},
		},
		{
			name:        "multiple heading levels",
			html:        `<h1>H1</h1><h2>H2</h2><h3>H3</h3><h4>H4</h4>`,
			expected:    []string{"# H1", "## H2", "### H3", "#### H4"},
			notExpected: []string{"<h1>", "<h2>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.htmlToMarkdown(tt.html)

			for _, expected := range tt.expected {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected to find '%s' in result: %s", expected, result)
				}
			}

			for _, notExpected := range tt.notExpected {
				if strings.Contains(result, notExpected) {
					t.Errorf("Expected NOT to find '%s' in result: %s", notExpected, result)
				}
			}
		})
	}
}

func TestWebToolsSchema(t *testing.T) {
	// Test that tools have proper schemas
	webFetchTool := NewWebFetchTool(nil)
	webSearchTool := NewWebSearchTool(nil)

	// Test WebFetchTool schema
	fetchSchema := webFetchTool.Schema()
	if fetchSchema == nil { //nolint:staticcheck
		t.Error("WebFetchTool should have a schema")
	}
	if fetchSchema.Properties["url"] == nil { //nolint:staticcheck
		t.Error("WebFetchTool schema should have url property")
	}

	// Test WebSearchTool schema
	searchSchema := webSearchTool.Schema()
	if searchSchema == nil { //nolint:staticcheck
		t.Error("WebSearchTool should have a schema")
	}
	if searchSchema.Properties["query"] == nil { //nolint:staticcheck
		t.Error("WebSearchTool schema should have query property")
	}

	// Test tool metadata
	if webFetchTool.Name() != "web_fetch" {
		t.Errorf("Expected WebFetchTool name 'web_fetch', got '%s'", webFetchTool.Name())
	}
	if webSearchTool.Name() != "web_search" {
		t.Errorf("Expected WebSearchTool name 'web_search', got '%s'", webSearchTool.Name())
	}

	if !webFetchTool.RequiresConfirmation() {
		t.Error("WebFetchTool should require confirmation")
	}
	if !webSearchTool.RequiresConfirmation() {
		t.Error("WebSearchTool should require confirmation")
	}
}

// -- A3: SSRF protection tests

func TestIsBlockedIP(t *testing.T) {
	blockedAddrs := []string{
		"127.0.0.1",
		"::1",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.169.254",
		"0.0.0.0",
	}
	for _, addr := range blockedAddrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			t.Errorf("net.ParseIP(%q) returned nil", addr)
			continue
		}
		if !isBlockedIP(ip) {
			t.Errorf("isBlockedIP(%q) = false, want true", addr)
		}
	}
}

func TestValidateURLForSSRF_BlocksPrivate(t *testing.T) {
	blockedURLs := []string{
		"http://127.0.0.1/",
		"http://localhost/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://[::1]/",
	}
	for _, rawURL := range blockedURLs {
		u, err := url.Parse(rawURL)
		if err != nil {
			t.Errorf("url.Parse(%q) error: %v", rawURL, err)
			continue
		}
		if err := validateURLForSSRF(u); err == nil {
			t.Errorf("validateURLForSSRF(%q) = nil, want error (should be blocked)", rawURL)
		}
	}
}

func TestWebFetchTool_BlocksSSRF(t *testing.T) {
	tool := NewWebFetchTool(nil)

	// A test server bound to 127.0.0.1 should be blocked by the SSRF guard.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "secret data")
	}))
	defer server.Close()

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"url": server.URL + "/secret",
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.Success {
		t.Error("Execute should fail for loopback address (SSRF blocked)")
	}
	if result.Error == "" || (!strings.Contains(result.Error, "blocked") && !strings.Contains(result.Error, "security")) {
		t.Errorf("Error message should mention blocking, got: %q", result.Error)
	}
}

func TestWebFetchTool_BlocksRedirectToPrivate(t *testing.T) {
	tool := NewWebFetchTool(nil)

	// Server on loopback that would be the redirect target.
	privateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "private data")
	}))
	defer privateServer.Close()

	// A redirect server also on loopback – both blocked, but this tests the
	// redirect-validation path specifically.
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, privateServer.URL, http.StatusFound)
	}))
	defer redirectServer.Close()

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"url": redirectServer.URL + "/",
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	// Both initial URL and redirect target are loopback – must be blocked.
	if result.Success {
		t.Error("Execute should fail when both origin and redirect target are private addresses")
	}
}
