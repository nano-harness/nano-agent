# nano-agent

[English](./README.md)

一个用Go语言构建的轻量级AI代码助手，具有模块化工具架构和基于回合的对话流程。nano-agent支持任何兼容OpenAI API的LLM提供商，用于智能代码分析、修改和生成任务。

## ✨ 特性

- **多种运行模式**: TUI交互模式、Bubble Tea TUI 模式、Daemon后台服务模式、Client客户端模式和一次性 Binary 模式
- **专家系统**: 通过`@expert-name`语法调用专门的子代理,包括内置的`@investigator`（只读代码探索）、`@help`（CLI帮助）和`@generalist`（通用代理）。支持通过YAML前置元数据在Markdown文件中定义自定义专家
- **多代理邮箱**: 通过Mailbox抽象实现父子代理之间的异步消息传递，在基于fork的并行执行期间实现结构化通信，支持内存和文件后端
- **后台任务管理**: 支持在后台运行长时间运行的Shell命令，具有实时输出流、任务监控和优雅关闭功能
- **基于回合的架构**: 通过智能工作流选择消除简单查询的过度规划
- **动态规划系统**: 实时待办事项列表生成和自适应执行
- **模块化工具系统**: 全面的文件操作、搜索、Web功能和内存管理
- **高级推理支持**: 原生支持推理模型（如o1、DeepSeek-R1），具有可配置的推理强度、令牌限制和优雅降级机制
- **技能系统**: 从个人（`~/.nano/skills/`）和项目（`.nano/skills/`）目录加载、解析和匹配技能，支持自动调用和优先级匹配
- **OpenSpec工作流**: 通过`/opsx:`斜杠命令实现规格驱动开发——结构化提案 → 规格 → 设计 → 任务 → 实现流水线
- **模型上下文协议(MCP)支持**: 连接外部MCP服务器以扩展功能
- **健康监控和诊断**: MCP连接的实时监控和全面诊断
- **交互式配置**: MCP服务器和高级配置的引导设置向导
- **REST API和WebSocket支持**: 用于守护进程模式的HTTP API和实时流式传输
- **智能模式切换**: 自动检测守护进程和无缝模式转换
- **高级流式显示**: 带有动画状态指示器、进度跟踪和优化消息渲染的实时反馈
- **高级内存管理**: 智能对话压缩、语义搜索和版本控制
- **上下文管理**: 具有可配置策略和自动优化的智能压缩
- **文件系统沙箱**: 进程级沙箱（Linux bwrap / macOS sandbox-exec）和路径级访问控制（允许/阻止/只读/隐藏路径）
- **Cron调度**: 基于Cron表达式的定期任务管理
- **工作区与Git工具**: 集成工作区管理器、Git操作、OSS存储和工程工具
- **中间件链**: 可插拔的安全、审计、指标和弹性中间件用于工具执行
- **事件系统**: 结构化事件调度、监控和验证，支持可观测性
- **安全特性**: 命令验证、基于工作目录的自动免确认、文件大小限制、路径验证和备份支持
- **增强的TUI界面**: 默认使用`tview`构建的仪表盘，带有动画横幅；也可选择非 Alt Screen 的 Bubble Tea TUI（实验性），提供贴近 Claude Code 的样式与配色，以及 Standard Figlet 细线 ASCII 艺术横幅
- **跨平台支持**: Linux、macOS和Windows的原生构建
- **开发工具**: 具有测试、代码检查和发布自动化的综合构建系统


## 权限自动免确认

nano-agent 会对 agent 工作目录内的只读 shell 命令（`grep`、`rg`、`ls`、`find` 等）和文件编辑工具（`write_file`、`edit_file`、`delete_file`）自动免确认。路径位于工作目录之外时仍会要求确认。详见 [权限自动免确认](./docs/development/PERMISSION_AUTO_APPROVAL.md)。

## Web 客户端集成

Daemon 集成请以 [Daemon API](./docs/operations/DAEMON_API.md) 为准。交互式 CLI 渲染统一经过 EventSource 层：`nano chat` 与 `nano lead-chat` 默认使用 BubbleTea，`--ui tview` 可切换到 tview 后端。自动化脚本请使用 `nano daemon execute --json "command"`，不要解析 TUI 输出。

