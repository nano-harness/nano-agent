# End-to-End Testing Infrastructure

This directory contains a comprehensive end-to-end (e2e) testing system for the nano-agent project, covering all execution modes and the sub-agent (expert) system including parallel execution.

## Overview

The e2e testing system validates complete user flows across:
- **TUI modes** (tview and bubbletea)
- **Daemon mode** (server lifecycle, WebSocket streaming, session management)
- **Client mode** (client-daemon interaction)
- **Binary mode** (patch generation, trajectory output)
- **Sub-agent/Expert system** (single and parallel execution)

## Architecture

### Testing Layers

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

### Key Components

#### 1. EnhancedMockServer (`enhanced_mock_server.go`)
A sophisticated mock LLM server with:
- **Queue-based responses**: `AddResponse()` for sequential scenarios
- **Rule-based routing**: `AddRule()` with matchers for parallel scenarios
- **Failure patterns**: `SetFailurePattern()` for error testing
- **Stream simulation**: Complete SSE streaming support
- **Request recording**: Full request history for debugging

**Critical for Parallel Tests**: Use rule-based routing (`AddRule` + matchers) instead of FIFO queue when multiple agents make concurrent requests.

#### 2. Shared Infrastructure (`shared/`)

##### `config.go`
- `NewTestConfig()`: Standard isolated test configuration
- `NewTestConfigWithFork()`: Custom fork concurrency for parallel tests

##### `daemon_harness.go`
- `DaemonHarness`: In-process daemon server on random port
- `WaitReady()`: Health check before tests
- Automatic cleanup via `t.Cleanup()`

##### `expert_harness.go`
- `CountEventsByWorkerID()`: Validate parallel event distribution
- `ExtractExpertResults()`: Parse expert metadata
- `AssertParallelExecution()`: Prove true parallelism via timestamp overlap
- `AssertExpertEvent()`: Validate expert event sequences

##### `git_helper.go`
- `InitTestRepo()`: Create git repository for binary mode tests

#### 3. AgentTestSuite (`suite.go`)
Base test suite providing:
- Automatic mock server setup
- Agent initialization with test config
- Event collection (`Events` field)
- Helper methods: `RunAgent()`, `AssertToolCalled()`, etc.

## Running Tests

### All E2E Tests
```bash
make e2e          # Run all e2e tests (90-180 seconds)
make e2e-coverage # Run with coverage report
```

### By Category
```bash
make e2e-daemon   # Daemon mode tests only
make e2e-client   # Client mode tests only
make e2e-tui      # TUI mode tests only
make e2e-binary   # Binary mode tests only
make e2e-expert   # Sub-agent/expert tests (single + parallel)
make test-e2e     # Real PTY smoke tests with race detector
```

### Real PTY TUI E2E

The Bubble Tea TUI has black-box PTY coverage in `tui/tui_e2e_test.go`, backed by
`Netflix/go-expect` and `creack/pty`. These tests build `cmd/nano`, attach it to
a real pseudoterminal, and assert startup, prompt readiness, file picker,
`/clear`, and Ctrl+C shutdown behavior.

```bash
go test -race -tags=e2e -timeout=5m ./e2e/tui/...
EXPECT_DEBUG=1 go test -tags=e2e -run TestE2E_TUI_ ./e2e/tui/...
```

Supported CI platforms are Linux and macOS. Windows PTY behavior is outside the
current support scope for `go-expect`.

### Single Test
```bash
go test -v -tags=e2e -run TestForkBatch_TrulyParallel ./e2e/...
```

### With Verbose Output
```bash
NANO_VERBOSE=true make e2e
```

## Build Tags

All e2e tests use the `//go:build e2e` build tag. This ensures:
- `make test` runs only unit tests (fast feedback, <10s)
- `make e2e` runs only integration tests (slower, 90-180s)
- Clear separation of concerns

**Important**: Every new e2e test file must start with:
```go
//go:build e2e

package e2e
```

## Writing E2E Tests

### Basic Test Structure

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

### Parallel Sub-Agent Tests Best Practices

**⚠️ Critical Design Pattern for Parallel Tests**

When testing `ForkBatch` or any scenario where multiple agents run concurrently:

