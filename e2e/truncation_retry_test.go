//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type TruncationSuite struct {
	AgentTestSuite
}

func TestTruncation(t *testing.T) {
	suite.Run(t, new(TruncationSuite))
}

func (s *TruncationSuite) TestTruncation_FinishReasonLengthEmitsEvent() {
	s.MockServer.AddResponse(MockResponse{
		Content:      "Partial model output",
		FinishReason: "length",
	})

	llmClient := s.Agent.GetLLMClient()
	client, ok := llmClient.(*llm.Client)
	require.True(s.T(), ok, "expected underlying LLM client to be *llm.Client")

	var events []event.StreamEvent
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.StreamCompletion(ctx, []llm.Message{{Role: "user", Content: "truncate"}}, func(ev event.StreamEvent) {
		events = append(events, ev)
	})
	require.NoError(s.T(), err)

	found := false
	for _, ev := range events {
		if ev.Metadata == nil {
			continue
		}
		truncated, _ := ev.Metadata["truncated"].(bool)
		if truncated && ev.Metadata["finish_reason"] == "length" {
			found = true
			break
		}
	}
	require.True(s.T(), found, "expected truncated=true and finish_reason=length metadata")
}
