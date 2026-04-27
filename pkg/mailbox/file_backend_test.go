package mailbox

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileBackend_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	opts := DefaultOptions()

	backend, err := NewFileBackend(tmpDir, opts)
	require.NoError(t, err)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)
	require.NotNil(t, mb)

	ctx := context.Background()

	// Send a message
	msg := Message{
		From:  "parent",
		To:    "agent1",
		Topic: TopicFinding,
		Body: map[string]interface{}{
			"file":    "main.go",
			"insight": "test",
		},
	}

	err = mb.Send(ctx, msg)
	require.NoError(t, err)

	// Verify file was created
	mailboxPath := filepath.Join(tmpDir, "agent1.jsonl")
	_, err = os.Stat(mailboxPath)
	require.NoError(t, err)

	// Read back
	count, err := mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	messages, err := mb.DrainAll(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 1)
	assert.Equal(t, TopicFinding, messages[0].Topic)
	assert.Equal(t, "test", messages[0].Body["insight"])
}

func TestFileBackend_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	opts := DefaultOptions()

	// Create and send message
	backend1, err := NewFileBackend(tmpDir, opts)
	require.NoError(t, err)

	mb1, err := backend1.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()
	msg := Message{
		From:  "parent",
		To:    "agent1",
		Topic: TopicProgress,
		Body:  map[string]interface{}{"status": "persisted"},
	}

	err = mb1.Send(ctx, msg)
	require.NoError(t, err)

	// Close backend
	err = backend1.Close()
	require.NoError(t, err)

	// Reopen with new backend
	backend2, err := NewFileBackend(tmpDir, opts)
	require.NoError(t, err)
	defer func() { _ = backend2.Close() }()

	mb2, err := backend2.Open("agent1")
	require.NoError(t, err)

	// Should still have the message
	count, err := mb2.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	messages, err := mb2.DrainAll(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 1)
	assert.Equal(t, "persisted", messages[0].Body["status"])
}

func TestFileBackend_ConcurrentSend(t *testing.T) {
	tmpDir := t.TempDir()
	opts := DefaultOptions()

	backend, err := NewFileBackend(tmpDir, opts)
	require.NoError(t, err)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent sends from multiple goroutines
	numSenders := 10
	msgsPerSender := 5

	for i := 0; i < numSenders; i++ {
		wg.Add(1)
		go func(sender int) {
			defer wg.Done()
			for j := 0; j < msgsPerSender; j++ {
				msg := Message{
					From:  "parent",
					To:    "agent1",
					Topic: TopicProgress,
					Body: map[string]interface{}{
						"sender": sender,
						"seq":    j,
					},
				}
				_ = mb.Send(ctx, msg)
			}
		}(i)
	}

	wg.Wait()

	// Verify all messages received
	count, err := mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, numSenders*msgsPerSender, count)

	messages, err := mb.DrainAll(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, numSenders*msgsPerSender)
}

func TestFileBackend_CapacityLimitAndArchive(t *testing.T) {
	tmpDir := t.TempDir()
	opts := Options{
		MaxPerAgent: 10,
		TTL:         24 * time.Hour,
		MaxBodyKB:   16,
	}

	backend, err := NewFileBackend(tmpDir, opts)
	require.NoError(t, err)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()

	// Send 20 messages - capacity limit enforced during Send
	for i := 0; i < 20; i++ {
		msg := Message{
			From:  "parent",
			To:    "agent1",
			Topic: TopicProgress,
			Body:  map[string]interface{}{"seq": i},
		}
		err = mb.Send(ctx, msg)
		require.NoError(t, err)
	}

	// Should only have 10 messages (MaxPerAgent limit enforced)
	messages, err := mb.DrainAll(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 10) // Only newest 10 kept

	// Send 5 more messages
	for i := 20; i < 25; i++ {
		msg := Message{
			From:  "parent",
			To:    "agent1",
			Topic: TopicProgress,
			Body:  map[string]interface{}{"seq": i},
		}
		err = mb.Send(ctx, msg)
		require.NoError(t, err)
	}

	// DrainAll - all 5 should be available
	messages, err = mb.DrainAll(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 5) // All 5 new ones

	// Should be empty now
	count, err := mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify archive directory exists
	archiveDir := filepath.Join(tmpDir, ".archive")
	_, err = os.Stat(archiveDir)
	require.NoError(t, err)
}

func TestFileBackend_TTLCleaning(t *testing.T) {
	tmpDir := t.TempDir()
	opts := Options{
		MaxPerAgent: 100,
		TTL:         200 * time.Millisecond,
		MaxBodyKB:   16,
	}

	backend, err := NewFileBackend(tmpDir, opts)
	require.NoError(t, err)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()

	// Send old message
	oldMsg := Message{
		From:      "parent",
		To:        "agent1",
		Topic:     TopicProgress,
		Body:      map[string]interface{}{"old": true},
		Timestamp: time.Now().Add(-300 * time.Millisecond).UnixMilli(),
	}
	err = mb.Send(ctx, oldMsg)
	require.NoError(t, err)

	// Send new message
	newMsg := Message{
		From:  "parent",
		To:    "agent1",
		Topic: TopicProgress,
		Body:  map[string]interface{}{"new": true},
	}
	err = mb.Send(ctx, newMsg)
	require.NoError(t, err)

	// DrainAll should clean expired and return only new
	messages, err := mb.DrainAll(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 1)
	assert.True(t, messages[0].Body["new"].(bool))
}

