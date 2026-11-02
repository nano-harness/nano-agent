# Plan 模式

[English](./PLAN_MODE.md)

Plan 模式是一种只读执行模式，专为安全的代码分析与规划而设计，避免修改文件或系统状态的风险。

## 概述

Plan 模式将 agent 限制为只读操作，禁止任何对文件系统的修改、会改变状态的 shell 命令执行，以及其他破坏性操作。它非常适合以下场景：

- **代码分析**：在进行修改之前先探索并理解代码库
- **规划阶段**：调研并设计实现方案
- **安全探索**：在无风险的情况下研究不熟悉的代码库
- **审查场景**：分析代码变更而不对其进行修改

## 切换权限模式

nano-agent 支持四种权限模式：

- **default**：对声明了 `RequiresConfirmation() == true` 的工具要求用户确认
- **acceptEdits**：自动批准文件系统写操作，但 shell 命令仍会提示确认
- **plan**：只读模式——阻止所有写操作和会改变状态的命令
- **yolo**：跳过所有权限检查（请谨慎使用）

### 激活 Plan 模式

```bash
# 在交互模式中通过斜杠命令激活
/permission plan

# 或使用简写形式
/plan
```

### 退出 Plan 模式

```bash
# 切换到 default 模式
/permission default

# 或切换到 acceptEdits 模式以自动批准文件编辑
/permission acceptEdits

# 或直接进入 YOLO 模式（不建议用于生产环境）
/yolo
```

## Plan 模式中允许的操作

Plan 模式允许以下只读操作：

### 文件系统操作
- `read_file` - 读取文件内容
- `list_directory` - 列出目录内容
- `search_files` - 按模式搜索文件
- `file_grep` - 在文件内容中搜索
- `glob_files` - 按 glob 模式匹配文件

### 代码分析
- `codebase_search` - 在整个代码库中搜索
- `search_code` - 面向代码的搜索
- `view_code` - 查看代码片段

### Web 操作（只读）
- `web_search` - 搜索网络
- `web_fetch` - 获取网页内容

### 规划工具
- `create_plan` - 创建实施计划
- `analyze_task` - 分析任务需求

### 记忆/上下文查询
- `search_memory` - 搜索会话记忆
- `list_memories` - 列出已存储的记忆

### 只读 Shell 命令
Plan 模式允许特定的只读 shell 命令（前缀匹配）：
- `ls`、`cat`、`head`、`tail` - 查看文件
- `grep`、`find` - 搜索操作
- `git status`、`git log`、`git diff`、`git show` - Git 查看（只读）
- `pwd`、`which` - 路径信息
- `echo`、`env`、`printenv` - 环境查看
- `stat`、`file`、`wc` - 文件信息
- `sort`、`uniq` - 数据处理
- `less`、`more`、`tree` - 内容查看

注意：Plan 模式通过检查命令字符串是否以允许的某个前缀开头，
来判断该 shell 命令是否为只读。

## Plan 模式中被阻止的操作

以下操作需要确认，因此会被阻止：

### 文件系统修改
- ❌ `write_file` - 写入文件
- ❌ `edit_file` - 编辑文件
- ❌ `delete_file` - 删除文件
- ❌ `create_directory` - 创建目录
- ❌ `move_file` - 移动文件

### 会改变状态的 Shell 命令
- ❌ `npm install` - 安装软件包
- ❌ `git commit` - Git 提交
- ❌ `git push` - Git 推送
- ❌ `rm` - 删除文件（即使是删除单个文件的 `rm` 也一律被阻止）
- ❌ `mv` - 移动文件
- ❌ `cp` - 复制文件
- ❌ `chmod` - 权限变更
- ❌ 构建/编译命令
- ❌ 测试执行

### 系统修改
- ❌ 软件包管理命令
- ❌ 系统配置变更
- ❌ 网络修改

## LLM 指引

当 Plan 模式激活时，LLM 会在系统提示词（system prompt）中收到全面的指引，说明：

1. **当前模式**：agent 正在 Plan 模式下运行
2. **允许的工具**：明确列出允许的只读操作
3. **禁止的操作**：清晰列出被阻止的修改
4. **Plan 模式中的角色**：指引其进行分析、规划、调研、记录和汇报
5. **如何退出**：退出 Plan 模式的说明

这确保 LLM 理解这些限制并相应地调整其行为。

## 使用示例

```bash
# 启动 nano-agent
nano chat

# 切换到 plan 模式
/permission plan

# 现在 agent 可以安全地探索代码库
> Can you analyze the authentication flow in this codebase?

# Agent 使用只读工具进行探索：
# - read_file src/auth/*.go
# - search_code "authentication"
# - git log --oneline src/auth/

# 准备实施变更时，退出 plan 模式
/permission default

# 现在 agent 可以进行修改了
> Please update the auth middleware to add rate limiting
```

## 与 Hook 和防火墙的集成

Plan 模式与危险命令防火墙协同工作，提供纵深防御：

1. **Plan 模式**在权限层面阻止工具调用
2. **危险命令防火墙**拦截即使通过了权限检查的风险命令
3. **Hook** 可以增加额外的校验逻辑

这种分层方式确保规划阶段的最大安全性。

## 配置

Plan 模式开箱即用，无需任何配置。它完全通过运行时的斜杠命令控制。

如需以编程方式访问：

```go
import "github.com/nano-harness/nano-agent/pkg/agent/permission"

// 创建处于 Plan 模式的 manager
mgr := permission.NewManager(permission.ModePlan, nil)

// 检查某个工具是否需要确认
needsConfirm := mgr.ShouldConfirm(toolName, params, tool)

// 动态切换模式
mgr.SetMode(permission.ModeDefault)
```

## 最佳实践

1. **从 Plan 模式开始**：探索不熟悉的代码时，先从 Plan 模式开始
2. **先设计再实现**：利用 Plan 模式理解代码库并设计你的方案
3. **有意识地切换**：只有在准备好进行修改时，才刻意切换到更宽松的模式
4. **配合防火墙使用**：即使在其他模式下也保持防火墙启用，以获得额外安全性

## 故障排查

### Plan 模式中出现 "Tool requires confirmation"

如果看到此消息，说明你尝试使用的工具不在只读白名单中。常见原因：

- 试图修改文件（`write_file`、`edit_file`）
- 运行构建/测试命令
- 执行会改变状态的 shell 命令

**解决方案**：改用只读的替代方案，或在确实需要修改时退出 Plan 模式。

### 只读命令被意外阻止

如果你认为某个只读命令被错误地阻止了：

1. 检查它是否在白名单中（见上文"Plan 模式中允许的操作"）
2. 该命令可能有副作用（例如 `git fetch` 会修改本地引用）
3. 自定义 hook 可能添加了额外限制

**解决方案**：切换到 `default` 或 `acceptEdits` 模式，或将该命令加入允许列表。

## 相关文档

- [Permission Policy](../development/PERMISSION_POLICY.md)
- [Dangerous Command Firewall](./FIREWALL.md)
- [Hooks](./HOOKS.md)
- [Permission Auto-Approval](../development/PERMISSION_AUTO_APPROVAL.md)
