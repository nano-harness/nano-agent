# 危险命令防火墙

[English](./FIREWALL.md)

危险命令防火墙（Dangerous Command Firewall）是一个内置安全特性，它会在 shell 命令执行前检测并拦截潜在的危险命令，在权限模式之外提供额外的一层保护。

## 概述

防火墙作为一个执行前（pre-execution）hook 运行，针对一整套危险模式对 shell 命令进行分析。当检测到高风险命令时，防火墙可以：

- **Confirm（确认）**：请求用户显式批准（默认）
- **Block（阻止）**：完全拒绝执行
- **Allow（放行）**：记录警告但允许执行（用于测试/开发）

防火墙通过捕获那些即使通过了其他权限检查的危险命令，提供**纵深防御（defense-in-depth）**安全能力。

## 自动启用

防火墙在 agent 启动时自动注册为内置的编程式 `pre_tool_use` hook，无需任何 shell hook 配置。它默认启用，可通过以下方式禁用：

```yaml
firewall:
  enabled: false
```

## 内置危险模式

防火墙包含三个严重级别（severity）的模式：

### 高严重级（破坏性命令）

这些命令可能对系统造成不可逆的损害：

- **`rm -rf /`** - 递归删除根目录
- **`rm -rf ~`** - 递归删除用户主目录
- **`:(){ :|:& };:`** - fork 炸弹尝试
- **`dd if=/dev/zero of=/dev/sda`** - 直接磁盘写入操作
- **`mkfs.*`** - 文件系统格式化
- **`chmod -R 777`** - 设置全局可写权限
- **`chown -R ... /`** - 对根目录递归变更所有权

### 中严重级（VCS 与系统操作）

这些命令可能导致数据丢失或难以恢复：

- **`git push --force`** - 强制推送到远程仓库
- **`git push ... main|master`** - 推送到受保护分支
- **`git reset --hard`** - 硬重置到之前的提交
- **`git clean -fd`** - 强制清理未跟踪文件
- **`sudo rm|mv|cp|dd|mkfs|chmod|chown`** - 特权文件操作
- **`> /dev/sd*`** - 输出重定向到块设备

### 低严重级（包管理）

这些命令可能影响系统软件包：

- **`apt|yum|dnf remove`** - 软件包移除
- **`npm uninstall -g`** - 全局软件包移除

## 配置

### YAML 配置

在你的 `config.yaml` 中配置防火墙：

```yaml
# Dangerous Command Firewall Configuration
firewall:
  enabled: true                    # Enable/disable the firewall
  severity_threshold: medium       # Only flag commands at or above this severity
  failure_policy: confirm          # What to do when dangerous command detected: confirm, block, or allow

  # Optional: Add custom dangerous patterns
  custom_patterns:
    - pattern: "dropdb.*"          # Regex pattern to match
      severity: high               # Severity level: high, medium, or low
      category: database           # Category for grouping (e.g., database, security, vcs)
      reason: "Database deletion"  # Human-readable explanation

    - pattern: "kubectl delete.*production"
      severity: high
      category: kubernetes
      reason: "Production k8s deletion"

  # Optional: Override/whitelist specific commands
  overrides:
    - "rm -rf /tmp/test"           # Exact command strings to always allow
    - "git push --force origin my-feature-branch"
```

### 配置选项

#### `enabled`（布尔值）
- **默认值**：`true`
- **说明**：防火墙的总开关。为 `false` 时，所有命令都会跳过防火墙检查直接放行。

#### `severity_threshold`（字符串）
- **默认值**：`medium`
- **可选值**：`low`、`medium`、`high`
- **说明**：只有达到或超过该严重级别的命令才会触发防火墙动作。
  - `low`：捕获所有命令，包括包管理操作
  - `medium`：捕获破坏性操作和 VCS 操作（推荐）
  - `high`：只捕获高度破坏性操作

#### `failure_policy`（字符串）
- **默认值**：`confirm`
- **可选值**：`confirm`、`block`、`allow`
- **说明**：检测到危险命令时采取的动作：
  - `confirm`：请求用户批准（开发环境推荐）
  - `block`：完全拒绝执行（CI/生产环境推荐）
  - `allow`：记录警告但允许执行（仅用于测试）

#### `custom_patterns`（数组）
- **默认值**：`[]`（空，仅使用内置模式）
- **说明**：添加组织特定的危险模式。
- **字段**：
  - `pattern`：Go 正则表达式（将使用 `regexp.MustCompile` 编译）
  - `severity`：`high`、`medium` 或 `low`
  - `category`：用于分组的任意类别字符串
  - `reason`：展示给用户的可读说明

