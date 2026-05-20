# nano-agent 当前 Agent 实现逻辑

本文聚焦当前仓库里 `pkg/agent` 的主实现，说明一个请求是如何从 `Agent` 初始化一路走到 LLM 调用、工具执行、上下文压缩、会话管理以及子代理分发的。

## 1. 入口与核心对象

当前 agent 的主入口在：

- `pkg/agent/agent.go`
- `pkg/agent/turn.go`
- `pkg/agent/system_prompt.go`
- `pkg/agent/tool_scheduler.go`
- `pkg/agent/session.go`
- `pkg/agent/context_compression.go`

其中最核心的两个对象是：

1. `Agent`：负责初始化运行环境、维护 toolbox / llm client / session manager / sub-agent 配置，并作为外部请求入口。
2. `Turn`：负责一次具体的执行回合（turn），内部包含 LLM 调用循环、工具调用、终止条件判断和上下文压缩逻辑。

---

## 2. Agent 初始化流程

`pkg/agent/agent.go` 中的 `New(cfg, approvalHandler)` 会完成主 agent 的初始化。

### 2.1 初始化阶段做了什么

1. 读取当前工作目录，构造 `tools.ToolboxConfig`
2. 创建 `sandbox.Middleware`（如果配置启用）
3. 创建 `tools.Toolbox`
4. 用 `toolbox.List()` 初始化 `llm.Client`
5. 启动一个 goroutine 监听 MCP 工具更新，并同步给 `llmClient.UpdateTools(...)`
6. 创建 `memory.Manager`
7. 创建 `ToolRecoveryStrategy` 和 `ToolScheduler`
8. 创建 `SessionManager`
9. 注册主代理、配置里的静态子代理、`unified_agent` 工具
10. 如果当前不是子代理，再注册 `spawn_sub_agents` 工具
11. 如果启用了 MCP，则异步启动 MCP client

### 2.2 初始化后的关键成员

`Agent` 实例里几个最重要的字段：

- `toolbox`：统一的工具注册表
- `llmClient`：负责流式 LLM 调用
- `toolScheduler`：负责并行工具调度、审批、重试与状态事件
- `memoryManager`：内存管理入口
- `sessionManager`：多 session 会话隔离
- `subAgents`：来自配置文件的静态子代理定义
- `loopDetector`：用于回合内循环检测

---

## 3. 请求处理主路径

对外常用入口最终都会走到：

- `ProcessStream(...)`
- `ProcessStreamWithMultimodal(...)`
- `ProcessStreamWithMultimodalAndSession(...)`

真正核心的是 `ProcessStreamWithMultimodalAndSession()`。

### 3.1 Session 先行

在 `pkg/agent/agent.go` 中：

1. 先通过 `sessionManager.GetOrCreateSession(sessionID)` 获取或创建会话
2. 发送 `session_info` 事件
3. 再调用 `processStreamWithSessionInternal(...)`

这意味着 nano-agent 的多轮对话上下文不是直接挂在 `Agent` 上，而是挂在 `Session` 上。

### 3.2 processStreamWithSessionInternal 的职责

这个方法是主路由函数，主要做 5 件事：

1. 解析 slash command，并根据命令定义临时限制本轮允许使用的工具
2. 检测是否显式触发子代理，或者是否应该走子代理选择器
3. 如果命中了子代理，则走统一代理工具或兼容旧的子代理执行路径
4. 如果没有命中子代理，则继续走主 agent 的 turn 执行
5. 从当前 session 取出历史消息，构造 `TurnConfig`，再创建并执行 `Turn`

---

## 4. 子代理路由逻辑

### 4.1 触发检测

当前主逻辑里分成两层：

1. `detectTriggeredSubAgents(userInput)`
   - 解析显式触发模式，例如 `@agentName`、`使用[agentName]`、`with:agentName`
2. `shouldUseSubAgent(userInput)`
   - 调用 `subAgentSelector.SelectSubAgent(...)`
   - 用于做非显式但策略驱动的子代理选择

