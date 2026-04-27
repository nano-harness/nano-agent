package mailbox

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryBackend_Basic(t *testing.T) {
	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)
	require.NotNil(t, mb)

	ctx := context.Background()

	// Send a message
	msg := Message{
		From:  "parent",
		To:    "agent1",
		Topic: TopicProgress,
		Body: map[string]interface{}{
			"status": "working",
		},
	}

	err = mb.Send(ctx, msg)
	require.NoError(t, err)

	// Check count
	count, err := mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Peek without marking read
	messages, err := mb.Peek(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, messages, 1)
	assert.Equal(t, TopicProgress, messages[0].Topic)

	// Still present
	count, err = mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// DrainAll atomically removes all messages
	messages, err = mb.DrainAll(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 1)

	// Now empty
	count, err = mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestMemoryBackend_Concurrent(t *testing.T) {
	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent sends
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			msg := Message{
				From:  "parent",
				To:    "agent1",
				Topic: TopicProgress,
				Body: map[string]interface{}{
					"id": id,
				},
			}
			_ = mb.Send(ctx, msg)
		}(i)
	}

	wg.Wait()

	// Check count
	count, err := mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 50, count)

	// DrainAll atomically removes all messages
	messages, err := mb.DrainAll(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 50)

	// All should be removed
	count, err = mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestMemoryBackend_CapacityLimit(t *testing.T) {
	opts := Options{
		MaxPerAgent: 10,
		TTL:         24 * time.Hour,
		MaxBodyKB:   16,
	}
	backend := NewMemoryBackend(opts)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()

	// Send 20 messages (exceeds limit of 10)
	for i := 0; i < 20; i++ {
		msg := Message{
			From:  "parent",
			To:    "agent1",
			Topic: TopicProgress,
			Body: map[string]interface{}{
				"seq": i,
			},
		}
		err = mb.Send(ctx, msg)
		require.NoError(t, err)
	}

	// Should have only 10 messages (oldest dropped)
	messages, err := mb.DrainAll(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 10)

	// Verify it's the newest 10
	assert.Equal(t, 10, messages[0].Body["seq"])
	assert.Equal(t, 19, messages[9].Body["seq"])

	// Check stats
	stats := backend.Stats()
	assert.Equal(t, int64(20), stats.TotalSent)
	assert.Equal(t, int64(10), stats.TotalDropped)
}

func TestMemoryBackend_TTL(t *testing.T) {
	opts := Options{
		MaxPerAgent: 100,
		TTL:         100 * time.Millisecond,
		MaxBodyKB:   16,
	}
	backend := NewMemoryBackend(opts)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()

	// Send message
	msg := Message{
		From:      "parent",
		To:        "agent1",
		Topic:     TopicProgress,
		Body:      map[string]interface{}{},
		Timestamp: time.Now().UnixMilli(),
	}
	err = mb.Send(ctx, msg)
	require.NoError(t, err)

	// Immediately should have 1 message
	count, err := mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Wait for TTL expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired and cleaned
	messages, err := mb.DrainAll(ctx)
	require.NoError(t, err)
	assert.Len(t, messages, 0)
}

func TestMemoryBackend_DrainAll(t *testing.T) {
	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)
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

	// Verify all 3 messages received
	for i := 0; i < 3; i++ {
		assert.Equal(t, i, messages[i].Body["i"])
	}

	// Should be empty now
	count, err = mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestMemoryBackend_ClosedBehavior(t *testing.T) {
	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()

	// Close backend
	err = backend.Close()
	require.NoError(t, err)

	// Operations should fail
	msg := Message{From: "parent", To: "agent1", Topic: TopicProgress, Body: map[string]interface{}{}}
	err = mb.Send(ctx, msg)
	assert.ErrorIs(t, err, ErrMailboxClosed)

	_, err = mb.Peek(ctx, 10)
	assert.ErrorIs(t, err, ErrMailboxClosed)

	_, err = mb.DrainAll(ctx)
	assert.ErrorIs(t, err, ErrMailboxClosed)

	_, err = mb.Count(ctx)
	assert.ErrorIs(t, err, ErrMailboxClosed)
}

func TestMemoryBackend_BodySizeLimit(t *testing.T) {
	opts := Options{
		MaxPerAgent: 100,
		TTL:         24 * time.Hour,
		MaxBodyKB:   1, // 1KB limit
	}
	backend := NewMemoryBackend(opts)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()

	// Create large body (>1KB)
	largeBody := make(map[string]interface{})
	for i := 0; i < 100; i++ {
		// Each key-value pair is ~30 bytes, 100 pairs = ~3KB
		largeBody[fmt.Sprintf("key_%d", i)] = "xxxxxxxxxxxxxxxxxxxx"
	}

	msg := Message{
		From:  "parent",
		To:    "agent1",
		Topic: TopicProgress,
		Body:  largeBody,
	}

	err = mb.Send(ctx, msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds limit")
}

func TestMemoryBackend_PeekLimit(t *testing.T) {
	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)
	defer func() { _ = backend.Close() }()

	mb, err := backend.Open("agent1")
	require.NoError(t, err)

	ctx := context.Background()

	// Send 10 messages
	for i := 0; i < 10; i++ {
		msg := Message{
			From:  "parent",
			To:    "agent1",
			Topic: TopicProgress,
			Body:  map[string]interface{}{"i": i},
		}
		err = mb.Send(ctx, msg)
		require.NoError(t, err)
	}

	// Peek with limit
	messages, err := mb.Peek(ctx, 3)
	require.NoError(t, err)
	assert.Len(t, messages, 3)

	// All still present
	count, err := mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 10, count)
}

func TestMemoryBackend_Clear(t *testing.T) {
	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)
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

	// Verify messages exist
	count, err := mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5, count)

	// Clear all messages
	err = mb.Clear(ctx)
	require.NoError(t, err)

	// Should be empty
	count, err = mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestMemoryBackend_Contract(t *testing.T) {
	opts := DefaultOptions()
	opts.MaxPerAgent = 10 // For ring buffer tests
	opts.TTL = 1 * time.Hour

	backend := NewMemoryBackend(opts)
	defer func() { _ = backend.Close() }()

	testBackendContract(t, backend, opts)
}
