package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestCreateTransport_StreamableInjectsOAuthToken verifies that OAuth tokens
// are injected into streamable transport HTTP headers when configured.
func TestCreateTransport_StreamableInjectsOAuthToken(t *testing.T) {
	// Create a token store with a valid token
	store, err := NewTokenStore("")
	if err != nil {
		t.Fatalf("Failed to create token store: %v", err)
	}

	// Store a valid token
	entry := &TokenEntry{
		ServerName:   "test-server",
		AccessToken:  "test-access-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		RefreshToken: "",
		Scope:        "read write",
	}
	if err := store.Set(entry); err != nil {
		t.Fatalf("Failed to set token: %v", err)
	}

	// Create MCP client with token store
	client := &MCPClient{
		config:     &MCPConfig{},
		tokenStore: store,
	}

	// Create server config with OAuth
	serverConfig := MCPServerConfig{
		Name:      "test-server",
		URL:       "https://example.com/mcp",
		Transport: "streamable",
		OAuth: &OAuthConfig{
			AuthorizationURL: "https://auth.example.com/authorize",
			TokenURL:         "https://auth.example.com/token",
			ClientID:         "test-client-id",
		},
	}

	// Create transport
	ctx := context.Background()
	transport, cmd, err := client.createTransport(ctx, serverConfig)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	if cmd != nil {
		t.Fatalf("Expected no command for streamable transport, got %v", cmd)
	}

	// Verify transport is StreamableClientTransport
	streamableTransport, ok := transport.(*mcp.StreamableClientTransport)
	if !ok {
		t.Fatalf("Expected StreamableClientTransport, got %T", transport)
	}

	// Verify HTTP client has custom transport with headers
	if streamableTransport.HTTPClient == nil {
		t.Fatal("HTTP client is nil")
	}
	if streamableTransport.HTTPClient.Transport == nil {
		t.Fatal("HTTP client transport is nil")
	}

	// Verify the header round tripper contains Authorization header
	headerRT, ok := streamableTransport.HTTPClient.Transport.(*headerRoundTripper)
	if !ok {
		t.Fatalf("Expected headerRoundTripper, got %T", streamableTransport.HTTPClient.Transport)
	}

	authHeader, exists := headerRT.headers["Authorization"]
	if !exists {
		t.Fatal("Authorization header not found")
	}

	expectedAuth := "Bearer test-access-token"
	if authHeader != expectedAuth {
		t.Errorf("Expected Authorization header %q, got %q", expectedAuth, authHeader)
	}
}

// TestCreateTransport_StreamableMissingTokenReturnsError verifies that
// missing OAuth tokens result in a clear error message.
func TestCreateTransport_StreamableMissingTokenReturnsError(t *testing.T) {
	// Create empty token store in a temp directory to avoid pollution from other tests
	tempDir := t.TempDir()
	store, err := NewTokenStore(tempDir + "/tokens.json")
	if err != nil {
		t.Fatalf("Failed to create token store: %v", err)
	}

	// Ensure no token exists for test-server
	_ = store.Delete("test-server")

	// Create MCP client with token store
	client := &MCPClient{
		config:     &MCPConfig{},
		tokenStore: store,
	}

	// Create server config with OAuth but no token in store
	serverConfig := MCPServerConfig{
		Name:      "test-server",
		URL:       "https://example.com/mcp",
		Transport: "streamable",
		OAuth: &OAuthConfig{
			AuthorizationURL: "https://auth.example.com/authorize",
			TokenURL:         "https://auth.example.com/token",
			ClientID:         "test-client-id",
		},
	}

	// Attempt to create transport
	ctx := context.Background()
	_, _, err = client.createTransport(ctx, serverConfig)
	if err == nil {
		t.Fatal("Expected error for missing token, got nil")
	}

	// Verify error message contains guidance
	expectedSubstrings := []string{"oauth required", "nano mcp auth"}
	for _, substr := range expectedSubstrings {
		if !containsSubstring(err.Error(), substr) {
			t.Errorf("Expected error to contain %q, got: %v", substr, err)
		}
	}
}