## 🚀 快速开始

### 前置要求

- Go 1.25或更高版本
- LLM API密钥（任何兼容OpenAI API的提供商）

### 安装

#### 方式一：一键安装脚本（推荐）

```bash
curl -sSL https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/install.sh | bash
```

脚本会自动检测系统 OS 和架构，并将 `nano` 安装到 `/usr/local/bin`。

#### 方式二：下载预编译二进制

从 OSS 直接下载对应平台的最新二进制文件：

| 平台 | 下载地址 |
|------|---------|
| Linux x86_64 | `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-linux-amd64` |
| Linux arm64 | `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-linux-arm64` |
| macOS x86_64 | `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-darwin-amd64` |
| macOS Apple Silicon | `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-darwin-arm64` |

```bash
# 示例：macOS Apple Silicon
curl -sSL https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-darwin-arm64 -o nano
chmod +x nano
sudo mv nano /usr/local/bin/
```

#### 方式三：源码编译

1. **克隆仓库**
   ```bash
   git clone https://github.com/nano-harness/nano-agent.git
   cd nano-agent
   ```

2. **安装依赖**
   ```bash
   make deps
   ```

3. **构建和运行**
   ```bash
   make build
   ./bin/nano
   ```

#### 安装后：配置环境变量

```bash
export NANO_API_KEY="your-llm-api-key"
# 或使用.env文件（推荐本地开发）
cp .env.example .env
set -a; source .env; set +a
```

### 环境变量

必需的环境变量:
```bash
export NANO_API_KEY="your-llm-api-key"
```

可选的环境变量:
- `NANO_BASE_URL`: API基础URL
- `NANO_MODEL`: 模型名称
- `NANO_VERBOSE`: 启用详细日志记录(true/false)
- `NANO_READ_FILE_MAX_LINES`: read_file工具的最大行数
- `NANO_SEARCH_MAX_RESULTS`: 搜索工具的最大结果数
- `NANO_WEB_REQUEST_TIMEOUT`: Web获取超时时间(秒)
- `NANO_WEB_SEARCH_TIMEOUT`: Web搜索超时时间(秒)
- `NANO_WEB_MAX_CONTENT_SIZE`: 最大Web内容大小(字节)
- `NANO_WEB_SEARCH_MAX_RESULTS`: 最大Web搜索结果数
- `NANO_FILE_DIFF_MAX_LINES`: 文件差异显示的最大行数
- `NANO_GIT_MAX_LOG_ENTRIES`: 最大git日志条目数
- `NANO_MEMORY_MAX_ENTRIES`: 最大持久内存条目数
- `SERPER_API_KEY`: Serper网络搜索的API密钥
- `TAVILY_API_KEY`: Tavily网络搜索的API密钥

您也可以在项目目录中创建`.env`文件来设置这些变量。
将 `.env` 中的变量导出到当前 shell（确保子进程也能继承）：
```bash
set -a; source .env; set +a
```

### 性能分析（pprof）

nano-agent 支持 Go pprof 进行性能分析。

- 启用：`NANO_ENABLE_PPROF=true`
- 端口：`NANO_PPROF_PORT=6060`（默认 6060）
- 访问（仅本机绑定）：`http://127.0.0.1:<port>/debug/pprof/`，例如：
  - `http://127.0.0.1:6060/debug/pprof/heap`
  - `http://127.0.0.1:6060/debug/pprof/profile?seconds=30`

说明：
- TUI 与二进制模式会启动本地 pprof 服务，并在退出时优雅关闭。
- 在 SWE-bench 评测中，二进制模式已默认在容器内开启 pprof；可通过 `docker exec` 进入容器查看：
  - `docker exec -it <container> sh -lc 'curl -s http://127.0.0.1:6060/debug/pprof/'`

## 🛠 使用方法

nano-agent支持多种运行模式，以适应不同的开发工作流程和部署场景。

### 运行模式

#### 1. TUI模式(交互式终端)
默认的交互模式，具有现代终端用户界面:

