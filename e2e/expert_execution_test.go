//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/stretchr/testify/suite"
)

// ExpertExecutionSuite tests single expert execution via ExpertRunner.
// This suite validates:
// - Expert execution lifecycle (started → finished events)
// - Template rendering with different input schemas
// - Output JSON parsing when OutputSchemaJSON is provided
// - Error handling for expert failures
// - Token usage tracking
type ExpertExecutionSuite struct {
	AgentTestSuite
	registry *agent.ExpertRegistry
	runner   *agent.ExpertRunner
}

func TestExpertExecutionSuite(t *testing.T) {
	suite.Run(t, new(ExpertExecutionSuite))
}

func (s *ExpertExecutionSuite) SetupTest() {
	// Call parent setup first
	s.AgentTestSuite.SetupTest()

	// Create expert registry with test experts
	s.registry = agent.NewExpertRegistry()

	// Register test expert with JSON output schema
	_ = s.registry.Register(&agent.Expert{
		Name:           "test-expert",
		DisplayName:    "Test Expert",
		Description:    "Test expert for e2e testing",
		Source:         "test",
		SystemPrompt:   "You are a test expert. Respond with JSON output.",
		QueryTemplate:  "Task: ${task}",
		MaxTurns:       5,
		MaxTimeMinutes: 2,
		AllowedTools:   []string{"*"},
		OutputName:     "result",
		OutputSchemaJSON: `{
			"type": "object",
			"properties": {
				"status": {"type": "string"},
				"message": {"type": "string"}
			},
			"required": ["status", "message"]
		}`,
		InputSchema: &agent.ExpertInputSchema{
			Type: "object",
			Properties: map[string]*agent.ExpertPropertySchema{
				"task": {Type: "string", Description: "Task to perform"},
			},
			Required: []string{"task"},
		},
	})

	// Register expert without output schema
	_ = s.registry.Register(&agent.Expert{
		Name:           "plain-expert",
		DisplayName:    "Plain Expert",
		Description:    "Expert without JSON output",
		Source:         "test",
		SystemPrompt:   "You are a plain text expert.",
		QueryTemplate:  "${request}",
		MaxTurns:       3,
		MaxTimeMinutes: 1,
		AllowedTools:   []string{"*"},
		OutputName:     "response",
		InputSchema: &agent.ExpertInputSchema{
			Type: "object",
			Properties: map[string]*agent.ExpertPropertySchema{
				"request": {Type: "string", Description: "User request"},
			},
			Required: []string{"request"},
		},
	})

	// Create expert runner
	s.runner = agent.NewExpertRunner(s.registry, agent.NewForkManager(s.Agent), nil)
}

// collectEvent is a callback for collecting events during expert execution
func (s *ExpertExecutionSuite) collectEvent(ev event.StreamEvent) {
	s.AgentTestSuite.AppendEvents(ev)
}

// FilterEventsByType returns all events matching the given type
func (s *ExpertExecutionSuite) FilterEventsByType(eventType event.EventType) []event.StreamEvent {
	var filtered []event.StreamEvent
	for _, ev := range s.Events {
		if ev.Type == eventType {
			filtered = append(filtered, ev)
		}
	}
	return filtered
}

// TestExpertExecution_BasicSuccess verifies successful expert execution.
func (s *ExpertExecutionSuite) TestExpertExecution_BasicSuccess() {
	// Setup mock response
	s.MockServer.AddResponse(MockResponse{
		Content: "Task completed successfully.",
	})

	// Execute expert
	ctx := context.Background()
	inputs := map[string]interface{}{
		"task": "run test",
	}

	result, err := s.runner.Run(ctx, "test-expert", inputs, s.collectEvent)

	// Assertions
	s.NoError(err)
	s.NotNil(result)
	s.Equal("test-expert", result.ExpertName)
	s.Equal("Test Expert", result.DisplayName)
	s.Contains(result.Task, "run test", "Task should contain input")
	s.Contains(result.Output, "Task completed", "Output should match mock")
	s.Greater(result.Duration, time.Duration(0), "Duration should be > 0")

	// Verify expert events
	startedEvents := s.FilterEventsByType(event.EventTypeExpertStarted)
	finishedEvents := s.FilterEventsByType(event.EventTypeExpertFinished)

	s.Len(startedEvents, 1, "Should have 1 started event")
	s.Len(finishedEvents, 1, "Should have 1 finished event")

	// Verify started event metadata
	startedMeta := startedEvents[0].Metadata
	s.Equal("test-expert", startedMeta["expert_name"])
	s.Equal("Test Expert", startedMeta["display_name"])
	s.Equal("test", startedMeta["source"])

	// Verify finished event metadata
	finishedMeta := finishedEvents[0].Metadata
	s.Equal("test-expert", finishedMeta["expert_name"])
	s.Equal(true, finishedMeta["success"])
	s.Greater(finishedMeta["duration_ms"], int64(0))
}