#### `overrides`（数组）
- **默认值**：`[]`（空）
- **说明**：绕过防火墙检查的精确命令字符串。
- **使用场景**：当某个特定的危险命令在你的上下文中确实安全时。
- **警告**：请谨慎使用——overrides 会削弱防火墙的意义。

## 默认配置

如果未提供防火墙配置，nano-agent 使用以下默认值：

```yaml
firewall:
  enabled: true
  severity_threshold: medium
  failure_policy: confirm
  custom_patterns: []
  overrides: []
```

## 使用示例

### 示例 1：默认配置（开发环境推荐）

```yaml
firewall:
  enabled: true
  severity_threshold: medium
  failure_policy: confirm
```

**行为**：
- 安全命令立即执行：`ls`、`git status`、`cat file.txt`
- 危险命令提示批准：`rm -rf /tmp/data`、`git push --force`
- 低严重级命令直接通过：`apt install`（低于 medium 阈值）

### 示例 2：严格的生产环境配置

```yaml
firewall:
  enabled: true
  severity_threshold: low
  failure_policy: block
```

**行为**：
- 所有危险命令都会被阻止，包括包管理操作
- 没有用户提示——直接拒绝
- 适用于危险命令绝不应运行的自动化环境

### 示例 3：带自定义模式的开发配置

```yaml
firewall:
  enabled: true
  severity_threshold: medium
  failure_policy: confirm

  custom_patterns:
    # Protect production databases
    - pattern: "psql.*DROP DATABASE"
      severity: high
      category: database
      reason: "Dropping database"

    # Catch dangerous Terraform commands
    - pattern: "terraform destroy"
      severity: high
      category: infrastructure
      reason: "Infrastructure destruction"

    # Warn about potential secrets
    - pattern: "export.*PASSWORD="
      severity: medium
      category: security
      reason: "Exporting password in plaintext"

  # Allow specific safe variants
  overrides:
    - "terraform destroy -target=module.test"
```

### 示例 4：禁用防火墙（不推荐）

```yaml
firewall:
  enabled: false
```

**行为**：
- 不进行任何命令检查
- 所有命令仅依据权限模式执行
- **警告**：只有在你已有其他安全控制措施时才可禁用

## 工作原理

### 执行流程

1. **工具调用**：Agent 请求执行 shell 命令
2. **Hook 触发**：内置的编程式防火墙 hook 在 `pre_tool_use` 事件时触发
3. **命令提取**：从工具参数中提取命令字符串
4. **Override 检查**：检查命令是否在 override 白名单中
5. **模式匹配**：将命令与内置模式 + 自定义模式进行匹配
6. **严重级检查**：验证匹配到的模式是否达到严重级阈值
7. **策略应用**：应用配置的 failure policy
8. **用户通知**：向用户展示警告和原因
9. **决策**：根据策略决定放行、确认或阻止

### 与权限模式的集成

防火墙可与所有权限模式协同工作：

| 权限模式 | 防火墙启用 | 结果 |
|----------------|------------------|---------|
| `default` | 是 | 安全命令放行，危险命令被防火墙捕获 |
| `acceptEdits` | 是 | 文件编辑放行，危险 shell 命令被防火墙捕获 |
| `plan` | 是 | Plan 模式阻止大多数命令，防火墙提供额外保护 |
| `yolo` | 是 | YOLO 绕过权限检查，但防火墙仍会运行 |

**纵深防御**：即使权限模式较为宽松，防火墙也提供了第二道安全网。

### 编程式 Hook 集成

防火墙使用中间件的 `ProgrammaticHook` 接口，因此它与用户自定义的 shell/HTTP hook 运行在同一个 hook 引擎中，无需调用外部进程。外部 hook 先运行；如果它们允许执行，内置防火墙随后会对 shell 命令求值，并根据 `firewall.failure_policy` 返回 `allow`、`confirm` 或 `block`。

## 警告消息

当检测到危险命令时，用户会看到详细的警告：

```
⚠️  Dangerous command detected
Command: rm -rf /tmp/important
Reason: recursive force deletion (rm -rf)
Severity: high
Category: destructive

[Confirm] [Reject]
```

## 编程访问

### 在代码中使用防火墙

```go
import (
    "github.com/nano-harness/nano-agent/pkg/agent/permission"
    "github.com/nano-harness/nano-agent/pkg/hookservice"
)

// Create firewall configuration
config := permission.FirewallConfig{
    Enabled:           true,
    SeverityThreshold: permission.SeverityMedium,
    FailurePolicy:     "confirm",
    CustomPatterns:    []permission.DangerousCommandRule{
        {
            Pattern:  regexp.MustCompile(`dropdb`),
            Severity: permission.SeverityHigh,
            Category: "database",
            Reason:   "Database deletion",
        },
    },
}

// Create firewall hook
firewallHook := permission.NewFirewallHook(config)

// Execute hook on command
params := map[string]interface{}{
    "command": "rm -rf /tmp/test",
}
decision, err := firewallHook.Execute(
    ctx,
    hookservice.EventPreToolUse,
    "run_shell_command",
    params,
)

// Check decision
switch decision.Action {
case hookservice.ActionAllow:
    // Execute command
case hookservice.ActionConfirm:
    // Ask user for approval
case hookservice.ActionBlock:
    // Refuse execution
}
```

