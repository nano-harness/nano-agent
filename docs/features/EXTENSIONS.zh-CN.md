# 扩展

[English](./EXTENSIONS.md)

扩展生态系统将 skills、MCP 服务器、tools、agents 和 commands 统一为通用的 manifest。

## Manifest 类型

支持的扩展类型：

- `skill`
- `mcp`
- `tool`
- `agent`
- `command`

`manage_extension` 可以列出 manifest、查看某个 manifest、诊断扩展、
检查信任/审计元数据，以及管理支持的安装/更新/移除
操作。

支持的操作：

- `list` / `status` - 列出所有扩展或显示某个扩展的状态
- `manifest` - 显示某个扩展的 manifest
- `install` / `update` - 安装或更新扩展
- `enable` / `disable` - 启用或禁用扩展
- `remove` - 移除扩展
- `doctor` - 诊断扩展的健康问题
- `trust` - 管理扩展的信任元数据
- `audit` - 查看扩展的审计信息

## 使用示例

### 列出所有扩展

```json
{
  "action": "list"
}
```

### 查看扩展状态

```json
{
  "action": "status",
  "name": "my-skill"
}
```

### 查看扩展 Manifest

```json
{
  "action": "manifest",
  "name": "my-mcp-server"
}
```

### 安装扩展

```json
{
  "action": "install",
  "name": "example-skill",
  "source": "https://example.com/skills/example-skill.md"
}
```

### 启用/禁用扩展

```json
{
  "action": "enable",
  "name": "my-skill"
}
```

```json
{
  "action": "disable",
  "name": "my-skill"
}
```

### 移除扩展

```json
{
  "action": "remove",
  "name": "my-skill"
}
```

### 诊断扩展问题

```json
{
  "action": "doctor",
  "name": "problematic-extension"
}
```

## 信任模型

Manifest 的信任元数据记录了扩展是否可以被视为可信：

- `runtime`：已在当前进程中注册。
- `local` / `configured`：从本地配置或文件系统加载。
- `remote`：HTTPS 远程来源，需要显式确认。
- `remote_insecure`：HTTP 远程来源，需要显式确认并升级传输方式。

通过 `manage_extension` 执行远程 skill 和 MCP 的安装/更新操作时，需要显式的确认处理器，绝不能静默应用。
运行时移除仅限个人 skill 安装和已配置的 MCP
服务器；tools、agent profiles 和 command 扩展则从其
运行时注册或项目声明中移除。

## 健康与权限

Manifest 会暴露：

- 健康状态与消息；
- 扩展请求或隐含的权限；
- 来源与元数据。

AgentProfile manifest 包含 `permission_mode` 和 `allowed_tools`。Command manifest 在存在时包含 `allowed-tools` 和 `permission-profile` 元数据。

## 相关文档

- [扩展事件 Schema](../development/EXTENSION_EVENT_SCHEMA.md)
- [配置指南](../development/CONFIGURATION.md)
- [多智能体系统](MULTI_AGENT.md)