```bash
# 以TUI模式运行(默认)
make run
# 或
nano

# 以调试日志运行
make run-debug

# 强制TUI模式(即使守护进程正在运行)
nano --tui "your command"
```

#### 4. Bubble Tea TUI（实验性）
两种基于 Bubble Tea 和 lipgloss 的交互式界面，样式贴近 Claude Code。

**内联模式（`--tea`）**：非 Alt Screen 模式，消息自然滚动于终端历史中。

**全屏模式（`--milktea`）**：Alt Screen 全屏虚拟滚动，适合长会话。启动动画在全屏模式下跳过，仅展示静态横幅。

```bash
# 启动内联 Bubble Tea TUI
nano --tea "快速任务"

# 启动全屏 Bubble Tea TUI
nano --milktea "快速任务"
```

核心特性：
- 消息着色：assistant（鼠尾草绿）、user（柔和蓝）、error（珊瑚红）——两种模式使用同一调色板
- 流式输出，100 ms 节流刷新（`--tea` 与 `--milktea` 均已节流）
- 20 帧启动动画横幅（仅 `--tea` 播放；`--milktea` 仅展示静态尾帧）
- 工具调用自动执行，状态与结果内联展示
- 快捷键：Ctrl+P 命令面板 · Ctrl+L 新会话 · Ctrl+T 思考块 · Ctrl+Y 复制 · Shift+Tab 切换权限模式
- `?`：切换完整快捷键速查表（状态栏默认显示简短提示）
- 状态栏显示工作目录缩写及 API Base URL（如已设置）

模式差异：
- 仅 `--tea`：`@文件名:行范围` 文件引用、Ctrl+R 反向历史搜索、Ctrl+F 全屏历史浏览
- 仅 `--milktea`：PgUp/PgDn/Home/End 虚拟滚动、响应式布局、`[` 导出历史到 scrollback、终端能力检测（ASCII 降级）
- Slash 命令（`/models`、`/routines`、`/cron`、`/skills` 等）**两种模式均支持**

快捷键：
- Enter：发送输入
- `Ctrl+Z`：取消当前任务
- `Ctrl+C`：退出（空闲时）或取消（`--tea` 执行中）；`--milktea` 下直接退出
- `?`：切换快捷键速查表

#### 2. Daemon模式(后台服务)
作为后台服务运行，适用于生产环境:

```bash
# 在后台启动守护进程
nano daemon start

# 通过守护进程执行命令
nano "修复main.go中的bug"

# 检查守护进程状态
nano daemon status

# 停止守护进程
nano daemon stop
```

#### 3. Client模式(API通信)
通过命令行或API与运行中的守护进程通信:

```bash
# 明确使用客户端模式
nano client exec "添加错误处理"

# 包含执行步骤（仅当需要在一次性请求中获取步骤摘要时）
nano client exec --include-steps "添加错误处理"

# 通过REST API执行
curl -X POST http://localhost:8080/api/v1/sessions/sess_demo/execute \
  -H "Content-Type: application/json" \
  -d '{"command": "实现功能X"}'

# 通过REST API执行并返回步骤（过滤掉thinking/token_stats等噪声事件）
curl -X POST http://localhost:8080/api/v1/sessions/sess_demo/execute \
  -H "Content-Type: application/json" \
  -d '{"command": "实现功能X", "include_steps": true}'
```

### 基本命令

```bash
# 快速开发构建
make dev

# 带版本信息的构建
make build

# 以TUI模式运行(默认)
make run

# 以调试日志运行
make run-debug

# 明确以TUI模式运行
make run-tui

# 以守护进程模式运行
make run-daemon

# 完整开发设置
make dev-setup
```

### Daemon模式详细说明

```bash
# 在后台启动守护进程
nano daemon start

# 通过守护进程执行命令
nano "修复main.go中的bug"

# 明确使用客户端模式
nano client exec "添加错误处理"

# 强制TUI模式(即使守护进程正在运行)
nano --tui "实现功能"

# 检查守护进程状态
nano daemon status

# 以JSON格式检查守护进程状态
nano daemon status --json

# 查看守护进程日志
nano daemon logs

# 查看最近的守护进程日志(最后50行)
nano daemon logs --lines 50

# 重启守护进程
nano daemon restart

# 停止守护进程
nano daemon stop

# 管理守护进程配置
nano daemon config show
nano daemon config set port 9000
nano daemon config set api_key "your-secret-key"
```

