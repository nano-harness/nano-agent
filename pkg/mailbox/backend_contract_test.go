package mailbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testBackendContract runs a comprehensive test suite against a backend implementation
// to ensure it conforms to the mailbox contract. Both memory and file backends should
// pass all these tests.
// TODO swarm: Phase 1 - Simplified tests for new interface (removed Ack/Nack/AckTimeout tests)
func testBackendContract(t *testing.T, backend Backend, opts Options) {
	t.Helper()

	t.Run("BasicSendAndDrainAll", func(t *testing.T) {
		testBasicSendAndDrainAll(t, backend)
	})

	t.Run("DrainAllRemovesMessages", func(t *testing.T) {
		testDrainAllRemovesMessages(t, backend)
	})

	t.Run("ClearRemovesAllMessages", func(t *testing.T) {
		testClearRemovesAllMessages(t, backend)
	})

	t.Run("RingBufferDropsOldest", func(t *testing.T) {
		testRingBufferDropsOldest(t, backend, opts)
	})

	t.Run("TTLExpiration", func(t *testing.T) {
		testTTLExpiration(t, backend, opts)
	})

	t.Run("Count", func(t *testing.T) {
		testCount(t, backend)
	})

	t.Run("PeekDoesNotRemove", func(t *testing.T) {
		testPeekDoesNotRemove(t, backend)
	})

	t.Run("HealthCheck", func(t *testing.T) {
		testHealthCheck(t, backend)
	})

	t.Run("AgentIDValidation", func(t *testing.T) {
		testAgentIDValidation(t, backend)
	})
}

func testBasicSendAndDrainAll(t *testing.T, backend Backend) {
	ctx := context.Background()
	mbox, err := backend.Open("test-agent-1")
	require.NoError(t, err)
	defer func() { _ = mbox.Close() }()

	// Send a message
	msg := Message{
		From:  "sender",
		To:    "test-agent-1",
		Topic: TopicProgress,
		Body:  map[string]interface{}{"status": "working"},
	}
	err = mbox.Send(ctx, msg)
	require.NoError(t, err)

	// DrainAll should return the message and remove it
	messages, err := mbox.DrainAll(ctx)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "sender", messages[0].From)
	assert.Equal(t, TopicProgress, messages[0].Topic)
	assert.NotEmpty(t, messages[0].ID)
	assert.Greater(t, messages[0].Timestamp, int64(0))

	// Mailbox should be empty after drain
	count, err := mbox.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func testDrainAllRemovesMessages(t *testing.T, backend Backend) {
	ctx := context.Background()
	mbox, err := backend.Open("test-agent-2")
	require.NoError(t, err)
	defer func() { _ = mbox.Close() }()

	// Send multiple messages
	for i := 0; i < 3; i++ {
		msg := Message{
			From:  "sender",
			To:    "test-agent-2",
			Topic: TopicFinding,
			Body:  map[string]interface{}{"seq": i},
		}
		err = mbox.Send(ctx, msg)
		require.NoError(t, err)
	}

	// DrainAll should return all messages
	messages, err := mbox.DrainAll(ctx)
	require.NoError(t, err)
	require.Len(t, messages, 3)

	// Mailbox should be empty
	count, err := mbox.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// DrainAll again should return empty
	messages2, err := mbox.DrainAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, messages2)
}

func testClearRemovesAllMessages(t *testing.T, backend Backend) {
	ctx := context.Background()
	mbox, err := backend.Open("test-agent-3")
	require.NoError(t, err)
	defer func() { _ = mbox.Close() }()

	// Send messages
	for i := 0; i < 5; i++ {
		msg := Message{
			From:  "sender",
			To:    "test-agent-3",
			Topic: TopicProgress,
			Body:  map[string]interface{}{"step": i},
		}
		err = mbox.Send(ctx, msg)
		require.NoError(t, err)
	}

	// Verify messages exist
	count, err := mbox.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5, count)

	// Clear should remove all
	err = mbox.Clear(ctx)
	require.NoError(t, err)

	// Mailbox should be empty
	count, err = mbox.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func testRingBufferDropsOldest(t *testing.T, backend Backend, opts Options) {
	if opts.MaxPerAgent <= 0 {
		t.Skip("MaxPerAgent not set")
	}

	ctx := context.Background()
	mbox, err := backend.Open("test-agent-4")
	require.NoError(t, err)
	defer func() { _ = mbox.Close() }()

	// Send MaxPerAgent messages
	for i := 0; i < opts.MaxPerAgent; i++ {
		msg := Message{
			From:  "sender",
			To:    "test-agent-4",
			Topic: TopicProgress,
			Body:  map[string]interface{}{"seq": i},
		}
		err = mbox.Send(ctx, msg)
		require.NoError(t, err)
	}

	// Count should be MaxPerAgent
	count, err := mbox.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, opts.MaxPerAgent, count)

	// Send one more - should drop oldest
	msg := Message{
		From:  "sender",
		To:    "test-agent-4",
		Topic: TopicProgress,
		Body:  map[string]interface{}{"seq": opts.MaxPerAgent},
	}
	err = mbox.Send(ctx, msg)
	require.NoError(t, err)

	// Count should still be MaxPerAgent
	count, err = mbox.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, opts.MaxPerAgent, count)

	// Drain and verify oldest was dropped
	messages, err := mbox.DrainAll(ctx)
	require.NoError(t, err)
	require.Len(t, messages, opts.MaxPerAgent)

	// First message should be seq=1 (seq=0 was dropped)
	// Note: JSON unmarshaling converts numbers to float64 for file backend
	firstSeq := messages[0].Body["seq"]
	if seqInt, ok := firstSeq.(int); ok {
		assert.Equal(t, 1, seqInt)
	} else if seqFloat, ok := firstSeq.(float64); ok {
		assert.Equal(t, float64(1), seqFloat)
	} else {
		t.Fatalf("unexpected type for seq: %T", firstSeq)
	}

	// Last message should be seq=MaxPerAgent
	lastSeq := messages[len(messages)-1].Body["seq"]
	if seqInt, ok := lastSeq.(int); ok {
		assert.Equal(t, opts.MaxPerAgent, seqInt)
	} else if seqFloat, ok := lastSeq.(float64); ok {
		assert.Equal(t, float64(opts.MaxPerAgent), seqFloat)
	} else {
		t.Fatalf("unexpected type for seq: %T", lastSeq)
	}
}

