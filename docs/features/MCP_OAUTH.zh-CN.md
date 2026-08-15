# MCP OAuth 2.0 集成指南

[English](./MCP_OAUTH.md)

本指南介绍如何在 nano-agent 中为需要 OAuth 的 MCP（Model Context Protocol）服务器配置和使用 OAuth 2.0 认证。

## 概述

nano-agent 支持带 PKCE（Proof Key for Code Exchange）的 OAuth 2.0 Authorization Code 流程，用于向需要 OAuth 的 MCP 服务器进行认证。配置完成后，access token 会自动注入到请求中，refresh token 则用于维持长期会话。

## 配置

### 带 OAuth 的服务器配置

在 `~/.nano/config.yaml` 中为你的 MCP 服务器添加 OAuth 配置：

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

### OAuth 配置字段

| 字段 | 是否必填 | 说明 |
|-------|----------|-------------|
| `authorization_url` | 是 | OAuth 2.0 授权端点 URL |
| `token_url` | 是 | OAuth 2.0 token 端点 URL |
| `client_id` | 是 | 你的 OAuth client ID |
| `client_secret` | 否 | OAuth client secret（使用 PKCE 的 public client 可省略） |
| `scopes` | 否 | 以空格分隔的要申请的 OAuth scope 列表 |
| `redirect_port` | 否 | OAuth 回调使用的本地端口（0 = 随机临时端口） |

## 授权流程

### 初始授权

运行 `nano mcp auth` 命令以启动 OAuth 流程：

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

授权流程如下：

1. **nano-agent 在 `127.0.0.1` 上启动一个本地 HTTP 服务器**（随机端口或配置的端口）
2. **生成 PKCE code verifier 和 code challenge**，用于安全授权
3. **打开浏览器**（或显示一个可手动打开的 URL）跳转到授权端点
4. **用户在浏览器中授权**该应用
5. **OAuth 提供方重定向**回本地回调 URL，并携带授权码（authorization code）
6. **nano-agent 将授权码交换**为 access token（以及可选的 refresh token）
7. **token 被存储**在 `~/.nano/mcp_tokens.json` 中，并设置了安全权限（0600）

### Token 存储

Token 存储在 `~/.nano/mcp_tokens.json` 中：

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

**安全性：** token 文件以 `0600` 权限（仅所有者可读写）创建，以保护敏感凭据。

### Token 自动注入

授权完成后，token 会自动注入到 MCP 请求中：

1. 当连接到配置了 `transport: streamable` 和 `oauth` 的服务器时
2. nano-agent 从 `~/.nano/mcp_tokens.json` 加载 token
3. 如果 token 已过期但存在 refresh token，会自动刷新 token
4. access token 以 HTTP 头的形式注入：`Authorization: Bearer <access_token>`
5. 服务器配置中用户提供的 `Authorization` 头优先级更高

### Token 刷新

当 access token 过期时：

- 如果有可用的 **refresh token**，nano-agent 会自动刷新 access token
- 新 token 会保存到 `~/.nano/mcp_tokens.json`
- 刷新过程在连接期间透明地进行

如果刷新失败或不存在 refresh token：

```
Error: oauth required for server "my-oauth-server": run 'nano mcp auth my-oauth-server'
```

## CLI 命令

### 授权一个服务器

```bash
nano mcp auth <server-name> [flags]
```

参数（flags）：
- `--auth-url`：OAuth 授权端点 URL
- `--token-url`：OAuth token 端点 URL
- `--client-id`：OAuth client ID
- `--client-secret`：OAuth client secret（可选）
- `--scopes`：以空格分隔的 OAuth scope

如果该服务器已在 `~/.nano/config.yaml` 中配置了 OAuth，可以省略这些 flags，它们会从配置中加载。命令行 flags 会覆盖配置值。

### 列出已存储的 Token

```bash
nano mcp auth --list
```

示例输出：
```
SERVER               TYPE         EXPIRES                        SCOPES
my-oauth-server      Bearer       2024-12-31 23:59              read write
another-server       Bearer       EXPIRED                       admin
```

### 撤销 Token

```bash
nano mcp auth <server-name> --revoke
```

这会从本地存储中删除 token。注意：这**不会**在 OAuth 提供方一侧撤销 token——你必须通过提供方的界面来完成撤销。

