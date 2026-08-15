# MCP OAuth 2.0 Integration Guide

[中文](./MCP_OAUTH.zh-CN.md)

This guide explains how to configure and use OAuth 2.0 authentication with MCP (Model Context Protocol) servers in nano-agent.

## Overview

nano-agent supports OAuth 2.0 Authorization Code flow with PKCE (Proof Key for Code Exchange) for authenticating with MCP servers that require OAuth. Once configured, access tokens are automatically injected into requests, and refresh tokens are used to maintain long-lived sessions.

## Configuration

### Server Configuration with OAuth

Add OAuth configuration to your MCP server in `~/.nano/config.yaml`:

```yaml
mcp:
  enable_client: true
  servers:
    - name: my-oauth-server
      description: Example OAuth-protected MCP server
      transport: streamable
      url: https://api.example.com/mcp
      enabled: true
      oauth:
        authorization_url: https://auth.example.com/oauth/authorize
        token_url: https://auth.example.com/oauth/token
        client_id: your-client-id
        client_secret: your-client-secret  # Optional for public clients
        scopes: read write                 # Space-separated scopes
        redirect_port: 0                   # 0 = random port (default)
```

### OAuth Configuration Fields

| Field | Required | Description |
|-------|----------|-------------|
| `authorization_url` | Yes | OAuth 2.0 authorization endpoint URL |
| `token_url` | Yes | OAuth 2.0 token endpoint URL |
| `client_id` | Yes | Your OAuth client ID |
| `client_secret` | No | OAuth client secret (omit for public clients using PKCE) |
| `scopes` | No | Space-separated list of OAuth scopes to request |
| `redirect_port` | No | Local port for OAuth callback (0 = random ephemeral port) |

## Authorization Flow

### Initial Authorization

Run the `nano mcp auth` command to start the OAuth flow:

```bash
# If OAuth is configured in server settings
nano mcp auth my-oauth-server

# Or provide OAuth parameters directly
nano mcp auth my-oauth-server \
  --auth-url https://auth.example.com/oauth/authorize \
  --token-url https://auth.example.com/oauth/token \
  --client-id your-client-id \
  --scopes "read write"
```

The authorization flow:

1. **nano-agent starts a local HTTP server** on `127.0.0.1` (random port or configured port)
2. **Generates PKCE code verifier and challenge** for secure authorization
3. **Opens your browser** (or displays a URL to open manually) to the authorization endpoint
4. **User authorizes** the application in their browser
5. **OAuth provider redirects** back to the local callback URL with authorization code
6. **nano-agent exchanges** the code for an access token (and optionally a refresh token)
7. **Tokens are stored** in `~/.nano/mcp_tokens.json` with secure permissions (0600)

### Token Storage

Tokens are stored in `~/.nano/mcp_tokens.json`:

```json
{
  "my-oauth-server": {
    "server_name": "my-oauth-server",
    "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "v1.a1b2c3d4...",
    "token_type": "Bearer",
    "expires_at": "2024-12-31T23:59:59Z",
    "scope": "read write"
  }
}
```

**Security:** The token file is created with permissions `0600` (owner read/write only) to protect sensitive credentials.

### Automatic Token Injection

Once authorized, tokens are automatically injected into MCP requests:

1. When connecting to a server with `transport: streamable` and `oauth` configured
2. nano-agent loads the token from `~/.nano/mcp_tokens.json`
3. If the token is expired but a refresh token exists, it automatically refreshes the token
4. The access token is injected as an HTTP header: `Authorization: Bearer <access_token>`
5. User-provided `Authorization` headers in server configuration take precedence

### Token Refresh

When an access token expires:

- If a **refresh token** is available, nano-agent automatically refreshes the access token
- The new token is saved to `~/.nano/mcp_tokens.json`
- The refresh happens transparently during connection

If refresh fails or no refresh token exists:

```
Error: oauth required for server "my-oauth-server": run 'nano mcp auth my-oauth-server'
```

## CLI Commands

### Authorize a Server

```bash
nano mcp auth <server-name> [flags]
```

Flags:
- `--auth-url`: OAuth authorization endpoint URL
- `--token-url`: OAuth token endpoint URL
- `--client-id`: OAuth client ID
- `--client-secret`: OAuth client secret (optional)
- `--scopes`: Space-separated OAuth scopes

If OAuth is configured in `~/.nano/config.yaml` for the server, you can omit the flags and they will be loaded from configuration. Command-line flags override configuration values.

### List Stored Tokens

```bash
nano mcp auth --list
```

Example output:
```
SERVER               TYPE         EXPIRES                        SCOPES
my-oauth-server      Bearer       2024-12-31 23:59              read write
another-server       Bearer       EXPIRED                       admin
```

### Revoke a Token

```bash
nano mcp auth <server-name> --revoke
```

This deletes the token from local storage. Note: This does NOT revoke the token on the OAuth provider's side—you must do that through the provider's interface.

## Adding OAuth Servers

### Interactive Wizard

```bash
nano mcp wizard
```

The wizard will guide you through adding an OAuth-enabled server (OAuth configuration currently requires manual editing of `config.yaml`).

### Non-Interactive

```bash
nano mcp add my-server \
  --transport streamable \
  --description "OAuth-protected server" \
  https://api.example.com/mcp
```

