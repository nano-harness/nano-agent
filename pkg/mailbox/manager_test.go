package mailbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_Basic(t *testing.T) {
	t.Cleanup(func() { ResetGlobal() })

	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)
	mgr := NewManager(backend)
	defer func() { _ = mgr.Close() }()

	// Get mailbox
	mb1, err := mgr.Of("agent1")
	require.NoError(t, err)
	require.NotNil(t, mb1)

	// Get same mailbox again - should be cached
	mb2, err := mgr.Of("agent1")
	require.NoError(t, err)
	assert.Equal(t, mb1, mb2) // Same instance

	// Get different mailbox
	mb3, err := mgr.Of("agent2")
	require.NoError(t, err)
	assert.NotEqual(t, mb1, mb3)
}

func TestManager_ConcurrentOf(t *testing.T) {
	t.Cleanup(func() { ResetGlobal() })

	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)
	mgr := NewManager(backend)
	defer func() { _ = mgr.Close() }()

	var wg sync.WaitGroup
	mailboxes := make([]Mailbox, 100)

	// Concurrent Of calls for same agent
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			mb, err := mgr.Of("agent1")
			if err == nil {
				mailboxes[idx] = mb
			}
		}(i)
	}

	wg.Wait()

	// All should be the same instance
	first := mailboxes[0]
	require.NotNil(t, first)

	for i := 1; i < 100; i++ {
		assert.Equal(t, first, mailboxes[i])
	}
}

func TestManager_Close(t *testing.T) {
	t.Cleanup(func() { ResetGlobal() })

	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)
	mgr := NewManager(backend)

	// Get some mailboxes
	_, err := mgr.Of("agent1")
	require.NoError(t, err)

	_, err = mgr.Of("agent2")
	require.NoError(t, err)

	// Close manager
	err = mgr.Close()
	require.NoError(t, err)

	// Further operations should fail
	_, err = mgr.Of("agent3")
	assert.ErrorIs(t, err, ErrMailboxClosed)
}

func TestManager_Stats(t *testing.T) {
	t.Cleanup(func() { ResetGlobal() })

	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)
	mgr := NewManager(backend)
	defer func() { _ = mgr.Close() }()

	ctx := context.Background()

	// Get mailboxes and send messages
	mb1, err := mgr.Of("agent1")
	require.NoError(t, err)

	mb2, err := mgr.Of("agent2")
	require.NoError(t, err)

	msg := Message{
		From:  "parent",
		To:    "agent1",
		Topic: TopicProgress,
		Body:  map[string]interface{}{},
	}

	_ = mb1.Send(ctx, msg)
	_ = mb2.Send(ctx, msg)

	// Check stats
	stats := mgr.Stats()
	assert.Equal(t, 2, stats.OpenMailboxes)
	assert.Equal(t, int64(2), stats.TotalSent)
}

func TestManager_Global(t *testing.T) {
	t.Cleanup(func() { ResetGlobal() })

	// Clean up any existing global
	if GlobalManagerOrNil() != nil {
		_ = GlobalManager().Close()
	}

	// Initialize global
	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)
	InitGlobal(backend)

	// Get global
	mgr := GlobalManager()
	require.NotNil(t, mgr)

	// Use global
	mb, err := mgr.Of("agent1")
	require.NoError(t, err)
	require.NotNil(t, mb)

	ctx := context.Background()
	msg := Message{
		From:  "parent",
		To:    "agent1",
		Topic: TopicProgress,
		Body:  map[string]interface{}{},
	}

	err = mb.Send(ctx, msg)
	require.NoError(t, err)

	// Verify
	count, err := mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Clean up
	_ = mgr.Close()
}

func TestManager_GlobalOrNil(t *testing.T) {
	t.Cleanup(func() { ResetGlobal() })

	// Clean up any existing global
	if GlobalManagerOrNil() != nil {
		_ = GlobalManager().Close()
		// Reset global state for clean test
		globalManagerMu.Lock()
		globalManager = nil
		globalManagerMu.Unlock()
	}

	// Should be nil initially
	mgr := GlobalManagerOrNil()
	assert.Nil(t, mgr)

	// Initialize
	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)
	InitGlobal(backend)

	// Should not be nil now
	mgr = GlobalManagerOrNil()
	assert.NotNil(t, mgr)

	// Clean up
	_ = mgr.Close()
}

func TestManager_GlobalReinit(t *testing.T) {
	t.Cleanup(func() { ResetGlobal() })

	// Clean up any existing global
	if GlobalManagerOrNil() != nil {
		_ = GlobalManager().Close()
	}

	// Initialize first time
	opts1 := DefaultOptions()
	backend1 := NewMemoryBackend(opts1)
	InitGlobal(backend1)

	mgr1 := GlobalManager()
	require.NotNil(t, mgr1)

	// Reinitialize (should close old and create new)
	opts2 := DefaultOptions()
	backend2 := NewMemoryBackend(opts2)
	InitGlobal(backend2)

	mgr2 := GlobalManager()
	require.NotNil(t, mgr2)

	// Should be different instance
	assert.NotEqual(t, mgr1, mgr2)

	// Clean up
	_ = mgr2.Close()
}

func TestManager_DoubleClose(t *testing.T) {
	t.Cleanup(func() { ResetGlobal() })

	opts := DefaultOptions()
	backend := NewMemoryBackend(opts)
	mgr := NewManager(backend)

	// First close
	err := mgr.Close()
	require.NoError(t, err)

	// Second close should not error
	err = mgr.Close()
	require.NoError(t, err)
}

func TestManager_NilBackend(t *testing.T) {
	t.Cleanup(func() { ResetGlobal() })

	mgr := NewManager(nil)
	defer func() { _ = mgr.Close() }()

	// Stats should work with nil backend
	stats := mgr.Stats()
	assert.Equal(t, Stats{}, stats)
}

func TestJanitor_RunsAndStops(t *testing.T) {
	t.Cleanup(func() { ResetGlobal() })

	opts := DefaultOptions()
	opts.TTL = 100 * time.Millisecond // Short TTL for testing

	backend := NewMemoryBackend(opts)
	mgr := NewManager(backend)
	defer func() { _ = mgr.Close() }()

	ctx := context.Background()

	// Get mailbox
	mb, err := mgr.Of("agent1")
	require.NoError(t, err)

	// Send a message with short TTL
	msg := Message{
		From:      "parent",
		To:        "agent1",
		Topic:     TopicProgress,
		Body:      map[string]interface{}{"test": true},
		Timestamp: time.Now().UnixMilli(),
	}
	err = mb.Send(ctx, msg)
	require.NoError(t, err)

	// Verify message exists
	count, err := mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Start janitor with short interval
	mgr.StartJanitor(ctx, 50*time.Millisecond)

	// Wait for TTL to expire and janitor to run
	time.Sleep(200 * time.Millisecond)

	// Message should be cleaned up
	count, err = mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "expired message should be cleaned by janitor")

	// Stop janitor
	mgr.StopJanitor()

	// Send another message
	msg2 := Message{
		From:      "parent",
		To:        "agent1",
		Topic:     TopicProgress,
		Body:      map[string]interface{}{"test": 2},
		Timestamp: time.Now().UnixMilli(),
	}
	err = mb.Send(ctx, msg2)
	require.NoError(t, err)

	// Wait longer than janitor interval
	time.Sleep(150 * time.Millisecond)

	// Message should still exist (janitor stopped)
	count, err = mb.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "message expired (TTL=100ms) but janitor stopped")
}
