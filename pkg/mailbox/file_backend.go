package mailbox

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/oklog/ulid/v2"
)

const (
	// Lock retry configuration (aligned with Claude Code)
	maxLockRetries = 30
	minLockDelay   = 5 * time.Millisecond
	maxLockDelay   = 100 * time.Millisecond
)

// fileBackend is a file-based persistent implementation of Backend
// TODO swarm: Phase 1 - Simplified to remove ack/nack complexity
type fileBackend struct {
	rootDir string
	opts    Options
	mu      sync.RWMutex
	boxes   map[string]*fileMailbox
	stats   Stats
	closed  bool
}

// NewFileBackend creates a new file-based backend
func NewFileBackend(rootDir string, opts Options) (Backend, error) {
	// Ensure root directory exists
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create mailbox directory: %w", err)
	}

	// Ensure archive directory exists
	archiveDir := filepath.Join(rootDir, ".archive")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create archive directory: %w", err)
	}

	return &fileBackend{
		rootDir: rootDir,
		opts:    opts,
		boxes:   make(map[string]*fileMailbox),
	}, nil
}

func (b *fileBackend) Open(agentID string) (Mailbox, error) {
	// Validate agent ID to prevent path traversal
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

	mb := &fileMailbox{
		agentID:  agentID,
		path:     filepath.Join(b.rootDir, agentID+".jsonl"),
		lockPath: filepath.Join(b.rootDir, agentID+".lock"),
		backend:  b,
	}
	b.boxes[agentID] = mb
	b.stats.OpenMailboxes++

	return mb, nil
}

func (b *fileBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true
	for _, mb := range b.boxes {
		_ = mb.Close()
	}
	b.boxes = nil
	return nil
}

func (b *fileBackend) Stats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.stats
}

func (b *fileBackend) HealthCheck(ctx context.Context) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return ErrMailboxClosed
	}

	// Check if root directory is writable
	testFile := filepath.Join(b.rootDir, ".healthcheck")
	if err := os.WriteFile(testFile, []byte("ok"), 0644); err != nil {
		return fmt.Errorf("root directory not writable: %w", err)
	}
	_ = os.Remove(testFile)

	// Check if archive directory exists and is writable
	archiveDir := filepath.Join(b.rootDir, ".archive")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return fmt.Errorf("archive directory not accessible: %w", err)
	}

	return nil
}

// fileMailbox is a file-based mailbox implementation using JSONL + flock
// TODO swarm: Phase 1 - Simplified to store messages as JSON array, removed ack/nack state
type fileMailbox struct {
	agentID  string
	path     string
	lockPath string
	backend  *fileBackend
	mu       sync.Mutex
	closed   bool

	// Cache for Peek/Count optimization
	cachedMessages []Message
	cacheModTime   time.Time
}

func (f *fileMailbox) Send(ctx context.Context, msg Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return ErrMailboxClosed
	}

	// Validate message
	if err := f.validateMessage(msg); err != nil {
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

	// Acquire lock with retry
	lock := flock.New(f.lockPath)
	if err := f.acquireLock(ctx, lock); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	// Read existing messages to check capacity
	messages, err := f.readMessagesNoLock()
	if err != nil {
		return fmt.Errorf("failed to read messages: %w", err)
	}

	// Append new message
	messages = append(messages, msg)

	// Apply capacity limit and archive if needed
	if f.backend.opts.MaxPerAgent > 0 && len(messages) > f.backend.opts.MaxPerAgent {
		var archived []Message
		messages, archived = f.applyCapacityLimit(messages)
		if len(archived) > 0 {
			_ = f.archiveMessages(archived)
			f.backend.mu.Lock()
			f.backend.stats.TotalDropped += int64(len(archived))
			f.backend.mu.Unlock()
		}
	}

	// Rewrite file with updated messages
	if err := f.rewriteFile(messages); err != nil {
		return err
	}

	f.backend.mu.Lock()
	f.backend.stats.TotalSent++
	f.backend.mu.Unlock()

	// Invalidate cache
	f.cachedMessages = nil

	return nil
}

func (f *fileMailbox) Peek(ctx context.Context, limit int) ([]Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil, ErrMailboxClosed
	}

	messages, err := f.readMessages(ctx)
	if err != nil {
		return nil, err
	}

	// Filter out expired messages
	messages = f.filterValid(messages)

	if limit > 0 && len(messages) > limit {
		messages = messages[:limit]
	}

	return messages, nil
}

func (f *fileMailbox) DrainAll(ctx context.Context) ([]Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil, ErrMailboxClosed
	}

	// Acquire lock
	lock := flock.New(f.lockPath)
	if err := f.acquireLock(ctx, lock); err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	// Read all messages
	messages, err := f.readMessagesNoLock()
	if err != nil {
		return nil, err
	}

	// Filter valid messages before draining
	valid := f.filterValid(messages)

	// Clear the mailbox (write empty file)
	if err := f.rewriteFile([]Message{}); err != nil {
		return nil, err
	}

	f.backend.mu.Lock()
	f.backend.stats.TotalRead += int64(len(valid))
	f.backend.mu.Unlock()

	// Invalidate cache
	f.cachedMessages = nil

	return valid, nil
}