func testTTLExpiration(t *testing.T, backend Backend, opts Options) {
	if opts.TTL == 0 {
		t.Skip("TTL disabled")
	}

	ctx := context.Background()
	mbox, err := backend.Open("test-agent-5")
	require.NoError(t, err)
	defer func() { _ = mbox.Close() }()

	// Send a message with past timestamp
	msg := Message{
		From:      "sender",
		To:        "test-agent-5",
		Topic:     TopicProgress,
		Body:      map[string]interface{}{"old": true},
		Timestamp: time.Now().Add(-opts.TTL - time.Hour).UnixMilli(),
	}
	err = mbox.Send(ctx, msg)
	require.NoError(t, err)

	// DrainAll should filter out expired message
	messages, err := mbox.DrainAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, messages)

	// Count should also exclude expired
	count, err := mbox.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func testCount(t *testing.T, backend Backend) {
	ctx := context.Background()
	mbox, err := backend.Open("test-agent-6")
	require.NoError(t, err)
	defer func() { _ = mbox.Close() }()

	// Initially empty
	count, err := mbox.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Send 3 messages
	for i := 0; i < 3; i++ {
		msg := Message{
			From:  "sender",
			To:    "test-agent-6",
			Topic: TopicProgress,
			Body:  map[string]interface{}{"seq": i},
		}
		err = mbox.Send(ctx, msg)
		require.NoError(t, err)
	}

	count, err = mbox.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// DrainAll removes all
	messages, err := mbox.DrainAll(ctx)
	require.NoError(t, err)
	require.Len(t, messages, 3)

	// Count should be 0
	count, err = mbox.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func testPeekDoesNotRemove(t *testing.T, backend Backend) {
	ctx := context.Background()
	mbox, err := backend.Open("test-agent-7")
	require.NoError(t, err)
	defer func() { _ = mbox.Close() }()

	// Send a message
	msg := Message{
		From:  "sender",
		To:    "test-agent-7",
		Topic: TopicProgress,
		Body:  map[string]interface{}{"test": true},
	}
	err = mbox.Send(ctx, msg)
	require.NoError(t, err)

	// Peek
	messages, err := mbox.Peek(ctx, 10)
	require.NoError(t, err)
	require.Len(t, messages, 1)

	// Count should still be 1
	count, err := mbox.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// DrainAll should still return it
	messages2, err := mbox.DrainAll(ctx)
	require.NoError(t, err)
	require.Len(t, messages2, 1)
	assert.Equal(t, messages[0].ID, messages2[0].ID)
}

func testHealthCheck(t *testing.T, backend Backend) {
	ctx := context.Background()
	err := backend.HealthCheck(ctx)
	assert.NoError(t, err)
}

func testAgentIDValidation(t *testing.T, backend Backend) {
	// Valid IDs should work
	validIDs := []string{"agent-123", "test_agent", "agent.with.dots"}
	for _, id := range validIDs {
		_, err := backend.Open(id)
		assert.NoError(t, err, "valid ID should work: %s", id)
	}

	// Invalid IDs should be rejected
	invalidIDs := []string{
		"",                       // empty
		"agent/with/slash",       // slash
		"agent\\with\\backslash", // backslash
		"agent..path",            // double dot
		"agent\x00null",          // null byte
	}
	for _, id := range invalidIDs {
		_, err := backend.Open(id)
		assert.Error(t, err, "invalid ID should be rejected: %q", id)
	}
}
