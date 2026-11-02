# 配置深度合并系统

[English](./config-deep-merge.md)

## 概述

Nano-agent 现在支持对来自多个层级的配置文件进行**深度合并（deep merge）**，允许将全局用户配置中的设置与项目专属配置组合在一起，而不是被整体替换。

## 配置层级

配置文件按以下优先级顺序加载和合并（从低到高）：

1. **默认值** - 来自 `DefaultConfig()` 的内置默认配置
2. **用户配置** - `~/.config/nano/config.yaml`（全局设置）
3. **项目配置** - `.nano.yaml`（项目专属设置）
4. **托管设置** - 企业/托管设置（如果存在）
5. **环境变量** - 环境变量覆盖

较高层级根据合并策略（见下文）覆盖较低层级。

## 合并策略

不同字段使用不同的合并策略：

### 替换策略（默认）

对于大多数标量字段和原子结构，较高层级会完全替换较低层级：
- `api_key`、`base_url`、`model`
- `timeout`、`max_file_size`
- 没有子策略的整个嵌套对象

### 追加策略

对于应当累积所有层级值的列表字段：
- `security.allow_rules` - 安全允许规则
- `security.deny_rules` - 安全拒绝规则
- `allowed_commands`、`blocked_commands`
- `allowed_env_vars`、`blocked_env_vars`
- `enabled_tools`、`disabled_tools`
- `sensitive_read_paths`、`arbitrary_exec_commands`

追加策略会自动对值进行去重。

### 按键合并策略

对于对象列表，其中各条目应按唯一键字段进行合并：
- `security.hooks`（键：`name`）- 安全 hooks
- `mcp.servers`（键：`name`）- MCP 服务器配置
- `image_generator.providers`（键：`provider`）- 图像生成 provider

具有相同键的条目会被递归合并。具有唯一键的条目会被追加。

## 示例

### 示例 1：安全 Hooks 合并

**用户配置**（`~/.config/nano/config.yaml`）：
```yaml
security:
  hooks:
    - name: "pre-commit"
      enabled: true
      command: "echo pre-commit hook"
    - name: "post-command"
      enabled: true
      command: "echo post-command hook"
  allow_rules:
    - "Bash(git *)"
```

**项目配置**（`.nano.yaml`）：
```yaml
mcp:
  enable_client: true
security:
  hooks:
    - name: "pre-commit"
      enabled: false  # Override: disable the user's pre-commit hook
    - name: "project-specific"
      enabled: true
      command: "npm run validate"
  allow_rules:
    - "Bash(npm *)"
```

**结果**：所有 hooks 均存在，并应用了项目覆盖：
- `pre-commit`：**已禁用**（被项目覆盖）
- `post-command`：**已启用**（来自用户配置，未更改）
- `project-specific`：**已启用**（项目新增）
- `allow_rules`：`["Bash(git *)", "Bash(npm *)"]`（已追加）
- `mcp.enable_client`：`true`（来自项目配置）

### 示例 2：MCP 服务器合并

**用户配置**：
```yaml
mcp:
  enable_client: true
  servers:
    - name: "filesystem"
      command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/home"]
    - name: "github"
      command: ["npx", "-y", "@modelcontextprotocol/server-github"]
```

**项目配置**：
```yaml
mcp:
  servers:
    - name: "filesystem"
      command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    - name: "project-db"
      command: ["node", "./db-mcp-server.js"]
```

**结果**：三个 MCP 服务器：
- `filesystem`：命令已更新为使用 `/tmp`（已合并）
- `github`：保持用户配置不变
- `project-db`：项目配置新增的服务器
- `enable_client`：`true`（继承自用户配置）

### 示例 3：显式清空

使用空数组显式清空某个字段：

**用户配置**：
```yaml
security:
  hooks:
    - name: "hook1"
      enabled: true
```

**项目配置**：
```yaml
security:
  hooks: []  # Explicitly clear all hooks
```

**结果**：没有任何 hooks（已被显式清空）。

## 旧版模式

为了向后兼容，你可以暂时恢复为单文件选择行为：

```bash
export NANO_CONFIG_LEGACY_SHADOW=1
nano
```

在旧版模式下：
- 如果 `.nano.yaml` 存在，则仅使用它（忽略全局配置）
- 如果 `.nano.yaml` 不存在，则使用 `~/.config/nano/config.yaml`

**注意**：旧版模式已被弃用，并将在未来的版本中移除。

## 迁移指南