func (f *fileMailbox) Clear(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return ErrMailboxClosed
	}

	// Acquire lock
	lock := flock.New(f.lockPath)
	if err := f.acquireLock(ctx, lock); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	// Write empty file
	if err := f.rewriteFile([]Message{}); err != nil {
		return err
	}

	// Invalidate cache
	f.cachedMessages = nil

	return nil
}

func (f *fileMailbox) Count(ctx context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return 0, ErrMailboxClosed
	}

	// Try to use cache if file hasn't been modified
	if f.cachedMessages != nil {
		stat, err := os.Stat(f.path)
		if err == nil && stat.ModTime().Equal(f.cacheModTime) {
			// Cache is valid, count valid messages from cache
			return len(f.filterValid(f.cachedMessages)), nil
		}
	}

	// Cache miss or invalid, read from file
	messages, err := f.readMessages(ctx)
	if err != nil {
		return 0, err
	}

	// Update cache
	if stat, err := os.Stat(f.path); err == nil {
		f.cachedMessages = messages
		f.cacheModTime = stat.ModTime()
	}

	return len(f.filterValid(messages)), nil
}

func (f *fileMailbox) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.closed = true
	return nil
}

// acquireLock attempts to acquire file lock with exponential backoff
func (f *fileMailbox) acquireLock(ctx context.Context, lock *flock.Flock) error {
	for i := 0; i < maxLockRetries; i++ {
		locked, err := lock.TryLockContext(ctx, 10*time.Millisecond)
		if err != nil {
			return err
		}
		if locked {
			return nil
		}

		// Exponential backoff with jitter
		delay := minLockDelay + time.Duration(rand.Int63n(int64(maxLockDelay-minLockDelay)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return fmt.Errorf("failed to acquire lock after %d retries", maxLockRetries)
}

// readMessages reads all messages with lock
func (f *fileMailbox) readMessages(ctx context.Context) ([]Message, error) {
	lock := flock.New(f.lockPath)
	if err := f.acquireLock(ctx, lock); err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	return f.readMessagesNoLock()
}

// readMessagesNoLock reads all messages (caller must hold lock)
func (f *fileMailbox) readMessagesNoLock() ([]Message, error) {
	file, err := os.Open(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Message{}, nil
		}
		return nil, fmt.Errorf("failed to open mailbox file: %w", err)
	}
	defer func() { _ = file.Close() }()

	var messages []Message
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			// Skip malformed lines
			continue
		}
		messages = append(messages, msg)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan mailbox file: %w", err)
	}

	return messages, nil
}

// rewriteFile atomically rewrites the mailbox file (caller must hold lock)
func (f *fileMailbox) rewriteFile(messages []Message) error {
	// Create temp file in same directory for atomic rename
	tmpFile, err := os.CreateTemp(filepath.Dir(f.path), ".mailbox-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	// Write all messages
	for _, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("failed to marshal message: %w", err)
		}
		if _, err := tmpFile.Write(append(data, '\n')); err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("failed to write message: %w", err)
		}
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, f.path); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// filterValid returns only non-expired messages
func (f *fileMailbox) filterValid(messages []Message) []Message {
	var valid []Message
	for _, msg := range messages {
		if !f.isExpired(msg) {
			valid = append(valid, msg)
		}
	}
	return valid
}

// isExpired checks if a message has exceeded TTL
func (f *fileMailbox) isExpired(msg Message) bool {
	if f.backend.opts.TTL == 0 {
		return false
	}
	age := time.Since(time.UnixMilli(msg.Timestamp))
	return age > f.backend.opts.TTL
}

// applyCapacityLimit enforces message count limit, returns (kept, archived)
func (f *fileMailbox) applyCapacityLimit(messages []Message) ([]Message, []Message) {
	if f.backend.opts.MaxPerAgent <= 0 || len(messages) <= f.backend.opts.MaxPerAgent {
		return messages, nil
	}

	// Sort by timestamp (oldest first)
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp < messages[j].Timestamp
	})

	// Keep newest messages
	dropCount := len(messages) - f.backend.opts.MaxPerAgent
	archived := messages[:dropCount]
	kept := messages[dropCount:]

	return kept, archived
}

// archiveMessages writes dropped messages to archive file
func (f *fileMailbox) archiveMessages(messages []Message) error {
	if len(messages) == 0 {
		return nil
	}

	archiveDir := filepath.Join(f.backend.rootDir, ".archive")
	date := time.Now().Format("2006-01-02")
	archivePath := filepath.Join(archiveDir, fmt.Sprintf("%s-%s.jsonl", f.agentID, date))

	file, err := os.OpenFile(archivePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open archive file: %w", err)
	}
	defer func() { _ = file.Close() }()

	for _, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		_, _ = file.Write(append(data, '\n'))
	}

	_ = file.Sync()
	return nil
}

// validateMessage checks message body size
func (f *fileMailbox) validateMessage(msg Message) error {
	if f.backend.opts.MaxBodyKB <= 0 {
		return nil
	}

	body, err := json.Marshal(msg.Body)
	if err != nil {
		return fmt.Errorf("%w: cannot marshal body: %v", ErrInvalidMsg, err)
	}

	sizeKB := len(body) / 1024
	if sizeKB > f.backend.opts.MaxBodyKB {
		return fmt.Errorf("%w: body size %dKB exceeds limit %dKB", ErrInvalidMsg, sizeKB, f.backend.opts.MaxBodyKB)
	}

	return nil
}
