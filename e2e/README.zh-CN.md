# 端到端测试基础设施

[English](./README.md)

本目录包含 nano-agent 项目完整的端到端（e2e）测试系统，覆盖所有执行模式以及包含并行执行在内的子代理（expert）系统。

## 概述

该 e2e 测试系统验证以下方面的完整用户流程：
- **TUI 模式**（tview 和 bubbletea）
- **Daemon 模式**（服务器生命周期、WebSocket 流式传输、会话管理）
- **Client 模式**（client-daemon 交互）
- **Binary 模式**（补丁生成、轨迹输出）
- **子代理/Expert 系统**（单任务与并行执行）

## 架构

### 测试分层

```
┌─────────────────────────────────────────────────────────────┐
│                    E2E Test Suite                            │
├─────────────────────────────────────────────────────────────┤
│ Daemon │ Client │ TUI │ Binary │ Expert Single │ Expert ║   │
│  Tests │ Tests  │Tests│ Tests  │     Tests     │Parallel║   │
├─────────────────────────────────────────────────────────────┤
│              Shared Test Infrastructure                      │
│  • DaemonHarness  • ExpertHarness  • Config Factories       │
│  • EnhancedMockServer  • Git Helper  • Assertion Helpers    │
├─────────────────────────────────────────────────────────────┤
│                  Agent Core + Toolbox                        │
└─────────────────────────────────────────────────────────────┘
```

### 关键组件

#### 1. EnhancedMockServer（`enhanced_mock_server.go`）
一个功能完善的 mock LLM 服务器，具备：
- **基于队列的响应**：`AddResponse()` 用于顺序场景
- **基于规则的路由**：`AddRule()` 配合 matcher，用于并行场景
- **故障模式**：`SetFailurePattern()` 用于错误测试
- **流式模拟**：完整的 SSE 流式传输支持
- **请求记录**：完整的请求历史，便于调试

**并行测试的关键点**：当多个 agent 并发发起请求时，请使用基于规则的路由（`AddRule` + matcher），而不是 FIFO 队列。

#### 2. 共享基础设施（`shared/`）

##### `config.go`
- `NewTestConfig()`：标准的隔离测试配置
- `NewTestConfigWithFork()`：为并行测试自定义 fork 并发度

##### `daemon_harness.go`
- `DaemonHarness`：运行在随机端口上的进程内 daemon 服务器
- `WaitReady()`：测试前的健康检查
- 通过 `t.Cleanup()` 自动清理

##### `expert_harness.go`
- `CountEventsByWorkerID()`：验证并行事件分布
- `ExtractExpertResults()`：解析 expert 元数据
- `AssertParallelExecution()`：通过时间戳重叠证明真正的并行性
- `AssertExpertEvent()`：验证 expert 事件序列

##### `git_helper.go`
- `InitTestRepo()`：为 binary 模式测试创建 git 仓库

#### 3. AgentTestSuite（`suite.go`）
基础测试套件，提供：
- 自动 mock server 搭建
- 使用测试配置初始化 agent
- 事件收集（`Events` 字段）
- 辅助方法：`RunAgent()`、`AssertToolCalled()` 等

## 运行测试

### 全部 E2E 测试
```bash
make e2e          # Run all e2e tests (90-180 seconds)
make e2e-coverage # Run with coverage report
```

### 按类别运行
```bash
make e2e-daemon   # Daemon mode tests only
make e2e-client   # Client mode tests only
make e2e-tui      # TUI mode tests only
make e2e-binary   # Binary mode tests only
make e2e-expert   # Sub-agent/expert tests (single + parallel)
make test-e2e     # Real PTY smoke tests with race detector
```

### 真实 PTY 的 TUI E2E

Bubble Tea TUI 在 `tui/tui_e2e_test.go` 中拥有黑盒 PTY 覆盖，底层依赖
`Netflix/go-expect` 和 `creack/pty`。这些测试会构建 `cmd/nano`，将其附加到
真实的伪终端（pseudoterminal）上，并断言启动、提示符就绪、文件选择器、
`/clear` 以及 Ctrl+C 退出行为。

