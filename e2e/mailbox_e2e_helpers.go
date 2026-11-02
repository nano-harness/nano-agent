//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/mailbox"
	"github.com/stretchr/testify/require"
)

// enableMailbox configures and initializes the mailbox system for testing.
// backend: "memory" or "file"
// Returns the mailbox config used for later assertions.
func enableMailbox(t *testing.T, suite *AgentTestSuite, backend string) *config.MailboxConfig {
	t.Helper()

	cfg := &config.MailboxConfig{
		Enabled:               true,
		Backend:               backend,
		MaxPerAgent:           1000,
		MaxBodyKB:             16,
		TTLDays:               7,
		AckTimeoutSec:         10,
		InjectionLimit:        5,
		InjectionMaxKB:        4,
		GuidancePromptEnabled: true,
		JanitorIntervalSec:    0, // Disable janitor background task in tests
	}

	if backend == "file" {
		cfg.RootDir = t.TempDir() // Use test-specific temp directory
	}

	// Set mailbox config in agent config
	suite.Config.Mailbox = cfg

	opts := mailbox.Options{
		MaxPerAgent: cfg.MaxPerAgent,
		TTL:         time.Duration(cfg.TTLDays) * 24 * time.Hour,
		MaxBodyKB:   cfg.MaxBodyKB,
		RootDir:     cfg.RootDir,
	}

	var backendImpl mailbox.Backend
	var err error
	switch backend {
	case "file":
		backendImpl, err = mailbox.NewFileBackend(cfg.RootDir, opts)
	default:
		backendImpl = mailbox.NewMemoryBackend(opts)
	}
	require.NoError(t, err, "failed to initialize mailbox backend")
	mailbox.InitGlobal(backendImpl)

	// Set the agent's ID to "main" so it matches the mailbox we'll get
	suite.Agent.SetID("main")

	// Set the agent's mailbox reference so it can drain/inject messages
	mgr := mailbox.GlobalManager()
	require.NotNil(t, mgr, "global mailbox manager should not be nil after initialization")

	// Get mailbox for "main" agent (default agent ID in e2e tests)
	agentMailbox, err := mgr.Of("main")
	require.NoError(t, err, "failed to get mailbox for main agent")
	suite.Agent.SetMailbox(agentMailbox)

	return cfg
}

// disableMailbox cleans up the global mailbox state after tests
func disableMailbox() {
	mailbox.ResetGlobal()
}

// sendMessageToolCall creates a MockToolCall for the send_message tool
func sendMessageToolCall(id, topic, body string, priority int) MockToolCall {
	// Parse body as JSON if possible, otherwise wrap as string
	var bodyObj interface{}
	if err := json.Unmarshal([]byte(body), &bodyObj); err != nil {
		// If not JSON, wrap it as a simple message
		bodyObj = map[string]interface{}{"message": body}
	}

	argsMap := map[string]interface{}{
		"to":       "parent",
		"topic":    topic,
		"body":     bodyObj,
		"priority": priority,
	}

	argsJSON, _ := json.Marshal(argsMap)
	return MockToolCall{
		ID:        id,
		Name:      "send_message",
		Arguments: string(argsJSON),
	}
}

// assertMailboxInjected checks if a recorded LLM request contains expected mailbox content.
func assertMailboxInjected(t *testing.T, mockServer *EnhancedMockServer, expectedSubstr string) {
	t.Helper()
	content := findMailboxContentContaining(mockServer, expectedSubstr)
	require.NotEmpty(t, content, "expected mailbox injection with substring %q", expectedSubstr)
}

func findMailboxContent(mockServer *EnhancedMockServer) string {
	return findMailboxContentContaining(mockServer, "")
}

func findMailboxContentContaining(mockServer *EnhancedMockServer, expectedSubstr string) string {
	reqs := mockServer.GetRecordedRequests()
	longest := ""

	// Look for "Mailbox Messages" in any message content. The current preprocessor
	// appends mailbox attachments to the user input.
	for _, req := range reqs {
		if rawMsgs, ok := req.Body["messages"].([]interface{}); ok {
			for _, rawMsg := range rawMsgs {
				msg, ok := rawMsg.(map[string]interface{})
				if !ok {
					continue
				}
				content := ""
				// Handle both string and array content
				if strContent, ok := msg["content"].(string); ok {
					content = strContent
				} else if arrContent, ok := msg["content"].([]interface{}); ok {
					for _, part := range arrContent {
						if partMap, ok := part.(map[string]interface{}); ok {
							if text, ok := partMap["text"].(string); ok {
								content += text
							}
						}
					}
				}

				if strings.Contains(content, "Mailbox Messages") {
					// Session title generation includes a summarized copy of the
					// conversation; skip that request so assertions inspect the
					// original LLM turn that received the mailbox attachment.
					if strings.Contains(content, "生成一个简短的标题") {
						continue
					}
					if expectedSubstr != "" && !strings.Contains(content, expectedSubstr) {
						continue
					}
					if len(content) > len(longest) {
						longest = content
					}
				}
			}
		}
	}
	return longest
}

// assertEventExists checks if an event of the given type exists in the event list
func assertEventExists(t *testing.T, events []string, eventType string) {
	t.Helper()
	for _, evt := range events {
		if strings.Contains(evt, eventType) {
			return
		}
	}
	require.Fail(t, "expected event type not found", "eventType=%s", eventType)
}

// waitForMailboxMessages polls the mailbox until the expected message count is reached or timeout
func waitForMailboxMessages(t *testing.T, agentID string, expectedCount int, timeout time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			mgr := mailbox.GlobalManager()
			require.NotNil(t, mgr)
			mbox, _ := mgr.Of(agentID)
			require.NotNil(t, mbox)
			count, _ := mbox.Count(context.Background())
			require.Fail(t, "timeout waiting for mailbox messages",
				"expected %d messages, got %d after %v", expectedCount, count, timeout)
		case <-ticker.C:
			mgr := mailbox.GlobalManager()
			if mgr == nil {
				continue
			}
			mbox, err := mgr.Of(agentID)
			if err != nil {
				continue
			}
			count, err := mbox.Count(context.Background())
			if err == nil && count == expectedCount {
				return
			}
		}
	}
}
