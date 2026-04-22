//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/stretchr/testify/suite"
)

// ParallelViaToolsSuite tests parallel sub-agent execution triggered via tool calls.
// This suite verifies that when a parent agent returns multiple task tool calls
// in a single turn, the tool scheduler dispatches them in parallel.
//
// Key differences from ParallelForkSuite:
// - Uses LLM tool calls (task tool) rather than direct ForkBatch API
// - Tests end-to-end integration with tool scheduler
// - Validates event sequences for tool-triggered parallel execution
type ParallelViaToolsSuite struct {
	AgentTestSuite
}

func TestParallelViaToolsSuite(t *testing.T) {
	suite.Run(t, new(ParallelViaToolsSuite))
}

// FilterEventsByType returns all events matching the given type
func (s *ParallelViaToolsSuite) FilterEventsByType(eventType event.EventType) []event.StreamEvent {
	var filtered []event.StreamEvent
	for _, ev := range s.Events {
		if ev.Type == eventType {
			filtered = append(filtered, ev)
		}
	}
	return filtered
}

// AssertToolCalledN asserts a tool was called exactly N times
func (s *ParallelViaToolsSuite) AssertToolCalledN(toolName string, count int) {
	s.AgentTestSuite.AssertToolCallCount(toolName, count)
}

func hasWorkerIDSuffix(workerIDs map[string]bool, suffix string) bool {
	for workerID := range workerIDs {
		if strings.HasSuffix(workerID, suffix) {
			return true
		}
	}
	return false
}

// TestParallelTaskTools_BasicDispatch verifies that multiple task tool calls
// in a single LLM response trigger parallel execution.
func (s *ParallelViaToolsSuite) TestParallelTaskTools_BasicDispatch() {
	// Parent agent returns 3 task tool calls in one turn
	s.MockServer.AddResponse(MockResponse{
		Content: "I'll dispatch three parallel tasks to analyze components.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_task_alpha",
				Name:      "task",
				Arguments: `{"prompt":"Analyze alpha component","subagent_type":"execute","description":"alpha-worker"}`,
			},
			{
				ID:        "call_task_beta",
				Name:      "task",
				Arguments: `{"prompt":"Analyze beta component","subagent_type":"execute","description":"beta-worker"}`,
			},
			{
				ID:        "call_task_gamma",
				Name:      "task",
				Arguments: `{"prompt":"Analyze gamma component","subagent_type":"execute","description":"gamma-worker"}`,
			},
		},
	})

	// Setup rule-based routing for each sub-agent
	s.MockServer.AddRule(MockRule{
		Name:    "alpha-analysis",
		Matcher: MatchTaskFieldContains("alpha"),
		Response: MockResponse{
			Content: "Alpha component analysis: All systems operational.",
		},
	})
	s.MockServer.AddRule(MockRule{
		Name:    "beta-analysis",
		Matcher: MatchTaskFieldContains("beta"),
		Response: MockResponse{
			Content: "Beta component analysis: Minor issues detected.",
		},
	})
	s.MockServer.AddRule(MockRule{
		Name:    "gamma-analysis",
		Matcher: MatchTaskFieldContains("gamma"),
		Response: MockResponse{
			Content: "Gamma component analysis: Performance optimal.",
		},
	})

	// Parent agent final response after receiving tool results
	s.MockServer.AddResponse(MockResponse{
		Content: "All three components have been analyzed successfully.",
	})

	// Execute
	_, err := s.RunAgent("Analyze our three main components in parallel.")
	s.NoError(err)

	// Verify task tool was called 3 times
	s.AssertToolCalledN("task", 3)

	// Verify all 3 sub-agents completed
	finishedEvents := s.FilterEventsByType(event.EventTypeExpertFinished)
	s.Len(finishedEvents, 3, "Should have 3 expert finished events")

	// Verify unique worker IDs
	workerIDs := make(map[string]bool)
	for _, ev := range finishedEvents {
		if ev.WorkerID != "" {
			workerIDs[ev.WorkerID] = true
		}
	}
	s.Equal(3, len(workerIDs), "Should have 3 distinct worker IDs")

	// Verify worker IDs preserve the description suffix while allowing a unique prefix.
	s.True(hasWorkerIDSuffix(workerIDs, "-alpha-worker"))
	s.True(hasWorkerIDSuffix(workerIDs, "-beta-worker"))
	s.True(hasWorkerIDSuffix(workerIDs, "-gamma-worker"))
}