// TestExpertExecution_TemplateRendering verifies input template rendering.
func (s *ExpertExecutionSuite) TestExpertExecution_TemplateRendering() {
	s.MockServer.AddResponse(MockResponse{
		Content: "Rendered template executed.",
	})

	ctx := context.Background()
	inputs := map[string]interface{}{
		"task": "analyze codebase structure",
	}

	result, err := s.runner.Run(ctx, "test-expert", inputs, s.collectEvent)

	s.NoError(err)
	s.NotNil(result)

	// Verify task was rendered with template: "Task: ${task}"
	s.Equal("Task: analyze codebase structure", result.Task)
}

// TestExpertExecution_JSONOutput verifies JSON output parsing.
func (s *ExpertExecutionSuite) TestExpertExecution_JSONOutput() {
	// Mock returns valid JSON matching schema
	jsonOutput := `{"status": "success", "message": "All tests passed"}`
	s.MockServer.AddResponse(MockResponse{
		Content: jsonOutput,
	})

	ctx := context.Background()
	inputs := map[string]interface{}{
		"task": "run validation",
	}

	result, err := s.runner.Run(ctx, "test-expert", inputs, s.collectEvent)

	s.NoError(err)
	s.NotNil(result)

	// Verify raw output (use JSONEq for key-order independence)
	expectedJSON := `{"status": "success", "message": "All tests passed"}`
	s.JSONEq(expectedJSON, result.Output, "JSON output should match expected structure")

	// Verify parsed JSON
	s.NotNil(result.OutputJSON, "OutputJSON should be parsed")
	s.Equal("success", result.OutputJSON["status"])
	s.Equal("All tests passed", result.OutputJSON["message"])
}

// TestExpertExecution_InvalidJSON verifies handling of malformed JSON.
func (s *ExpertExecutionSuite) TestExpertExecution_InvalidJSON() {
	// Mock returns invalid JSON
	s.MockServer.AddResponse(MockResponse{
		Content: "This is not valid JSON",
	})

	ctx := context.Background()
	inputs := map[string]interface{}{
		"task": "generate report",
	}

	result, err := s.runner.Run(ctx, "test-expert", inputs, s.collectEvent)

	s.NoError(err, "Expert should succeed even with invalid JSON")
	s.NotNil(result)

	// Verify OutputJSON is nil (parsing failed)
	s.Nil(result.OutputJSON, "OutputJSON should be nil for invalid JSON")

	// Raw output should still be available
	s.Equal("This is not valid JSON", result.Output)
}

// TestExpertExecution_PlainTextExpert verifies expert without JSON schema.
func (s *ExpertExecutionSuite) TestExpertExecution_PlainTextExpert() {
	s.MockServer.AddResponse(MockResponse{
		Content: "Plain text response from expert.",
	})

	ctx := context.Background()
	inputs := map[string]interface{}{
		"request": "help me with this",
	}

	result, err := s.runner.Run(ctx, "plain-expert", inputs, s.collectEvent)

	s.NoError(err)
	s.NotNil(result)
	s.Equal("plain-expert", result.ExpertName)
	s.Equal("Plain Expert", result.DisplayName)

	// Verify task was rendered with plain-expert template: "${request}"
	s.Equal("help me with this", result.Task)

	// No JSON parsing should occur
	s.Nil(result.OutputJSON)
	s.Equal("Plain text response from expert.", result.Output)
}

