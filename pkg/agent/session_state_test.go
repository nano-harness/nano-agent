package agent

import (
	"testing"
	"time"
)

func TestSessionStateTransitionsAndExpiration(t *testing.T) {
	if !SessionStateIdle.CanTransitionTo(SessionStateActive) {
		t.Fatal("idle should transition to active")
	}
	if SessionStateIdle.CanTransitionTo(SessionStateSuspended) {
		t.Fatal("idle should not transition directly to suspended")
	}

	session := NewSessionWithID("state_test")
	if err := session.TransitionState(SessionStateActive, "start"); err != nil {
		t.Fatalf("transition active: %v", err)
	}
	if err := session.TransitionState(SessionStateIdle, "done"); err != nil {
		t.Fatalf("transition idle: %v", err)
	}

	session.LastActiveAt = time.Now().Add(-31 * time.Minute)
	expired, reason := session.IsExpiredByState(time.Now(), 30*time.Minute, 2*time.Hour)
	if !expired || reason != "idle_ttl" {
		t.Fatalf("expected idle ttl expiration, got expired=%v reason=%q", expired, reason)
	}

	session.State = SessionStateAwaitingInput
	session.StateChangedAt = time.Now().Add(-90 * time.Minute)
	expired, _ = session.IsExpiredByState(time.Now(), 30*time.Minute, 2*time.Hour)
	if expired {
		t.Fatal("awaiting_input should not use idle ttl")
	}
	session.StateChangedAt = time.Now().Add(-3 * time.Hour)
	expired, reason = session.IsExpiredByState(time.Now(), 30*time.Minute, 2*time.Hour)
	if !expired || reason != "awaiting_input_timeout" {
		t.Fatalf("expected awaiting_input_timeout, got expired=%v reason=%q", expired, reason)
	}

	session.State = SessionStateSuspended
	session.StateChangedAt = time.Now().Add(-24 * time.Hour)
	expired, reason = session.IsExpiredByState(time.Now(), time.Minute, time.Minute)
	if expired || reason != "" {
		t.Fatalf("suspended sessions should not expire, got expired=%v reason=%q", expired, reason)
	}
}
