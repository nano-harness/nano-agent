package agent

import (
	"os"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/llm"
)

func newTestTurn() *Turn {
	return &Turn{
		ID:                  "turn_test",
		UserInput:           "test",
		WorkingDir:          ".",
		Messages:            []llm.Message{},
		History:             []ExecutionStep{},
		Actions:             []string{},
		ToolResults:         []interfaces.ToolResult{},
		TokenStats:          &event.TokenStats{},
		compressionStrategy: NewCompressionStrategy(),
		CompletionCriteria: &CompletionCriteria{
			ErrorThreshold:    10,
			CurrentIteration:  0,
			ConsecutiveErrors: 0,
			ErrorCount:        0,
		},
		Status: CompletionStatus{IsComplete: false, Progress: 0.0},
	}
}

func TestTurnCompletionImplicit(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	_, err := config.LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	t.Run("NonCompletionMetadataDoesNotComplete", func(t *testing.T) {
		turn := newTestTurn()

		err := turn.addToolResultsToContext(map[string]*interfaces.ToolResult{
			"call_1": {
				Success:    true,
				LLMContent: "ok",
				Metadata: map[string]interface{}{
					"tool_name":               "some_tool",
					"task_completion_signal":  true,
					"completion_confidence":   1.0,
					"some_other_completion":   true,
					"another_completion_flag": true,
				},
			},
		})
		if err != nil {
			t.Fatalf("addToolResultsToContext returned error: %v", err)
		}
		// With implicit completion, tools don't trigger completion - only finish_reason="stop" + no tool calls does
		if turn.CompletionCriteria.TaskCompleted {
			t.Fatalf("expected TaskCompleted=false, got true")
		}
	})

	t.Run("MarkTaskCompletedWorks", func(t *testing.T) {
		turn := newTestTurn()

		// Simulate implicit completion (model returned text without tool calls)
		turn.MarkTaskCompleted("natural-completion: model returned text without tool calls")

		if !turn.CompletionCriteria.TaskCompleted {
			t.Fatalf("expected TaskCompleted=true, got false")
		}
	})
}