#### ❌ DON'T Use Queue-Based Responses
```go
// WRONG: Race condition - unpredictable which agent gets which response
s.MockServer.AddResponse(MockResponse{Content: "Response 1"})
s.MockServer.AddResponse(MockResponse{Content: "Response 2"})
s.MockServer.AddResponse(MockResponse{Content: "Response 3"})
```

#### ✅ DO Use Rule-Based Routing
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

#### Available Matchers

```go
// Match by user message content (useful for task routing)
MatchUserMessageContains("keyword")

// Match by system prompt (useful for expert identification)
MatchSystemPromptContains("expert-name")

// Alias for task field matching in ForkBatch tests
MatchTaskFieldContains("unique task identifier")
```

#### Validating Parallel Execution

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

#### Testing Timing-Based Parallelism

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

### Using DaemonHarness

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

## Test Coverage Goals

### Current Coverage (as of Phase 1 completion)
- ✅ Build tag isolation established
- ✅ Makefile targets configured
- ✅ Mock server with rule-based routing
- ✅ Shared infrastructure (daemon, expert, git helpers)
- ✅ CI workflow configured

### Pending Implementation
- ⏳ Daemon lifecycle tests
- ⏳ Client-daemon integration tests
- ⏳ TUI event loop tests
- ⏳ Binary mode patch generation tests
- ⏳ Expert trigger and execution tests
- ⏳ **Parallel sub-agent execution tests** (most critical)

## Troubleshooting

### Tests Hanging
- Check that `UserInfo.AutoDetectUserInfo = false` in test config
- Ensure all agents are properly shut down via `defer agent.Shutdown()`
- Verify daemon harness uses `t.Cleanup()` for automatic cleanup

### Flaky Parallel Tests
- Always use `AddRule` with matchers, never `AddResponse` for concurrent scenarios
- Use `AssertParallelExecution()` to verify true parallelism
- Add generous timeout buffers for CI environments (2-3x local execution time)

### Mock Routing Issues
- Log request bodies to see what messages are sent: `mockServer.RecordedRequests`
- Verify matcher logic matches actual message structure
- Use `MatchSystemPromptContains()` for expert routing by name

### Build Tag Not Working
- Ensure `//go:build e2e` is the **first line** of the file (before package)
- Verify with: `go list -tags='' ./e2e/...` (should show no packages)
- Verify with: `go list -tags=e2e ./e2e/...` (should show github.com/nano-harness/nano-agent/e2e)

## CI Integration

The e2e workflow runs on:
- Pull requests to main
- Pushes to main
- Manual workflow dispatch

Timeout: 15 minutes (sufficient for full suite)

Coverage reports are uploaded as artifacts and can be downloaded from the Actions tab.

## Performance Expectations

- **Unit tests** (`make test`): <10 seconds
- **Full e2e suite** (`make e2e`): 90-180 seconds
- **Daemon tests**: 30-60 seconds
- **Expert tests**: 40-80 seconds (parallel tests are slower due to concurrent execution)

## Contributing

When adding new e2e tests:

1. **Always add build tag**: `//go:build e2e` as first line
2. **Use suite pattern**: Extend `AgentTestSuite` or create focused suite
3. **Document parallel test patterns**: If testing concurrency, document matcher usage
4. **Add to appropriate Makefile target**: Update test name pattern in Makefile
5. **Validate locally first**: Run `make e2e` and `make e2e -count=10` for stability

## Out of Scope

This e2e testing system intentionally does NOT cover:
- Real LLM API compatibility (use mock server)
- True process isolation (`nano daemon start` fork behavior)
- Real terminal rendering for tview (requires PTY)
- Actual binary artifact behavior (`make build` executables)
- Complete markdown expert file loading (unit tested separately in `expert_loader_test.go`)

These scenarios require black-box testing or manual QA and are beyond the scope of in-process e2e tests.

## References

- Agent core tests: `pkg/agent/*_test.go`
- Fork system: `pkg/agent/fork.go`, `pkg/agent/fork_test.go`
- Expert system: `pkg/agent/expert*.go`, `pkg/agent/expert_test.go`
- Daemon server: `pkg/daemon/server.go`, `pkg/daemon/*_test.go`
- Session management: `pkg/agent/session.go`
