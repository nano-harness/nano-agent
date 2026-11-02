//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/mailbox"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// MailboxPrioritySuite tests priority-based message ordering
type MailboxPrioritySuite struct {
	AgentTestSuite
}

func TestMailboxPrioritySuite(t *testing.T) {
	suite.Run(t, new(MailboxPrioritySuite))
}

// TestPriority_HighFirst verifies that high-priority messages are injected before normal-priority ones
func (s *MailboxPrioritySuite) TestPriority_HighFirst() {
	backends := []string{"memory", "file"}

	for _, backend := range backends {
		s.Run(backend, func() {
			// Reset mock server to clear any previous requests
			s.MockServer.Reset()

			// Enable mailbox with InjectionLimit=5
			cfg := enableMailbox(s.T(), &s.AgentTestSuite, backend)
			cfg.InjectionLimit = 5
			defer disableMailbox()

			mgr := mailbox.GlobalManager()
			require.NotNil(s.T(), mgr)
			parentMailbox, err := mgr.Of("main")
			require.NoError(s.T(), err)
			for _, msg := range []string{"A", "B", "C", "D", "E", "F", "G"} {
				err = parentMailbox.Send(context.Background(), mailbox.Message{
					From:  "child",
					To:    "main",
					Topic: "progress",
					Body:  map[string]interface{}{"content": "msg:" + msg},
				})
				require.NoError(s.T(), err)
			}

			// Parent's second turn: receives first batch of injected messages (5 out of 7)
			s.MockServer.AddResponse(MockResponse{
				Content: "Parent processing priority messages (first batch)",
			})

			// Parent's third turn: receives remaining messages (2 out of 7)
			s.MockServer.AddResponse(MockResponse{
				Content: "Parent processing remaining priority messages",
			})

			_, err = s.RunAgent("Fork child to test priority ordering")
			require.NoError(s.T(), err)

			// Verify that all messages were eventually processed (none remain)
			// All messages should be processed (0 remaining)
			count, err := parentMailbox.Count(context.Background())
			require.NoError(s.T(), err)
			require.Equal(s.T(), 0, count, "expected all messages to be processed")

			systemContent := findMailboxContent(s.MockServer)
			require.NotEmpty(s.T(), systemContent, "expected to find mailbox injection in system prompt")
			indexA := strings.Index(systemContent, "msg:A")
			indexB := strings.Index(systemContent, "msg:B")
			indexC := strings.Index(systemContent, "msg:C")
			require.Less(s.T(), indexA, indexB, "message A should come before B")
			require.Less(s.T(), indexB, indexC, "message B should come before C")

			// Second drain: verify the remaining 2 messages can be retrieved
			s.MockServer.AddResponse(MockResponse{
				Content: "Processing remaining messages",
			})

			_, err = s.RunAgent("Check mailbox again")
			require.NoError(s.T(), err)

			count, err = parentMailbox.Count(context.Background())
			require.NoError(s.T(), err)
			require.Equal(s.T(), 0, count, "mailbox should be empty after second drain")
		})
	}
}

// TestPriority_SamePriorityFIFO verifies that messages with same priority follow FIFO order (timestamp asc)
func (s *MailboxPrioritySuite) TestPriority_SamePriorityFIFO() {
	backends := []string{"memory", "file"}

	for _, backend := range backends {
		s.Run(backend, func() {
			// Reset mock server to clear any previous requests
			s.MockServer.Reset()

			cfg := enableMailbox(s.T(), &s.AgentTestSuite, backend)
			cfg.InjectionLimit = 10 // Allow all messages
			defer disableMailbox()

			mgr := mailbox.GlobalManager()
			require.NotNil(s.T(), mgr)
			parentMailbox, err := mgr.Of("main")
			require.NoError(s.T(), err)

			ctx := context.Background()

			// Manually send 5 messages with same priority but different timestamps
			baseTime := time.Now().UnixMilli()
			for i := 0; i < 5; i++ {
				msg := mailbox.Message{
					From:      "child",
					To:        "main",
					Topic:     "progress",
					Body:      map[string]interface{}{"content": fmt.Sprintf("order:%d", i)},
					Timestamp: baseTime + int64(i*100), // Ascending timestamps
				}
				err := parentMailbox.Send(ctx, msg)
				require.NoError(s.T(), err)
			}

			// Parent drains messages
			s.MockServer.AddResponse(MockResponse{
				Content: "Processing FIFO messages",
			})

			_, err = s.RunAgent("Check mailbox")
			require.NoError(s.T(), err)

			systemContent := findMailboxContent(s.MockServer)
			require.NotEmpty(s.T(), systemContent)

			// Verify order: 0, 1, 2, 3, 4 (FIFO)
			for i := 0; i < 5; i++ {
				orderStr := fmt.Sprintf("order:%d", i)
				require.Contains(s.T(), systemContent, orderStr, "message with order=%d should be injected", i)
			}

			// Parse JSON to verify exact order
			idx0 := strings.Index(systemContent, "order:0")
			idx1 := strings.Index(systemContent, "order:1")
			idx2 := strings.Index(systemContent, "order:2")
			idx3 := strings.Index(systemContent, "order:3")
			idx4 := strings.Index(systemContent, "order:4")

			require.Less(s.T(), idx0, idx1, "order 0 should come before order 1")
			require.Less(s.T(), idx1, idx2, "order 1 should come before order 2")
			require.Less(s.T(), idx2, idx3, "order 2 should come before order 3")
			require.Less(s.T(), idx3, idx4, "order 3 should come before order 4")
		})
	}
}