```bash
go test -race -tags=e2e -timeout=5m ./e2e/tui/...
EXPECT_DEBUG=1 go test -tags=e2e -run TestE2E_TUI_ ./e2e/tui/...
```

受支持的 CI 平台为 Linux 和 macOS。Windows 的 PTY 行为不在
`go-expect` 当前的支持范围内。

### 单个测试
```bash
go test -v -tags=e2e -run TestForkBatch_TrulyParallel ./e2e/...
```

### 带详细输出
```bash
NANO_VERBOSE=true make e2e
```

## 构建标签（Build Tags）

所有 e2e 测试都使用 `//go:build e2e` 构建标签。这确保了：
- `make test` 只运行单元测试（快速反馈，<10 秒）
- `make e2e` 只运行集成测试（较慢，90-180 秒）
- 关注点清晰分离

**重要**：每个新的 e2e 测试文件必须以下列内容开头：
```go
//go:build e2e

package e2e
```

## 编写 E2E 测试

### 基本测试结构

```go
//go:build e2e

package e2e

import (
    "testing"
    "github.com/stretchr/testify/suite"
)

type MyTestSuite struct {
    AgentTestSuite
}

func TestMyTestSuite(t *testing.T) {
    suite.Run(t, new(MyTestSuite))
}

func (s *MyTestSuite) TestBasicScenario() {
    // Add mock responses
    s.MockServer.AddResponse(MockResponse{
        Content: "Test response",
    })

    // Run agent
    _, err := s.RunAgent("test command")
    s.NoError(err)

    // Assert expectations
    s.AssertToolCalled("some_tool")
}
```

### 并行子代理测试最佳实践

**⚠️ 并行测试的关键设计模式**

当测试 `ForkBatch` 或任何多个 agent 并发运行的场景时：

#### ❌ 不要使用基于队列的响应
```go
// WRONG: Race condition - unpredictable which agent gets which response
s.MockServer.AddResponse(MockResponse{Content: "Response 1"})
s.MockServer.AddResponse(MockResponse{Content: "Response 2"})
s.MockServer.AddResponse(MockResponse{Content: "Response 3"})
```

#### ✅ 应该使用基于规则的路由
```go
// CORRECT: Content-based routing ensures each agent gets correct response
s.MockServer.AddRule(MockRule{
    Name: "agent-1",
    Matcher: MatchTaskFieldContains("task for agent 1"),
    Response: MockResponse{Content: "Response for agent 1"},
})
s.MockServer.AddRule(MockRule{
    Name: "agent-2",
    Matcher: MatchTaskFieldContains("task for agent 2"),
    Response: MockResponse{Content: "Response for agent 2"},
})
```

#### 可用的 Matcher

```go
// Match by user message content (useful for task routing)
MatchUserMessageContains("keyword")

// Match by system prompt (useful for expert identification)
MatchSystemPromptContains("expert-name")

// Alias for task field matching in ForkBatch tests
MatchTaskFieldContains("unique task identifier")
```

#### 验证并行执行

```go
// 1. Verify worker ID distribution
counts := CountEventsByWorkerID(s.Events, event.EventTypeToolUse)
s.Equal(3, len(counts)) // 3 distinct workers

// 2. Verify event attribution
for workerID, count := range counts {
    s.Greater(count, 0) // Each worker made progress
}

// 3. Verify true parallelism (time overlap)
AssertParallelExecution(s.T(), s.Events, 3)
```

#### 测试基于时间的并行性

```go
// Add delays to prove concurrent execution
s.MockServer.AddRule(MockRule{
    Matcher: MatchTaskFieldContains("slow task"),
    Response: MockResponse{
        Content: "Done",
        Delay: 200 * time.Millisecond, // Each agent takes 200ms
    },
})

// Measure total time
start := time.Now()
results, err := fm.ForkBatch(ctx, []ForkConfig{...}) // 3 tasks
duration := time.Since(start)

// If parallel: ~200ms. If serial: ~600ms
s.Less(duration, 400*time.Millisecond) // Allow 2x buffer for CI
```

### 使用 DaemonHarness

