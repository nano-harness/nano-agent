// Package mailbox provides multi-agent asynchronous message passing capabilities.
//
// Mailbox is a structured message queue abstraction for agent-to-agent communication,
// complementary to the event.EventDispatcher (which handles in-process fire-and-forget events).
//
// Key differences from EventDispatcher:
//   - Mailbox: Agent-to-agent, stateful, persistent, messages can be replayed/acknowledged
//   - EventDispatcher: In-process, fire-and-forget, no read state, for UI/observability
//
// Architecture:
//   - Backend: Pluggable storage (memory, file, future: redis)
//   - Manager: Lifecycle management and caching
//   - Mailbox: Per-agent inbox interface
package mailbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Topic constants - standard message topics for structured communication
const (
	TopicProgress          = "progress"           // Sub-agent progress updates
	TopicFinding           = "finding"            // Sub-agent structured discoveries
	TopicAmendTask         = "amend_task"         // Parent→child task amendments
	TopicPermissionRequest = "permission_request" // Sub-agent sensitive tool auth request (v2)
	TopicPermissionGrant   = "permission_grant"   // Permission granted response
	TopicPermissionDeny    = "permission_deny"    // Permission denied response
)

// Common errors
var (
	ErrMailboxClosed = errors.New("mailbox is closed")
	ErrQuotaExceeded = errors.New("mailbox quota exceeded")
	ErrInvalidMsg    = errors.New("invalid message")
	ErrInvalidAgent  = errors.New("invalid agent ID")
)

// Message is a single unit of communication between agents
// TODO swarm: Phase 1 - Simplified message model (removed Priority, InflightAt, DeliveryCount, Read)
type Message struct {
	ID        string                 `json:"id"`        // ULID, globally unique
	From      string                 `json:"from"`      // Sender agent ID
	To        string                 `json:"to"`        // Recipient agent ID
	Topic     string                 `json:"topic"`     // See Topic* constants
	Body      map[string]interface{} `json:"body"`      // Structured payload
	Timestamp int64                  `json:"timestamp"` // Unix milliseconds
	ReplyToID string                 `json:"reply_to_id,omitempty"`
}

// Mailbox is a per-recipient inbox abstraction
// TODO swarm: Phase 1 - Simplified interface (removed Ack/Nack/MarkRead, added DrainAll + Clear)
type Mailbox interface {
	// Send delivers a message to this mailbox
	Send(ctx context.Context, msg Message) error

	// Peek returns up to limit messages without removing them
	Peek(ctx context.Context, limit int) ([]Message, error)

	// DrainAll returns all messages and removes them atomically
	DrainAll(ctx context.Context) ([]Message, error)

	// Clear removes all messages from the mailbox
	Clear(ctx context.Context) error

	// Count returns the total number of messages
	Count(ctx context.Context) (int, error)

	// Close releases resources associated with this mailbox
	Close() error
}

// Backend is the pluggable storage backend for mailboxes
type Backend interface {
	// Open returns a mailbox for the specified agent
	Open(agentID string) (Mailbox, error)

	// Close shuts down the backend and releases all resources
	Close() error

	// Stats returns usage statistics
	Stats() Stats

	// HealthCheck verifies the backend is operational
	// This is used for daemon health monitoring and Redis/SQLite backend validation
	HealthCheck(ctx context.Context) error
}

// Stats provides observability into mailbox usage
type Stats struct {
	OpenMailboxes int   // Currently open mailboxes
	TotalSent     int64 // Total messages sent
	TotalRead     int64 // Total messages read
	TotalDropped  int64 // Total messages dropped due to quota
}

// Options controls mailbox behavior and limits
// TODO swarm: Phase 1 - Removed AckTimeout (no longer needed with simplified model)
type Options struct {
	MaxPerAgent int           // Max messages per agent inbox (default: 1000)
	TTL         time.Duration // Message retention period (default: 7 days)
	MaxBodyKB   int           // Max message body size in KB (default: 16)
	RootDir     string        // Root directory for file backend (default: ~/.nano/teams/<team>/mailbox)
}

// DefaultOptions returns sensible defaults for mailbox configuration
func DefaultOptions() Options {
	return Options{
		MaxPerAgent: 1000,
		TTL:         7 * 24 * time.Hour,
		MaxBodyKB:   16,
		RootDir:     "", // Will be set by config layer
	}
}

// ValidateAgentID checks if an agent ID is valid for use in mailbox operations.
// It prevents path traversal and other security issues in file-based backends.
func ValidateAgentID(id string) error {
	if id == "" {
		return fmt.Errorf("%w: empty agent ID", ErrInvalidAgent)
	}
	if len(id) > 255 {
		return fmt.Errorf("%w: agent ID too long (max 255 chars)", ErrInvalidAgent)
	}
	// Prevent path traversal
	if strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return fmt.Errorf("%w: agent ID cannot contain slashes", ErrInvalidAgent)
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("%w: agent ID cannot contain '..'", ErrInvalidAgent)
	}
	// Prevent null bytes
	if strings.Contains(id, "\x00") {
		return fmt.Errorf("%w: agent ID cannot contain null bytes", ErrInvalidAgent)
	}
	return nil
}
