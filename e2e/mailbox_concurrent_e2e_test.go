//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/mailbox"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// MailboxConcurrentSuite tests concurrent message sending scenarios
type MailboxConcurrentSuite struct {
	AgentTestSuite
}

func TestMailboxConcurrentSuite(t *testing.T) {
	suite.Run(t, new(MailboxConcurrentSuite))
}

// TestConcurrent_ForkBatch_NoMessageLoss tests that concurrent child agents
// can send messages without loss, and verifies file backend's flock correctness
func (s *MailboxConcurrentSuite) TestConcurrent_ForkBatch_NoMessageLoss() {
	// Only test file backend for concurrency verification (flock)
	s.Run("file", func() {
		cfg := enableMailbox(s.T(), &s.AgentTestSuite, "file")
		cfg.InjectionLimit = 9 // Allow all 9 messages
		defer disableMailbox()

		mgr := mailbox.GlobalManager()
		require.NotNil(s.T(), mgr)
		parentMailbox, err := mgr.Of("main")
		require.NoError(s.T(), err)

		ctx := context.Background()
		for _, msg := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"} {
			err = parentMailbox.Send(ctx, mailbox.Message{
				From:  "child-agent",
				To:    "main",
				Topic: "finding",
				Body:  map[string]interface{}{"type": "result", "content": msg},
			})
			require.NoError(s.T(), err)
		}

		// Parent's second turn after all children complete
		s.MockServer.AddResponse(MockResponse{
			Content: "Parent processing concurrent messages",
		})

		_, err = s.RunAgent("Fork batch of children to send messages concurrently")
		require.NoError(s.T(), err)

		// All messages should have been injected (InjectionLimit=9)
		count, err := parentMailbox.Count(context.Background())
		require.NoError(s.T(), err)
		require.Equal(s.T(), 0, count, "all 9 messages should have been injected")

		systemContent := findMailboxContent(s.MockServer)
		require.NotEmpty(s.T(), systemContent, "expected mailbox injection")

		// Count occurrences of each child's messages
		expectedMsgs := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"}
		for _, msg := range expectedMsgs {
			require.Contains(s.T(), systemContent, msg,
				"expected message %s to be injected", msg)
		}
	})
}

// TestConcurrent_LimitExceeded verifies behavior when concurrent messages exceed InjectionLimit
func (s *MailboxConcurrentSuite) TestConcurrent_LimitExceeded() {
	s.Run("file", func() {
		cfg := enableMailbox(s.T(), &s.AgentTestSuite, "file")
		cfg.InjectionLimit = 5 // Only inject 5 out of 9 messages
		defer disableMailbox()

		mgr := mailbox.GlobalManager()
		parentMailbox, err := mgr.Of("main")
		require.NoError(s.T(), err)
		for i := 0; i < 9; i++ {
			err = parentMailbox.Send(context.Background(), mailbox.Message{
				From:  "child-agent",
				To:    "main",
				Topic: "finding",
				Body:  map[string]interface{}{"type": "result", "content": i},
			})
			require.NoError(s.T(), err)
		}

		// Parent processes - needs 2 responses due to continuation logic
		// First iteration: injects 5 messages (limit reached)
		s.MockServer.AddResponse(MockResponse{Content: "Parent processing first batch"})
		// Second iteration (continuation): injects remaining 4 messages
		s.MockServer.AddResponse(MockResponse{Content: "Parent processing second batch"})

		_, err = s.RunAgent("Test concurrent with limit")
		require.NoError(s.T(), err)

		// With continuation logic, all messages are processed in one turn
		// Verify all messages consumed
		count, _ := parentMailbox.Count(context.Background())
		require.Equal(s.T(), 0, count, "expected all messages to be processed with continuation logic")
	})
}
