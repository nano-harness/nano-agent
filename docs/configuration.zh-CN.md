# 配置

[English](./configuration.md)

## 环境变量

`nano` 按以下优先级读取环境变量：

- `NANO_API_KEY`（推荐）—— 你的 LLM 提供商 API 密钥
- `API_KEY`（旧版回退）—— 同上，为向后兼容而保留
- `NANO_BASE_URL` / `BASE_URL` —— 提供商的自定义基础 URL
- `NANO_MODEL` / `MODEL` —— 模型名称（例如 `claude-sonnet-4-20250514`）
- `NANO_BINARY_TIMEOUT_MS` —— `nano binary exec` 的超时时间（0 = 不限制）
- `NANO_PERMISSION_MODE` —— 权限模式：`auto`、`ask`、`off`
- `NANO_SESSION_ID` / `SYMPHONY_ISSUE_ID` —— 用于 hook 路由的会话 ID
- `NANO_ORCHESTRATOR_PROFILE` —— 嵌入式执行配置档案

推荐使用 `NANO_*` 前缀以避免与其他工具冲突。不带前缀的名称
仍然可用，以保证向后兼容。

## MCP 配置文件

`nano binary exec` 通过 `--mcp-config` flag 接受与 Claude Code 兼容的
`.mcp.json`。这让你无需编辑 `nano.yaml` 即可注册 MCP server：

```bash
nano binary exec --mcp-config ./symphony.mcp.json --allowedTools 'mcp_symphony_*' --output-dir ./out "your prompt"
```

`.mcp.json` 是一个带有 `mcpServers` 的标准 JSON 对象：

```json
{
  "mcpServers": {
    "symphony": {
      "type": "http",
      "url": "http://localhost:8080/sse",
      "headers": {
        "X-Symphony-Token": "secret-token"
      }
    }
  }
}
```

支持的传输方式：

| `type` | nano 传输方式 | 必需字段 |
|---|---|---|
| `http`、`sse`、`streamable` | `streamable` | `url` |
| `stdio` | `stdio` | `command`（可选 `args`） |
| 省略（自动检测） | 根据 `command` 与 `url` 推断 | `command` 或 `url` 之一 |

## 工具权限

`nano binary exec` 支持细粒度的工具允许/拒绝列表：

```bash
nano binary exec \
  --allowedTools 'mcp_symphony_*' \
  --allowedTools 'ReadFile' \
  --disallowedTools 'Bash' \
  --disallowedTools 'WriteFile' \
  --output-dir ./out "prompt"
```

规则：

- `--allowedTools` 可重复使用。对于给定的工具名称，第一个匹配的前缀生效。
- `--disallowedTools` 对匹配前缀的优先级高于 `--allowedTools`。
- 如果两个 flag 都未提供，则使用配置层的 `mcp.allowed_tools` / `mcp.disallowed_tools` 列表。
- 如果完全没有配置任何列表，则允许所有工具（仍受常规权限模式约束）。

## 配置查看

查看合并后的配置（包括环境变量覆盖和默认值）：

```bash
nano config show --effective
```

读取单个键（支持点号路径）：

```bash
nano config get api_key
nano config get advanced.fork.max_depth
```

## 配置持久化

`nano mcp add` 和 `nano mcp auth` 只会更新 `config.yaml` 中的 `mcp:` 块。
其他键、注释和顺序都会保留。这是一次精确更新 —— 而不是完整重写配置。