如果两层都没有命中，就直接用主 agent 继续执行，不额外让 LLM 先做一次编排判断。

### 4.2 命中子代理后的执行路径

如果 toolbox 里注册了 `unified_agent` 工具，则优先走：

- `processWithUnifiedTool(...)`

否则回退到旧路径：

- 单个子代理：`processWithSubAgent(...)`
- 多个子代理：`processWithMultipleSubAgents(...)`

### 4.3 多子代理模式

`processWithMultipleSubAgents(...)` 会：

1. 并发启动多个子代理
2. 给每个子代理的输出加上前缀
3. 收集所有子代理的有效输出
4. 等所有子代理完成后，再由主 agent 统一做聚合总结

因此它本质上是“并发执行 + 主代理汇总”的模式。

### 4.4 单子代理模式

`processWithSubAgent(...)` 会：

1. 根据 `agentName` 找到配置项
2. 如果配置 `UseIPC=true`，转走 `processWithSubAgentIPC(...)`
3. 否则在当前进程里克隆一份配置，创建短生命周期子 agent
4. 把 `SubAgents` 清空，避免递归继续委派
5. 根据 `AllowedTools` 过滤子代理可见工具
6. 如果内存功能启用，再补注册 memory tools
7. 重新调用 `subAgent.llmClient.UpdateTools(allowedTools)`
8. 最后让这个子 agent 自己执行 `ProcessStream(...)`

### 4.5 IPC 子代理模式

`processWithSubAgentIPC(...)` 是另一个隔离层次：

1. 启动本地临时 HTTP 子进程
2. 随机分配本地端口和 API key
3. 用健康检查等待服务 ready
4. 再通过本地 IPC client 把任务转给子代理

这个模式适合更强隔离，但成本也更高。

### 4.6 动态子代理

除了配置文件里的静态子代理，当前实现还支持运行时动态创建子代理：

- 工具入口：`pkg/agent/spawn_subagents_tool.go`
- 运行实体：`pkg/agent/dynamic_subagent.go`

主 agent 会在自身初始化时注册 `spawn_sub_agents`，但只注册给顶层主 agent，不注册给子代理。  
这样主 agent 可以在执行过程中临时创建 `DynamicSubAgent`，而子代理不会无限递归继续生成新的子代理。

`DynamicSubAgent` 的执行方式和静态 in-process 子代理类似：

1. 继承父 agent 的基础配置与 toolbox
2. 使用传入的 `systemPrompt` 定制角色
3. 清空 `SubAgents`
4. 设置 `IsSubAgent=true`
5. 创建一个短生命周期的派生 agent 执行任务

### 4.7 AgentProfile 与 team-lead teammate

PR 12 的配置化 multi-agent 入口使用项目目录下的 `.nano/agents`：

- `pkg/agentprofile` 负责发现 `.nano/agents/*.yaml|*.yml|*.json|*.md`。
- profile 字段包括 `name`、`description`、`initial_prompt`、`permission_mode`、`allowed_tools`、`model`、`kind`、`color`。
- `@agent-name <task>` 已弃用：自定义 agent profile 现在通过 `/agent-name <task>` 触发（dispatcher 会改写为 `spawn_teammate` 调用指引）。
- `spawn_teammate` 会读取同名 profile，缺省填充 `initial_prompt`、`permission_mode`、`kind` 与 `color`。
- teammate runner 会复制父配置并为子 agent 单独设置 profile 中声明的 permission mode，避免修改父 agent 权限。

示例：

```yaml
# .nano/agents/reviewer.yaml
description: Review code changes
initial_prompt: Review the requested patch and report risks.
permission_mode: acceptEdits
allowed_tools: [read_file, run_shell_command]
kind: in_process
color: "#00ff00"
```

---

## 5. Turn 执行循环

主 agent 没有走子代理时，会创建 `Turn` 并调用 `Turn.Execute(ctx)`。  
这部分是当前 agent 的核心闭环。