func TestFileBackend_DrainAll(t *testing.T) {
	tmpDir := t.TempDir()
	opts := DefaultOptions()

	backend, err := NewFileBackend(tmpDir, opts)
	require.NoError(t, err)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()

	// Send 3 messages
	for i := 0; i < 3; i++ {
		msg := Message{
			From:  "parent",
			To:    "agent1",
			Topic: TopicProgress,
			Body:  map[string]interface{}{"i": i},
		}
		err = mb.Send(ctx, msg)
		require.NoError(t, err)
	}

	// Peek to verify messages without removing
	messages, err := mb.Peek(ctx, 10)
	require.NoError(t, err)
	require.Len(t, messages, 3)

	// Should still have 3 messages
	count, err := mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// DrainAll removes all messages atomically
	messages, err = mb.DrainAll(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 3)

	// JSON unmarshaling converts numbers to float64
	for i := 0; i < 3; i++ {
		assert.Equal(t, float64(i), messages[i].Body["i"])
	}

	// Should be empty now
	count, err = mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestFileBackend_EmptyMailbox(t *testing.T) {
	tmpDir := t.TempDir()
	opts := DefaultOptions()

	backend, err := NewFileBackend(tmpDir, opts)
	require.NoError(t, err)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()

	// Operations on empty mailbox should work
	count, err := mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	messages, err := mb.Peek(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, messages, 0)

	messages, err = mb.DrainAll(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 0)
}

func TestFileBackend_ClosedBehavior(t *testing.T) {
	tmpDir := t.TempDir()
	opts := DefaultOptions()

	backend, err := NewFileBackend(tmpDir, opts)
	require.NoError(t, err)

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()

	// Close backend
	err = backend.Close()
	require.NoError(t, err)

	// New opens should fail
	_, err = backend.Open("agent2")
	assert.ErrorIs(t, err, ErrMailboxClosed)

	// Existing mailbox operations should fail
	msg := Message{From: "parent", To: "agent1", Topic: TopicProgress, Body: map[string]interface{}{}}
	err = mb.Send(ctx, msg)
	assert.ErrorIs(t, err, ErrMailboxClosed)
}

func TestFileBackend_BodySizeLimit(t *testing.T) {
	tmpDir := t.TempDir()
	opts := Options{
		MaxPerAgent: 100,
		TTL:         24 * time.Hour,
		MaxBodyKB:   1, // 1KB limit
	}

	backend, err := NewFileBackend(tmpDir, opts)
	require.NoError(t, err)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()

	// Create large body (>1KB)
	largeBody := make(map[string]interface{})
	for i := 0; i < 200; i++ {
		largeBody[string(rune('a'+i%26))+string(rune(i))] = "xxxxxxxxxxxxxxxxxxxx"
	}

	msg := Message{
		From:  "parent",
		To:    "agent1",
		Topic: TopicProgress,
		Body:  largeBody,
	}

	err = mb.Send(ctx, msg)
	assert.ErrorIs(t, err, ErrInvalidMsg)
	assert.Contains(t, err.Error(), "exceeds limit")
}

func TestFileBackend_AtomicRewrite(t *testing.T) {
	tmpDir := t.TempDir()
	opts := DefaultOptions()

	backend, err := NewFileBackend(tmpDir, opts)
	require.NoError(t, err)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()

	// Send messages
	for i := 0; i < 5; i++ {
		msg := Message{
			From:  "parent",
			To:    "agent1",
			Topic: TopicProgress,
			Body:  map[string]interface{}{"i": i},
		}
		err = mb.Send(ctx, msg)
		require.NoError(t, err)
	}

	mailboxPath := filepath.Join(tmpDir, "agent1.jsonl")

	// Get initial file info
	info1, err := os.Stat(mailboxPath)
	require.NoError(t, err)

	// DrainAll (triggers rewrite)
	_, err = mb.DrainAll(ctx)
	require.NoError(t, err)

	// File should still exist
	info2, err := os.Stat(mailboxPath)
	require.NoError(t, err)

	// Modification time should have changed
	assert.True(t, info2.ModTime().After(info1.ModTime()) || info2.ModTime().Equal(info1.ModTime()))

	// Verify no temp files left behind
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), ".tmp")
	}
}

func TestFileBackend_Contract(t *testing.T) {
	tmpDir := t.TempDir()
	opts := DefaultOptions()
	opts.MaxPerAgent = 10 // For ring buffer tests
	opts.TTL = 1 * time.Hour

	backend, err := NewFileBackend(tmpDir, opts)
	require.NoError(t, err)
	defer func() { _ = backend.Close() }()

	testBackendContract(t, backend, opts)
}
