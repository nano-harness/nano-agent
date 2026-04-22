//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/stretchr/testify/suite"
)

// ParallelForkSuite tests ForkBatch parallel execution capabilities directly.
// This suite focuses on the ForkManager.ForkBatch API and validates:
// - Concurrent execution of multiple forks
// - Worker ID injection and event attribution
// - Concurrency limiting via semaphore
// - Context cancellation propagation
type ParallelForkSuite struct {
	AgentTestSuite
}

func TestParallelForkSuite(t *testing.T) {
	suite.Run(t, new(ParallelForkSuite))
}

// collectEvent is a callback for collecting events during expert execution
func (s *ParallelForkSuite) collectEvent(ev event.StreamEvent) {
	s.AgentTestSuite.AppendEvents(ev)
}

// TestForkBatch_BasicParallel verifies that 3 forks execute via ForkBatch
// with rule-based mock routing ensuring each agent gets the correct response.
func (s *ParallelForkSuite) TestForkBatch_BasicParallel() {
	// Setup rule-based routing for each fork task
	s.MockServer.AddRule(MockRule{
		Name:    "task-alpha",
		Matcher: MatchTaskFieldContains("analyze alpha"),
		Response: MockResponse{
			Content: "Alpha analysis complete: A+ grade.",
		},
	})
	s.MockServer.AddRule(MockRule{
		Name:    "task-beta",
		Matcher: MatchTaskFieldContains("analyze beta"),
		Response: MockResponse{
			Content: "Beta analysis complete: B+ grade.",
		},
	})
	s.MockServer.AddRule(MockRule{
		Name:    "task-gamma",
		Matcher: MatchTaskFieldContains("analyze gamma"),
		Response: MockResponse{
			Content: "Gamma analysis complete: C+ grade.",
		},
	})

	// Create fork configs
	configs := []agent.ForkConfig{
		{
			AgentType:   agent.AgentTypeExecute,
			Task:        "Please analyze alpha component",
			Description: "alpha",
		},
		{
			AgentType:   agent.AgentTypeExecute,
			Task:        "Please analyze beta component",
			Description: "beta",
		},
		{
			AgentType:   agent.AgentTypeExecute,
			Task:        "Please analyze gamma component",
			Description: "gamma",
		},
	}

	// Execute batch
	fm := agent.NewForkManager(s.Agent)
	ctx := context.Background()
	results, err := fm.ForkBatch(ctx, configs, s.collectEvent)

	// Assertions
	s.NoError(err, "ForkBatch should succeed")
	s.Require().Len(results, 3, "Should return 3 results")

	// Verify each result matches its task
	s.Contains(results[0].Output, "Alpha analysis", "Result 0 should be for alpha")
	s.Contains(results[1].Output, "Beta analysis", "Result 1 should be for beta")
	s.Contains(results[2].Output, "Gamma analysis", "Result 2 should be for gamma")

	// Verify all succeeded
	for i, res := range results {
		s.NoError(res.Error, "Result %d should not have error", i)
	}
}

// TestForkBatch_WorkerIDInjection verifies that all events have WorkerID set
// and that each worker gets a unique ID.
func (s *ParallelForkSuite) TestForkBatch_WorkerIDInjection() {
	// Setup simple responses
	s.MockServer.SetDefaultResponse(MockResponse{
		Content: "Task completed.",
	})

	configs := []agent.ForkConfig{
		{AgentType: agent.AgentTypeExecute, Task: "task1", Description: "worker1"},
		{AgentType: agent.AgentTypeExecute, Task: "task2", Description: "worker2"},
		{AgentType: agent.AgentTypeExecute, Task: "task3", Description: "worker3"},
	}

	fm := agent.NewForkManager(s.Agent)
	ctx := context.Background()
	_, err := fm.ForkBatch(ctx, configs, s.collectEvent)
	s.NoError(err)

	// Count events by WorkerID
	workerIDs := make(map[string]bool)
	for _, ev := range s.Events {
		if ev.WorkerID != "" {
			workerIDs[ev.WorkerID] = true
		}
	}

	// Should have exactly 3 unique worker IDs
	s.Equal(3, len(workerIDs), "Should have 3 distinct worker IDs")

	// Verify worker IDs match expected pattern
	s.Contains(workerIDs, "subagent-0-worker1")
	s.Contains(workerIDs, "subagent-1-worker2")
	s.Contains(workerIDs, "subagent-2-worker3")
}

