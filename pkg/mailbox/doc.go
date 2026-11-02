/*
Package mailbox provides multi-agent asynchronous message passing for nano-agent.

# Overview

Mailbox enables structured communication between parent and child agents during
fork-based parallel execution. It is complementary to event.EventDispatcher:

  - EventDispatcher: In-process, fire-and-forget events for UI/observability
  - Mailbox: Agent-to-agent, stateful messages with read tracking

# Architecture

	┌─────────────┐
	│   Manager   │  Lifecycle & caching
	└──────┬──────┘
	       │
	┌──────▼──────┐
	│   Backend   │  Pluggable storage
	└──────┬──────┘
	       │
	┌──────▼──────┐
	│   Mailbox   │  Per-agent inbox
	└─────────────┘

# Backend Selection

Choose backend based on execution mode:

  - Memory: Single-process, CLI mode, testing
  - File: Daemon mode, crash recovery needed, multi-process

# Usage Example

	// Initialize global manager
	opts := mailbox.DefaultOptions()
	backend := mailbox.NewMemoryBackend(opts)
	mailbox.InitGlobal(backend)

	// Get agent's inbox
	mgr := mailbox.GlobalManager()
	inbox, _ := mgr.Of("agent-123")

	// Send message
	msg := mailbox.Message{
		From:  "parent",
		To:    "agent-123",
		Topic: mailbox.TopicProgress,
		Body: map[string]interface{}{
			"status": "investigating",
			"file":   "main.go",
		},
	}
	inbox.Send(ctx, msg)

	// DrainAll and remove messages
	messages, _ := inbox.DrainAll(ctx)

# Integration with Agent Turn

Messages are automatically injected into agent context at the start of each LLM turn
via the `InjectMessages` function. DrainAll atomically retrieves and removes all messages,
simplifying message handling and preventing message loss.

# Cross-Process Daemon

For daemon/multi-process scenarios, use file backend with flock:

  - File backend uses flock + atomic rewrite for consistency
  - Mailbox files: ~/.nano/teams/<team>/mailbox/<agent-id>.json
  - Lock files: ~/.nano/teams/<team>/mailbox/<agent-id>.lock
  - Archive files: ~/.nano/teams/<team>/mailbox/archive/<msg-id>.json

Redis backend (planned) will provide distributed coordination for multi-instance deployments.

# Sub-Agent Communication

Sub-agents use the send_message tool to communicate with parent:

	send_message(
		to="parent",
		topic="finding",
		body={
			"file": "pkg/agent/fork.go",
			"insight": "Found circular dependency risk"
		}
	)

# Message Topics

Standard topics for structured communication:

  - progress: Sub-agent progress updates
  - finding: Sub-agent discoveries/insights
  - amend_task: Parent task amendments
  - permission_request: Sensitive tool authorization (v2)
  - permission_grant/deny: Permission responses

# Limits & Quotas

  - MaxPerAgent: 1000 messages per inbox (oldest dropped)
  - TTL: 7 days (expired messages cleaned on drain)
  - MaxBodyKB: 16KB per message body
  - Injection: 5 messages / 4KB per turn (priority sorted)

# Thread Safety

All operations are thread-safe. File backend uses flock with exponential
backoff (30 retries, 5-100ms).
*/
package mailbox