// TestParallelTaskTools_WithExploreAgents verifies parallel execution
// with explore-type agents (which have different tool access).
func (s *ParallelViaToolsSuite) TestParallelTaskTools_WithExploreAgents() {
	// Parent dispatches 2 explore agents in parallel
	s.MockServer.AddResponse(MockResponse{
		Content: "I'll explore two areas of the codebase in parallel.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_explore_auth",
				Name:      "task",
				Arguments: `{"prompt":"Find authentication implementation","subagent_type":"explore","description":"auth-explorer"}`,
			},
			{
				ID:        "call_explore_db",
				Name:      "task",
				Arguments: `{"prompt":"Find database layer","subagent_type":"explore","description":"db-explorer"}`,
			},
		},
	})

	// Setup responses for explore agents
	// Make matchers more specific to avoid matching parent's prompt
	s.MockServer.AddRule(MockRule{
		Name:    "auth-exploration",
		Matcher: MatchTaskFieldContains("Find authentication implementation"),
		Response: MockResponse{
			Content: "Found authentication in pkg/auth/handler.go",
		},
	})
	s.MockServer.AddRule(MockRule{
		Name:    "db-exploration",
		Matcher: MatchTaskFieldContains("Find database layer"),
		Response: MockResponse{
			Content: "Found database layer in pkg/storage/db.go",
		},
	})

	// Parent final response
	s.MockServer.AddResponse(MockResponse{
		Content: "Both areas explored successfully.",
	})

	// Execute
	_, err := s.RunAgent("Explore two assigned areas of the codebase in parallel.")
	s.NoError(err)

	// Verify both experts completed with correct agent types
	finishedEvents := s.FilterEventsByType(event.EventTypeExpertFinished)
	s.Len(finishedEvents, 2)

	// Extract agent types from events
	agentTypes := make(map[agent.AgentType]int)
	for _, ev := range finishedEvents {
		// AgentType should be in metadata
		if meta := ev.Metadata; meta != nil {
			if at, ok := meta["agent_type"].(string); ok {
				agentTypes[agent.AgentType(at)]++
			}
		}
	}

	// Both should be explore type
	s.Equal(2, agentTypes[agent.AgentTypeExplore])
}

// TestParallelTaskTools_MixedSuccess verifies that partial failures
// don't block other parallel tasks.
func (s *ParallelViaToolsSuite) TestParallelTaskTools_MixedSuccess() {
	// Parent dispatches 4 tasks
	s.MockServer.AddResponse(MockResponse{
		Content: "Dispatching four parallel tasks.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_task1",
				Name:      "task",
				Arguments: `{"prompt":"Success task 1","subagent_type":"execute"}`,
			},
			{
				ID:        "call_task2",
				Name:      "task",
				Arguments: `{"prompt":"Fail task 2","subagent_type":"execute"}`,
			},
			{
				ID:        "call_task3",
				Name:      "task",
				Arguments: `{"prompt":"Success task 3","subagent_type":"execute"}`,
			},
			{
				ID:        "call_task4",
				Name:      "task",
				Arguments: `{"prompt":"Fail task 4","subagent_type":"execute"}`,
			},
		},
	})

	// Setup success/failure pattern
	s.MockServer.AddRule(MockRule{
		Name:    "success-tasks",
		Matcher: MatchTaskFieldContains("Success"),
		Response: MockResponse{
			Content: "Task completed successfully.",
		},
	})
	s.MockServer.AddRule(MockRule{
		Name:    "fail-tasks",
		Matcher: MatchTaskFieldContains("Fail"),
		Response: MockResponse{
			Error: 400, // Permanent error avoids retry/circuit-breaker interactions
		},
	})

	// Parent handles mixed results
	s.MockServer.AddResponse(MockResponse{
		Content: "Two tasks succeeded, two failed.",
	})

	// Execute
	_, err := s.RunAgent("Execute four tasks in parallel.")
	s.NoError(err, "Parent agent should handle partial failures")

	// All 4 experts should finish (even failed ones emit finish events)
	finishedEvents := s.FilterEventsByType(event.EventTypeExpertFinished)
	s.Len(finishedEvents, 4)

	// Count successes and failures
	successCount := 0
	failureCount := 0
	for _, ev := range finishedEvents {
		if meta := ev.Metadata; meta != nil {
			if success, ok := meta["success"].(bool); ok {
				if success {
					successCount++
				} else {
					failureCount++
				}
			}
		}
	}

	s.Equal(2, successCount, "Should have 2 successful tasks")
	s.Equal(2, failureCount, "Should have 2 failed tasks")
}

