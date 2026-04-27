package mailbox

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Manager manages mailbox lifecycle and caching
type Manager struct {
	backend Backend
	mu      sync.RWMutex
	cache   map[string]Mailbox
	closed  bool

	// Janitor fields
	janitorCancel context.CancelFunc
	janitorDone   chan struct{}
}

// NewManager creates a new mailbox manager with the given backend
func NewManager(backend Backend) *Manager {
	return &Manager{
		backend: backend,
		cache:   make(map[string]Mailbox),
	}
}

// Of returns the mailbox for the specified agent (cached)
func (m *Manager) Of(agentID string) (Mailbox, error) {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, ErrMailboxClosed
	}
	if mb, exists := m.cache[agentID]; exists {
		m.mu.RUnlock()
		return mb, nil
	}
	m.mu.RUnlock()

	// Acquire write lock to create
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, ErrMailboxClosed
	}

	// Double-check after acquiring write lock
	if mb, exists := m.cache[agentID]; exists {
		return mb, nil
	}

	mb, err := m.backend.Open(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to open mailbox for %s: %w", agentID, err)
	}

	m.cache[agentID] = mb
	return mb, nil
}

// Close closes all mailboxes and the backend
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	m.closed = true

	// Stop janitor if running
	if m.janitorCancel != nil {
		m.janitorCancel()
		<-m.janitorDone
		m.janitorCancel = nil
		m.janitorDone = nil
	}

	// Close all cached mailboxes
	for _, mb := range m.cache {
		_ = mb.Close()
	}
	m.cache = nil

	// Close backend
	if m.backend != nil {
		return m.backend.Close()
	}

	return nil
}

// Stats returns backend statistics
func (m *Manager) Stats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.backend == nil {
		return Stats{}
	}

	return m.backend.Stats()
}

// StartJanitor starts the background janitor for periodic cleanup
// The janitor runs cleanup tasks at the specified interval
func (m *Manager) StartJanitor(ctx context.Context, interval time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing janitor if running
	if m.janitorCancel != nil {
		m.janitorCancel()
		<-m.janitorDone
	}

	janitorCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	m.janitorCancel = cancel
	m.janitorDone = done

	go runJanitor(janitorCtx, m, interval, done)
}

// StopJanitor stops the background janitor
func (m *Manager) StopJanitor() {
	m.mu.Lock()
	cancel := m.janitorCancel
	done := m.janitorDone
	m.janitorCancel = nil
	m.janitorDone = nil
	m.mu.Unlock()

	// Signal cancellation and wait outside the lock to avoid deadlock
	if cancel != nil {
		cancel()
		<-done
	}
}

// Global singleton manager (similar to EventDispatcher pattern)
var (
	globalManager   *Manager
	globalManagerMu sync.RWMutex
	janitorCancel   context.CancelFunc
	janitorDone     chan struct{}
)

// InitGlobal initializes the global mailbox manager
func InitGlobal(backend Backend) {
	globalManagerMu.Lock()
	defer globalManagerMu.Unlock()

	if globalManager != nil {
		_ = globalManager.Close()
		// Stop janitor if running
		if janitorCancel != nil {
			janitorCancel()
			<-janitorDone
			janitorCancel = nil
			janitorDone = nil
		}
	}

	globalManager = NewManager(backend)
}

// GlobalManager returns the global mailbox manager
// Returns nil if not initialized (no longer panics)
func GlobalManager() *Manager {
	globalManagerMu.RLock()
	defer globalManagerMu.RUnlock()
	return globalManager
}

// GlobalManagerOrNil returns the global manager if initialized, nil otherwise
// This is the preferred method to use when the manager might not be initialized
func GlobalManagerOrNil() *Manager {
	globalManagerMu.RLock()
	defer globalManagerMu.RUnlock()
	return globalManager
}

// ResetGlobal closes and resets the global manager
// Used for testing and daemon restart scenarios
func ResetGlobal() {
	globalManagerMu.Lock()
	defer globalManagerMu.Unlock()

	if globalManager != nil {
		_ = globalManager.Close()
		globalManager = nil
	}

	// Stop janitor if running
	if janitorCancel != nil {
		janitorCancel()
		<-janitorDone
		janitorCancel = nil
		janitorDone = nil
	}
}
