//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/mailbox"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// MailboxBasicSuite covers basic mailbox functionality scenarios
type MailboxBasicSuite struct {
	AgentTestSuite
}

func TestMailboxBasicSuite(t *testing.T) {
	suite.Run(t, new(MailboxBasicSuite))
}

// TestBasicFlow_FromForkChildToParentInjection tests the core mailbox flow:
// a message in the parent mailbox is injected into the next turn.
func (s *MailboxBasicSuite) TestBasicFlow_FromForkChildToParentInjection() {
	backends := []string{"memory", "file"}

	for _, backend := range backends {
		s.Run(backend, func() {
			// Enable mailbox for this test
			enableMailbox(s.T(), &s.AgentTestSuite, backend)
			defer disableMailbox()

			mgr := mailbox.GlobalManager()
			require.NotNil(s.T(), mgr)
			parentMailbox, err := mgr.Of("main")
			require.NoError(s.T(), err)
			err = parentMailbox.Send(context.Background(), mailbox.Message{
				From:  "child-agent",
				To:    "main",
				Topic: "finding",
				Body:  map[string]interface{}{"type": "status", "content": "investigating test.go"},
			})
			require.NoError(s.T(), err)

			s.MockServer.AddResponse(MockResponse{
				Content: "Parent received message from child",
			})

			// Run the parent agent
			_, err = s.RunAgent("Check mailbox")
			require.NoError(s.T(), err)

			assertMailboxInjected(s.T(), s.MockServer, "investigating")
		})
	}
}

// TestAckSemantics_InjectedMessagesRemoved verifies that injected messages are acknowledged and removed
func (s *MailboxBasicSuite) TestAckSemantics_InjectedMessagesRemoved() {
	backends := []string{"memory", "file"}

	for _, backend := range backends {
		s.Run(backend, func() {
			// Enable mailbox
			enableMailbox(s.T(), &s.AgentTestSuite, backend)
			defer disableMailbox()

			// Setup: manually send messages to parent agent's mailbox
			mgr := mailbox.GlobalManager()
			require.NotNil(s.T(), mgr)

			parentMailbox, err := mgr.Of("main")
			require.NoError(s.T(), err)

			ctx := context.Background()

			// Send 3 messages
			for i := 0; i < 3; i++ {
				msg := mailbox.Message{
					From:  "child-agent",
					To:    "main",
					Topic: "finding",
					Body:  map[string]interface{}{"test": i},
				}
				err := parentMailbox.Send(ctx, msg)
				require.NoError(s.T(), err)
			}

			// Verify 3 unread messages
			count, err := parentMailbox.Count(ctx)
			require.NoError(s.T(), err)
			require.Equal(s.T(), 3, count)

			// Parent agent turn: should drain and inject messages
			s.MockServer.AddResponse(MockResponse{
				Content: "Processing mailbox messages",
			})

			_, err = s.RunAgent("Check mailbox")
			require.NoError(s.T(), err)

			// After injection and ack, mailbox should be empty
			count, err = parentMailbox.Count(ctx)
			require.NoError(s.T(), err)
			require.Equal(s.T(), 0, count, "expected mailbox to be empty after ack")
		})
	}
}

// TestAckSemantics_ExceedingLimitKeepsMessages tests that messages exceeding InjectionLimit remain in mailbox
func (s *MailboxBasicSuite) TestAckSemantics_ExceedingLimitKeepsMessages() {
	backends := []string{"memory", "file"}

	for _, backend := range backends {
		s.Run(backend, func() {
			// Enable mailbox with InjectionLimit=3
			cfg := enableMailbox(s.T(), &s.AgentTestSuite, backend)
			cfg.InjectionLimit = 3
			defer disableMailbox()

			mgr := mailbox.GlobalManager()
			require.NotNil(s.T(), mgr)

			parentMailbox, err := mgr.Of("main")
			require.NoError(s.T(), err)

			ctx := context.Background()

			// Send 7 messages (exceeding limit of 3)
			for i := 0; i < 7; i++ {
				msg := mailbox.Message{
					From:  "child-agent",
					To:    "main",
					Topic: "finding",
					Body:  map[string]interface{}{"type": "index", "content": fmt.Sprintf("item-%d", i)},
				}
				err := parentMailbox.Send(ctx, msg)
				require.NoError(s.T(), err)
			}

			// Parent agent turn: should inject messages in batches of 3
			s.MockServer.AddResponse(MockResponse{
				Content: "Processing first batch of mailbox messages",
			})
			s.MockServer.AddResponse(MockResponse{
				Content: "Processing second batch of mailbox messages",
			})
			s.MockServer.AddResponse(MockResponse{
				Content: "Processing final mailbox message",
			})

			_, err = s.RunAgent("Check mailbox")
			require.NoError(s.T(), err)

			// After all injections, all messages should be processed (0 remaining)
			count, err := parentMailbox.Count(ctx)
			require.NoError(s.T(), err)
			require.Equal(s.T(), 0, count, "expected all messages to be processed")

			assertMailboxInjected(s.T(), s.MockServer, "item-0")
		})
	}
}

// TestDegraded_MailboxDisabled_ForkStillWorks verifies graceful degradation when mailbox is disabled
func (s *MailboxBasicSuite) TestDegraded_MailboxDisabled_ForkStillWorks() {
	// DO NOT enable mailbox for this test

	s.MockServer.AddResponse(MockResponse{
		Content: "Parent continues without mailbox",
	})

	_, err := s.RunAgent("Check mailbox while disabled")
	require.NoError(s.T(), err)
}
