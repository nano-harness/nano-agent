package agent

import (
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