### 开发命令

```bash
# 运行测试
make test

# 运行带覆盖率的测试
make test-coverage

# 格式化和检查代码
make check

# 更新依赖
make deps-update
```

### 图像生成工具

内置的 `image_generate` 工具可通过 OpenRouter、Seedream 等提供商进行图片生成或已有图片编辑。

- 参数：
  - `prompt`（必填）：描述要生成或编辑的图像。
  - `image_urls`（可选）：图像输入数组；支持 HTTP/HTTPS 与 `data:image/...` base64。编辑时传入一个或多个；为空表示纯生成模式。
  - `aspect_ratio`（可选）：可选值 `1:1`、`2:3`、`3:2`、`3:4`、`4:3`、`4:5`、`5:4`、`9:16`、`16:9`、`21:9`。
  - `provider`（可选）：`openrouter` 或 `seedream`；默认使用已配置的提供商。

- 输出：
  - 返回一个或多个图片 URL。可用 Markdown 嵌入：`![image](URL)`。

- 说明：
  - 仅使用 `image_urls` 作为图像输入；单图编辑传一个元素的数组即可。
  - URL 会上传到临时 OSS 存储，便于分享与复用。

## 👥 专家系统

nano-agent提供了一个专门的专家系统，允许您使用`@expert-name`语法将任务委托给专用代理。

> **⚠️ 重要变更**: 从v0.8.0开始，旧的`fork`工具已被移除。LLM不能再自主派生子代理。所有专家调用必须由用户使用`@expert-name`语法显式触发。

### 内置专家

nano-agent包含三个始终可用的内置专家：

- **`@investigator`** - 只读代码库探索和分析
  - 用于理解代码库、查找实现、分析架构
  - 无法修改文件——完全只读访问
  - 非常适合："在哪里处理认证？"、"此组件如何工作？"

- **`@help`** - nano-agent CLI使用助手
  - 回答关于nano-agent功能和用法的问题
  - 从nano-agent文档提供帮助
  - 非常适合："如何配置MCP服务器？"、"可用哪些斜杠命令？"

- **`@generalist`** - 具有完整工具访问权限的通用代理
  - 可以读取、写入、执行——具有完整工具集访问权限
  - 用于需要多种工具的通用任务
  - 与主代理类似，但作为专注的子任务运行

### 使用专家

通过`@expert-name`语法触发专家，后跟您的请求：

```bash
# 探索代码库（只读）
@investigator 查找所有认证代码并解释其工作原理

# 获取nano-agent使用帮助
@help 如何配置MCP服务器？

# 使用完整工具访问委托任务
@generalist 重构用户模块以使用新的auth系统
```

### 专家命令

使用斜杠命令管理和检查专家：

```bash
# 列出所有可用专家
/agents

# 显示特定专家的详细信息
/agents:show investigator
```

### 自定义专家

通过在以下位置创建带有YAML前置元数据的Markdown文件来定义您自己的专家：

- **用户级**: `~/.config/nano/agents/` - 您的个人专家
- **项目级**: `.nano/agents/` - 特定项目的专家

示例自定义专家文件 (`~/.config/nano/agents/my-coder.md`):

```markdown
---
name: coder
displayName: "精英编码员"
description: "擅长编写生产质量代码的专家编码员"
model: "claude-opus-4"
temperature: 0.7
maxTurns: 30
maxTimeMinutes: 15
allowedTools:
  - read_file
  - write_file
  - edit_file
  - execute_shell
input:
  objective:
    type: string
    description: "要实现的编码目标"
output:
  name: code
  description: "生成的代码和解释"
---

您是一位专业的编码专家。编写干净、可维护、经过充分测试的代码。
遵循最佳实践并包含适当的错误处理。
```

然后使用以下命令触发：

```bash
@coder 实现一个带有速率限制的缓存系统
```

### 专家vs主代理

**何时使用专家：**
- 您想委托特定的子任务
- 您需要专门的行为（只读、特定工具集）
- 您想要一个专注的上下文用于子任务
- 您更喜欢显式控制何时使用子代理