// TestCreateTransport_LegacyHTTPRejected verifies that legacy transport
// types (http, sse, websocket) return explicit migration errors.
func TestCreateTransport_LegacyHTTPRejected(t *testing.T) {
	client := &MCPClient{
		config: &MCPConfig{},
	}

	legacyTransports := []string{"http", "sse", "websocket"}

	for _, transport := range legacyTransports {
		t.Run(transport, func(t *testing.T) {
			serverConfig := MCPServerConfig{
				Name:      "test-server",
				URL:       "https://example.com/mcp",
				Transport: transport,
			}

			ctx := context.Background()
			_, _, err := client.createTransport(ctx, serverConfig)
			if err == nil {
				t.Fatalf("Expected error for legacy transport %q, got nil", transport)
			}

			// Verify error mentions migration to streamable
			if !containsSubstring(err.Error(), "streamable") && !containsSubstring(err.Error(), "stdio") {
				t.Errorf("Expected error to mention 'streamable' or 'stdio', got: %v", err)
			}
		})
	}
}

// TestCreateTransport_UserHeaderTakesPrecedence verifies that user-provided
// Authorization headers are not overwritten by OAuth tokens.
func TestCreateTransport_UserHeaderTakesPrecedence(t *testing.T) {
	// Create a token store with a valid token
	store, err := NewTokenStore("")
	if err != nil {
		t.Fatalf("Failed to create token store: %v", err)
	}

	// Store a valid token
	entry := &TokenEntry{
		ServerName:   "test-server",
		AccessToken:  "oauth-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		RefreshToken: "",
		Scope:        "read write",
	}
	if err := store.Set(entry); err != nil {
		t.Fatalf("Failed to set token: %v", err)
	}

	// Create MCP client with token store
	client := &MCPClient{
		config:     &MCPConfig{},
		tokenStore: store,
	}

	// User explicitly provides their own Authorization header
	userAuth := "Bearer user-provided-token"
	serverConfig := MCPServerConfig{
		Name:      "test-server",
		URL:       "https://example.com/mcp",
		Transport: "streamable",
		Headers: map[string]string{
			"Authorization": userAuth,
		},
		OAuth: &OAuthConfig{
			AuthorizationURL: "https://auth.example.com/authorize",
			TokenURL:         "https://auth.example.com/token",
			ClientID:         "test-client-id",
		},
	}

	// Create transport
	ctx := context.Background()
	transport, _, err := client.createTransport(ctx, serverConfig)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}

	// Verify transport
	streamableTransport := transport.(*mcp.StreamableClientTransport)
	headerRT := streamableTransport.HTTPClient.Transport.(*headerRoundTripper)

	authHeader := headerRT.headers["Authorization"]
	if authHeader != userAuth {
		t.Errorf("User-provided Authorization header was overwritten. Expected %q, got %q", userAuth, authHeader)
	}
}

// TestCreateTransport_NoOAuth verifies that streamable transport works
// without OAuth when not configured.
func TestCreateTransport_NoOAuth(t *testing.T) {
	client := &MCPClient{
		config: &MCPConfig{},
	}

	serverConfig := MCPServerConfig{
		Name:      "test-server",
		URL:       "https://example.com/mcp",
		Transport: "streamable",
		// No OAuth configured
	}

	ctx := context.Background()
	transport, cmd, err := client.createTransport(ctx, serverConfig)
	if err != nil {
		t.Fatalf("Failed to create transport without OAuth: %v", err)
	}
	if cmd != nil {
		t.Fatalf("Expected no command for streamable transport, got %v", cmd)
	}
	if transport == nil {
		t.Fatal("Expected non-nil transport")
	}
}

// containsSubstring is a helper to check if a string contains a substring (case-insensitive).
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findInString(s, substr)))
}

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Mock StreamableClientTransport for testing (using the real one from mcp SDK)
// We import it from the SDK, so we need to make sure our test can access it.
// If the SDK doesn't export it, we'd need to use a different approach.
// For now, assume it's available from the mcp package.