// TestExpertExecution_NonexistentExpert verifies error handling.
func (s *ExpertExecutionSuite) TestExpertExecution_NonexistentExpert() {
	ctx := context.Background()
	inputs := map[string]interface{}{
		"task": "do something",
	}

	result, err := s.runner.Run(ctx, "nonexistent", inputs, s.collectEvent)

	s.Error(err, "Should error for nonexistent expert")
	s.Nil(result, "Result should be nil")
	s.Contains(err.Error(), "not found")

	// No events should be emitted
	startedEvents := s.FilterEventsByType(event.EventTypeExpertStarted)
	finishedEvents := s.FilterEventsByType(event.EventTypeExpertFinished)
	s.Empty(startedEvents)
	s.Empty(finishedEvents)
}

// TestExpertExecution_LLMError verifies handling of LLM errors.
func (s *ExpertExecutionSuite) TestExpertExecution_LLMError() {
	// Mock returns permanent error (401 triggers fast-fail, not retry)
	s.MockServer.AddResponse(MockResponse{
		Error: 401, // Unauthorized - permanent error
	})

	ctx := context.Background()
	inputs := map[string]interface{}{
		"task": "failing task",
	}

	result, err := s.runner.Run(ctx, "test-expert", inputs, s.collectEvent)

	s.Error(err, "Should error when LLM fails")
	s.NotNil(result, "Result object should still be created")
	s.Equal("test-expert", result.ExpertName)
	s.Equal(err, result.Error)

	// Verify finished event shows failure
	finishedEvents := s.FilterEventsByType(event.EventTypeExpertFinished)
	s.Len(finishedEvents, 1)

	finishedMeta := finishedEvents[0].Metadata
	s.Equal(false, finishedMeta["success"], "Success should be false")
	s.NotEmpty(finishedEvents[0].Error, "Error field should be set")
}

// TestExpertExecution_TokenTracking verifies token usage tracking.
func (s *ExpertExecutionSuite) TestExpertExecution_TokenTracking() {
	// Mock response
	s.MockServer.AddResponse(MockResponse{
		Content: "Token tracking test.",
		// Note: MockResponse doesn't currently support Usage field
		// Token tracking is tested via integration in other tests
	})

	ctx := context.Background()
	inputs := map[string]interface{}{
		"task": "test tokens",
	}

	result, err := s.runner.Run(ctx, "test-expert", inputs, s.collectEvent)

	s.NoError(err)
	s.NotNil(result)

	// Verify token counts - check plumbing works, not exact values (streaming may affect counts)
	// Note: token values may be int or int64 depending on platform
	s.Greater(int(result.TokensUsed.InputTokens), 0, "Input tokens should be tracked")
	s.Greater(int(result.TokensUsed.OutputTokens), 0, "Output tokens should be tracked")
	s.Greater(int(result.TokensUsed.TotalTokens), 0, "Total tokens should be tracked")
	s.Equal(result.TokensUsed.TotalTokens, result.TokensUsed.InputTokens+result.TokensUsed.OutputTokens, "Total should equal input + output")

	// Verify token info in finished event
	finishedEvents := s.FilterEventsByType(event.EventTypeExpertFinished)
	s.Len(finishedEvents, 1)

	finishedMeta := finishedEvents[0].Metadata
	tokensUsed := numericMetadataValue(finishedMeta["tokens_used"])
	inputTokens := numericMetadataValue(finishedMeta["input_tokens"])
	outputTokens := numericMetadataValue(finishedMeta["output_tokens"])
	s.Greater(tokensUsed, 0, "Finished event should include token count")
	s.Greater(inputTokens, 0, "Finished event should include input tokens")
	s.Greater(outputTokens, 0, "Finished event should include output tokens")
}

// TestExpertExecution_EventCallback verifies event callback is called.
func (s *ExpertExecutionSuite) TestExpertExecution_EventCallback() {
	s.MockServer.AddResponse(MockResponse{
		Content: "Event callback test.",
	})

	// Create custom event collector
	callbackEvents := make([]event.StreamEvent, 0)
	callback := func(ev event.StreamEvent) {
		callbackEvents = append(callbackEvents, ev)
	}

	ctx := context.Background()
	inputs := map[string]interface{}{
		"task": "test events",
	}

	_, err := s.runner.Run(ctx, "test-expert", inputs, callback)

	s.NoError(err)

	// Verify callback received both started and finished events
	s.GreaterOrEqual(len(callbackEvents), 2, "Should have at least started + finished")

	hasStarted := false
	hasFinished := false
	for _, ev := range callbackEvents {
		if ev.Type == event.EventTypeExpertStarted {
			hasStarted = true
		}
		if ev.Type == event.EventTypeExpertFinished {
			hasFinished = true
		}
	}

	s.True(hasStarted, "Callback should receive started event")
	s.True(hasFinished, "Callback should receive finished event")
}