**何时使用主代理：**
- 您正在进行对话式工作流程
- 任务不需要特殊的工具限制
- 您想要单一、连续的上下文

### 配置

在`.nano.yaml`中配置专家系统：

```yaml
# 自动发现目录中的自定义专家
expert_directories:
  - "~/.config/nano/agents"  # 用户级专家
  - ".nano/agents"           # 项目级专家

# 专家默认设置
expert_defaults:
  maxTurns: 20
  maxTimeMinutes: 10
  temperature: 0.7
```

## 🔄 后台任务管理

nano-agent支持在后台运行长时间运行的Shell命令，具有全面的任务管理功能。

### 功能特性

- **非阻塞执行**: 超过超时限制的命令会自动在后台模式下运行
- **实时输出流**: 使用可配置的回调和16MB缓冲区限制流式传输命令输出
- **任务监控**: 列出、监控和检索后台任务的输出
- **优雅关闭**: 使用 SIGTERM → 宽限期 → SIGKILL 自动清理，确保进程正确终止
- **会话隔离**: 任务按会话隔离，每个会话最多100个任务
- **日志管理**: 每个任务100MB日志文件大小限制，防止磁盘耗尽

### 后台任务工具

- **`execute_shell`** 配合 `is_background: true`: 显式以后台模式运行命令
- **`bash_output`**: 使用增量读取和偏移跟踪检索任务输出
- **`kill_bash`**: 优雅地终止后台任务
- **`list_background_tasks`**: 列出当前会话的所有任务及其状态

### 使用示例

```bash
# 在后台运行长时间运行的命令
execute_shell "npm run build" --is-background true

# 检查后台任务输出
bash_output <task_id>

# 列出所有后台任务
list_background_tasks

# 终止后台任务
kill_bash <task_id>
```

### 自动后台模式

超过超时限制（默认120秒，最大600秒）的命令会自动转换为后台执行，返回任务ID以供监控。

## 🧠 推理支持

nano-agent为OpenAI的o1推理模型提供原生支持，通过可配置的推理参数实现增强的问题解决能力。

### 功能特性

- **可配置推理强度**: 在低、中、高推理强度级别之间选择
- **令牌管理**: 设置最大推理令牌数或使用模型默认值
- **优雅降级**: 当推理失败时自动降级到标准模型
- **统计跟踪**: 监控推理使用情况、令牌消耗和降级事件
- **灵活配置**: 按项目或全局启用/禁用推理

### 配置

在您的`.nano.yaml`配置中启用推理:

```yaml
reasoning:
  enabled: true           # 启用推理模式
  effort: "medium"        # 推理强度: low, medium, high
  max_tokens: 0          # 最大推理令牌数 (0 = 使用模型默认值)
  exclude: false         # 从响应中排除推理令牌
```

### 使用示例

```bash
# 使用推理进行复杂问题解决
nano "分析这个代码库并建议架构改进"

# 使用特定推理强度级别
nano --config reasoning.effort=high "调试这个复杂的并发问题"

# 在守护进程模式下检查推理统计
curl http://localhost:8080/api/v1/stats
```

### 推理统计

系统跟踪全面的推理使用统计:

- **推理启用**: 请求是否使用了推理
- **推理令牌**: 用于推理的令牌数量
- **推理强度**: 应用的强度级别
- **推理降级**: 是否发生了到标准模型的降级
- **推理延迟**: 推理处理所花费的时间

### 最佳实践

1. **复杂任务使用更高强度**: 对于架构决策、调试复杂问题或代码审查，设置`effort: "high"`
2. **监控令牌使用**: 跟踪推理令牌消耗以优化成本
3. **配置降级**: 确保推理模型不可用时的优雅降级
4. **项目特定设置**: 为不同类型的项目使用不同的推理配置

## 🏗 架构

### 核心组件

