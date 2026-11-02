package agent

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

func TestTurnExecutorHandlesTaskDoneWithoutRegisteredTool(t *testing.T) {
	turn := NewTurn("finish", nil)
	var sawCompletion bool
	turn.SetEventHandler(func(ev event.StreamEvent) {
		if ev.Type == event.EventTypeTaskCompletion {
			sawCompletion = true
		}
	})
	executor := newTurnExecutor(turn)

	remaining, results := executor.handleTaskDoneTools([]ToolToExecute{{
		ID:   "done-1",
		Name: "task_done",
		Parameters: map[string]interface{}{
			"status":  "success",
			"summary": "completed",
		},
	}})

	if len(remaining) != 0 {
		t.Fatalf("remaining tools = %d, want 0", len(remaining))
	}
	if !turn.CompletionCriteria.TaskCompleted || !turn.Status.IsComplete || !sawCompletion {
		t.Fatal("task_done did not mark turn complete")
	}
	if result := results["done-1"]; result == nil || !result.Success {
		t.Fatalf("unexpected task_done result: %+v", result)
	}
}

func TestTurnExecuteTaskDoneFromToolCallEventKeepsContextConsistent(t *testing.T) {
	mockClient := llm.NewMockClient()
	mockClient.Responses = []llm.MockResponse{{
		ToolCalls: []tools.ToolCall{{
			ID:   "done-1",
			Name: "task_done",
			Arguments: map[string]interface{}{
				"status":  "success",
				"summary": "all done",
			},
		}},
	}}
	turn := NewTurn("finish", &TurnConfig{
		LLMClient: mockClient,
		AgentConfig: &config.Config{
			CustomSystemPrompt: "test",
		},
	})

	if err := turn.Execute(context.Background()); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !turn.CompletionCriteria.TaskCompleted || !turn.Status.IsComplete {
		t.Fatal("task_done tool call did not complete the turn")
	}
	if len(mockClient.Calls) != 1 {
		t.Fatalf("LLM calls = %d, want 1", len(mockClient.Calls))
	}

	var assistantWithTaskDone bool
	var matchingToolResult bool
	for _, msg := range turn.Messages {
		if msg.Role == "assistant" {
			for _, tc := range msg.ToolCalls {
				if tc.ID == "done-1" && tc.Name == "task_done" {
					assistantWithTaskDone = true
				}
			}
		}
		if msg.Role == "tool" && msg.ToolCallID == "done-1" {
			matchingToolResult = true
		}
	}
	if !assistantWithTaskDone {
		t.Fatal("assistant task_done tool call was not preserved in conversation context")
	}
	if !matchingToolResult {
		t.Fatal("task_done tool result was not appended with matching tool_call_id")
	}
}