## 添加 OAuth 服务器

### 交互式向导

```bash
nano mcp wizard
```

向导会引导你添加一个启用 OAuth 的服务器（OAuth 配置目前需要手动编辑 `config.yaml`）。

### 非交互方式

```bash
nano mcp add my-server \
  --transport streamable \
  --description "OAuth-protected server" \
  https://api.example.com/mcp
```

然后手动编辑 `~/.nano/config.yaml` 以添加 `oauth` 部分。

添加 OAuth 配置后，运行：

```bash
nano mcp auth my-server \
  --auth-url https://auth.example.com/oauth/authorize \
  --token-url https://auth.example.com/oauth/token \
  --client-id your-client-id
```

OAuth 配置会被自动持久化到服务器设置中。

## 故障排查

### 错误："oauth required for server 'xxx': run 'nano mcp auth xxx'"

**原因：** token 存储中未找到 token，或 token 已过期且没有 refresh token。

**解决方案：** 运行 `nano mcp auth <server-name>` 重新授权。

### 错误："transport 'http' is no longer supported"

**原因：** 配置中使用了旧的 transport 类型。

**解决方案：** 在 `~/.nano/config.yaml` 中将 `transport: http` 改为 `transport: streamable`：

```yaml
# Before (not supported)
transport: http
url: https://api.example.com/mcp

# After (correct)
transport: streamable
url: https://api.example.com/mcp
```

### 授权时浏览器未打开

授权 URL 会打印到控制台。手动将其复制粘贴到你的浏览器中即可。

### Token 刷新失败

如果 token 自动刷新失败：

1. 检查 OAuth 提供方是否签发了 refresh token
2. 确认 token 未在提供方一侧被撤销
3. 重新授权：`nano mcp auth <server-name>`

### 端口已被占用

如果在授权过程中遇到"端口已被占用"（port already in use）错误，可以：

1. 等待占用该端口的进程释放端口
2. 在 OAuth 配置中配置一个不同的 `redirect_port`

## 从旧 Transport 迁移

如果你的 MCP 服务器配置了旧的 transport 类型（`http`、`sse`、`websocket`），必须迁移到 `streamable`：

### 迁移前（旧式 - 不再支持）

```yaml
mcp:
  servers:
    - name: my-server
      transport: http
      url: https://api.example.com/mcp
```

### 迁移后（正确）

```yaml
mcp:
  servers:
    - name: my-server
      transport: streamable
      url: https://api.example.com/mcp
```

行为是相同的——`streamable` 使用 HTTP 加 SSE（Server-Sent Events）实现双向通信。

## 安全最佳实践

1. **使用 PKCE**：nano-agent 始终为 public client 使用 PKCE，即使没有 client secret 也能提供额外的安全性。

2. **安全的 token 存储**：token 文件以 `0600` 权限创建。切勿分享或提交 `~/.nano/mcp_tokens.json`。

3. **使用 HTTPS**：授权和 token 端点务必使用 `https://` URL，以保护传输中的 token。

4. **限制 scope**：只申请你的应用所需的最小 scope。

5. **Token 轮换**：如果某个 token 泄露，立即通过 OAuth 提供方的界面将其撤销，并使用 nano-agent 重新授权。

## 示例

### GitHub OAuth 示例

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

授权：
```bash
nano mcp auth github-mcp
```

### Google OAuth 示例

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

授权：
```bash
nano mcp auth google-mcp
```

## 技术细节

### PKCE 流程

nano-agent 实现了 [RFC 7636 - Proof Key for Code Exchange (PKCE)](https://tools.ietf.org/html/rfc7636)：

1. 生成 48 字节的随机 code verifier（base64url 编码）
2. 对 verifier 做 SHA256 哈希作为 code challenge
3. 在授权请求中包含 `code_challenge` 和 `code_challenge_method=S256`
4. 在 token 交换请求中包含 `code_verifier`

这可以防御授权码拦截攻击，对于没有 client secret 的 public client 尤为重要。

### Token 生命周期

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

### Header 优先级

如果你在服务器配置中显式提供了 `Authorization` 头，它的优先级高于 OAuth token：

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

这允许你在测试或其他场景下覆盖 OAuth。