### 5.1 Turn 初始化

`NewTurnWithMultimodal(...)` 会把以下内容打包进 turn：

- 当前工作目录
- toolbox
- llm client
- memory manager
- tool scheduler
- 当前 session 的历史消息
- 系统提示词构建器 `SystemPromptBuilder`
- 上下文压缩策略 `CompressionStrategy`
- 完成条件 `CompletionCriteria`

### 5.2 Execute 主循环

`Turn.Execute()` 的主循环大致如下：

1. 发送 planner / executor 状态事件
2. 判断是否满足终止条件
3. 调用 `requestOpenAIAPI(ctx)`
4. 获取 LLM 的文本输出和工具调用列表
5. 若没有工具调用，检查是否违反了必须调用 `task_done` 的协议
6. 若有工具调用，则并行执行工具
7. 把工具结果追加回消息上下文
8. 检查是否完成
9. 循环进入下一轮

结束时还会尝试保存会话记忆，并关闭 turn。

---

## 6. LLM 调用前后发生了什么

`Turn.requestOpenAIAPI(ctx)` 里关键步骤很清晰：

1. `ensureSystemPrompt()`：确保 system prompt 已经插入消息列表
2. `ensureUserMessage()`：确保当前用户输入只追加一次
3. `ShouldCompress()`：如果上下文接近阈值则先压缩
4. `LLMClient.StreamCompletion(...)`：执行流式生成
5. 将 assistant 回复和 tool calls 追加回 `t.Messages`
6. 增加当前 iteration 计数

这里的一个关键点是：**system prompt、历史消息、当前用户输入、工具结果消息，最终都会统一落在 `t.Messages` 上，作为下一轮 LLM 的完整上下文。**

---

## 7. 系统提示词是怎么拼起来的

系统提示词逻辑主要在 `pkg/agent/system_prompt.go`。

`BuildEnhancedSystemPrompt(...)` 在当前实现里大致由几部分拼接而成：

1. `BuildBaseSystemPrompt()`：基础角色说明、工作目录、git/sandbox 环境信息
2. 用户环境信息：时区、操作系统、shell、editor、可用编程工具
3. 工具目录：按类别列出工具、参数 schema、required 字段和示例参数
4. 子代理说明：当前可用子代理、模型、允许工具、system prompt
5. 执行策略：工具调用规范、执行时的原则
6. 当前 goals：如果 turn 配置里带了 goals，会附加到 prompt 末尾

因此当前 nano-agent 的 prompt 不是一个固定模板，而是“基础模板 + 环境信息 + 工具清单 + 子代理能力 + 当前目标”的组合产物。

---

## 8. 工具调用与并行调度

工具调度核心在 `pkg/agent/tool_scheduler.go`。

### 8.1 Turn 里如何执行工具

在 `Turn.Execute()` 中，模型返回的 tool calls 会先被转换成 `ToolToExecute`，然后统一走：

- `executeToolCallsInParallel(...)`
- 底层实际调用 `ToolScheduler.ExecuteParallel(...)`

### 8.2 ToolScheduler 做什么

`ToolScheduler` 负责：

- 工具调用校验
- 状态流转：`validating` → `scheduled` → `executing` → `success/error/cancelled`
- 审批流（如果配置了 `approvalHandler`）
- 并发执行
- 重试与恢复策略
- 向上层发送 worker 事件

这意味着 turn 本身更像“编排者”，真正的工具生命周期管理集中在 scheduler 中。

### 8.3 工具结果如何回流到上下文

工具执行完成后，`addToolResultsToContext(...)` 会：

1. 把每个结果包装成 `role=tool` 的消息
2. 追加到 `t.Messages`
3. 记入 `t.ToolResults`
4. 记录 execution history
5. 如果工具是 `task_done` 且成功，则标记任务完成

---

## 9. 上下文压缩逻辑

上下文压缩在 `pkg/agent/context_compression.go` 与 `pkg/agent/turn.go`。

### 9.1 什么时候触发