// TestParallelTaskTools_EventSequencing verifies that events from parallel
// tasks are properly attributed with WorkerID.
func (s *ParallelViaToolsSuite) TestParallelTaskTools_EventSequencing() {
	// Parent dispatches 3 tasks
	s.MockServer.AddResponse(MockResponse{
		Content: "Dispatching three workers.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_w1",
				Name:      "task",
				Arguments: `{"prompt":"Worker 1 task","subagent_type":"execute","description":"w1"}`,
			},
			{
				ID:        "call_w2",
				Name:      "task",
				Arguments: `{"prompt":"Worker 2 task","subagent_type":"execute","description":"w2"}`,
			},
			{
				ID:        "call_w3",
				Name:      "task",
				Arguments: `{"prompt":"Worker 3 task","subagent_type":"execute","description":"w3"}`,
			},
		},
	})

	// Simple responses
	s.MockServer.SetDefaultResponse(MockResponse{
		Content: "Task done.",
	})

	// Parent final response
	s.MockServer.AddResponse(MockResponse{
		Content: "All workers completed.",
	})

	// Execute
	_, err := s.RunAgent("Run three workers.")
	s.NoError(err)

	// Verify each worker has complete event sequence: started -> finished.
	expectedSuffixes := []string{"-w1", "-w2", "-w3"}
	for _, suffix := range expectedSuffixes {
		// Find started event
		startedFound := false
		finishedFound := false

		for _, ev := range s.Events {
			if strings.HasSuffix(ev.WorkerID, suffix) {
				if ev.Type == event.EventTypeExpertStarted {
					startedFound = true
				}
				if ev.Type == event.EventTypeExpertFinished {
					finishedFound = true
				}
			}
		}

		s.True(startedFound, "Worker with suffix %s should have started event", suffix)
		s.True(finishedFound, "Worker with suffix %s should have finished event", suffix)
	}
}

// TestParallelTaskTools_DuplicateDescriptions verifies duplicate descriptions
// still produce distinct WorkerIDs across separate task tool calls.
func (s *ParallelViaToolsSuite) TestParallelTaskTools_DuplicateDescriptions() {
	s.MockServer.AddResponse(MockResponse{
		Content: "Dispatching two similar tasks.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_dup_one",
				Name:      "task",
				Arguments: `{"prompt":"Investigate first duplicate path","subagent_type":"execute","description":"shared-desc"}`,
			},
			{
				ID:        "call_dup_two",
				Name:      "task",
				Arguments: `{"prompt":"Investigate second duplicate path","subagent_type":"execute","description":"shared-desc"}`,
			},
		},
	})

	s.MockServer.AddRule(MockRule{
		Name:    "duplicate-one",
		Matcher: MatchTaskFieldContains("first duplicate"),
		Response: MockResponse{
			Content: "First duplicate complete.",
		},
	})
	s.MockServer.AddRule(MockRule{
		Name:    "duplicate-two",
		Matcher: MatchTaskFieldContains("second duplicate"),
		Response: MockResponse{
			Content: "Second duplicate complete.",
		},
	})

	s.MockServer.AddResponse(MockResponse{
		Content: "Both duplicate-description tasks completed.",
	})

	_, err := s.RunAgent("Run two similar investigations in parallel.")
	s.NoError(err)

	finishedEvents := s.FilterEventsByType(event.EventTypeExpertFinished)
	s.Len(finishedEvents, 2)

	workerIDs := make(map[string]bool)
	suffixMatches := 0
	for _, ev := range finishedEvents {
		if ev.WorkerID == "" {
			continue
		}
		workerIDs[ev.WorkerID] = true
		if strings.HasSuffix(ev.WorkerID, "-shared-desc") {
			suffixMatches++
		}
	}

	s.Len(workerIDs, 2, "Duplicate descriptions should still yield unique worker IDs")
	s.Equal(2, suffixMatches, "Both workers should retain the description suffix")
}