Then manually edit `~/.nano/config.yaml` to add the `oauth` section.

After adding the OAuth configuration, run:

```bash
nano mcp auth my-server \
  --auth-url https://auth.example.com/oauth/authorize \
  --token-url https://auth.example.com/oauth/token \
  --client-id your-client-id
```

The OAuth configuration will be automatically persisted to the server settings.

## Troubleshooting

### Error: "oauth required for server 'xxx': run 'nano mcp auth xxx'"

**Cause:** No token found in token store, or token expired without a refresh token.

**Solution:** Run `nano mcp auth <server-name>` to re-authorize.

### Error: "transport 'http' is no longer supported"

**Cause:** Using legacy transport type in configuration.

**Solution:** Change `transport: http` to `transport: streamable` in `~/.nano/config.yaml`:

```yaml
# Before (not supported)
transport: http
url: https://api.example.com/mcp

# After (correct)
transport: streamable
url: https://api.example.com/mcp
```

### Browser doesn't open during authorization

The authorization URL will be printed to the console. Copy and paste it into your browser manually.

### Token refresh fails

If automatic token refresh fails:

1. Check that the OAuth provider issued a refresh token
2. Verify the token hasn't been revoked on the provider's side
3. Re-authorize: `nano mcp auth <server-name>`

### Port already in use

If you get a "port already in use" error during authorization, either:

1. Wait for the conflicting process to release the port
2. Configure a different `redirect_port` in the OAuth config

## Migration from Legacy Transports

If you have MCP servers configured with legacy transport types (`http`, `sse`, `websocket`), you must migrate to `streamable`:

### Before (Legacy - Not Supported)

```yaml
mcp:
  servers:
    - name: my-server
      transport: http
      url: https://api.example.com/mcp
```

### After (Correct)

```yaml
mcp:
  servers:
    - name: my-server
      transport: streamable
      url: https://api.example.com/mcp
```

The behavior is the same—`streamable` uses HTTP with SSE (Server-Sent Events) for bidirectional communication.

## Security Best Practices

1. **Use PKCE**: nano-agent always uses PKCE for public clients, providing additional security even without a client secret.

2. **Secure token storage**: Token files are created with `0600` permissions. Never share or commit `~/.nano/mcp_tokens.json`.

3. **Use HTTPS**: Always use `https://` URLs for authorization and token endpoints to protect tokens in transit.

4. **Scope limitation**: Request only the minimum scopes your application needs.

5. **Token rotation**: If a token is compromised, revoke it immediately through the OAuth provider's interface and re-authorize with nano-agent.

## Examples

### GitHub OAuth Example

```yaml
mcp:
  servers:
    - name: github-mcp
      transport: streamable
      url: https://api.github.com/mcp
      oauth:
        authorization_url: https://github.com/login/oauth/authorize
        token_url: https://github.com/login/oauth/access_token
        client_id: your_github_client_id
        scopes: repo user
```

Authorize:
```bash
nano mcp auth github-mcp
```

### Google OAuth Example

```yaml
mcp:
  servers:
    - name: google-mcp
      transport: streamable
      url: https://example.googleapis.com/mcp
      oauth:
        authorization_url: https://accounts.google.com/o/oauth2/v2/auth
        token_url: https://oauth2.googleapis.com/token
        client_id: your_google_client_id.apps.googleusercontent.com
        scopes: https://www.googleapis.com/auth/userinfo.email
```

Authorize:
```bash
nano mcp auth google-mcp
```

## Technical Details

### PKCE Flow

nano-agent implements [RFC 7636 - Proof Key for Code Exchange (PKCE)](https://tools.ietf.org/html/rfc7636):

1. Generates 48-byte random code verifier (base64url-encoded)
2. Creates SHA256 hash of verifier as code challenge
3. Includes `code_challenge` and `code_challenge_method=S256` in authorization request
4. Includes `code_verifier` in token exchange request

This protects against authorization code interception attacks, especially important for public clients without a client secret.

### Token Lifecycle

```
┌─────────────────────────────────────────────────────────────┐
│ 1. User runs: nano mcp auth my-server                       │
└───────────────────────┬─────────────────────────────────────┘
                        ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. Browser opens, user authorizes                            │
└───────────────────────┬─────────────────────────────────────┘
                        ▼
┌─────────────────────────────────────────────────────────────┐
│ 3. Tokens stored in ~/.nano/mcp_tokens.json                 │
└───────────────────────┬─────────────────────────────────────┘
                        ▼
┌─────────────────────────────────────────────────────────────┐
│ 4. nano-agent connects to MCP server                         │
│    - Loads token from store                                  │
│    - Checks if expired                                       │
│    - Refreshes if needed                                     │
│    - Injects Authorization: Bearer <token>                   │
└─────────────────────────────────────────────────────────────┘
```

### Header Precedence

If you provide an `Authorization` header explicitly in server configuration, it takes precedence over OAuth tokens:

```yaml
mcp:
  servers:
    - name: my-server
      transport: streamable
      url: https://api.example.com/mcp
      headers:
        Authorization: "Bearer my-static-token"  # This takes precedence
      oauth:
        # OAuth config here - will NOT override the above header
```

This allows you to override OAuth for testing or other purposes.