在每次 LLM 调用前，`requestOpenAIAPI()` 都会先检查：

- `t.ShouldCompress()`

如果 token 使用量接近阈值，就执行 `CompressMessages(...)`。

### 9.2 压缩策略

`CompressionStrategy` 的核心思路是：

1. 保留 system message
2. 保留最近若干轮消息
3. 对更早的历史做摘要压缩
4. 尽量避免把 tool call / tool result 链路从中间截断

压缩完成后会产出 `CompressionInfo`，并通过事件流上报压缩前后 token 数、压缩比例和摘要内容。

---

## 10. Session 与历史消息管理

session 逻辑在 `pkg/agent/session.go`。

### 10.1 Session 的作用

每个 `Session` 保存：

- `ConversationHistory`
- 创建时间 / 最近活跃时间
- token 统计与时长
- metadata

因此多轮对话是按 session 隔离的，而不是所有请求共享一个全局上下文。

### 10.2 SessionManager 的作用

`SessionManager` 负责：

- `GetOrCreateSession(...)`
- session TTL 管理
- 定期清理过期 session
- 持久化到本地或 OSS 存储

### 10.3 历史清洗

在进入 turn 前，还会做一次消息序列清理，避免历史中残留不完整的 tool-call/tool-result 序列，破坏后续模型调用顺序。

---

## 11. 配置如何影响 agent

配置加载入口在 `pkg/config/config.go` 的 `LoadConfig(...)`。

对 agent 影响最大的配置包括：

- `APIKey` / `BaseURL` / `Model`
- `SubAgents`
- `EnableMCP` / `MCP`
- `AllowedCommands` / `BlockedCommands`
- `AllowedEnvVars` / `BlockedEnvVars`
- `ToolRecovery`
- `Sandbox`
- `Memory`
- `Turn`
- `ContextConfig`

其中几个特别关键：

1. `SubAgents`：决定是否注册静态子代理和 `unified_agent`
2. `IsSubAgent`：防止给子代理注册仅主 agent 可用的工具，例如 `spawn_sub_agents`
3. `ToolRecovery`：控制工具失败后的默认重试与 per-tool 策略
4. `ContextConfig`：决定上下文压缩阈值、保留比例和最近保留轮数

### 11.1 沙箱设计补充

当前 sandbox 实现提供 Linux `bwrap`、macOS `sandbox-exec` 以及 `PathChecker` 路径级访问控制。后续沙箱重构应从“Shell 命令包装器”升级为统一 Sandbox Runtime，并把 Docker 作为优先级更高、隔离性更强的执行后端。完整设计见 [沙箱设计方案](./SANDBOX_DESIGN.md)。

---

## 12. 当前实现可以概括成什么模式

如果用一句话概括，当前 nano-agent 的实现逻辑是：

> **一个以 `Agent` 为外层编排器、以 `Turn` 为单轮执行核心、以 `ToolScheduler` 为工具执行中枢、以 `SessionManager` 为多轮上下文隔离层、并通过静态/动态子代理扩展能力边界的回合制 agent 架构。**

它的几个鲜明特点是：

- 主路径是回合制循环，而不是一次性单请求执行
- 工具调用是模型驱动、scheduler 并发执行
- system prompt 是动态拼装的
- session/history 与 turn/execution 解耦
- 子代理既支持进程内克隆，也支持 IPC 隔离
- 上下文压缩被放在每次 LLM 调用之前，属于主执行链的一部分

---

## 13. 推荐阅读顺序

如果要继续深入源码，建议按下面顺序读：

1. `pkg/agent/agent.go`
2. `pkg/agent/turn.go`
3. `pkg/agent/system_prompt.go`
4. `pkg/agent/tool_scheduler.go`
5. `pkg/agent/session.go`
6. `pkg/agent/context_compression.go`
7. `pkg/agent/unified_agent.go`
8. `pkg/agent/dynamic_subagent.go`

这样能先把主链路看清，再看子代理和高级能力。