- **回合系统** (`pkg/agent/turn.go`): 管理带有流式事件的单次交互周期
- **代理** (`pkg/agent/agent.go`): 回合执行和上下文管理的主要协调器
- **工具调度器** (`pkg/agent/tool_scheduler.go`): 智能工具选择和执行调度
- **上下文压缩** (`pkg/agent/context_compression.go`): 自动对话历史压缩和优化
- **LLM客户端** (`pkg/llm/`): 支持函数调用和错误处理的流式对话
- **工具系统** (`pkg/tools/`): 用于各种操作的模块化工具架构
- **守护进程服务器** (`pkg/daemon/`): 带有REST API和WebSocket支持的后台服务模式

### 工具模块

- **文件系统** (`pkg/tools/filesystem/`): 文件操作(读取、写入、编辑、删除和代码骨架)
- **系统** (`pkg/tools/system/`): 带有安全控制和验证的Shell命令执行，并通过 CLI 命令完成文件查找/搜索
- **Web** (`pkg/tools/web/`): Web获取、搜索功能和API集成
- **内存** (`pkg/memory/`): 上下文管理、存储和语义搜索
- **MCP工具** (`pkg/tools/mcp/`): 模型上下文协议工具集成和管理

### 运行模式

1. **TUI模式**(交互式): 增强的实时终端界面，具有改进的消息显示、流式动画、用户友好的确认对话框和文本选择/复制功能(Ctrl+A全选，Ctrl+C复制)，用于开发和调试
2. **Daemon模式**(服务): 带有REST API的后台服务，用于生产环境
3. **Client模式**: 通过命令行或API与运行中的守护进程通信

### 工作流类型

1. **复杂任务**: 智能工具调度 → 上下文感知执行 → 自适应错误恢复
2. **简单查询**: 带有自动模式检测和优化的直接对话响应
3. **交互式会话**: 流式响应和实时工具执行反馈

## 🧪 测试

运行测试套件:

```bash
# 运行所有测试
make test

# 运行带覆盖率的测试
make test-coverage

# 运行HTML覆盖率报告的测试
make test-coverage-html

# 运行竞态条件测试
make test-race

# 运行基准测试
make benchmark
```

测试文件位于`test/testdata/`，包含各种编程语言的示例。

### SWE-bench评估

nano-agent已在SWE-bench基准测试上进行了评估，该基准测试评估解决真实世界GitHub问题的能力。SWE-bench是一个综合基准，使用实际的GitHub问题及其相应的修复来评估语言模型在软件工程任务上的表现。

#### 最新测试结果

在31个实例的综合测试集上，nano-agent取得了以下结果:

```json
{
    "total_instances": 31,
    "submitted_instances": 31,
    "completed_instances": 31,
    "resolved_instances": 19,
    "unresolved_instances": 12,
    "empty_patch_instances": 0,
    "error_instances": 0,
    "success_rate": "61.3%"
}
```

**性能摘要:**
- ✅ **成功率**: 61.3% (19/31个问题已解决)
- ✅ **完成率**: 100% (31/31个实例已完成)
- ✅ **零错误**: 没有失败的执行或空补丁

**已解决的问题 (共19个):**
- `django__django-10880` - Django框架问题
- `django__django-10914` - Django框架问题
- `django__django-11133` - Django框架问题
- `matplotlib__matplotlib-13989` - 绘图库问题
- `matplotlib__matplotlib-14623` - 绘图库问题
- `matplotlib__matplotlib-23314` - 绘图库问题
- `matplotlib__matplotlib-24149` - 绘图库问题
- `matplotlib__matplotlib-25311` - 绘图库问题
- `pydata__xarray-2905` - 数据分析库问题
- `pydata__xarray-3095` - 数据分析库问题
- `pytest-dev__pytest-5262` - 测试框架问题
- `pytest-dev__pytest-5631` - 测试框架问题
- `scikit-learn__scikit-learn-10297` - 机器学习库问题
- `scikit-learn__scikit-learn-10844` - 机器学习库问题
- `scikit-learn__scikit-learn-10908` - 机器学习库问题
- `sympy__sympy-11618` - 符号数学库问题
- `sympy__sympy-12096` - 符号数学库问题
- `sympy__sympy-12419` - 符号数学库问题
- `sympy__sympy-20590` - 符号数学库问题