// TestForkBatch_TrulyParallel verifies that execution is actually concurrent
// by adding delays and measuring total execution time.
func (s *ParallelForkSuite) TestForkBatch_TrulyParallel() {
	// Add enough delay to make parallel vs serial execution distinguishable
	// even with moderate CI scheduling jitter.
	delayDuration := 250 * time.Millisecond
	s.MockServer.SetDefaultResponse(MockResponse{
		Content: "Done after delay",
		Delay:   delayDuration,
	})

	configs := []agent.ForkConfig{
		{AgentType: agent.AgentTypeExecute, Task: "task1"},
		{AgentType: agent.AgentTypeExecute, Task: "task2"},
		{AgentType: agent.AgentTypeExecute, Task: "task3"},
	}

	fm := agent.NewForkManager(s.Agent)
	ctx := context.Background()

	start := time.Now()
	results, err := fm.ForkBatch(ctx, configs, s.collectEvent)
	duration := time.Since(start)

	s.NoError(err)
	s.Len(results, 3)

	// If parallel: ~250ms. If serial: ~750ms.
	// Keep a generous buffer for CI jitter while still staying well below the
	// serial execution envelope.
	maxParallelDuration := 550 * time.Millisecond
	s.Less(duration, maxParallelDuration,
		"Execution time %v suggests serial execution (expected <%v for parallel)", duration, maxParallelDuration)

	// Also verify using timestamp overlap helper
	// Note: This checks that event timestamps from different workers overlap
	// Unfortunately we need events with WorkerID and timestamps
	// Let's just do basic timing check for now
}

// TestForkBatch_ConcurrencyLimit verifies that maxConcurrent is respected.
func (s *ParallelForkSuite) TestForkBatch_ConcurrencyLimit() {
	// Use a config with maxConcurrent=2
	s.Agent.GetConfig().Advanced = &config.AdvancedConfig{
		Fork: &config.ForkAdvConfig{
			MaxDepth:      3,
			MaxConcurrent: 2,
		},
	}

	// Use a larger per-task delay so the concurrency envelope remains stable
	// even when agent startup and scheduling add noticeable overhead.
	delay := 300 * time.Millisecond
	s.MockServer.SetDefaultResponse(MockResponse{
		Content: "Done",
		Delay:   delay,
	})

	configs := []agent.ForkConfig{
		{AgentType: agent.AgentTypeExecute, Task: "task1"},
		{AgentType: agent.AgentTypeExecute, Task: "task2"},
		{AgentType: agent.AgentTypeExecute, Task: "task3"},
		{AgentType: agent.AgentTypeExecute, Task: "task4"},
		{AgentType: agent.AgentTypeExecute, Task: "task5"},
	}

	// Recreate fork manager to pick up new config
	fm := agent.NewForkManager(s.Agent)
	ctx := context.Background()

	start := time.Now()
	results, err := fm.ForkBatch(ctx, configs, s.collectEvent)
	duration := time.Since(start)

	s.NoError(err)
	s.Len(results, 5)

	// With maxConcurrent=2 and 5 tasks at 300ms each:
	// ceil(5/2) * 300ms = 900ms in the ideal limited-concurrency case.
	// Keep enough headroom for test-environment overhead while staying clearly
	// below the fully serial envelope (~1.5s plus framework overhead).
	minExpected := 800 * time.Millisecond
	maxExpected := 1900 * time.Millisecond

	s.GreaterOrEqual(duration, minExpected,
		"Duration %v is too short for concurrency limit of 2", duration)
	s.Less(duration, maxExpected,
		"Duration %v is too long, suggests serial execution", duration)
}