### 检查单个命令

```go
import "github.com/nano-harness/nano-agent/pkg/agent/permission"

// Check if a command is dangerous
command := "git push --force origin main"
rule, isDangerous := permission.CheckCommand(command)

if isDangerous {
    fmt.Printf("Dangerous: %s\n", rule.Reason)
    fmt.Printf("Severity: %s\n", rule.Severity)
    fmt.Printf("Category: %s\n", rule.Category)
}
```

### 在运行时添加自定义规则

```go
// Create firewall with initial config
config := permission.DefaultFirewallConfig()

// Add custom patterns
config.CustomPatterns = append(config.CustomPatterns,
    permission.DangerousCommandRule{
        Pattern:  regexp.MustCompile(`heroku.*destroy`),
        Severity: permission.SeverityHigh,
        Category: "cloud",
        Reason:   "Heroku app destruction",
    },
)

hook := permission.NewFirewallHook(config)
```

## 敏感文件检测

除了命令检测之外，防火墙还提供敏感文件模式匹配：

```go
// Check if a file path is sensitive
isSensitive := permission.IsSensitiveFile(".env")
// Returns: true

isSensitive = permission.IsSensitiveFile("README.md")
// Returns: false
```

### 内置敏感模式

- `*.env`、`.env*` - 环境变量文件
- `*.pem`、`*.key`、`*.p12`、`*.pfx` - 私钥
- `*credentials*`、`*secrets*`、`*password*` - 凭据文件
- `.kube/config`、`.aws/credentials` - 云配置
- `.ssh/*`、`.gnupg/*` - SSH 和 GPG 密钥
- `id_rsa*`、`id_ed25519*` - SSH 密钥
- `*.crt`、`*.cer` - 证书

## 测试

### 单元测试

测试单个危险模式：

```bash
go test ./pkg/agent/permission -run TestCheckCommand
```

### 集成测试

测试完整的防火墙流程：

```bash
go test -tags=integration ./pkg/agent/permission -run TestM1
```

## 最佳实践

1. **保持防火墙启用**：即使权限模式较宽松，也始终启用防火墙
2. **使用 Confirm 策略**：开发环境中，`confirm` 在安全性和易用性之间提供了最佳平衡
3. **阈值设为 Medium**：从 `medium` 阈值开始，再根据需求调整
4. **最小化 Overrides**：只为经过仔细审查的命令添加 override
5. **自定义模式**：为组织特定的危险操作添加模式
6. **认真审阅警告**：不要盲目批准危险命令——先阅读警告内容
7. **审计日志**：启用审计日志，跟踪哪些危险命令被批准过

## 故障排查

### 防火墙阻止了安全命令

**症状**：一个安全的命令被标记为危险。

**解决方案**：
1. 检查它是否无意中匹配了某个模式（例如 `rm README.txt` 匹配了 `rm` 模式）
2. 如果该特定命令在你的上下文中确实安全，将其加入 `overrides`
3. 如果模式过于激进，调低 `severity_threshold`
4. 如果某个内置模式不正确，联系维护者

### 防火墙没有捕获危险命令

**症状**：危险命令未经警告就执行了。

**解决方案**：
1. 确认配置中 `firewall.enabled: true`
2. 检查 `severity_threshold` 是否设置得过高
3. 审查命令模式——它可能不匹配任何内置规则
4. 为该特定命令类型添加自定义模式
5. 确认内置的编程式防火墙 hook 未在配置中被禁用

### 自定义模式不生效

**症状**：自定义模式无法匹配命令。

**解决方案**：
1. 验证正则表达式语法——在适当位置使用单词边界 `\b`
2. 用 Go 的 `regexp` 包语法测试正则表达式
3. 检查 `pattern` 是否为合法的正则表达式（例如用 `\bdropdb\b` 而不是 `dropdb*`）
4. 确认配置文件已正确加载（`nano config show`）

## 相关文档

- [Permission Policy](../development/PERMISSION_POLICY.md)
- [Plan Mode](./PLAN_MODE.md)
- [Hooks](./HOOKS.md)
- [Permission Auto-Approval](../development/PERMISSION_AUTO_APPROVAL.md)