**未解决问题 (共12个):**
- `astropy__astropy-12907` - 天文学库问题
- `astropy__astropy-13033` - 天文学库问题
- `astropy__astropy-13236` - 天文学库问题
- `astropy__astropy-14365` - 天文学库问题
- `astropy__astropy-14995` - 天文学库问题
- `django__django-10097` - Django框架问题
- `django__django-10554` - Django框架问题
- `django__django-11179` - Django框架问题
- `matplotlib__matplotlib-20488` - 绘图库问题
- `psf__requests-2317` - HTTP库问题
- `sphinx-doc__sphinx-10323` - 文档工具问题
- `sphinx-doc__sphinx-10435` - 文档工具问题

#### 运行SWE-bench测试

要在nano-agent上运行SWE-bench评估:

```bash
# 导航到SWE-bench测试目录
cd swe_bench_test

# 安装依赖
pip install -r requirements.txt

# 运行评估
python run_swe_bench.py
```

有关SWE-bench的更多信息，请访问[官方仓库](https://github.com/princeton-nlp/SWE-bench)。

## ⚙️ 配置

nano-agent支持灵活的多级配置系统，具有清晰的优先级排序。

### 配置优先级

设置按以下优先级顺序加载(从高到低):

1. **当前目录配置** (`.nano.yaml`) - **最高优先级**
2. **全局配置** (`~/.config/nano/config.yaml`)
3. **项目配置** (`.nano.yaml`)
4. **环境变量** - 最低优先级

### 快速设置

```bash
# 设置必需的API密钥
export NANO_API_KEY="your-llm-api-key"

# 复制示例配置
cp .nano.yaml.example .nano.yaml

# 查看配置加载顺序
nano config paths
```

### MCP配置

模型上下文协议（MCP）使 nano-agent 能够连接到外部服务器以扩展功能。

#### 支持的传输类型

- **`stdio`**: 标准输入/输出进程通信（本地服务器的默认方式）
- **`streamable`**: 使用服务器发送事件（SSE）的 HTTP 传输，用于远程服务器
- **`inmemory`**: 内存传输，用于测试

**注意**: 旧的传输类型（`http`、`sse`、`websocket`）不再受支持。请使用 `streamable` 用于基于 HTTP 的服务器。

#### 快速入门

```bash
# 交互式配置向导
nano mcp wizard

# 非交互式添加服务器
nano mcp add filesystem npx -y @modelcontextprotocol/server-filesystem /tmp

# 添加远程 HTTP 服务器
nano mcp add myserver --transport streamable https://api.example.com/mcp

# 列出已配置的服务器
nano mcp list

# 测试服务器连接
nano mcp test filesystem
```

#### 配置文件

创建或编辑 `~/.nano/config.yaml`:

```yaml
enable_mcp: true
mcp:
  enable_client: true
  default_transport: "stdio"
  timeout: 30s
  max_retries: 3
  servers:
    # 本地 stdio 服务器
    - name: "filesystem"
      description: "文件系统操作"
      transport: "stdio"
      command: ["npx", "@modelcontextprotocol/server-filesystem", "./workspace"]
      enabled: true

    # 带 OAuth 的远程 HTTP 服务器
    - name: "api-server"
      description: "远程 API 服务器"
      transport: "streamable"
      url: "https://api.example.com/mcp"
      enabled: true
      oauth:
        authorization_url: "https://auth.example.com/oauth/authorize"
        token_url: "https://auth.example.com/oauth/token"
        client_id: "your-client-id"
        scopes: "read write"
```

#### OAuth 2.0 身份验证

对于需要 OAuth 身份验证的服务器：

```bash
# 使用 OAuth 授权（打开浏览器）
nano mcp auth api-server

# 查看已存储的令牌
nano mcp auth --list

# 撤销令牌
nano mcp auth api-server --revoke
```

令牌会自动注入到请求中，并在需要时自动刷新。详细的 OAuth 配置请参见 [docs/MCP_OAUTH.md](docs/MCP_OAUTH.md)。

#### 从旧传输类型迁移

如果您的服务器配置使用了 `transport: http`、`transport: sse` 或 `transport: websocket`，请更新为使用 `transport: streamable`：

```yaml
# 之前（不支持）
transport: http
url: https://api.example.com/mcp

# 之后（正确）
transport: streamable
url: https://api.example.com/mcp
```

## 🔄 Daemon模式

nano-agent支持作为后台守护进程服务运行，适用于生产环境和集成场景。

### 快速Daemon设置

```bash
# 启动守护进程
nano daemon start

# 检查状态
nano daemon status

# 通过守护进程执行命令
nano "实现用户认证"

# 停止守护进程
nano daemon stop
```

### Daemon功能

- **REST API**: 用于命令执行和状态监控的HTTP端点
- **WebSocket支持**: 实时流式通信
- **进程管理**: 自动PID文件处理和优雅关闭
- **安全性**: API密钥认证和TLS支持
- **配置**: 基于JSON的配置和CLI管理
- **日志记录**: 可配置的日志文件和轮转支持

### API端点

- `GET /api/v1/health` - 健康检查
- `GET /api/v1/status` - 获取代理状态
- `WS /api/v1/stream` - WebSocket流式传输
- `GET /api/v1/mcp/*` - MCP相关端点
- `GET|POST /api/v1/memory/*` - 内存管理
- `POST /api/v1/sessions/{id}/execute` - 在指定会话内执行
- `GET|POST|DELETE /api/v1/sessions/*` - 会话管理

## 🌟 模型上下文协议(MCP)

nano-agent包含全面的MCP支持，用于连接外部服务器和扩展功能:

### 快速MCP设置

1. **使用配置工具:**
   ```bash
   nano mcp config
   ```

2. **检查MCP状态:**
   ```bash
   nano mcp status
   nano mcp diagnostics
   ```

3. **列出可用工具:**
   ```bash
   nano mcp list-tools
   ```

### MCP功能

- **健康监控**: 带有自动恢复的实时服务器健康检查
- **全面诊断**: 性能、错误和系统状态的详细报告
- **交互式配置**: 服务器设置的分步向导
- **多种传输类型**: 支持 stdio、streamable 和内存传输
- **预定义服务器**: 流行MCP服务器的快速设置(文件系统、git、网络搜索等)
- **自定义服务器支持**: 自定义MCP服务器的简易集成

## 🤝 贡献

1. Fork仓库
2. 创建功能分支 (`git checkout -b feature/amazing-feature`)
3. 进行更改
4. 运行测试和质量检查 (`make check`)
5. 提交更改 (`git commit -m 'Add amazing feature'`)
6. 推送到分支 (`git push origin feature/amazing-feature`)
7. 打开Pull Request

### 开发指南

- 遵循Go约定和最佳实践
- 为新功能编写测试
- 使用现有的代码风格和模式
- 根据需要更新文档
- 确保所有质量检查通过 (`make check`)

### 研发流程规范

- 分支：`main` 为主干；功能开发用 `feature/<topic>`，修复用 `fix/<topic>`，紧急修复用 `hotfix/<topic>`。
- 提交：建议使用 Conventional Commits（如 `feat: ...` / `fix: ...`），一个提交只做一类变更。
- PR：描述背景/变更点/影响面/测试方式；尽量注明影响的模式（CLI/daemon/TUI）；至少 1 人 review 后再合并。
- 质量门禁：提交前本地跑 `go fmt ./...`、`go test ./...`，并确保 `make check` 通过。
- 密钥管理：API key 与密码放在本地 `.env`（不要提交到仓库）。

## 📄 许可证

本项目是开源的。请查看许可证文件了解详情。

## 🙏 致谢

- 使用[DeepSeek AI](https://www.deepseek.com/)构建智能代码助手
- 使用[Cobra](https://github.com/spf13/cobra)作为CLI框架
- 终端UI由[tview](https://github.com/rivo/tview)提供支持
- 通过[go-sdk](https://github.com/modelcontextprotocol/go-sdk)提供模型上下文协议客户端支持，用于连接外部MCP服务器

## 📞 支持

- 报告问题: [GitHub Issues](https://github.com/nano-harness/nano-agent/issues)
- 文档: 查看README文件获取全面的使用指导
- 配置: 查看`.nano.yaml.example`获取配置示例和环境变量文档

---

用❤️为想要智能、轻量级代码助手的开发者构建。