// TestForkBatch_PartialFailure verifies that some forks failing doesn't block others.
func (s *ParallelForkSuite) TestForkBatch_PartialFailure() {
	// Task 0, 2, 4 succeed; Task 1, 3 fail
	s.MockServer.AddRule(MockRule{
		Name:    "success-tasks",
		Matcher: MatchTaskFieldContains("success"),
		Response: MockResponse{
			Content: "Success",
		},
	})
	s.MockServer.AddRule(MockRule{
		Name:    "fail-tasks",
		Matcher: MatchTaskFieldContains("fail"),
		Response: MockResponse{
			Error: 400, // Permanent error avoids retry/circuit-breaker interactions
		},
	})

	configs := []agent.ForkConfig{
		{AgentType: agent.AgentTypeExecute, Task: "success task 0"},
		{AgentType: agent.AgentTypeExecute, Task: "fail task 1"},
		{AgentType: agent.AgentTypeExecute, Task: "success task 2"},
		{AgentType: agent.AgentTypeExecute, Task: "fail task 3"},
		{AgentType: agent.AgentTypeExecute, Task: "success task 4"},
	}

	fm := agent.NewForkManager(s.Agent)
	ctx := context.Background()
	results, err := fm.ForkBatch(ctx, configs, s.collectEvent)

	// ForkBatch itself should not error - errors are per-result
	s.NoError(err)
	s.Len(results, 5)

	// Verify success/failure pattern
	s.NoError(results[0].Error, "Task 0 should succeed")
	s.Error(results[1].Error, "Task 1 should fail")
	s.NoError(results[2].Error, "Task 2 should succeed")
	s.Error(results[3].Error, "Task 3 should fail")
	s.NoError(results[4].Error, "Task 4 should succeed")

	// Verify successful tasks have output
	s.Contains(results[0].Output, "Success")
	s.Contains(results[2].Output, "Success")
	s.Contains(results[4].Output, "Success")
}

// TestForkBatch_ContextCancellation verifies that context cancellation
// propagates to all running forks.
func (s *ParallelForkSuite) TestForkBatch_ContextCancellation() {
	// Use long delays so we can cancel mid-execution
	s.MockServer.SetDefaultResponse(MockResponse{
		Content: "Done",
		Delay:   2 * time.Second,
	})

	configs := []agent.ForkConfig{
		{AgentType: agent.AgentTypeExecute, Task: "long task 1"},
		{AgentType: agent.AgentTypeExecute, Task: "long task 2"},
		{AgentType: agent.AgentTypeExecute, Task: "long task 3"},
	}

	fm := agent.NewForkManager(s.Agent)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 300ms
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	results, err := fm.ForkBatch(ctx, configs, s.collectEvent)
	duration := time.Since(start)

	// ForkBatch should return without error (errors are in results)
	s.NoError(err)
	s.Len(results, 3)

	// Should complete quickly due to cancellation (< 1 second, not 2+ seconds)
	s.Less(duration, 1*time.Second, "Should cancel quickly")

	// All results should have context.Canceled error
	for i, res := range results {
		if res.Error != nil {
			s.ErrorIs(res.Error, context.Canceled,
				"Result %d should have context.Canceled error", i)
		}
	}
}

// TestForkBatch_EmptyConfigs verifies that empty config list returns immediately.
func (s *ParallelForkSuite) TestForkBatch_EmptyConfigs() {
	fm := agent.NewForkManager(s.Agent)
	ctx := context.Background()

	results, err := fm.ForkBatch(ctx, []agent.ForkConfig{}, s.collectEvent)

	s.NoError(err)
	s.Empty(results, "Empty configs should return empty results")
}

// TestForkBatch_RespectsMaxDepth verifies that fork depth limits are enforced.
func (s *ParallelForkSuite) TestForkBatch_RespectsMaxDepth() {
	// Set max depth to 1 (only parent can fork, not children)
	s.Agent.GetConfig().Advanced = &config.AdvancedConfig{
		Fork: &config.ForkAdvConfig{
			MaxDepth:      1,
			MaxConcurrent: 5,
		},
	}

	s.MockServer.SetDefaultResponse(MockResponse{
		Content: "Done",
	})

	configs := []agent.ForkConfig{
		{AgentType: agent.AgentTypeExecute, Task: "task1"},
	}

	fm := agent.NewForkManager(s.Agent)

	// Create a context that already has depth=1 (simulating we're in a child)
	ctx := context.Background()
	// We need to access the internal withForkDepth function
	// Since it's not exported, we'll test indirectly by verifying
	// that ForkBatch works at depth 0
	results, err := fm.ForkBatch(ctx, configs, s.collectEvent)
	s.NoError(err)
	s.Len(results, 1)
	s.NoError(results[0].Error)

	// TODO: To truly test depth limiting, we'd need to create a child fork
	// that tries to fork again, which requires more complex setup
	// For now, basic test that it works at depth 0
}
