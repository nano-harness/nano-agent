package agent

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type lifecycleHookFunc func(context.Context, string, SessionLifecycleEvent, map[string]interface{}) error

func (f lifecycleHookFunc) OnSessionLifecycle(ctx context.Context, sessionID string, ev SessionLifecycleEvent, meta map[string]interface{}) error {
	return f(ctx, sessionID, ev, meta)
}

func TestSessionLifecycleHooksSerialAndContinueOnError(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Shutdown()

	var mu sync.Mutex
	var calls []string
	sm.AddLifecycleHook(lifecycleHookFunc(func(ctx context.Context, sessionID string, ev SessionLifecycleEvent, meta map[string]interface{}) error {
		mu.Lock()
		calls = append(calls, "first")
		mu.Unlock()
		return errors.New("boom")
	}))
	sm.AddLifecycleHook(lifecycleHookFunc(func(ctx context.Context, sessionID string, ev SessionLifecycleEvent, meta map[string]interface{}) error {
		mu.Lock()
		calls = append(calls, "second")
		mu.Unlock()
		return nil
	}))

	sm.emitLifecycle(context.Background(), "s1", SessionLifecycleCheckpoint, nil)
	if !reflect.DeepEqual(calls, []string{"first", "second"}) {
		t.Fatalf("hooks not called serially/in order: %#v", calls)
	}
}

func TestSessionLifecycleHookTimeout(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Shutdown()

	started := make(chan struct{})
	sm.AddLifecycleHook(lifecycleHookFunc(func(ctx context.Context, sessionID string, ev SessionLifecycleEvent, meta map[string]interface{}) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}))

	done := make(chan struct{})
	go func() {
		sm.emitLifecycle(context.Background(), "s1", SessionLifecycleBeforeShutdown, nil)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("hook did not start")
	}
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("hook timeout was not enforced")
	}
}