### 破坏性变更

**之前（旧行为）**：
- 项目配置完全替换用户配置
- 当项目配置存在时，所有全局 hooks、MCP 服务器和规则都会丢失

**之后（新行为）**：
- 项目配置与用户配置合并
- 除非被显式覆盖或清空，否则全局 hooks、MCP 服务器和规则都会被保留

### 迁移步骤

1. **检查你的项目配置**：如果你有依赖于隐式清空全局设置的项目 `.nano.yaml` 文件，可能需要使用空数组显式清空它们：
   ```yaml
   security:
     hooks: []  # Explicitly clear if that's what you want
   ```

2. **先用旧版模式测试**：如果不确定，请使用 `NANO_CONFIG_LEGACY_SHADOW=1` 进行测试，以确保新行为适用于你的使用场景。

3. **更新文档**：更新任何描述配置行为的项目 README 文件。

### 常见迁移场景

#### 场景 1：项目原本有意不使用任何 hooks

**旧项目配置**（隐式清空了全局 hooks）：
```yaml
mcp:
  enable_client: true
```

**新行为**：现在会继承全局 hooks。

**修复方法**（如果你希望保持旧行为）：
```yaml
security:
  hooks: []  # Explicitly clear
mcp:
  enable_client: true
```

#### 场景 2：项目希望完全替换安全设置

**旧**：项目配置中的 security 块会替换所有全局安全设置。

**新**：安全字段按照各自的策略进行合并。

**修复方法**：显式清空每个你想要替换的字段：
```yaml
security:
  hooks: []
  allow_rules: []
  deny_rules: []
```

## 实现细节

### 合并算法

合并系统使用带有显式合并策略的递归算法：

1. 将所有配置层级加载到 `map[string]interface{}` 中
2. 根据路径逐字段应用合并策略
3. 对于有子策略的 map，递归合并嵌套结构
4. 对于没有子策略的 map，整体替换（默认）
5. 对于列表，应用策略：替换（默认）、追加或按键合并
6. 将最终合并后的 map 转换回 `Config` 结构体

### 策略定义

合并策略在 `pkg/config/loader.go` 中定义：

```go
policies := map[string]merger.MergePolicy{
    "security.allow_rules": {Strategy: merger.StrategyAppend},
    "security.deny_rules":  {Strategy: merger.StrategyAppend},
    "security.hooks":       {Strategy: merger.StrategyMergeByKey, KeyField: "name"},
    "mcp.servers":          {Strategy: merger.StrategyMergeByKey, KeyField: "name"},
    // ... more policies
}
```

### 添加新的合并策略

要为某个字段添加新的合并策略：

1. 编辑 `pkg/config/loader.go` 中的 `buildMergePolicies()`
2. 添加字段路径及所需的策略
3. 在 `pkg/config/loader_test.go` 中添加单元测试

示例：
```go
policies["my_new_field.items"] = merger.MergePolicy{
    Strategy: merger.StrategyMergeByKey,
    KeyField: "id",
}
```

## 测试

### 单元测试

```bash
# Test the merger package
go test ./pkg/config/merger/... -v

# Test config loading
go test ./pkg/config/... -v
```

### 集成测试

参见 `pkg/config/loader_test.go`，其中包含涵盖以下内容的全面集成测试：
- 用户 + 项目配置合并
- 仅用户配置
- 仅项目配置
- 显式空数组清空
- 追加策略去重
- 带更新的按键合并
- 旧版模式兼容性

## 故障排查

### 问题：我的全局 hooks 没有被使用

**原因**：项目配置中有空的 `hooks: []`，它显式清空了这些 hooks。

**解决方案**：从项目配置中移除空的 hooks 数组，或者在项目层级添加你想要的 hooks。

### 问题：应用的规则太多

**原因**：来自全局和项目配置的规则都被追加了。

**解决方案**：在项目配置中使用 `allow_rules: []` 显式清空全局规则，然后只添加你想要的规则。

### 问题：暂时需要旧版行为

**解决方案**：设置 `NANO_CONFIG_LEGACY_SHADOW=1` 环境变量。

## 未来工作

- 在下一个主要版本中移除 `NANO_CONFIG_LEGACY_SHADOW` 标志
- 添加 `nano config show` 命令以显示最终合并后的配置
- 添加 `nano config debug` 命令以显示每个值来自哪个层级
- 支持 `.nano.local.yaml`（被 git 忽略的本地覆盖）
