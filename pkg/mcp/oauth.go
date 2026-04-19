package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuthConfig holds OAuth 2.0 / PKCE settings for an MCP server.
type OAuthConfig struct {
	// AuthorizationURL is the OAuth authorization endpoint.
	AuthorizationURL string `json:"authorization_url" yaml:"authorization_url"`
	// TokenURL is the OAuth token endpoint.
	TokenURL string `json:"token_url" yaml:"token_url"`
	// ClientID is the registered OAuth client ID.
	ClientID string `json:"client_id" yaml:"client_id"`
	// ClientSecret is optional for public clients.
	ClientSecret string `json:"client_secret,omitempty" yaml:"client_secret,omitempty"`
	// Scopes is the space-separated list of requested scopes.
	Scopes string `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	// RedirectPort is the localhost callback port (default: random ephemeral).
	RedirectPort int `json:"redirect_port,omitempty" yaml:"redirect_port,omitempty"`
}

// OAuthClient performs the PKCE Authorization Code flow for a single server.
type OAuthClient struct {
	cfg   *OAuthConfig
	store *TokenStore
}

// NewOAuthClient creates an OAuthClient backed by the given TokenStore.
func NewOAuthClient(cfg *OAuthConfig, store *TokenStore) *OAuthClient {
	return &OAuthClient{cfg: cfg, store: store}
}

// Authorize runs the full PKCE flow for the given server name.
// It opens a local HTTP listener to capture the authorization code callback.
// openURL is called with the authorization URL so callers can open a browser.
func (c *OAuthClient) Authorize(ctx context.Context, serverName string, openURL func(string) error) (*TokenEntry, error) {
	// ── 1. Generate PKCE verifier + challenge ──────────────────────────────────
	verifier, err := generateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("oauth: generate verifier: %w", err)
	}
	challenge := codeChallenge(verifier)

	// ── 2. Start local callback server ────────────────────────────────────────
	port := c.cfg.RedirectPort
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("oauth: listen on :%d: %w", port, err)
	}
	defer ln.Close() //nolint:errcheck
	actualPort := ln.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", actualPort)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	srv := &http.Server{ReadHeaderTimeout: 15 * time.Second}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		errParam := r.URL.Query().Get("error")
		if errParam != "" {
			errCh <- fmt.Errorf("oauth callback error: %s – %s", errParam, r.URL.Query().Get("error_description"))
			http.Error(w, "Authorization failed. You may close this tab.", http.StatusBadRequest)
			return
		}
		if code == "" {
			errCh <- fmt.Errorf("oauth callback: missing code parameter")
			http.Error(w, "Missing code. You may close this tab.", http.StatusBadRequest)
			return
		}
		codeCh <- code
		_, _ = fmt.Fprintln(w, "Authorization successful! You may close this tab.")
	})

	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			errCh <- serveErr
		}
	}()
	defer srv.Shutdown(context.Background()) //nolint:errcheck

	// ── 3. Build authorization URL ───────────────────────────────────────────
	state, _ := generateState()
	authURL, err := c.buildAuthURL(redirectURI, challenge, state)
	if err != nil {
		return nil, err
	}
	if openURL != nil {
		if err := openURL(authURL); err != nil {
			fmt.Printf("Open this URL in your browser:\n\n  %s\n\n", authURL)
		}
	}

	// ── 4. Wait for callback ─────────────────────────────────────────────────
	var code string
	select {
	case code = <-codeCh:
	case err = <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// ── 5. Exchange code for token ────────────────────────────────────────────
	entry, err := c.exchangeCode(ctx, code, verifier, redirectURI)
	if err != nil {
		return nil, err
	}
	entry.ServerName = serverName

	// ── 6. Persist token ─────────────────────────────────────────────────────
	if c.store != nil {
		if err := c.store.Set(entry); err != nil {
			return nil, fmt.Errorf("oauth: save token: %w", err)
		}
	}
	return entry, nil
}

// RefreshToken uses the stored refresh token to obtain a new access token.
func (c *OAuthClient) RefreshToken(ctx context.Context, serverName string) (*TokenEntry, error) {
	existing := c.store.Get(serverName)
	if existing == nil || existing.RefreshToken == "" {
		return nil, fmt.Errorf("oauth: no refresh token for server %q", serverName)
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {existing.RefreshToken},
		"client_id":     {c.cfg.ClientID},
	}
	if c.cfg.ClientSecret != "" {
		form.Set("client_secret", c.cfg.ClientSecret)
	}

	entry, err := c.postTokenRequest(ctx, form)
	if err != nil {
		return nil, fmt.Errorf("oauth: refresh: %w", err)
	}
	if entry.RefreshToken == "" {
		entry.RefreshToken = existing.RefreshToken // keep old refresh token if new one not issued
	}
	entry.ServerName = serverName

	if c.store != nil {
		if err := c.store.Set(entry); err != nil {
			return nil, fmt.Errorf("oauth: save refreshed token: %w", err)
		}
	}
	return entry, nil
}

// GetValidToken returns a valid token for the server, refreshing if necessary.
func (c *OAuthClient) GetValidToken(ctx context.Context, serverName string) (*TokenEntry, error) {
	entry := c.store.Get(serverName)
	if entry == nil {
		return nil, fmt.Errorf("oauth: no token for server %q; run 'nano mcp auth %s'", serverName, serverName)
	}
	if entry.IsExpired() && entry.RefreshToken != "" {
		return c.RefreshToken(ctx, serverName)
	}
	return entry, nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

func (c *OAuthClient) buildAuthURL(redirectURI, challenge, state string) (string, error) {
	u, err := url.Parse(c.cfg.AuthorizationURL)
	if err != nil {
		return "", fmt.Errorf("oauth: parse auth URL: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", c.cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	if c.cfg.Scopes != "" {
		q.Set("scope", c.cfg.Scopes)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (c *OAuthClient) exchangeCode(ctx context.Context, code, verifier, redirectURI string) (*TokenEntry, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {c.cfg.ClientID},
		"code_verifier": {verifier},
	}
	if c.cfg.ClientSecret != "" {
		form.Set("client_secret", c.cfg.ClientSecret)
	}
	return c.postTokenRequest(ctx, form)
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (c *OAuthClient) postTokenRequest(ctx context.Context, form url.Values) (*TokenEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("token response parse: %w", err)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("token error %q: %s", tr.Error, tr.ErrorDesc)
	}

	entry := &TokenEntry{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
		Scope:        tr.Scope,
	}
	if tr.ExpiresIn > 0 {
		entry.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return entry, nil
}

func generateCodeVerifier() (string, error) {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func codeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func generateState() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), err
}