```go
func TestDaemonScenario(t *testing.T) {
    mockServer := NewEnhancedMockServer()
    defer mockServer.Close()

    harness := shared.NewDaemonHarness(t, mockServer)
    err := harness.WaitReady(5 * time.Second)
    require.NoError(t, err)

    // Use harness.Client to interact with daemon
    resp, err := harness.Client.Execute(ctx, "test command", sessionID, timeout, false)
    require.NoError(t, err)
}
```

## 测试覆盖目标

### 当前覆盖情况（截至第一阶段完成）
- ✅ 已建立构建标签隔离
- ✅ 已配置 Makefile 目标
- ✅ 支持基于规则路由的 mock server
- ✅ 共享基础设施（daemon、expert、git 辅助工具）
- ✅ 已配置 CI 工作流

### 待实现
- ⏳ Daemon 生命周期测试
- ⏳ Client-daemon 集成测试
- ⏳ TUI 事件循环测试
- ⏳ Binary 模式补丁生成测试
- ⏳ Expert 触发与执行测试
- ⏳ **并行子代理执行测试**（最关键）

## 故障排查

### 测试挂起
- 检查测试配置中是否设置了 `UserInfo.AutoDetectUserInfo = false`
- 确保所有 agent 都通过 `defer agent.Shutdown()` 正确关闭
- 确认 daemon harness 使用 `t.Cleanup()` 进行自动清理

### 并行测试不稳定（Flaky）
- 并发场景中始终使用带 matcher 的 `AddRule`，绝不要使用 `AddResponse`
- 使用 `AssertParallelExecution()` 验证真正的并行性
- 为 CI 环境添加宽裕的超时缓冲（本地执行时间的 2-3 倍）

### Mock 路由问题
- 记录请求体以查看实际发送的消息：`mockServer.RecordedRequests`
- 确认 matcher 逻辑与实际消息结构匹配
- 使用 `MatchSystemPromptContains()` 按名称路由 expert

### 构建标签不生效
- 确保 `//go:build e2e` 是文件的**第一行**（在 package 之前）
- 用以下命令验证：`go list -tags='' ./e2e/...`（应不显示任何包）
- 用以下命令验证：`go list -tags=e2e ./e2e/...`（应显示 github.com/nano-harness/nano-agent/e2e）

## CI 集成

e2e 工作流在以下情况下运行：
- 向 main 分支发起的 Pull Request
- 推送到 main 分支
- 手动触发工作流（workflow dispatch）

超时时间：15 分钟（足以运行完整测试套件）

覆盖率报告会作为工件（artifact）上传，可从 Actions 标签页下载。

## 性能预期

- **单元测试**（`make test`）：<10 秒
- **完整 e2e 套件**（`make e2e`）：90-180 秒
- **Daemon 测试**：30-60 秒
- **Expert 测试**：40-80 秒（并行测试因并发执行而更慢）

## 贡献指南

添加新的 e2e 测试时：

1. **始终添加构建标签**：`//go:build e2e` 作为第一行
2. **使用 suite 模式**：扩展 `AgentTestSuite` 或创建专门的 suite
3. **记录并行测试模式**：如果测试并发场景，请记录 matcher 的用法
4. **添加到相应的 Makefile 目标**：在 Makefile 中更新测试名称模式
5. **先在本地验证**：运行 `make e2e` 和 `make e2e -count=10` 以确保稳定性

## 范围之外

本 e2e 测试系统有意不覆盖以下内容：
- 真实 LLM API 兼容性（请使用 mock server）
- 真正的进程隔离（`nano daemon start` 的 fork 行为）
- tview 的真实终端渲染（需要 PTY）
- 实际二进制产物的行为（`make build` 生成的可执行文件）
- 完整的 markdown expert 文件加载（在 `expert_loader_test.go` 中单独进行单元测试）

这些场景需要黑盒测试或人工 QA，超出了进程内 e2e 测试的范围。

## 参考资料

- Agent 核心测试：`pkg/agent/*_test.go`
- Fork 系统：`pkg/agent/fork.go`、`pkg/agent/fork_test.go`
- Expert 系统：`pkg/agent/expert*.go`、`pkg/agent/expert_test.go`
- Daemon 服务器：`pkg/daemon/server.go`、`pkg/daemon/*_test.go`
- 会话管理：`pkg/agent/session.go`