// TestParallelTaskTools_RespectsConcurrencyLimit verifies that
// the maxConcurrent config is respected when dispatching parallel tasks.
func (s *ParallelViaToolsSuite) TestParallelTaskTools_RespectsConcurrencyLimit() {
	// Configure max concurrency of 2
	s.Agent.GetConfig().Advanced = &config.AdvancedConfig{
		Fork: &config.ForkAdvConfig{
			MaxDepth:      3,
			MaxConcurrent: 2,
		},
	}

	// Parent dispatches 5 tasks
	s.MockServer.AddResponse(MockResponse{
		Content: "Dispatching five tasks with concurrency limit.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_1",
				Name:      "task",
				Arguments: `{"prompt":"Task 1","subagent_type":"execute"}`,
			},
			{
				ID:        "call_2",
				Name:      "task",
				Arguments: `{"prompt":"Task 2","subagent_type":"execute"}`,
			},
			{
				ID:        "call_3",
				Name:      "task",
				Arguments: `{"prompt":"Task 3","subagent_type":"execute"}`,
			},
			{
				ID:        "call_4",
				Name:      "task",
				Arguments: `{"prompt":"Task 4","subagent_type":"execute"}`,
			},
			{
				ID:        "call_5",
				Name:      "task",
				Arguments: `{"prompt":"Task 5","subagent_type":"execute"}`,
			},
		},
	})

	// Add delay to each response to make timing observable
	s.MockServer.SetDefaultResponse(MockResponse{
		Content: "Task done.",
		// Note: We can't easily verify timing in this test since we don't
		// have direct access to execution timing. The timing tests are
		// better suited for ParallelForkSuite which calls ForkBatch directly.
	})

	// Parent final response
	s.MockServer.AddResponse(MockResponse{
		Content: "All tasks completed.",
	})

	// Execute
	_, err := s.RunAgent("Run five tasks with concurrency limit.")
	s.NoError(err)

	// All 5 should complete
	finishedEvents := s.FilterEventsByType(event.EventTypeExpertFinished)
	s.Len(finishedEvents, 5, "All 5 tasks should complete despite concurrency limit")

	// Note: Detailed timing verification of maxConcurrent is in ParallelForkSuite.
	// Here we just verify that all tasks complete successfully.
}

// TestParallelTaskTools_NoParallelism verifies that single task tool call
// doesn't trigger parallel execution path.
func (s *ParallelViaToolsSuite) TestParallelTaskTools_NoParallelism() {
	// Parent dispatches only 1 task
	s.MockServer.AddResponse(MockResponse{
		Content: "Dispatching single task.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_single",
				Name:      "task",
				Arguments: `{"prompt":"Single task","subagent_type":"execute","description":"solo"}`,
			},
		},
	})

	// Sub-agent response
	s.MockServer.AddResponse(MockResponse{
		Content: "Single task completed.",
	})

	// Parent final response
	s.MockServer.AddResponse(MockResponse{
		Content: "Done.",
	})

	// Execute
	_, err := s.RunAgent("Run one task.")
	s.NoError(err)

	// Verify exactly 1 expert finished
	finishedEvents := s.FilterEventsByType(event.EventTypeExpertFinished)
	s.Len(finishedEvents, 1)

	// Verify worker ID retains the task description even if uniqueness prefixes are added.
	s.True(strings.HasSuffix(finishedEvents[0].WorkerID, "-solo"))
}

// TestParallelTaskTools_EmptyToolCalls verifies that when parent agent
// returns no tool calls, no sub-agents are spawned.
func (s *ParallelViaToolsSuite) TestParallelTaskTools_EmptyToolCalls() {
	// Parent returns text only, no tool calls
	s.MockServer.AddResponse(MockResponse{
		Content: "I'll handle this myself without delegating.",
	})

	// Execute
	_, err := s.RunAgent("Do a simple task.")
	s.NoError(err)

	// No expert events should be emitted
	startedEvents := s.FilterEventsByType(event.EventTypeExpertStarted)
	finishedEvents := s.FilterEventsByType(event.EventTypeExpertFinished)

	s.Empty(startedEvents, "No experts should start")
	s.Empty(finishedEvents, "No experts should finish")
}
