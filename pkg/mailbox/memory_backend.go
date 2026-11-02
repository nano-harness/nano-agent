package mailbox

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/oklog/ulid/v2"
)

// memoryBackend is an in-memory implementation of Backend
// TODO swarm: Phase 1 - Simplified to remove ack/nack complexity
type memoryBackend struct {
	opts   Options
	mu     sync.RWMutex
	boxes  map[string]*memoryMailbox
	stats  Stats
	closed bool
}

// NewMemoryBackend creates a new in-memory backend
func NewMemoryBackend(opts Options) Backend {
	return &memoryBackend{
		opts:  opts,
		boxes: make(map[string]*memoryMailbox),
	}
}

func (b *memoryBackend) Open(agentID string) (Mailbox, error) {
	// Validate agent ID
	if err := ValidateAgentID(agentID); err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, ErrMailboxClosed
	}

	if mb, exists := b.boxes[agentID]; exists {
		return mb, nil
	}

	mb := &memoryMailbox{
		agentID:  agentID,
		backend:  b,
		messages: make([]Message, 0),
	}
	b.boxes[agentID] = mb
	b.stats.OpenMailboxes++

	return mb, nil
}

func (b *memoryBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true
	for _, mb := range b.boxes {
		_ = mb.Close()
	}
	b.boxes = nil
	return nil
}

func (b *memoryBackend) Stats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.stats
}

func (b *memoryBackend) HealthCheck(ctx context.Context) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrMailboxClosed
	}
	return nil
}

// memoryMailbox is an in-memory mailbox implementation
// TODO swarm: Phase 1 - Simplified to remove read state and in-flight tracking
type memoryMailbox struct {
	agentID  string
	backend  *memoryBackend
	mu       sync.Mutex
	messages []Message
	closed   bool
}

func (m *memoryMailbox) Send(ctx context.Context, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	logger.Infof("DEBUG memory_backend.Send: agentID=%s, msg.ID=%s, from=%s, to=%s, topic=%s",
		m.agentID, msg.ID, msg.From, msg.To, msg.Topic)

	if m.closed {
		return ErrMailboxClosed
	}

	// Validate message body size
	if err := m.validateMessage(msg); err != nil {
		return err
	}

	// Generate ID if not provided
	if msg.ID == "" {
		msg.ID = ulid.Make().String()
	}

	// Set timestamp if not provided
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}

	// Apply capacity limit (ring buffer behavior - drop oldest message)
	if len(m.messages) >= m.backend.opts.MaxPerAgent {
		m.messages = m.messages[1:]
		m.backend.mu.Lock()
		m.backend.stats.TotalDropped++
		m.backend.mu.Unlock()
	}

	m.messages = append(m.messages, msg)

	m.backend.mu.Lock()
	m.backend.stats.TotalSent++
	m.backend.mu.Unlock()

	return nil
}

func (m *memoryMailbox) Peek(ctx context.Context, limit int) ([]Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, ErrMailboxClosed
	}

	valid := m.getValidMessages()
	if limit > 0 && len(valid) > limit {
		valid = valid[:limit]
	}

	return valid, nil
}

func (m *memoryMailbox) DrainAll(ctx context.Context) ([]Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, ErrMailboxClosed
	}

	// Get valid messages before draining
	valid := m.getValidMessages()

	// Clear the mailbox
	m.messages = nil

	m.backend.mu.Lock()
	m.backend.stats.TotalRead += int64(len(valid))
	m.backend.mu.Unlock()

	return valid, nil
}

func (m *memoryMailbox) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrMailboxClosed
	}

	m.messages = nil
	return nil
}

func (m *memoryMailbox) Count(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, ErrMailboxClosed
	}

	return len(m.getValidMessages()), nil
}

func (m *memoryMailbox) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	m.messages = nil
	return nil
}

// getValidMessages returns non-expired messages (caller must hold lock)
func (m *memoryMailbox) getValidMessages() []Message {
	var valid []Message
	for _, msg := range m.messages {
		if !m.isExpired(msg) {
			valid = append(valid, msg)
		}
	}
	return valid
}

// isExpired checks if a message has exceeded TTL (caller must hold lock)
func (m *memoryMailbox) isExpired(msg Message) bool {
	if m.backend.opts.TTL == 0 {
		return false
	}
	age := time.Since(time.UnixMilli(msg.Timestamp))
	return age > m.backend.opts.TTL
}

// validateMessage checks message body size
func (m *memoryMailbox) validateMessage(msg Message) error {
	if m.backend.opts.MaxBodyKB <= 0 {
		return nil
	}

	body, err := json.Marshal(msg.Body)
	if err != nil {
		return fmt.Errorf("%w: cannot marshal body: %v", ErrInvalidMsg, err)
	}

	sizeKB := len(body) / 1024
	if sizeKB > m.backend.opts.MaxBodyKB {
		return fmt.Errorf("%w: body size %dKB exceeds limit %dKB", ErrInvalidMsg, sizeKB, m.backend.opts.MaxBodyKB)
	}

	return nil
}