// TestExpertExecution_NilCallback verifies handling of nil callback.
func (s *ExpertExecutionSuite) TestExpertExecution_NilCallback() {
	s.MockServer.AddResponse(MockResponse{
		Content: "No callback test.",
	})

	ctx := context.Background()
	inputs := map[string]interface{}{
		"task": "test without callback",
	}

	// Pass nil callback - should not crash
	result, err := s.runner.Run(ctx, "test-expert", inputs, nil)

	s.NoError(err)
	s.NotNil(result)
	s.Equal("test-expert", result.ExpertName)
}

// TestExpertExecution_ComplexJSONOutput tests parsing of nested JSON.
func (s *ExpertExecutionSuite) TestExpertExecution_ComplexJSONOutput() {
	// Create expert with complex schema
	_ = s.registry.Register(&agent.Expert{
		Name:          "complex-json",
		DisplayName:   "Complex JSON Expert",
		Source:        "test",
		SystemPrompt:  "Return complex JSON.",
		QueryTemplate: "${input}",
		OutputName:    "report",
		OutputSchemaJSON: `{
			"type": "object",
			"properties": {
				"summary": {"type": "string"},
				"items": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"value": {"type": "number"}
						}
					}
				}
			}
		}`,
		InputSchema: &agent.ExpertInputSchema{
			Type: "object",
			Properties: map[string]*agent.ExpertPropertySchema{
				"input": {Type: "string"},
			},
			Required: []string{"input"},
		},
	})

	// Mock complex JSON response
	complexJSON := `{
		"summary": "Test results",
		"items": [
			{"name": "test1", "value": 100},
			{"name": "test2", "value": 200}
		]
	}`
	s.MockServer.AddResponse(MockResponse{
		Content: complexJSON,
	})

	ctx := context.Background()
	inputs := map[string]interface{}{
		"input": "generate report",
	}

	result, err := s.runner.Run(ctx, "complex-json", inputs, nil)

	s.NoError(err)
	s.NotNil(result)
	s.NotNil(result.OutputJSON)

	// Verify parsed structure
	s.Equal("Test results", result.OutputJSON["summary"])

	items, ok := result.OutputJSON["items"].([]interface{})
	s.True(ok, "items should be array")
	s.Len(items, 2)

	// Verify first item
	item1 := items[0].(map[string]interface{})
	s.Equal("test1", item1["name"])
	s.Equal(json.Number("100"), item1["value"])
}

// TestExpertExecution_EventTimestamps verifies event timestamps are set.
func (s *ExpertExecutionSuite) TestExpertExecution_EventTimestamps() {
	s.MockServer.AddResponse(MockResponse{
		Content: "Timestamp test.",
	})

	ctx := context.Background()
	inputs := map[string]interface{}{
		"task": "test timestamps",
	}

	startTime := time.Now().Unix()
	_, err := s.runner.Run(ctx, "test-expert", inputs, s.collectEvent)
	endTime := time.Now().Unix()

	s.NoError(err)

	// Check timestamps
	startedEvents := s.FilterEventsByType(event.EventTypeExpertStarted)
	finishedEvents := s.FilterEventsByType(event.EventTypeExpertFinished)

	s.Len(startedEvents, 1)
	s.Len(finishedEvents, 1)

	// Verify timestamps are within test execution window
	s.GreaterOrEqual(startedEvents[0].Timestamp, startTime)
	s.LessOrEqual(startedEvents[0].Timestamp, endTime)

	s.GreaterOrEqual(finishedEvents[0].Timestamp, startedEvents[0].Timestamp)
	s.LessOrEqual(finishedEvents[0].Timestamp, endTime)
}

func numericMetadataValue(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
